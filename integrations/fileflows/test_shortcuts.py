"""Exercise generated ordering control flow; this is not an Apple runtime test."""
import copy
import itertools
from pathlib import PurePosixPath
import plistlib
import re
import unittest
from uuid import uuid4

from build_shortcuts import build, choose_clip_order, file_name, find_ready_uploads, token, validate


class OrderWorkflow:
    """Evaluate the small, side-effect-free picker action subset with mock Photos."""

    def __init__(self, photos, selections, generate=choose_clip_order):
        self.photos, self.selections = photos, iter(selections)
        self.actions, self.outputs, self.variables, self.menus = [], {}, {}, []
        self.result = generate(self.add)
        validate({"WFWorkflowActions": self.actions})

    def add(self, kind, **parameters):
        uid = str(uuid4())
        parameters["UUID"] = uid
        self.actions.append({"WFWorkflowActionIdentifier": "is.workflow.actions." + kind,
                             "WFWorkflowActionParameters": parameters})
        return {"Type": "ActionOutput", "OutputUUID": uid}

    def value(self, value):
        if not isinstance(value, dict):
            return value
        kind = value.get("WFSerializationType")
        if kind == "WFTextTokenAttachment":
            return self.value(value["Value"])
        if kind == "WFTextTokenString":
            content = value["Value"]
            raw = content["string"].encode("utf-16-le")
            for position, attachment in sorted(content["attachmentsByRange"].items(),
                                                key=lambda item: int(item[0][1:].split(",")[0]), reverse=True):
                index = int(position[1:].split(",")[0]) * 2
                raw = raw[:index] + str(self.value(attachment)).encode("utf-16-le") + raw[index+2:]
            return raw.decode("utf-16-le")
        if value.get("Type") == "ActionOutput":
            return self.outputs[value["OutputUUID"]]
        if value.get("Type") == "ExtensionInput":
            return self.photos
        if value.get("Type") == "Variable":
            return self.value(value["Variable"]) if "Variable" in value else self.variables[value["VariableName"]]
        raise AssertionError(value)

    @staticmethod
    def as_list(value):
        return value if isinstance(value, list) else [] if value is None else [value]

    def run(self, start=0, end=None, depth=0):
        end = len(self.actions) if end is None else end
        result = None
        i = start
        while i < end:
            action = self.actions[i]
            kind = action["WFWorkflowActionIdentifier"].removeprefix("is.workflow.actions.")
            p = action["WFWorkflowActionParameters"]
            if p.get("WFControlFlowMode") == 0:
                group = p["GroupingIdentifier"]
                ends = [j for j in range(i+1, end) if self.actions[j]["WFWorkflowActionParameters"].get("GroupingIdentifier") == group]
                finish = ends[-1]
                if kind == "conditional":
                    value = self.value(p["WFInput"])
                    if p["WFCondition"] == 100:
                        condition = bool(self.as_list(value))
                    elif p["WFCondition"] == 2:
                        condition = value > p["WFNumberValue"]
                    else:
                        condition = value == self.value(p["WFConditionalActionString"])
                    middle = ends[0]
                    result = self.run(i+1, middle, depth) if condition else self.run(middle+1, finish, depth)
                else:
                    self.test_each(kind)
                    item_name = "Repeat Item" + (" " + str(depth+1) if depth else "")
                    index_name = "Repeat Index" + (" " + str(depth+1) if depth else "")
                    result = []
                    for number, item in enumerate(self.as_list(self.value(p["WFInput"])), 1):
                        self.variables[item_name], self.variables[index_name] = item, number
                        result.extend(self.as_list(self.run(i+1, finish, depth+1)))
                self.outputs[self.actions[finish]["WFWorkflowActionParameters"]["UUID"]] = result
                i = finish+1
                continue
            incoming = self.value(p["WFInput"]) if "WFInput" in p else None
            if kind == "gettext": result = self.value(p["WFTextActionText"])
            elif kind == "count": result = len(self.as_list(self.value(p["Input"])))
            elif kind == "list": result = p["WFItems"]
            elif kind == "nothing": result = None
            elif kind == "getvariable": result = self.value(p["WFVariable"])
            elif kind == "setvariable":
                self.variables[p["WFVariableName"]] = result = incoming
            elif kind == "appendvariable":
                name = p["WFVariableName"]
                result = self.as_list(self.variables.get(name)) + self.as_list(incoming)
                self.variables[name] = result
            elif kind == "choosefromlist":
                self.menus.append(copy.deepcopy(self.as_list(incoming)))
                selected = next(self.selections)
                # A selection is a label or the unique original clip number.
                result = next(item for item in self.as_list(incoming)
                              if item == selected or isinstance(item, dict) and item["name"].startswith(str(selected) + " - "))
            elif kind == "getitemname": result = incoming["name"]
            elif kind == "properties.files":
                assert p["WFContentItemPropertyName"] == "File Path"
                item = incoming
                result = item.get("path", "/Workspace/" + item["name"])
            elif kind == "file.getfoldercontents": result = self.value(p["WFFolder"])["children"]
            elif kind == "properties.images": result = incoming["recorded_at"]
            elif kind == "format.date": result = self.value(p["WFDate"])
            elif kind == "setitemname": result = {**incoming, "name": self.value(p["WFName"])}
            elif kind == "text.match": result = re.findall(p["WFMatchTextPattern"], self.value(p["text"]))
            elif kind == "getitemfromlist":
                index = 1 if p["WFItemSpecifier"] == "First Item" else int(self.value(p["WFItemIndex"]))
                result = self.as_list(incoming)[index-1]
            else: raise AssertionError(kind)
            self.outputs[p["UUID"]] = result
            i += 1
        return result

    @staticmethod
    def test_each(kind):
        assert kind == "repeat.each", kind


