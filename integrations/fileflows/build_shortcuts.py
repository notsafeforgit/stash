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
    # Text parameter slots require this envelope even for a single variable.
    # WFTextTokenAttachment in such a slot appears empty in Shortcuts.
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


def action_builder(actions):
    def add(identifier, **params):
        uid = str(uuid4()).upper()
        params["UUID"] = uid
        actions.append({"WFWorkflowActionIdentifier": "is.workflow.actions." + identifier,
                        "WFWorkflowActionParameters": params})
        return {"Type": "ActionOutput", "OutputUUID": uid, "OutputName": "Result"}
    return add


def find_ready_uploads(add, contents):
    """Keep unfinished local drafts out of the wrapper's upload retry path."""
    empty = add("nothing")
    for name in ("Ready Uploads", "Unfinished Uploads"):
        add("setvariable", WFInput=token(empty), WFVariableName=name)
    folders, ours, files, manifest, ready = (str(uuid4()).upper() for _ in range(5))
    add("repeat.each", WFInput=token(contents), GroupingIdentifier=folders, WFControlFlowMode=0)
    folder = variable("Repeat Item")
    name = add("getitemname", WFInput=token(folder))
    match = add("text.match", text=text(name), WFMatchTextPattern=r"^stash-upload-[a-f0-9]{64}\.part$")
    add("conditional", GroupingIdentifier=ours, WFControlFlowMode=0,
        WFInput={"Type": "Variable", "Variable": token(match)}, WFCondition=100)
    children = add("file.getfoldercontents", WFFolder=token(folder), Recursive=False)
    add("repeat.each", WFInput=token(children), GroupingIdentifier=files, WFControlFlowMode=0)
    name = add("getitemname", WFInput=token(variable("Repeat Item 2")))
    add("conditional", GroupingIdentifier=manifest, WFControlFlowMode=0,
        WFInput={"Type": "Variable", "Variable": token(name)}, WFCondition=4,
        WFConditionalActionString="stash-clips.json")
    add("getvariable", WFVariable=token(variable("Repeat Item 2")))
    add("conditional", GroupingIdentifier=manifest, WFControlFlowMode=1)
    add("nothing")
    add("conditional", GroupingIdentifier=manifest, WFControlFlowMode=2)
    manifests = add("repeat.each", GroupingIdentifier=files, WFControlFlowMode=2)
    add("conditional", GroupingIdentifier=ready, WFControlFlowMode=0,
        WFInput={"Type": "Variable", "Variable": token(manifests)}, WFCondition=100)
    add("appendvariable", WFInput=token(folder), WFVariableName="Ready Uploads")
    add("conditional", GroupingIdentifier=ready, WFControlFlowMode=1)
    add("appendvariable", WFInput=token(folder), WFVariableName="Unfinished Uploads")
    add("conditional", GroupingIdentifier=ready, WFControlFlowMode=2)
    add("conditional", GroupingIdentifier=ours, WFControlFlowMode=1)
    add("nothing")
    add("conditional", GroupingIdentifier=ours, WFControlFlowMode=2)
    add("repeat.each", GroupingIdentifier=folders, WFControlFlowMode=2)
    return add("getvariable", WFVariable=token(variable("Ready Uploads")))


