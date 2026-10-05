import { useContext, useEffect, useId, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { Plus, X } from "lucide-react";
import { useMsg } from "@/hooks/message";
import type {
  MetadataPolicyAPI,
  PolicyField,
  PolicyReference,
} from "@/core/native-archive/metadata-policy-api";
import type { PolicyMappingValues } from "@/core/native-archive/metadata-policy-form";
import { createMetadataReviewAPI } from "@/core/native-archive/metadata-review-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import { ExistingEntityPicker } from "@/components/detail/native-metadata/picker";
import {
  fieldMessages,
  ReviewError,
} from "@/components/detail/native-metadata/shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";
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
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { PolicySelectionPending } from "./pending";

function ReferenceValues({
  api,
  field,
  value,
  id,
  disabled,
  onChange,
}: {
  api: MetadataPolicyAPI;
  field: PolicyField;
  value: string | string[] | null;
  id: string;
  disabled: boolean;
  onChange: (text: string) => void;
}) {
  const msg = useMsg();
  const [identityAPI] = useState(() => createMetadataReviewAPI(api.endpoint));
  const [page, setPage] = useState(0);
  const selection = useRef<AbortController | null>(null);
  const changePending = useContext(PolicySelectionPending);
  const [names, setNames] = useState<PolicyReference[]>([]);
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const values = Array.isArray(value) ? value : value ? [value] : [];
  const currentPage = Math.min(
    page,
    Math.max(0, Math.ceil(values.length / 25) - 1),
  );
  const visible = values.slice(currentPage * 25, (currentPage + 1) * 25);
  const key = JSON.stringify(visible);
  useEffect(() => () => selection.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController();
    setError(undefined);
    setNames([]);
    void api
      .references(JSON.parse(key), controller.signal)
      .then((names) => {
        if (!controller.signal.aborted) setNames(names);
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(error);
      });
    return () => controller.abort();
  }, [api, key]);
  function update(ids: string[]) {
    onChange(
      JSON.stringify(field.type === "reference" ? (ids.at(-1) ?? null) : ids),
    );
  }
  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor={id}>
          {msg("metadata_policy.choose_entry", "Choose a library entry")}
        </FieldLabel>
        {field.reference_kind && (
          <ExistingEntityPicker
            id={id}
            kind={field.reference_kind}
            disabled={disabled || busy}
            onChange={(choice) => {
              const controller = new AbortController();
              selection.current?.abort();
              selection.current = controller;
              setBusy(true);
              changePending(true);
              setError(undefined);
              void identityAPI
                .identity(
                  field.reference_kind ?? "performer",
                  choice.id,
                  controller.signal,
                )
                .then((identity) => {
                  if (controller.signal.aborted) return;
                  if (
                    identity.kind !== field.reference_kind ||
                    String(identity.local_id) !== choice.id
                  )
                    throw new NativeArchiveError(0, "identity_mismatch");
                  update([...new Set([...values, identity.uuid])]);
                })
                .catch((error) => {
                  if (!controller.signal.aborted) setError(error);
                })
                .finally(() => {
                  setBusy(false);
                  changePending(false);
                });
            }}
          />
        )}
        <FieldDescription>
          {msg(
            "metadata_policy.fixed_entries_help",
            "These entries are selected explicitly. Source publishers are not automatically treated as depicted performers.",
          )}
        </FieldDescription>
      </Field>
      {busy && <Spinner />}
      {error !== undefined && <ReviewError error={error} />}
      {visible.map((uuid, index) => {
        const ref = names.find((item) => item.requested_uuid === uuid);
        return (
          <div
            key={`${uuid}:${index}`}
            className="flex items-center justify-between gap-2"
          >
            <span data-selectable-text className="min-w-0 wrap-anywhere">
              {ref?.entity
                ? `${ref.name}${ref.disambiguation ? ` (${ref.disambiguation})` : ""} (#${ref.entity.local_id})`
                : uuid}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              disabled={disabled || busy}
              aria-label={msg(
                "metadata_policy.remove_entry",
                "Remove selected entry",
              )}
              onClick={() => update(values.filter((item) => item !== uuid))}
            >
              <X />
            </Button>
          </div>
        );
      })}
      {values.length > 25 && (
        <div className="flex gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={currentPage === 0}
            onClick={() => setPage(currentPage - 1)}
          >
            {msg("actions.previous", "Previous")}
          </Button>
          <Button
            type="button"
            variant="outline"
            className="w-fit"
            onClick={() => setPage(currentPage + 1)}
            disabled={(currentPage + 1) * 25 >= values.length}
          >
            {msg("actions.next", "Next")}
          </Button>
        </div>
      )}
    </FieldGroup>
  );
}

