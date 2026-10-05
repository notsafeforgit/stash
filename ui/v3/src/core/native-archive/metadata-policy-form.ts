import { z } from "zod";
import type {
  MetadataPolicy,
  PolicyDefinition,
  PolicyField,
  PolicyKind,
  PolicyRule,
} from "./metadata-policy-api";

const mapping = z.object({
  id: z.string(),
  target: z.string(),
  mode: z.enum(["jq", "value"]),
  text: z.string(),
  performer_names: z.boolean(),
  reference_names: z.boolean(),
});
const rule = z.object({
  enabled: z.boolean(),
  on_create: z.boolean(),
  on_existing: z.boolean(),
  skip_organized_on_create: z.boolean(),
  mark_organized: z.boolean(),
  organized_requires: z.array(z.string()).max(32),
  filename_title_fallback: z.boolean(),
  mappings: z.array(mapping).max(32),
});
const form = z.object({
  enabled: z.boolean(),
  apply_to_scans: z.boolean(),
  scene: rule,
  image: rule,
  reason: z.string(),
});
export type PolicyFormValues = z.infer<typeof form>;
export type PolicyRuleValues = z.infer<typeof rule>;
export type PolicyMappingValues = z.infer<typeof mapping>;
export type PolicyFields = Record<PolicyKind, PolicyField[]>;

export function policyFormValues(
  policy: Pick<MetadataPolicy, "definition"> | null,
): PolicyFormValues {
  function values(kind: PolicyKind): PolicyRuleValues {
    const saved = policy?.definition.rules?.[kind];
    return {
      enabled: policy ? !!saved : true,
      on_create: saved?.on_create ?? true,
      on_existing: saved?.on_existing ?? false,
      skip_organized_on_create: saved?.skip_organized_on_create ?? false,
      mark_organized: saved?.mark_organized ?? false,
      organized_requires: saved?.organized_requires ?? [],
      filename_title_fallback: saved?.filename_title_fallback ?? true,
      mappings: Object.entries(saved?.mappings ?? {}).map(
        ([target, value]) => ({
          id: `${kind}-${target}`,
          target,
          mode: "jq" in value ? "jq" : "value",
          text: "jq" in value ? value.jq : JSON.stringify(value.value, null, 2),
          performer_names: value.performer_names ?? false,
          reference_names: value.reference_names ?? false,
        }),
      ),
    };
  }
  return {
    enabled: policy?.definition.enabled ?? false,
    apply_to_scans: policy?.definition.apply_to_scans ?? true,
    scene: values("scene"),
    image: values("image"),
    reason: "",
  };
}

export function policyDefinitionFromForm(
  values: PolicyFormValues,
): PolicyDefinition {
  const rules: Partial<Record<PolicyKind, PolicyRule>> = {};
  for (const kind of ["scene", "image"] as const) {
    const { enabled, mappings, organized_requires, ...flags } = values[kind];
    if (!enabled) continue;
    rules[kind] = {
      ...flags,
      ...(organized_requires.length ? { organized_requires } : {}),
      mappings: Object.fromEntries(
        mappings.map((row) => [
          row.target,
          {
            ...(row.mode === "jq"
              ? { jq: row.text }
              : { value: JSON.parse(row.text) }),
            ...(row.performer_names ? { performer_names: true } : {}),
            ...(row.reference_names ? { reference_names: true } : {}),
          },
        ]),
      ),
    };
  }
  return {
    enabled: values.enabled,
    apply_to_scans: values.apply_to_scans,
    rules,
  };
}

const referenceName = z
  .string()
  .refine(
    (name) =>
      !!name &&
      name.trim() === name &&
      new TextEncoder().encode(name).length <= 1024 &&
      !/\p{Cc}/u.test(name),
  );
const nameValues = {
  reference: referenceName.nullable(),
  references: z.array(referenceName).max(128),
  groups: z
    .array(
      z
        .object({
          name: referenceName,
          scene_index: z.number().int().safe().nullable().optional(),
        })
        .strict(),
    )
    .max(128),
};

export function policyFormSchema(fields: PolicyFields) {
  return form.superRefine((values, ctx) => {
    const issue = (path: (string | number)[], message: string) =>
      ctx.addIssue({ code: "custom", path, message });
    if (
      new TextEncoder().encode(values.reason).length > 4096 ||
      /\p{Cc}/u.test(values.reason)
    )
      issue(["reason"], "invalid_reason");
    for (const kind of ["scene", "image"] as const) {
      const rules = values[kind];
      if (!rules.enabled) continue;
      const required = new Set<string>();
      for (const target of rules.organized_requires) {
        if (
          target === "organized" ||
          required.has(target) ||
          !fields[kind].some((field) => field.name === target)
        )
          issue([kind, "organized_requires"], "invalid_requirement");
        required.add(target);
      }
      const seen = new Set<string>();
      for (const [index, row] of rules.mappings.entries()) {
        const path = [kind, "mappings", index];
        const field = fields[kind].find((field) => field.name === row.target);
        if (
          !fields[kind].some((field) => field.name === row.target) ||
          seen.has(row.target)
        )
          issue([...path, "target"], "invalid_target");
        seen.add(row.target);
        if (
          (row.performer_names &&
            (row.target !== "performers" || row.reference_names)) ||
          (row.reference_names && !field?.reference_kind)
        )
          issue([...path, "reference_names"], "invalid_names");
        if (row.mode === "jq") {
          if (!row.text.trim()) issue([...path, "text"], "empty_expression");
        } else {
          try {
            const value: unknown = JSON.parse(row.text);
            if (row.reference_names || row.performer_names) {
              const schema =
                field && field.type in nameValues
                  ? nameValues[field.type as keyof typeof nameValues]
                  : null;
              if (!schema?.safeParse(value).success)
                issue([...path, "text"], "invalid_names");
            }
          } catch {
            issue([...path, "text"], "invalid_value");
          }
        }
        if (row.target === "organized" && rules.mark_organized)
          issue([...path, "target"], "organized_conflict");
      }
    }
    try {
      if (
        new TextEncoder().encode(
          JSON.stringify(policyDefinitionFromForm(values)),
        ).length > 131072
      )
        issue([], "definition_too_large");
    } catch {
      /* Invalid constant values already have a field error. */
    }
  });
}
