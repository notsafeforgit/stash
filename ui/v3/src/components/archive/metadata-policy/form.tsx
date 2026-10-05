import { useCallback, useId, useState } from "react";
import { useIntl } from "react-intl";
import { useForm } from "@tanstack/react-form";
import { useMsg } from "@/hooks/message";
import type {
  MetadataPolicyAPI,
  PolicyDefinition,
  PolicyKind,
} from "@/core/native-archive/metadata-policy-api";
import {
  policyDefinitionFromForm,
  policyFormSchema,
  type PolicyFields,
  type PolicyFormValues,
  type PolicyRuleValues,
} from "@/core/native-archive/metadata-policy-form";
import { requestUUID } from "@/core/native-archive/review-storage";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { fieldMessages } from "@/components/detail/native-metadata/shared";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldContent,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldError,
  FieldSet,
  FieldLegend,
} from "@/components/ui/field";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { PolicyMappingEditor, AddPolicyMapping } from "./mapping";
import { PolicySelectionPending } from "./pending";

function RuleFields({
  api,
  value,
  fields,
  disabled,
  onChange,
}: {
  api: MetadataPolicyAPI;
  value: PolicyRuleValues;
  fields: PolicyFields[PolicyKind];
  disabled: boolean;
  onChange: (value: PolicyRuleValues) => void;
}) {
  const id = useId();
  const msg = useMsg();
  const intl = useIntl();
  const options = [
    {
      key: "enabled",
      label: msg(
        "metadata_policy.kind_enabled",
        "Configure rules for this media type",
      ),
      help: "",
    },
    {
      key: "on_create",
      label: msg("metadata_policy.on_create", "Apply when an item is created"),
      help: msg(
        "metadata_policy.on_create_help",
        "Apply to a newly registered scene or image after its initial fields have been stored.",
      ),
    },
    {
      key: "on_existing",
      label: msg(
        "metadata_policy.on_existing",
        "Apply when existing items are processed",
      ),
      help: msg(
        "metadata_policy.on_existing_help",
        "Allow rescans and incoming source captures to update inherited fields. Explicit edits and clears remain protected.",
      ),
    },
    {
      key: "skip_organized_on_create",
      label: msg(
        "metadata_policy.skip_organized",
        "Skip new items created as organized",
      ),
      help: msg(
        "metadata_policy.skip_organized_help",
        "Some API imports supply organized=true in the creation request. Skip creation rules for those items; ordinary new file scans usually start unorganized.",
      ),
    },
    {
      key: "mark_organized",
      label: msg(
        "metadata_policy.mark_organized",
        "Mark organized after metadata is selected",
      ),
      help: msg(
        "metadata_policy.mark_organized_help",
        "Requires a successful mapping with no unresolved names. A filename-only title fallback does not mark an item organized.",
      ),
    },
    {
      key: "filename_title_fallback",
      label: msg(
        "metadata_policy.filename_fallback",
        "Use the filename when no title is selected",
      ),
      help: msg(
        "metadata_policy.filename_fallback_help",
        "Fill an empty inherited title from the filename without its extension. A selected title, explicit clear or broken title expression is preserved for review.",
      ),
    },
  ] as const;
  return (
    <FieldGroup>
      <FieldSet>
        <FieldLegend>
          {msg("metadata_policy.when", "When these rules apply")}
        </FieldLegend>
        <FieldGroup>
          {options.map((option) => {
            const blocked =
              disabled || (option.key !== "enabled" && !value.enabled);
            return (
              <Field
                key={option.key}
                orientation="horizontal"
                data-disabled={blocked}
              >
                <Switch
                  id={`${id}-${option.key}`}
                  checked={value[option.key]}
                  disabled={blocked}
                  onCheckedChange={(checked) =>
                    onChange({ ...value, [option.key]: checked })
                  }
                />
                <FieldContent>
                  <FieldLabel htmlFor={`${id}-${option.key}`}>
                    {option.label}
                  </FieldLabel>
                  {option.help && (
                    <FieldDescription>{option.help}</FieldDescription>
                  )}
                </FieldContent>
              </Field>
            );
          })}
        </FieldGroup>
      </FieldSet>
      {(value.mark_organized || value.organized_requires.length > 0) && (
        <FieldSet
          disabled={disabled || !value.enabled || !value.mark_organized}
        >
          <FieldLegend>
            {msg("metadata_policy.required_fields", "Required metadata")}
          </FieldLegend>
          <FieldDescription>
            {msg(
              "metadata_policy.required_fields_help",
              "Select fields that must have values before this rule marks an item organized. Existing selected values count. These requirements never unmark an item.",
            )}
          </FieldDescription>
          <FieldGroup className="grid gap-3 sm:grid-cols-2">
            {fields
              .filter((field) => field.name !== "organized")
              .map((field) => {
                const blocked =
                  disabled || !value.enabled || !value.mark_organized;
                return (
                  <Field
                    key={field.name}
                    orientation="horizontal"
                    data-disabled={blocked}
                  >
                    <Checkbox
                      id={`${id}-require-${field.name}`}
                      checked={value.organized_requires.includes(field.name)}
                      disabled={blocked}
                      onCheckedChange={(checked) =>
                        onChange({
                          ...value,
                          organized_requires: checked
                            ? [...value.organized_requires, field.name]
                            : value.organized_requires.filter(
                                (name) => name !== field.name,
                              ),
                        })
                      }
                    />
                    <FieldLabel htmlFor={`${id}-require-${field.name}`}>
                      {intl.formatMessage({
                        id: fieldMessages[field.name] ?? field.name,
                      })}
                    </FieldLabel>
                  </Field>
                );
              })}
          </FieldGroup>
        </FieldSet>
      )}
      {value.mappings.map((row, index) => (
        <PolicyMappingEditor
          key={row.id}
          api={api}
          row={row}
          fields={fields}
          used={value.mappings.map((item) => item.target)}
          disabled={disabled || !value.enabled}
          onChange={(next) =>
            onChange({
              ...value,
              mappings: value.mappings.map((item, i) =>
                i === index ? next : item,
              ),
            })
          }
          onRemove={() =>
            onChange({
              ...value,
              mappings: value.mappings.filter((_, i) => i !== index),
            })
          }
        />
      ))}
      <AddPolicyMapping
        disabled={
          disabled || !value.enabled || value.mappings.length >= fields.length
        }
        onClick={() => {
          const target = fields.find(
            (field) => !value.mappings.some((row) => row.target === field.name),
          );
          if (target)
            onChange({
              ...value,
              mappings: [
                ...value.mappings,
                {
                  id: requestUUID(),
                  target: target.name,
                  mode: "jq",
                  text: "empty",
                  performer_names: false,
                },
              ],
            });
        }}
      />
    </FieldGroup>
  );
}