def rebuild_wrapper(wrapper, processor_name):
    old = wrapper["WFWorkflowActions"]
    # Preserve the original Workspace, Blink destination, and Photos picker.
    workspace = copy.deepcopy(old[3]["WFWorkflowActionParameters"]["WFFolder"])
    destination = copy.deepcopy(old[6]["WFWorkflowActionParameters"]["WFFolder"])
    picker = copy.deepcopy(old[9]["WFWorkflowActionParameters"])
    picker.pop("UUID", None)
    actions = []
    add = action_builder(actions)
    shared, pending, retry, unfinished, transfers = (str(uuid4()).upper() for _ in range(5))

    def has_value(group, value):
        add("conditional", GroupingIdentifier=group, WFControlFlowMode=0,
            WFInput={"Type": "Variable", "Variable": token(value)}, WFCondition=100)

    original = {"Type": "ExtensionInput"}
    has_value(shared, original)
    add("setvariable", WFInput=token(original), WFVariableName="InputMedia")
    add("conditional", GroupingIdentifier=shared, WFControlFlowMode=1)
    contents = add("file.getfoldercontents", WFFolder=workspace, Recursive=False)
    ready = find_ready_uploads(add, contents)
    has_value(pending, ready)
    options = add("list", WFItems=["Retry saved uploads", "Choose new clips"])
    choice = add("choosefromlist", WFInput=token(options), WFChooseFromListActionSelectMultiple=False,
                 WFChooseFromListActionPrompt="There are saved uploads in Workspace. Retry them or choose new clips?")
    add("conditional", GroupingIdentifier=retry, WFControlFlowMode=0,
        WFInput={"Type": "Variable", "Variable": token(choice)}, WFCondition=4,
        WFConditionalActionString="Retry saved uploads")
    add("repeat.each", WFInput=token(ready), GroupingIdentifier=transfers, WFControlFlowMode=0)
    add("file.move", WFFolder=destination, WFFile=token(variable("Repeat Item")))
    add("repeat.each", GroupingIdentifier=transfers, WFControlFlowMode=2)
    add("alert", WFAlertActionTitle="Uploads retried", WFAlertActionCancelButtonShown=False,
        WFAlertActionMessage="The server will check the clips before processing them. Originals in Photos have been kept.")
    add("exit")
    add("conditional", GroupingIdentifier=retry, WFControlFlowMode=1)
    add("nothing")
    add("conditional", GroupingIdentifier=retry, WFControlFlowMode=2)
    add("conditional", GroupingIdentifier=pending, WFControlFlowMode=1)
    add("nothing")
    add("conditional", GroupingIdentifier=pending, WFControlFlowMode=2)
    has_value(unfinished, variable("Unfinished Uploads"))
    add("alert", WFAlertActionTitle="An earlier upload was unfinished", WFAlertActionCancelButtonShown=True,
        WFAlertActionMessage="Choose the original clips again to start over. The unfinished copies remain in Shortcuts Workspace and will not be uploaded. You can remove those copies in Files after the new upload succeeds.")
    add("conditional", GroupingIdentifier=unfinished, WFControlFlowMode=1)
    add("nothing")
    add("conditional", GroupingIdentifier=unfinished, WFControlFlowMode=2)
    photos = add("selectphoto", **picker)
    add("setvariable", WFInput=token(photos), WFVariableName="InputMedia")
    add("conditional", GroupingIdentifier=shared, WFControlFlowMode=2)
    add("runworkflow", WFInput=token(variable("InputMedia")), WFWorkflowName=processor_name,
        WFWorkflow={"workflowName": processor_name, "isSelf": False})
    wrapper["WFWorkflowActions"] = actions