function ConstantValue({
  api,
  row,
  field,
  id,
  disabled,
  onChange,
}: {
  api: MetadataPolicyAPI;
  row: PolicyMappingValues;
  field?: PolicyField;
  id: string;
  disabled: boolean;
  onChange: (text: string) => void;
}) {
  let value: unknown;
  let valid = true;
  try {
    value = JSON.parse(row.text);
  } catch {
    valid = false;
  }
  const msg = useMsg();
  if (
    (row.reference_names || row.performer_names) &&
    field?.type === "references" &&
    Array.isArray(value) &&
    value.every((item) => typeof item === "string")
  )
    return (
      <Textarea
        id={id}
        disabled={disabled}
        value={value.join("\n")}
        onChange={(event) =>
          onChange(
            JSON.stringify(
              event.target.value ? event.target.value.split("\n") : [],
            ),
          )
        }
      />
    );
  if (
    row.reference_names &&
    field?.type === "reference" &&
    (value === null || typeof value === "string")
  )
    return (
      <Input
        id={id}
        disabled={disabled}
        value={value ?? ""}
        onChange={(event) =>
          onChange(JSON.stringify(event.target.value || null))
        }
      />
    );
  if (
    field?.type === "references" &&
    Array.isArray(value) &&
    value.every((item) => typeof item === "string")
  )
    return (
      <ReferenceValues
        id={id}
        api={api}
        field={field}
        value={value}
        disabled={disabled}
        onChange={onChange}
      />
    );
  if (
    field?.type === "reference" &&
    (value === null || typeof value === "string")
  )
    return (
      <ReferenceValues
        id={id}
        api={api}
        field={field}
        value={value}
        disabled={disabled}
        onChange={onChange}
      />
    );
  if (field?.type === "boolean" && typeof value === "boolean")
    return (
      <Checkbox
        id={id}
        disabled={disabled}
        checked={value}
        onCheckedChange={(checked) => onChange(JSON.stringify(checked))}
      />
    );
  if (
    field?.type === "integer" &&
    (value === null || typeof value === "number")
  )
    return (
      <Input
        id={id}
        type="number"
        min={0}
        max={100}
        step={1}
        disabled={disabled}
        value={value ?? ""}
        onChange={(event) =>
          onChange(event.target.value === "" ? "null" : event.target.value)
        }
      />
    );
  if (field?.type === "date" && (value === null || typeof value === "string"))
    return (
      <Input
        id={id}
        type="date"
        disabled={disabled}
        value={value ?? ""}
        onChange={(event) =>
          onChange(JSON.stringify(event.target.value || null))
        }
      />
    );
  if (field?.type === "string" && typeof value === "string")
    return (
      <Textarea
        id={id}
        disabled={disabled}
        value={value}
        onChange={(event) => onChange(JSON.stringify(event.target.value))}
      />
    );
  return (
    <>
      <Textarea
        id={id}
        disabled={disabled}
        value={row.text}
        aria-invalid={!valid}
        onChange={(event) => onChange(event.target.value)}
      />
      <FieldDescription>
        {msg(
          "metadata_policy.json_value_help",
          "Enter one JSON value. It is stored as structured data, without an extra JSON string layer.",
        )}
      </FieldDescription>
      {!valid && (
        <FieldError>
          {msg("metadata_policy.invalid_json", "Enter a valid JSON value.")}
        </FieldError>
      )}
    </>
  );
}

