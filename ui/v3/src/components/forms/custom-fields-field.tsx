import { useEffect, useMemo, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { Input } from "src/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "src/components/ui/input-group";

export type CustomFieldMap = { [key: string]: unknown };

const MAX_FIELD_NAME_LENGTH = 64;

function isNumeric(v: string): boolean {
  return /^-?(?:0|(?:[1-9][0-9]*))(?:\.[0-9]+)?$/.test(v);
}

function coerceValue(v: string): string | number {
  return isNumeric(v) ? Number(v) : v;
}

function valueToString(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (Array.isArray(v)) return v.join(", ");
  return JSON.stringify(v);
}

interface CustomFieldsFieldProps {
  value: CustomFieldMap;
  onChange: (value: CustomFieldMap) => void;
  disabled?: boolean;
}

let rowIdCounter = 0;
const makeRowId = () => `cf-row-${++rowIdCounter}`;

export function CustomFieldsField({
  value,
  onChange,
  disabled = false,
}: CustomFieldsFieldProps) {
  const intl = useIntl();

  const fieldNames = useMemo(() => {
    const names = Object.keys(value);
    names.sort();
    return names;
  }, [value]);

  // Stable per-row keys so React doesn't shuffle inputs while a row is
  // focused (mirrors UrlListField).
  const keysRef = useRef<Map<string, string>>(new Map());
  fieldNames.forEach((n) => {
    if (!keysRef.current.has(n)) keysRef.current.set(n, makeRowId());
  });
  for (const k of Array.from(keysRef.current.keys())) {
    if (!fieldNames.includes(k)) keysRef.current.delete(k);
  }

  const [newField, setNewField] = useState("");
  const [newValue, setNewValue] = useState("");

  const newFieldError = useMemo<string | undefined>(() => {
    const trimmed = newField.trim();
    if (newField === "") return undefined;
    if (newField.length > MAX_FIELD_NAME_LENGTH) {
      return intl.formatMessage({
        id: "errors.custom_fields.field_name_length",
      });
    }
    if (trimmed !== newField) {
      return intl.formatMessage({
        id: "errors.custom_fields.field_name_whitespace",
      });
    }
    if (Object.hasOwn(value, newField)) {
      return intl.formatMessage({
        id: "errors.custom_fields.duplicate_field",
      });
    }
    return undefined;
  }, [newField, value, intl]);

  function updateValue(field: string, raw: string) {
    onChange({ ...value, [field]: coerceValue(raw) });
  }

  function removeField(field: string) {
    const next = { ...value };
    delete next[field];
    onChange(next);
  }

  function commitNewField() {
    if (!newField || newFieldError) return;
    onChange({ ...value, [newField]: coerceValue(newValue) });
    setNewField("");
    setNewValue("");
  }

  return (
    <div className="flex flex-col gap-2">
      {fieldNames.length > 0 && (
        <div className="flex flex-col gap-1.5">
          {fieldNames.map((name) => (
            <ExistingFieldRow
              key={keysRef.current.get(name)}
              name={name}
              rawValue={valueToString(value[name])}
              disabled={disabled}
              onChangeValue={(v) => updateValue(name, v)}
              onRemove={() => removeField(name)}
              removeAriaLabel={intl.formatMessage(
                {
                  id: "actions.remove_field",
                  defaultMessage: "Remove {field}",
                },
                { field: name },
              )}
            />
          ))}
        </div>
      )}

      <div className="flex flex-col gap-1 pt-1 border-t border-border/40">
        <div className="flex flex-col sm:flex-row gap-1.5">
          <Input
            type="text"
            value={newField}
            placeholder={intl.formatMessage({ id: "custom_fields.field" })}
            disabled={disabled}
            className="sm:w-1/3"
            onChange={(e) => setNewField(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitNewField();
              }
            }}
            aria-invalid={newFieldError ? true : undefined}
          />
          <InputGroup className="flex-1">
            <InputGroupInput
              value={newValue}
              placeholder={intl.formatMessage({ id: "custom_fields.value" })}
              disabled={disabled}
              onChange={(e) => setNewValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  commitNewField();
                }
              }}
            />
            <InputGroupAddon align="inline-end">
              <InputGroupButton
                type="button"
                size="icon-xs"
                variant="ghost"
                disabled={disabled || !newField || !!newFieldError}
                onClick={commitNewField}
                aria-label={intl.formatMessage({
                  id: "actions.add",
                  defaultMessage: "Add",
                })}
              >
                <PlusIcon className="pointer-events-none size-3.5" />
              </InputGroupButton>
            </InputGroupAddon>
          </InputGroup>
        </div>
        {newFieldError && (
          <p className="text-xs text-destructive">{newFieldError}</p>
        )}
      </div>
    </div>
  );
}

function ExistingFieldRow({
  name,
  rawValue,
  disabled,
  onChangeValue,
  onRemove,
  removeAriaLabel,
}: {
  name: string;
  rawValue: string;
  disabled: boolean;
  onChangeValue: (v: string) => void;
  onRemove: () => void;
  removeAriaLabel: string;
}) {
  // Local buffer so the user can type freely; commit on blur/Enter to
  // run the type coercion (numeric strings -> Number).
  const [draft, setDraft] = useState(rawValue);
  useEffect(() => {
    setDraft(rawValue);
  }, [rawValue]);

  function commit() {
    if (draft !== rawValue) onChangeValue(draft);
  }

  return (
    <div className="flex flex-col sm:flex-row gap-1.5">
      <div
        className="sm:w-1/3 px-2 py-1.5 text-sm font-medium truncate"
        title={name}
      >
        {name}
      </div>
      <InputGroup className="flex-1">
        <InputGroupInput
          value={draft}
          disabled={disabled}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commit();
            }
          }}
        />
        <InputGroupAddon align="inline-end">
          <InputGroupButton
            type="button"
            variant="ghost"
            size="icon-xs"
            disabled={disabled}
            onClick={onRemove}
            aria-label={removeAriaLabel}
          >
            <Trash2Icon className="pointer-events-none size-3.5" />
          </InputGroupButton>
        </InputGroupAddon>
      </InputGroup>
    </div>
  );
}

/**
 * Build a CustomFieldsInput payload for a *Update mutation. For *Create
 * mutations the schema accepts a bare Map — pass `value` directly there.
 */
export function customFieldsUpdateInput(value: CustomFieldMap) {
  return { full: value };
}
