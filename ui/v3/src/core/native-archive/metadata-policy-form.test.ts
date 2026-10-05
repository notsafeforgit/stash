import { expect, it } from "vitest";
import {
  policyDefinitionSchema,
  samePolicyDefinition,
} from "./metadata-policy-api";
import {
  policyDefinitionFromForm,
  policyFormSchema,
  policyFormValues,
  type PolicyFields,
} from "./metadata-policy-form";

const fields: PolicyFields = {
  scene: [
    { name: "title", type: "string", clear_value: "" },
    {
      name: "performers",
      type: "references",
      clear_value: [],
      reference_kind: "performer",
    },
    { name: "organized", type: "boolean", clear_value: false },
    {
      name: "studio",
      type: "reference",
      clear_value: null,
      reference_kind: "studio",
    },
    {
      name: "tags",
      type: "references",
      clear_value: [],
      reference_kind: "tag",
    },
    {
      name: "groups",
      type: "groups",
      clear_value: [],
      reference_kind: "group",
    },
  ],
  image: [{ name: "title", type: "string", clear_value: "" }],
};
it("retains plain multiline jq and typed constant values without a JSON string inside JSON", () => {
  const values = policyFormValues(null);
  values.scene.mappings = [
    {
      id: "one",
      target: "title",
      mode: "jq",
      text: ".source.metadata.title\n// .context.filename",
      performer_names: false,
      reference_names: false,
    },
    {
      id: "two",
      target: "performers",
      mode: "value",
      text: '["10000000-0000-4000-8000-000000000001"]',
      performer_names: false,
      reference_names: false,
    },
  ];
  const definition = policyDefinitionFromForm(
    policyFormSchema(fields).parse(values),
  );
  expect(definition.rules?.scene?.mappings?.title).toEqual({
    jq: values.scene.mappings[0]?.text,
  });
  expect(definition.rules?.scene?.mappings?.performers).toEqual({
    value: ["10000000-0000-4000-8000-000000000001"],
  });
});

it("rejects unsupported and duplicate targets, malformed values and conflicting organized controls", () => {
  const values = policyFormValues(null);
  const schema = policyFormSchema(fields);
  for (const target of ["id", "files", "source", "capture_uuid"]) {
    values.scene.mappings = [
      {
        id: "row",
        target,
        mode: "jq",
        text: ".entity.title",
        performer_names: false,
        reference_names: false,
      },
    ];
    expect(schema.safeParse(values).success).toBe(false);
  }
  values.scene.mappings = [
    {
      id: "row",
      target: "title",
      mode: "value",
      text: "unquoted",
      performer_names: false,
      reference_names: false,
    },
  ];
  expect(schema.safeParse(values).success).toBe(false);
  values.scene.mappings[0]!.text = '"A title"';
  values.scene.mappings.push({ ...values.scene.mappings[0]!, id: "other" });
  expect(schema.safeParse(values).success).toBe(false);
  values.scene.mark_organized = true;
  values.scene.mappings = [
    {
      id: "row",
      target: "organized",
      mode: "value",
      text: "true",
      performer_names: false,
      reference_names: false,
    },
  ];
  expect(schema.safeParse(values).success).toBe(false);
});

it("keeps new policies disabled and permits deactivating a kind without deleting the other kind's settings", () => {
  const values = policyFormValues(null);
  expect(values.enabled).toBe(false);
  values.scene.enabled = false;
  values.scene.mappings = [
    {
      id: "row",
      target: "title",
      mode: "value",
      text: "unfinished",
      performer_names: false,
      reference_names: false,
    },
  ];
  expect(policyFormSchema(fields).safeParse(values).success).toBe(true);
  const definition = policyDefinitionFromForm(values);
  expect(definition.rules).not.toHaveProperty("scene");
  expect(definition.rules?.image?.filename_title_fallback).toBe(true);
});

it("preserves selected completeness fields and rejects unsupported, repeated and recursive requirements", () => {
  const values = policyFormValues(null);
  values.scene.mark_organized = true;
  values.scene.organized_requires = ["title", "performers"];
  const definition = policyDefinitionFromForm(
    policyFormSchema(fields).parse(values),
  );
  expect(definition.rules?.scene?.organized_requires).toEqual([
    "title",
    "performers",
  ]);
  expect(policyFormValues({ definition }).scene.organized_requires).toEqual([
    "title",
    "performers",
  ]);
  for (const targets of [["unknown"], ["title", "title"], ["organized"]]) {
    values.scene.organized_requires = targets;
    expect(policyFormSchema(fields).safeParse(values).success).toBe(false);
  }
});

it("treats omitted and empty completeness lists identically for old policies and save recovery", () => {
  const original = policyDefinitionFromForm(policyFormValues(null));
  expect(original.rules?.scene).not.toHaveProperty("organized_requires");
  const withEmpty = structuredClone(original);
  if (withEmpty.rules?.scene) withEmpty.rules.scene.organized_requires = [];
  const parsed = policyDefinitionSchema.parse(withEmpty);
  expect(samePolicyDefinition(original, parsed)).toBe(true);
});

it("validates relationship names using each target's shape", () => {
  const schema = policyFormSchema(fields);
  const values = policyFormValues(null);
  for (const [target, value, valid] of [
    ["studio", "Studio name", true],
    ["studio", null, true],
    ["studio", ["Studio"], false],
    ["tags", ["Tag alias"], true],
    ["tags", [], true],
    ["tags", "Tag", false],
    ["groups", [{ name: "Album", scene_index: 2 }], true],
    ["groups", [], true],
    ["groups", ["Album"], false],
    ["groups", [{ name: "Album", scene_index: 1.5 }], false],
    ["groups", [{ name: "Album", extra: true }], false],
    ["performers", [" Known alias "], false],
    ["performers", ["界".repeat(342)], false],
    ["title", "Title", false],
  ] as const) {
    values.scene.mappings = [
      {
        id: "names",
        target,
        mode: "value",
        text: JSON.stringify(value),
        performer_names: false,
        reference_names: true,
      },
    ];
    expect(
      schema.safeParse(values).success,
      `${target}: ${JSON.stringify(value)}`,
    ).toBe(valid);
    if (valid)
      expect(
        policyDefinitionFromForm(values).rules?.scene?.mappings?.[target],
      ).toEqual({ value, reference_names: true });
  }
});

it("preserves previously saved native performer-name mappings until edited", () => {
  const definition = policyDefinitionFromForm(policyFormValues(null));
  if (definition.rules?.scene)
    definition.rules.scene.mappings = {
      performers: { value: ["Known alias"], performer_names: true },
    };
  const values = policyFormValues({ definition });
  expect(policyFormSchema(fields).safeParse(values).success).toBe(true);
  expect(
    samePolicyDefinition(definition, policyDefinitionFromForm(values)),
  ).toBe(true);
  values.scene.mappings[0]!.reference_names = true;
  expect(policyFormSchema(fields).safeParse(values).success).toBe(false);
});