class ClipOrderTests(unittest.TestCase):
    def setUp(self):
        # Deliberately identical Photos filenames; their stable input indexes
        # must still distinguish them and retain their original metadata.
        self.photos = [{"name": "IMG_0001.MOV", "recorded_at": "2026-10-" + day,
                        "asset": index} for index, day in enumerate(["12", "10", "11"], 1)]

    def test_every_custom_order_uses_each_original_photo_once(self):
        for order in itertools.permutations([1, 2, 3]):
            with self.subTest(order=order):
                flow = OrderWorkflow(self.photos, ["Choose clip order", *order])
                output = flow.run()
                self.assertEqual(output, [self.photos[i-1] for i in order])
                self.assertEqual(flow.variables["Clip Order"], "input")
                self.assertEqual([len(menu) for menu in flow.menus[1:]], [3, 2, 1])
                self.assertEqual(flow.variables["Remaining Clips"], [])
                self.assertTrue(all(p["name"] == "IMG_0001.MOV" for p in self.photos))

    def test_chronological_option_leaves_sorting_to_fileflows(self):
        flow = OrderWorkflow(self.photos, ["Recording date (oldest first)"])
        self.assertEqual(flow.run(), self.photos)
        self.assertEqual(flow.variables["Clip Order"], "recorded_at")
        self.assertEqual(len(flow.menus), 1)

    def test_single_clip_does_not_prompt_for_order(self):
        flow = OrderWorkflow(self.photos[:1], [])
        self.assertEqual(flow.run(), self.photos[:1])
        self.assertEqual(flow.variables["Clip Order"], "recorded_at")
        self.assertEqual(flow.menus, [])