def choose_clip_order(add):
    """Pick original Photos in order, before exporting files or creating a batch."""
    original = {"Type": "ExtensionInput"}
    order = add("gettext", WFTextActionText="recorded_at")
    add("setvariable", WFInput=token(order), WFVariableName="Clip Order")
    count = add("count", Input=token(original))
    multiple, custom = str(uuid4()).upper(), str(uuid4()).upper()

    def condition(group, value, comparison, **params):
        add("conditional", GroupingIdentifier=group, WFControlFlowMode=0,
            WFInput={"Type": "Variable", "Variable": token(value)}, WFCondition=comparison, **params)

    def otherwise(group):
        add("conditional", GroupingIdentifier=group, WFControlFlowMode=1)

    def end(group):
        return add("conditional", GroupingIdentifier=group, WFControlFlowMode=2)

    condition(multiple, count, 2, WFNumberValue=1)
    options = add("list", WFItems=["Recording date (oldest first)", "Choose clip order"])
    choice = add("choosefromlist", WFInput=token(options), WFChooseFromListActionPrompt="How should the clips be joined?",
                 WFChooseFromListActionSelectMultiple=False)
    condition(custom, choice, 4, WFConditionalActionString="Choose clip order")
    order = add("gettext", WFTextActionText="input")
    add("setvariable", WFInput=token(order), WFVariableName="Clip Order")
    # Names are only for the picker. Resolve the selected index back to the
    # original Photo so renaming/coercion cannot discard its recording date.
    labels = str(uuid4()).upper()
    add("repeat.each", WFInput=token(original), GroupingIdentifier=labels, WFControlFlowMode=0)
    clip = variable("Repeat Item")
    name = add("getitemname", WFInput=token(clip))
    date = add("properties.images", WFInput=token(clip), WFContentItemPropertyName="Date Taken")
    date = add("format.date", WFDate=text(date), WFDateFormatStyle="Medium", WFTimeFormatStyle="Short")
    add("setitemname", WFInput=token(clip), WFName=text(variable("Repeat Index"), " - ", date, " - ", name),
        WFDontIncludeFileExtension=False)
    choices = add("repeat.each", GroupingIdentifier=labels, WFControlFlowMode=2)
    add("setvariable", WFInput=token(choices), WFVariableName="Remaining Clips")
    empty = add("nothing")
    add("setvariable", WFInput=token(empty), WFVariableName="Ordered Clips")
    ordering = str(uuid4()).upper()
    add("repeat.each", WFInput=token(choices), GroupingIdentifier=ordering, WFControlFlowMode=0)
    chosen = add("choosefromlist", WFInput=token(variable("Remaining Clips")),
                 WFChooseFromListActionPrompt=text("Choose clip ", variable("Repeat Index"), " of ", count),
                 WFChooseFromListActionSelectMultiple=False)
    chosen_name = add("getitemname", WFInput=token(chosen))
    index = add("text.match", text=text(chosen_name), WFMatchTextPattern="^[0-9]+")
    index = add("getitemfromlist", WFInput=token(index), WFItemSpecifier="First Item")
    chosen_original = add("getitemfromlist", WFInput=token(original), WFItemSpecifier="Item At Index", WFItemIndex=token(index))
    add("appendvariable", WFInput=token(chosen_original), WFVariableName="Ordered Clips")
    # Filter by the unique picker name rather than a Photos filename, which
    # can be repeated across cameras. Each selected clip disappears once.
    remaining, selected = str(uuid4()).upper(), str(uuid4()).upper()
    add("repeat.each", WFInput=token(variable("Remaining Clips")), GroupingIdentifier=remaining, WFControlFlowMode=0)
    candidate_name = add("getitemname", WFInput=token(variable("Repeat Item 2")))
    condition(selected, candidate_name, 4, WFConditionalActionString=text(chosen_name))
    add("nothing")
    otherwise(selected)
    add("getvariable", WFVariable=token(variable("Repeat Item 2")))
    end(selected)
    left = add("repeat.each", GroupingIdentifier=remaining, WFControlFlowMode=2)
    add("setvariable", WFInput=token(left), WFVariableName="Remaining Clips")
    add("repeat.each", GroupingIdentifier=ordering, WFControlFlowMode=2)
    add("getvariable", WFVariable=token(variable("Ordered Clips")))
    otherwise(custom)
    add("getvariable", WFVariable=token(original))
    end(custom)
    otherwise(multiple)
    add("getvariable", WFVariable=token(original))
    return end(multiple)


