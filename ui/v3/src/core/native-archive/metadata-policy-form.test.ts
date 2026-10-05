import { expect, it } from "vitest";
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
    },
    {
      id: "two",
      target: "performers",
      mode: "value",
      text: '["10000000-0000-4000-8000-000000000001"]',
      performer_names: false,
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
    },
  ];
  expect(policyFormSchema(fields).safeParse(values).success).toBe(true);
  const definition = policyDefinitionFromForm(values);
  expect(definition.rules).not.toHaveProperty("scene");
  expect(definition.rules?.image?.filename_title_fallback).toBe(true);
});
