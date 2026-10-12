#!/usr/bin/env python3
"""Create unsigned copies of the user's two exported iPhone workflows."""
import argparse
import copy
import plistlib
from pathlib import Path
from uuid import uuid4


def token(value):
    return {"Value": value, "WFSerializationType": "WFTextTokenAttachment"}


def variable(name):
    return {"Type": "Variable", "VariableName": name}


def text(*pieces):
    string, attachments = "", {}
    for piece in pieces:
        if isinstance(piece, dict):
            # NSRange offsets use UTF-16 code units.
            attachments["{%d, 1}" % (len(string.encode("utf-16-le")) // 2)] = piece
            string += "\ufffc"
        else:
            string += piece
    return {"Value": {"string": string, "attachmentsByRange": attachments}, "WFSerializationType": "WFTextTokenString"}


def dictionary(items):
    values = [{"WFItemType": 0, "WFKey": text(key), "WFValue": text(value)} for key, value in items.items()]
    return {"Value": {"WFDictionaryFieldValueItems": values}, "WFSerializationType": "WFDictionaryFieldValue"}


def build(wrapper, processor):
    wrapper, processor = copy.deepcopy(wrapper), copy.deepcopy(processor)
    old = processor["WFWorkflowActions"]
    local_folder = copy.deepcopy(old[3]["WFWorkflowActionParameters"]["WFFolder"])
    destination = copy.deepcopy(old[24]["WFWorkflowActionParameters"]["WFFolder"])
    actions = []

    def add(identifier, **params):
        uid = str(uuid4()).upper()
        params["UUID"] = uid
        actions.append({"WFWorkflowActionIdentifier": "is.workflow.actions." + identifier, "WFWorkflowActionParameters": params})
        return {"Type": "ActionOutput", "OutputUUID": uid, "OutputName": "Result"}

    add("comment", WFCommentActionText="Upload ordered clips to Stash through FileFlows. Clips are joined by recording date, oldest first. The earliest clip supplies the scene date. Title and description are JSON-escaped automatically. No NFO is created.")
    title = add("ask", WFAskActionPrompt="Video title?", WFAllowsMultilineText=False)
    title = add("text.trimwhitespace", WFInput=token(title))
    details = add("ask", WFAskActionPrompt="What happened in the clip?", WFAllowsMultilineText=True)
    now = add("date", WFDateActionMode="Current Date")
    now = add("format.date", WFDate=token(now), WFDateFormatStyle="ISO 8601")
    random1 = add("number.random", WFRandomNumberMinimum=100000000, WFRandomNumberMaximum=999999999)
    random2 = add("number.random", WFRandomNumberMinimum=100000000, WFRandomNumberMaximum=999999999)
    entropy = add("gettext", WFTextActionText=text(now, "-", random1, "-", random2))
    batch = add("hash", WFInput=token(entropy), WFHashType="SHA256")
    folder = add("file.createfolder", WFFolder=local_folder, WFFilePath=text("stash-upload-", batch, ".part"))
    group = str(uuid4()).upper()
    add("repeat.each", WFInput=token({"Type": "ExtensionInput"}), GroupingIdentifier=group, WFControlFlowMode=0)
    clip = variable("Repeat Item")
    recorded = add("properties.images", WFInput=token(clip), WFContentItemPropertyName="Date Taken")
    recorded = add("format.date", WFDate=token(recorded), WFDateFormatStyle="ISO 8601")
    # The recording date is read from the original Photo before exporting it.
    file_clip = dict(clip, Aggrandizements=[{"Type": "WFCoercionVariableAggrandizement", "CoercionItemClass": "WFAVAssetContentItem"}])
    renamed = add("setitemname", WFInput=token(file_clip), WFName=text("clip-", variable("Repeat Index")), WFDontIncludeFileExtension=False)
    saved = add("documentpicker.save", WFFolder=token(folder), WFInput=token(renamed), WFAskWhereToSave=False, WFFileDestinationPath="", WFSaveFileOverwrite=False)
    name = add("getitemname", WFInput=token(saved))
    digest = add("hash", WFInput=token(saved), WFHashType="SHA256")
    add("dictionary", WFItems=dictionary({"name": name, "recorded_at": recorded, "sha256": digest}))
    clips = add("repeat.each", GroupingIdentifier=group, WFControlFlowMode=2)
    manifest = add("dictionary", WFItems=dictionary({"format": "stash-clip-batch-v1", "batch_id": batch,
                   "title": title, "details": details, "date": ""}))
    manifest = add("setvalueforkey", WFDictionary=token(manifest), WFDictionaryKey="clips", WFDictionaryValue=token(clips))
    serialized = add("gettext", WFTextActionText=text(manifest))
    named = add("setitemname", WFInput=token(serialized), WFName="stash-clips.json", WFDontIncludeFileExtension=True)
    add("documentpicker.save", WFFolder=token(folder), WFInput=token(named), WFAskWhereToSave=False, WFFileDestinationPath="", WFSaveFileOverwrite=False)
    add("file.move", WFFolder=destination, WFFile=token(folder))
    # Preserve the user's existing post-upload Photos deletion action. It is
    # reached only after staging, hashing, manifest creation and upload succeed.
    actions.append(copy.deepcopy(old[25]))
    processor["WFWorkflowActions"] = actions
    processor["WFWorkflowName"] = "Process and Organize Videos - Stash"
    wrapper["WFWorkflowName"] = "Upload Jess Videos - Stash"
    for action in wrapper["WFWorkflowActions"]:
        params = action["WFWorkflowActionParameters"]
        if action["WFWorkflowActionIdentifier"] == "is.workflow.actions.runworkflow":
            params["WFWorkflowName"] = processor["WFWorkflowName"]
            # Cross-device imports must resolve by name, not the old local UUID.
            params["WFWorkflow"] = {"workflowName": processor["WFWorkflowName"], "isSelf": False}
        if action["WFWorkflowActionIdentifier"] == "is.workflow.actions.setvariable":
            params.get("WFInput", {}).get("Value", {}).pop("Aggrandizements", None)
    return wrapper, processor


def validate(workflow):
    actions = workflow["WFWorkflowActions"]
    seen, stack = set(), []

    def references(value):
        if isinstance(value, dict):
            if value.get("Type") == "ActionOutput" and value.get("OutputUUID") not in seen:
                raise ValueError("A shortcut references an unavailable action output")
            for child in value.values():
                references(child)
        elif isinstance(value, list):
            for child in value:
                references(child)

    for action in actions:
        params = action["WFWorkflowActionParameters"]
        references(params)
        uid = params.get("UUID")
        if uid in seen:
            raise ValueError("Repeated action UUID")
        if uid:
            seen.add(uid)
        if "WFControlFlowMode" in params:
            mode, group = params["WFControlFlowMode"], params["GroupingIdentifier"]
            if mode == 0:
                stack.append(group)
            elif not stack or stack[-1] != group:
                raise ValueError("Mismatched control-flow group")
            elif mode == 2:
                stack.pop()
    if stack:
        raise ValueError("Unclosed control-flow group")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("wrapper", type=Path)
    parser.add_argument("processor", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    workflows = build(plistlib.loads(args.wrapper.read_bytes()), plistlib.loads(args.processor.read_bytes()))
    for workflow in workflows:
        validate(workflow)
        name = workflow["WFWorkflowName"]
        for extension, format in (("shortcut", plistlib.FMT_BINARY), ("plist", plistlib.FMT_XML)):
            (args.output / (name + "." + extension)).write_bytes(plistlib.dumps(workflow, fmt=format, sort_keys=False))
        print(name, len(workflow["WFWorkflowActions"]), "actions")


if __name__ == "__main__":
    main()