def build(wrapper, processor):
    wrapper, processor = copy.deepcopy(wrapper), copy.deepcopy(processor)
    old = processor["WFWorkflowActions"]
    local_folder = copy.deepcopy(old[3]["WFWorkflowActionParameters"]["WFFolder"])
    destination = copy.deepcopy(old[24]["WFWorkflowActionParameters"]["WFFolder"])
    actions = []
    add = action_builder(actions)

    add("comment", WFCommentActionText="Upload ordered clips to Stash through FileFlows. Choose recording date (oldest first) or pick clips in your own order. The earliest recording still supplies the scene date. Title and description are JSON-escaped automatically. No NFO is created.")
    title = add("ask", WFAskActionPrompt="Video title?", WFAllowsMultilineText=False)
    title = add("text.trimwhitespace", WFInput=text(title))
    details = add("ask", WFAskActionPrompt="What happened in the clip?", WFAllowsMultilineText=True)
    ordered_input = choose_clip_order(add)
    now = add("date", WFDateActionMode="Current Date")
    now = add("format.date", WFDate=text(now), WFDateFormatStyle="ISO 8601")
    random1 = add("number.random", WFRandomNumberMinimum=100000000, WFRandomNumberMaximum=999999999)
    random2 = add("number.random", WFRandomNumberMinimum=100000000, WFRandomNumberMaximum=999999999)
    entropy = add("gettext", WFTextActionText=text(now, "-", random1, "-", random2))
    batch = add("hash", WFInput=token(entropy), WFHashType="SHA256")
    folder = add("file.createfolder", WFFolder=local_folder, WFFilePath=text("stash-upload-", batch, ".part"))
    group = str(uuid4()).upper()
    add("repeat.each", WFInput=token(ordered_input), GroupingIdentifier=group, WFControlFlowMode=0)
    clip = variable("Repeat Item")
    recorded = add("properties.images", WFInput=token(clip), WFContentItemPropertyName="Date Taken")
    recorded = add("format.date", WFDate=text(recorded), WFDateFormatStyle="ISO 8601")
    # The recording date is read from the original Photo before exporting it.
    file_clip = dict(clip, Aggrandizements=[{"Type": "WFCoercionVariableAggrandizement", "CoercionItemClass": "WFAVAssetContentItem"}])
    renamed = add("setitemname", WFInput=token(file_clip), WFName=text("clip-", variable("Repeat Index")), WFDontIncludeFileExtension=False)
    saved = add("documentpicker.save", WFFolder=token(folder), WFInput=token(renamed), WFAskWhereToSave=False, WFFileDestinationPath="", WFSaveFileOverwrite=False)
    name = add("getitemname", WFInput=token(saved))
    digest = add("hash", WFInput=token(saved), WFHashType="SHA256")
    add("dictionary", WFItems=dictionary({"name": name, "recorded_at": recorded, "sha256": digest}))
    clips = add("repeat.each", GroupingIdentifier=group, WFControlFlowMode=2)
    manifest = add("dictionary", WFItems=dictionary({"format": "stash-clip-batch-v1", "batch_id": batch,
                   "title": title, "details": details, "date": "", "order": variable("Clip Order")}))
    # Set Dictionary Value uses a text parameter for its value. A lone token
    # inside that text preserves the list of dictionaries; token(clips) uses
    # the wrong envelope and fails with "No value provided for dictionary key clips".
    manifest = add("setvalueforkey", WFDictionary=token(manifest), WFDictionaryKey="clips", WFDictionaryValue=text(clips))
    serialized = add("gettext", WFTextActionText=text(manifest))
    named = add("setitemname", WFInput=token(serialized), WFName="stash-clips.json", WFDontIncludeFileExtension=True)
    add("documentpicker.save", WFFolder=token(folder), WFInput=token(named), WFAskWhereToSave=False, WFFileDestinationPath="", WFSaveFileOverwrite=False)
    add("file.move", WFFolder=destination, WFFile=token(folder))
    # Preserve the user's existing post-upload Photos deletion action. It is
    # reached only after staging, hashing, manifest creation and upload succeed.
    actions.append(copy.deepcopy(old[25]))
    processor["WFWorkflowActions"] = actions
    processor["WFWorkflowName"] = "Process and Organize Videos"
    wrapper["WFWorkflowName"] = "Upload Jess Videos"
    rebuild_wrapper(wrapper, processor["WFWorkflowName"])
    return wrapper, processor


def validate(workflow):
    actions = workflow["WFWorkflowActions"]
    seen, stack = set(), []
    # Action parameter types, not the referred values, determine the envelope.
    # Format Date / Trim Whitespace match the user's native exports; Set
    # Dictionary Value uses WFTextInputParameter (docs.scpl.dev/actions/setdictionaryvalue).
    text_slots = {
        "format.date": ("WFDate",),
        "text.trimwhitespace": ("WFInput",),
        "text.match": ("text", "WFMatchTextPattern"),
        "setvalueforkey": ("WFDictionaryKey", "WFDictionaryValue"),
    }

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
        kind = action["WFWorkflowActionIdentifier"].removeprefix("is.workflow.actions.")
        if kind == "text.match" and "WFInput" in params:
            raise ValueError("Match Text takes its input in text, not WFInput")
        for slot in text_slots.get(kind, ()):
            value = params.get(slot)
            if isinstance(value, dict) and value.get("WFSerializationType") != "WFTextTokenString":
                raise ValueError(f"{kind}.{slot} requires a text-token string")
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
