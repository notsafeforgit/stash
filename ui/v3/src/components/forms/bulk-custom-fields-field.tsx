import { useId } from "react";
import { useIntl } from "react-intl";
import { useQuery } from "@apollo/client/react";
import { PlusIcon, XIcon } from "lucide-react";
import {
  CustomFieldNamesDocument,
  type FilterMode,
} from "@/core/generated-graphql";
import { useEditableRows } from "@/hooks/use-editable-rows";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CustomFieldNameInput } from "./custom-field-name-input";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  bulkCustomFieldsSchema,
  type BulkCustomFieldsValue,
  type BulkCustomFieldChange,
  type BulkCustomFieldSummary,
} from "./bulk-custom-fields";
import type { useBulkCustomFields } from "./use-bulk-custom-fields";

interface BulkCustomFieldsFieldProps {
  value: BulkCustomFieldsValue;
  onChange: (value: BulkCustomFieldsValue) => void;
  entityMode: FilterMode;
  state: ReturnType<typeof useBulkCustomFields>;
  disabled?: boolean;
}

const emptyValue: NonNullable<BulkCustomFieldsValue> = {
  shared: [],
  added: [],
};
const actions = ["keep", "set", "clear", "remove"] as const;
const actionMessages = {
  keep: { label: "actions.keep", description: "custom_fields.bulk.keep" },
  set: { label: "actions.set", description: "custom_fields.bulk.set" },
  clear: {
    label: "custom_fields.bulk.clear_value",
    description: "custom_fields.bulk.clear",
  },
  remove: {
    label: "custom_fields.bulk.remove_field",
    description: "custom_fields.bulk.remove",
  },
} satisfies Record<
  BulkCustomFieldChange["action"],
  { label: string; description: string }
>;

