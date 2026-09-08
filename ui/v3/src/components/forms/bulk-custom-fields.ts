import { z } from "zod";
import type {
  BulkCustomFieldSummaryQuery,
  CustomFieldsInput,
} from "@/core/generated-graphql";
import { coerceCustomFieldValue } from "./custom-field-value";

const newFieldSchema = z.object({ name: z.string(), value: z.string() });
const sharedFieldSchema = newFieldSchema.extend({
  action: z.enum(["keep", "set", "clear", "remove"]),
});
const valueSchema = z
  .object({
    shared: z.array(sharedFieldSchema),
    added: z.array(newFieldSchema),
  })
  .optional();

export type BulkCustomFieldsValue = z.infer<typeof valueSchema>;
export type BulkCustomFieldChange = z.infer<typeof sharedFieldSchema>;
export type BulkCustomFieldSummary =
  BulkCustomFieldSummaryQuery["bulkCustomFieldSummary"];

export function hasBulkCustomFieldChanges(value: BulkCustomFieldsValue) {
  return (
    !!value &&
    (value.added.length > 0 ||
      value.shared.some(({ action }) => action !== "keep"))
  );
}

export function bulkCustomFieldsSchema(
  summary: BulkCustomFieldSummary | undefined,
) {
  return valueSchema.superRefine((value, ctx) => {
    if (!value) return;
    if (hasBulkCustomFieldChanges(value) && (!summary || summary.count === 0)) {
      ctx.addIssue({
        code: "custom",
        message: "custom_fields.bulk.unavailable",
      });
      return;
    }
    const sharedNames = new Set(summary?.shared_names);
    const existingNames = new Set([
      ...sharedNames,
      ...(summary?.partial_names ?? []),
    ]);
    for (const section of ["shared", "added"] as const) {
      const names = value[section].map(({ name }) => name);
      value[section].forEach(({ name }, index) => {
        const message =
          name.trim() === ""
            ? "errors.custom_fields.field_name_required"
            : name !== name.trim()
              ? "errors.custom_fields.field_name_whitespace"
              : new TextEncoder().encode(name).length > 64
                ? "custom_fields.bulk.name_length"
                : names.indexOf(name) !== names.lastIndexOf(name)
                  ? "errors.custom_fields.duplicate_field"
                  : section === "shared" && !sharedNames.has(name)
                    ? "custom_fields.bulk.not_shared"
                    : section === "added" && existingNames.has(name)
                      ? "custom_fields.bulk.already_exists"
                      : undefined;
        if (message)
          ctx.addIssue({
            code: "custom",
            path: [section, index, "name"],
            message,
          });
      });
    }
  });
}

/** Patch only explicit changes. Clear retains a field with an empty string;
 * Remove deletes its key. Keep never rewrites even differing current values. */
export function bulkCustomFieldsInput(
  value: BulkCustomFieldsValue,
): CustomFieldsInput | undefined {
  if (!value || !hasBulkCustomFieldChanges(value)) return undefined;
  return {
    partial: Object.fromEntries([
      ...value.shared
        .filter(({ action }) => action === "set" || action === "clear")
        .map(({ name, action, value }) => [
          name,
          action === "clear" ? "" : coerceCustomFieldValue(value),
        ]),
      ...value.added.map(({ name, value }) => [
        name,
        coerceCustomFieldValue(value),
      ]),
    ]),
    remove: value.shared
      .filter(({ action }) => action === "remove")
      .map(({ name }) => name),
  };
}
