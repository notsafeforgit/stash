"""Exercise generated ordering control flow; this is not an Apple runtime test."""
import copy
import itertools
import re
import unittest
from uuid import uuid4

from build_shortcuts import choose_clip_order, validate


class OrderWorkflow:
    """Evaluate the small, side-effect-free picker action subset with mock Photos."""

    def __init__(self, photos, selections):
        self.photos, self.selections = photos, iter(selections)
        self.actions, self.outputs, self.variables, self.menus = [], {}, {}, []
        self.result = choose_clip_order(self.add)
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
                    condition = value > p["WFNumberValue"] if p["WFCondition"] == 2 else value == self.value(p["WFConditionalActionString"])
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
            elif kind == "properties.images": result = incoming["recorded_at"]
            elif kind == "format.date": result = self.value(p["WFDate"])
            elif kind == "setitemname": result = {**incoming, "name": self.value(p["WFName"])}
            elif kind == "text.match": result = re.findall(p["WFMatchTextPattern"], incoming)
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


if __name__ == "__main__":
    unittest.main()
