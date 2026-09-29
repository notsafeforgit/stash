import { useId } from "react";
import { useIntl } from "react-intl";
import { PlusIcon, XIcon } from "lucide-react";
import { z } from "zod";
import { useEditableRows } from "@/hooks/use-editable-rows";
import { useMsg } from "@/hooks/message";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";

const storedMappingsSchema = z.record(z.string(), z.string());
const rowsSchema = z.array(
  z.object({ target: z.string(), expression: z.string() }),
);
type MappingRow = z.infer<typeof rowsSchema>[number];

function isMappingRows(value: unknown): value is MappingRow[] {
  return rowsSchema.safeParse(value).success;
}

export function mappingDraft(value: unknown): MappingRow[] | string {
  try {
    const mappings = storedMappingsSchema.parse(
      typeof value === "string" ? JSON.parse(value) : value,
    );
    return Object.entries(mappings).map(([target, expression]) => ({
      target,
      expression,
    }));
  } catch {
    // Retain malformed saved values so they can be repaired without data loss.
    return typeof value === "string" ? value : (JSON.stringify(value) ?? "");
  }
}

export function useMappingValue() {
  const msg = useMsg();
  return (value: unknown): Record<string, string> => {
    if (!isMappingRows(value)) {
      throw new Error(
        msg(
          "config.plugins.mapping_invalid",
          "Repair the saved mapping data before saving.",
        ),
      );
    }
    const names = new Set<string>();
    for (const { target, expression } of value) {
      if (!target.trim() || !expression.trim()) {
        throw new Error(
          msg(
            "config.plugins.mapping_required",
            "Each mapping needs a target field and a jq expression.",
          ),
        );
      }
      if (names.has(target)) {
        throw new Error(
          msg(
            "config.plugins.mapping_duplicate",
            "Each target field can only appear once.",
          ),
        );
      }
      names.add(target);
    }
    return Object.fromEntries(
      value.map(({ target, expression }) => [target, expression]),
    );
  };
}

export function PluginMappingEditor({
  value,
  onChange,
  disabled,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
  disabled: boolean;
}) {
  const id = useId();
  const msg = useMsg();
  if (!isMappingRows(value)) {
    return (
      <Field data-invalid data-disabled={disabled}>
        <FieldLabel htmlFor={id}>
          {msg("config.plugins.mapping_stored_data", "Saved mapping data")}
        </FieldLabel>
        <Textarea
          id={id}
          value={
            typeof value === "string" ? value : (JSON.stringify(value) ?? "")
          }
          onChange={(event) => onChange(mappingDraft(event.target.value))}
          disabled={disabled}
          aria-invalid
          aria-describedby={`${id}-error`}
          spellCheck={false}
        />
        <FieldError id={`${id}-error`}>
          {msg(
            "config.plugins.mapping_invalid",
            "Repair the saved mapping data before saving.",
          )}
        </FieldError>
      </Field>
    );
  }
  return <MappingRows value={value} onChange={onChange} disabled={disabled} />;
}

function MappingRows({
  value,
  onChange,
  disabled,
}: {
  value: MappingRow[];
  onChange: (value: MappingRow[]) => void;
  disabled: boolean;
}) {
  const id = useId();
  const msg = useMsg();
  const intl = useIntl();
  const { rows, update, remove, append } = useEditableRows(value, onChange);
  return (
    <FieldGroup className="gap-3">
      <FieldDescription>
        {msg(
          "config.plugins.mapping_help",
          "Write jq directly, including quotes and line breaks.",
        )}
      </FieldDescription>
      {rows.map(({ key, value: row }, index) => {
        const targetId = `${id}-${key}-target`;
        const expressionId = `${id}-${key}-expression`;
        const duplicate =
          !!row.target &&
          value.filter((item) => item.target === row.target).length > 1;
        return (
          <FieldGroup
            key={key}
            className="gap-3 rounded-lg border p-3 @lg/field-group:grid @lg/field-group:grid-cols-[minmax(8rem,1fr)_minmax(0,3fr)_auto] @lg/field-group:items-start"
          >
            <Field data-invalid={duplicate} data-disabled={disabled}>
              <FieldLabel htmlFor={targetId}>
                {msg("config.plugins.mapping_target", "Target field")}
              </FieldLabel>
              <Input
                id={targetId}
                value={row.target}
                onChange={(event) =>
                  update(index, { ...row, target: event.target.value })
                }
                disabled={disabled}
                spellCheck={false}
                autoComplete="off"
                aria-invalid={duplicate}
                aria-describedby={duplicate ? `${targetId}-error` : undefined}
              />
              {duplicate && (
                <FieldError id={`${targetId}-error`}>
                  {msg(
                    "config.plugins.mapping_duplicate",
                    "Each target field can only appear once.",
                  )}
                </FieldError>
              )}
            </Field>
            <Field data-disabled={disabled}>
              <FieldLabel htmlFor={expressionId}>
                {msg("config.plugins.mapping_expression", "jq expression")}
              </FieldLabel>
              <Textarea
                id={expressionId}
                value={row.expression}
                onChange={(event) =>
                  update(index, { ...row, expression: event.target.value })
                }
                disabled={disabled}
                rows={2}
                spellCheck={false}
                autoComplete="off"
              />
            </Field>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className="self-end @lg/field-group:mt-6 @lg/field-group:self-start"
              disabled={disabled}
              onClick={() => remove(index)}
              aria-label={intl.formatMessage(
                {
                  id: "config.plugins.mapping_remove",
                  defaultMessage: "Remove mapping {field}",
                },
                { field: row.target || index + 1 },
              )}
            >
              <XIcon />
            </Button>
          </FieldGroup>
        );
      })}
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="self-start"
        disabled={disabled}
        onClick={() => append({ target: "", expression: "" })}
      >
        <PlusIcon data-icon="inline-start" />
        {msg("config.plugins.mapping_add", "Add mapping")}
      </Button>
    </FieldGroup>
  );
}