export function BulkCustomFieldsField({
  value,
  onChange,
  entityMode,
  state,
  disabled,
}: BulkCustomFieldsFieldProps) {
  const intl = useIntl();
  const id = useId();
  const current = value ?? emptyValue;
  const { summary } = state;

  function updateShared(change: BulkCustomFieldChange) {
    onChange({
      ...current,
      shared: [
        ...current.shared.filter(({ name }) => name !== change.name),
        change,
      ],
    });
  }

  return (
    <FieldSet className="gap-3">
      <FieldLegend variant="label">
        {intl.formatMessage({ id: "custom_fields.title" })}
      </FieldLegend>
      <FieldDescription>
        {intl.formatMessage({ id: "custom_fields.bulk.edit" })}
      </FieldDescription>
      {state.loading && (
        <FieldDescription role="status">
          {intl.formatMessage({ id: "loading.generic" })}
        </FieldDescription>
      )}
      {state.error && (
        <>
          <FieldError>
            {intl.formatMessage({ id: "custom_fields.bulk.unavailable" })}
          </FieldError>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="self-start"
            disabled={disabled}
            onClick={() => {
              // Query errors remain visible through state.error.
              void state.refetch().catch(() => {});
            }}
          >
            {intl.formatMessage({ id: "actions.retry" })}
          </Button>
        </>
      )}
      {summary &&
        (summary.count === 0 ? (
          <FieldDescription>
            {intl.formatMessage({ id: "custom_fields.bulk.no_items" })}
          </FieldDescription>
        ) : (
          <>
            {summary.shared_names.length === 0 && (
              <FieldDescription>
                {intl.formatMessage({ id: "custom_fields.bulk.no_shared" })}
              </FieldDescription>
            )}
            {summary.shared_names.map((name, index) => {
              const change = current.shared.find(
                (item) => item.name === name,
              ) ?? { name, action: "keep", value: "" };
              const valueId = `${id}-shared-${index}`;
              return (
                <FieldSet key={name} className="gap-2 rounded-lg border p-3">
                  <FieldLegend variant="label" className="mb-0 break-all">
                    {name}
                  </FieldLegend>
                  <ToggleGroup<BulkCustomFieldChange["action"]>
                    value={[change.action]}
                    onValueChange={([action]) => {
                      if (action) updateShared({ ...change, action });
                    }}
                    disabled={disabled}
                    variant="outline"
                    size="sm"
                    aria-label={intl.formatMessage(
                      { id: "custom_fields.bulk.action" },
                      { field: name },
                    )}
                  >
                    {actions.map((action) => (
                      <ToggleGroupItem<BulkCustomFieldChange["action"]>
                        key={action}
                        value={action}
                      >
                        {intl.formatMessage({
                          id: actionMessages[action].label,
                        })}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                  <FieldDescription>
                    {intl.formatMessage({
                      id: actionMessages[change.action].description,
                    })}
                  </FieldDescription>
                  {change.action === "set" && (
                    <Field className="gap-1">
                      <FieldLabel htmlFor={valueId}>
                        {intl.formatMessage({ id: "custom_fields.value" })}
                      </FieldLabel>
                      <Input
                        id={valueId}
                        value={change.value}
                        onChange={(event) =>
                          updateShared({ ...change, value: event.target.value })
                        }
                        disabled={disabled}
                      />
                    </Field>
                  )}
                </FieldSet>
              );
            })}
            {summary.partial_names.length > 0 && (
              <FieldDescription>
                {intl.formatMessage(
                  { id: "custom_fields.bulk.partial" },
                  { count: summary.partial_names.length },
                )}
              </FieldDescription>
            )}
            <NewCustomFields
              value={current.added}
              onChange={(added) => onChange({ ...current, added })}
              summary={summary}
              entityMode={entityMode}
              disabled={disabled}
            />
          </>
        ))}
      <FieldDescription>
        {intl.formatMessage({ id: "custom_fields.bulk.scope_change" })}
      </FieldDescription>
    </FieldSet>
  );
}

function NewCustomFields({
  value,
  onChange,
  summary,
  entityMode,
  disabled,
}: {
  value: NonNullable<BulkCustomFieldsValue>["added"];
  onChange: (value: NonNullable<BulkCustomFieldsValue>["added"]) => void;
  summary: BulkCustomFieldSummary;
  entityMode: FilterMode;
  disabled?: boolean;
}) {
  const intl = useIntl();
  const id = useId();
  const rows = useEditableRows(value, onChange);
  const validation = bulkCustomFieldsSchema(summary).safeParse({
    shared: [],
    added: value,
  });
  const { data, loading, error } = useQuery(CustomFieldNamesDocument, {
    variables: { mode: entityMode },
    skip: value.length === 0,
    fetchPolicy: "cache-and-network",
  });
  const existing = new Set([...summary.shared_names, ...summary.partial_names]);
  const options =
    data?.customFieldNames.filter((name) => !existing.has(name)) ?? [];

  return (
    <FieldGroup className="gap-3">
      {rows.rows.map(({ key, value: change }, index) => {
        const nameId = `${id}-${key}-name`;
        const valueId = `${id}-${key}-value`;
        const errorId = `${id}-${key}-error`;
        const nameError = validation.error?.issues.find(
          (issue) => issue.path[0] === "added" && issue.path[1] === index,
        )?.message;
        return (
          <FieldGroup key={key} className="gap-2 rounded-lg border p-3">
            <Field data-invalid={!!nameError} className="gap-1">
              <div className="flex items-center justify-between gap-2">
                <FieldLabel htmlFor={nameId}>
                  {intl.formatMessage({ id: "custom_fields.field" })}
                </FieldLabel>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  disabled={disabled}
                  onClick={() => rows.remove(index)}
                  aria-label={intl.formatMessage({
                    id: "custom_fields.bulk.discard",
                  })}
                >
                  <XIcon />
                </Button>
              </div>
              <CustomFieldNameInput
                id={nameId}
                value={change.name}
                onChange={(name) => rows.update(index, { ...change, name })}
                options={options}
                loading={loading}
                disabled={disabled}
                aria-invalid={!!nameError}
                aria-describedby={nameError ? errorId : undefined}
              />
              {nameError && (
                <FieldError id={errorId}>
                  {intl.formatMessage({ id: nameError })}
                </FieldError>
              )}
            </Field>
            <Field className="gap-1">
              <FieldLabel htmlFor={valueId}>
                {intl.formatMessage({ id: "custom_fields.value" })}
              </FieldLabel>
              <Input
                id={valueId}
                value={change.value}
                onChange={(event) =>
                  rows.update(index, { ...change, value: event.target.value })
                }
                disabled={disabled}
              />
            </Field>
          </FieldGroup>
        );
      })}
      {value.length > 0 && error && (
        <FieldDescription>
          {intl.formatMessage({
            id: "custom_fields.bulk.suggestions_unavailable",
          })}
        </FieldDescription>
      )}
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="self-start"
        disabled={disabled}
        onClick={() => rows.append({ name: "", value: "" })}
      >
        <PlusIcon data-icon="inline-start" />
        {intl.formatMessage({ id: "custom_fields.bulk.add" })}
      </Button>
    </FieldGroup>
  );
}