class ShortcutEncodingTests(unittest.TestCase):
    """Check the serialized action contract that the picker simulation cannot."""

    def setUp(self):
        # No private Photos or file-provider bookmarks in the test fixture.
        self.wrapper = {"WFWorkflowActions": [
            {"WFWorkflowActionIdentifier": "is.workflow.actions.comment",
             "WFWorkflowActionParameters": {}} for _ in range(14)
        ]}
        self.wrapper["WFWorkflowActions"][3]["WFWorkflowActionParameters"]["WFFolder"] = {"bookmark": "workspace"}
        self.wrapper["WFWorkflowActions"][6]["WFWorkflowActionParameters"]["WFFolder"] = {"bookmark": "upload"}
        self.wrapper["WFWorkflowActions"][9]["WFWorkflowActionParameters"] = {
            "WFSelectMultiplePhotos": True, "WFPhotoPickerTypes": "Videos"}
        self.processor = {"WFWorkflowActions": [
            {"WFWorkflowActionIdentifier": "is.workflow.actions.comment",
             "WFWorkflowActionParameters": {}} for _ in range(26)
        ]}
        self.processor["WFWorkflowActions"][3]["WFWorkflowActionParameters"]["WFFolder"] = {"bookmark": "workspace"}
        self.processor["WFWorkflowActions"][24]["WFWorkflowActionParameters"]["WFFolder"] = {"bookmark": "upload"}
        self.processor["WFWorkflowActions"][25] = {
            "WFWorkflowActionIdentifier": "is.workflow.actions.deletephotos",
            "WFWorkflowActionParameters": {"photos": token({"Type": "ExtensionInput"})},
        }
        wrapper, processor = build(self.wrapper, self.processor)
        self.wrapper, self.processor = [plistlib.loads(plistlib.dumps(w)) for w in (wrapper, processor)]

    def parameters(self, kind):
        return [a["WFWorkflowActionParameters"] for a in self.processor["WFWorkflowActions"]
                if a["WFWorkflowActionIdentifier"] == "is.workflow.actions." + kind]

    def test_clips_value_is_a_single_text_token_referencing_repeat_results(self):
        # WFDictionaryValue is WFTextInputParameter, unlike WFDictionary.
        # A bare WFTextTokenAttachment here caused the on-device "No value
        # provided for dictionary key clips" failure. Do not coerce the list
        # to JSON text or put literal text around its one embedded token.
        setting, = self.parameters("setvalueforkey")
        self.assertEqual(setting["WFDictionaryKey"], "clips")
        self.assertEqual(setting["WFDictionaryValue"]["WFSerializationType"], "WFTextTokenString")
        value = setting["WFDictionaryValue"]["Value"]
        self.assertEqual(value["string"], "\ufffc")
        self.assertEqual(set(value["attachmentsByRange"]), {"{0, 1}"})
        reference = value["attachmentsByRange"]["{0, 1}"]
        self.assertEqual(reference["Type"], "ActionOutput")
        actions = self.processor["WFWorkflowActions"]
        finish = next(i for i, a in enumerate(actions)
                      if a["WFWorkflowActionParameters"].get("UUID") == reference["OutputUUID"])
        self.assertEqual(actions[finish]["WFWorkflowActionIdentifier"], "is.workflow.actions.repeat.each")
        self.assertEqual(actions[finish]["WFWorkflowActionParameters"]["WFControlFlowMode"], 2)
        self.assertEqual(actions[finish-1]["WFWorkflowActionIdentifier"], "is.workflow.actions.dictionary")
        validate(self.processor)

    def test_rejects_disconnected_text_field_references(self):
        for kind, slot in (("setvalueforkey", "WFDictionaryValue"), ("format.date", "WFDate"),
                           ("text.trimwhitespace", "WFInput"), ("text.match", "text")):
            with self.subTest(kind=kind, slot=slot):
                workflow = copy.deepcopy(self.processor)
                params = next(a["WFWorkflowActionParameters"] for a in workflow["WFWorkflowActions"]
                              if a["WFWorkflowActionIdentifier"] == "is.workflow.actions." + kind)
                params[slot] = token(params[slot]["Value"]["attachmentsByRange"]["{0, 1}"])
                with self.assertRaisesRegex(ValueError, "requires a text-token string"):
                    validate(workflow)

    def test_keeps_original_names_and_resolves_helper_by_name(self):
        self.assertEqual(self.wrapper["WFWorkflowName"], "Upload Jess Videos")
        self.assertEqual(self.processor["WFWorkflowName"], "Process and Organize Videos")
        helper = next(a["WFWorkflowActionParameters"] for a in self.wrapper["WFWorkflowActions"]
                      if a["WFWorkflowActionIdentifier"] == "is.workflow.actions.runworkflow")
        self.assertEqual(helper["WFWorkflowName"], "Process and Organize Videos")
        self.assertEqual(helper["WFWorkflow"], {"workflowName": "Process and Organize Videos", "isSelf": False})
        validate(self.wrapper)

    def test_recording_dates_explicitly_include_time_and_timezone(self):
        # ISO 8601 without this switch emitted only YYYY-MM-DD on the phone,
        # which loses chronological order and fails FileFlows' timestamp check.
        dates = [p for p in self.parameters("format.date") if p.get("WFDateFormatStyle") == "ISO 8601"]
        self.assertEqual(len(dates), 2)
        self.assertTrue(all(p.get("WFISO8601IncludeTime") is True for p in dates))