export function MetadataPolicyForm({
  api,
  initialValues,
  fields,
  disabled: externalDisabled,
  onSave,
  preview,
}: {
  api: MetadataPolicyAPI;
  initialValues: PolicyFormValues;
  fields: PolicyFields;
  disabled: boolean;
  onSave: (definition: PolicyDefinition, reason: string) => Promise<void>;
  preview: (definition: PolicyDefinition | null) => React.ReactNode;
}) {
  const msg = useMsg();
  const id = useId();
  const [pendingSelections, setPendingSelections] = useState(0);
  const changePending = useCallback(
    (pending: boolean) =>
      setPendingSelections((count) => count + (pending ? 1 : -1)),
    [],
  );
  const disabled = externalDisabled || pendingSelections > 0;
  const schema = policyFormSchema(fields);
  const form = useForm({
    defaultValues: initialValues,
    validators: { onChange: schema },
    onSubmit: ({ value }) =>
      onSave(policyDefinitionFromForm(value), value.reason),
  });
  return (
    <PolicySelectionPending value={changePending}>
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          if (!disabled) void form.handleSubmit();
        }}
      >
        <FieldGroup>
          <form.Field name="enabled">
            {(field) => (
              <Field orientation="horizontal" data-disabled={disabled}>
                <Switch
                  id={`${id}-enabled`}
                  checked={field.state.value}
                  disabled={disabled}
                  onCheckedChange={field.handleChange}
                />
                <FieldContent>
                  <FieldLabel htmlFor={`${id}-enabled`}>
                    {msg(
                      "metadata_policy.enabled",
                      "Enable this metadata policy",
                    )}
                  </FieldLabel>
                  <FieldDescription>
                    {msg(
                      "metadata_policy.enabled_help",
                      "Changes take effect for future processing after you save. Previewing a draft does not change library metadata.",
                    )}
                  </FieldDescription>
                </FieldContent>
              </Field>
            )}
          </form.Field>
          <form.Field name="apply_to_scans">
            {(field) => (
              <Field orientation="horizontal" data-disabled={disabled}>
                <Switch
                  id={`${id}-scans`}
                  checked={field.state.value}
                  disabled={disabled}
                  onCheckedChange={field.handleChange}
                />
                <FieldContent>
                  <FieldLabel htmlFor={`${id}-scans`}>
                    {msg(
                      "metadata_policy.scans",
                      "Use for file scans in this folder",
                    )}
                  </FieldLabel>
                  <FieldDescription>
                    {msg(
                      "metadata_policy.scans_help",
                      "The most specific matching folder wins. A disabled policy with this option selected prevents parent-folder rules from applying here. Equal folder matches require review.",
                    )}
                  </FieldDescription>
                </FieldContent>
              </Field>
            )}
          </form.Field>
          <Tabs defaultValue="scene">
            <TabsList>
              <TabsTrigger value="scene">{msg("scenes", "Scenes")}</TabsTrigger>
              <TabsTrigger value="image">{msg("images", "Images")}</TabsTrigger>
            </TabsList>
            {(["scene", "image"] as const).map((kind) => (
              <TabsContent key={kind} value={kind}>
                <form.Field name={kind}>
                  {(field) => (
                    <RuleFields
                      api={api}
                      value={field.state.value}
                      fields={fields[kind]}
                      disabled={disabled}
                      onChange={field.handleChange}
                    />
                  )}
                </form.Field>
              </TabsContent>
            ))}
          </Tabs>
          <form.Field name="reason">
            {(field) => (
              <Field
                data-disabled={disabled}
                data-invalid={!field.state.meta.isValid}
              >
                <FieldLabel htmlFor={`${id}-reason`}>
                  {msg(
                    "collections.reason",
                    "Reason for this change (optional)",
                  )}
                </FieldLabel>
                <Input
                  id={`${id}-reason`}
                  disabled={disabled}
                  value={field.state.value}
                  aria-invalid={!field.state.meta.isValid}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
        </FieldGroup>
        <form.Subscribe selector={(state) => state.values}>
          {(values) => {
            const valid = schema.safeParse(values);
            return (
              <>
                {!valid.success && (
                  <FieldError>
                    {msg(
                      "metadata_policy.invalid_form",
                      "Check the mapping targets and values. Each field needs one mapping, a nonempty jq expression or a valid JSON value.",
                    )}
                  </FieldError>
                )}
                {preview(
                  valid.success && !disabled
                    ? policyDefinitionFromForm(valid.data)
                    : null,
                )}
                <Button
                  type="submit"
                  className="w-fit"
                  disabled={disabled || !valid.success}
                >
                  {msg("metadata_policy.save", "Save metadata policy")}
                </Button>
              </>
            );
          }}
        </form.Subscribe>
      </form>
    </PolicySelectionPending>
  );
}