export function PolicyMappingEditor({
  api,
  row,
  fields,
  used,
  disabled,
  onChange,
  onRemove,
}: {
  api: MetadataPolicyAPI;
  row: PolicyMappingValues;
  fields: PolicyField[];
  used: string[];
  disabled: boolean;
  onChange: (row: PolicyMappingValues) => void;
  onRemove: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const id = useId();
  const field = fields.find((item) => item.name === row.target);
  const options = fields
    .filter((item) => item.name === row.target || !used.includes(item.name))
    .map((item) => ({
      value: item.name,
      label: intl.formatMessage({ id: fieldMessages[item.name] ?? item.name }),
    }));
  return (
    <FieldSet data-disabled={disabled}>
      <FieldLegend>
        {field
          ? intl.formatMessage({ id: fieldMessages[field.name] ?? field.name })
          : msg("metadata_policy.mapping", "Field mapping")}
      </FieldLegend>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor={`${id}-target`}>
            {msg("metadata_policy.target_field", "Target field")}
          </FieldLabel>
          <Select
            items={options}
            value={row.target}
            disabled={disabled}
            onValueChange={(target) => {
              if (target && target !== row.target)
                onChange({
                  ...row,
                  target,
                  performer_names: false,
                  reference_names: false,
                  text:
                    row.mode === "jq"
                      ? "empty"
                      : JSON.stringify(
                          fields.find((item) => item.name === target)
                            ?.clear_value ?? null,
                        ),
                });
            }}
          >
            <SelectTrigger id={`${id}-target`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {options.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel id={`${id}-mode`}>
            {msg("metadata_policy.mapping_mode", "Value source")}
          </FieldLabel>
          <ToggleGroup
            aria-labelledby={`${id}-mode`}
            variant="outline"
            disabled={disabled}
            value={[row.mode]}
            onValueChange={(values) => {
              const mode = values[0];
              if ((mode === "jq" || mode === "value") && mode !== row.mode)
                onChange({
                  ...row,
                  mode,
                  text:
                    mode === "jq"
                      ? "empty"
                      : JSON.stringify(field?.clear_value ?? null),
                });
            }}
          >
            <ToggleGroupItem value="jq">
              {msg("metadata_policy.jq", "jq expression")}
            </ToggleGroupItem>
            <ToggleGroupItem value="value">
              {msg("metadata_policy.constant", "Fixed value")}
            </ToggleGroupItem>
          </ToggleGroup>
        </Field>
        {field?.reference_kind && (
          <Field orientation="horizontal" data-disabled={disabled}>
            <Checkbox
              id={`${id}-names`}
              checked={row.reference_names || row.performer_names}
              disabled={disabled}
              onCheckedChange={(checked) =>
                onChange({
                  ...row,
                  performer_names: false,
                  reference_names: checked,
                  text:
                    row.mode === "value"
                      ? JSON.stringify(field.clear_value)
                      : row.text,
                })
              }
            />
            <FieldContent>
              <FieldLabel htmlFor={`${id}-names`}>
                {msg("metadata_policy.match_names", "Match names")}
              </FieldLabel>
              <FieldDescription>
                {msg(
                  "metadata_policy.match_names_help",
                  "Match exact names and performer, studio or tag aliases. Missing or ambiguous names stay for review; existing relationships are preserved.",
                )}
              </FieldDescription>
            </FieldContent>
          </Field>
        )}
        <Field data-disabled={disabled}>
          <FieldLabel htmlFor={`${id}-value`}>
            {row.mode === "jq"
              ? msg("metadata_policy.jq", "jq expression")
              : msg("metadata_policy.constant", "Fixed value")}
          </FieldLabel>
          {row.mode === "jq" ? (
            <Textarea
              id={`${id}-value`}
              disabled={disabled}
              value={row.text}
              onChange={(event) =>
                onChange({ ...row, text: event.target.value })
              }
            />
          ) : (
            <ConstantValue
              api={api}
              id={`${id}-value`}
              row={row}
              field={field}
              disabled={disabled}
              onChange={(text) => onChange({ ...row, text })}
            />
          )}
          {row.mode === "jq" && (
            <FieldDescription>
              {msg(
                "metadata_policy.jq_help",
                "Read .source, .entity and .context. Return empty to omit this field. Internal plugin settings are not included.",
              )}
            </FieldDescription>
          )}
        </Field>
        {(row.reference_names || row.performer_names) && (
          <FieldDescription>
            {field?.type === "reference"
              ? msg(
                  "metadata_policy.name_shape_studio",
                  "Use one studio name, or null to clear it. A blank fixed value means null.",
                )
              : field?.type === "groups"
                ? msg(
                    "metadata_policy.name_shape_groups",
                    "Use an array of objects with name and optional scene_index. Group matching uses canonical names.",
                  )
                : msg(
                    "metadata_policy.name_shape_list",
                    "Use an array of names. For a fixed value, enter one name per line. An empty list clears the field.",
                  )}
          </FieldDescription>
        )}
        <Button
          type="button"
          variant="outline"
          className="w-fit"
          disabled={disabled}
          onClick={onRemove}
        >
          <X data-icon="inline-start" />
          {msg("metadata_policy.remove_mapping", "Remove mapping")}
        </Button>
      </FieldGroup>
    </FieldSet>
  );
}

export function AddPolicyMapping({
  disabled,
  onClick,
}: {
  disabled: boolean;
  onClick: () => void;
}) {
  const msg = useMsg();
  return (
    <Button
      type="button"
      variant="outline"
      className="w-fit"
      disabled={disabled}
      onClick={onClick}
    >
      <Plus data-icon="inline-start" />
      {msg("metadata_policy.add_mapping", "Add field mapping")}
    </Button>
  );
}