class FileNameTests(unittest.TestCase):
    def test_rejects_folder_parameter_in_file_details(self):
        # Get Details of Files takes WFInput. WFFolder belongs to folder
        # actions; the phone silently returned no path with that parameter.
        flow = OrderWorkflow({}, [], lambda add: file_name(add, {"Type": "ExtensionInput"}))
        params = flow.actions[0]["WFWorkflowActionParameters"]
        params['WFFolder'] = params.pop('WFInput')
        with self.assertRaisesRegex(ValueError, 'requires WFInput'):
            validate({'WFWorkflowActions': flow.actions})

    def test_uses_saved_path_when_display_name_hides_extension(self):
        for name in ("clip-1.mov", "clip-2.MP4", "clip-3.m4v", "stash-clips.json",
                     "stash-upload-" + "a"*64 + ".part"):
            for suffix in ("", "/"):
                with self.subTest(name=name, suffix=suffix):
                    item = {"name": PurePosixPath(name).stem, "path": "/Workspace/" + name + suffix}
                    flow = OrderWorkflow(item, [], lambda add: file_name(add, {"Type": "ExtensionInput"}))
                    self.assertEqual(flow.run(), name)


class UploadRetryTests(unittest.TestCase):
    def test_only_manifest_batches_are_offered_for_retry(self):
        finished = {"name": "stash-upload-" + "a"*64 + ".part",
                    "children": [{"name": "clip-1.mov"}, {"name": "stash-clips.json"}]}
        unfinished = {"name": "stash-upload-" + "b"*64 + ".part",
                      "children": [{"name": "clip-1.mov"}]}
        empty = {"name": "stash-upload-" + "c"*64 + ".part", "children": []}
        # Unrelated entries deliberately have no children field: they must
        # never be opened as a folder, moved, or deleted by recovery.
        unrelated = [{"name": "old movie"}, {"name": "notes.txt"}]
        entries = [unfinished, finished, empty, *unrelated]
        before = copy.deepcopy(entries)
        flow = OrderWorkflow(entries, [], lambda add: find_ready_uploads(add, {"Type": "ExtensionInput"}))
        self.assertEqual(flow.run(), [finished])
        self.assertEqual(flow.variables["Unfinished Uploads"], [unfinished, empty])
        self.assertEqual(entries, before)
        self.assertEqual(flow.menus, [])

    def test_empty_workspace_has_nothing_to_retry(self):
        flow = OrderWorkflow([], [], lambda add: find_ready_uploads(add, {"Type": "ExtensionInput"}))
        self.assertEqual(flow.as_list(flow.run()), [])
        self.assertEqual(flow.as_list(flow.variables["Unfinished Uploads"]), [])

    def test_retry_finds_manifest_with_hidden_extensions(self):
        folder_name = "stash-upload-" + "a"*64
        batch = {"name": folder_name, "path": "/Workspace/" + folder_name + ".part/",
                 "children": [{"name": "stash-clips", "path": "/Workspace/" + folder_name + ".part/stash-clips.json"}]}
        flow = OrderWorkflow([batch], [], lambda add: find_ready_uploads(add, {"Type": "ExtensionInput"}))
        self.assertEqual(flow.run(), [batch])
        self.assertEqual(flow.as_list(flow.variables["Unfinished Uploads"]), [])


if __name__ == "__main__":
    unittest.main()
