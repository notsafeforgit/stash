import { useIntl } from "react-intl";
import { BanIcon, CheckIcon, XIcon } from "lucide-react";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";

type BoolMode = "keep" | "true" | "false";

interface BulkBooleanFieldProps {
  /** undefined = no change, true/false = set to that value on every item. */
  value: boolean | undefined;
  onChange: (v: boolean | undefined) => void;
  disabled?: boolean;
  /** Optional override for the True/False button labels. Defaults to Yes/No. */
  trueLabel?: string;
  falseLabel?: string;
}

/**
 * Tri-state boolean for bulk update dialogs.
 * Three explicit options: Keep / Yes / No.
 *
 * - undefined: leave existing values untouched
 * - true:      set every selected item to true
 * - false:     set every selected item to false
 */
export function BulkBooleanField({
  value,
  onChange,
  disabled,
  trueLabel,
  falseLabel,
}: BulkBooleanFieldProps) {
  const intl = useIntl();

  const mode: BoolMode =
    value === undefined ? "keep" : value ? "true" : "false";

  function handleValueChange(values: BoolMode[]) {
    if (values.length === 0) return;
    const next = values[0];
    if (next === mode) return;
    if (next === "keep") onChange(undefined);
    else if (next === "true") onChange(true);
    else onChange(false);
  }

  const trueText =
    trueLabel ?? intl.formatMessage({ id: "yes", defaultMessage: "Yes" });
  const falseText =
    falseLabel ?? intl.formatMessage({ id: "no", defaultMessage: "No" });

  // Wrapper div absorbs the parent Field's `*:w-full` rule so the inline-flex
  // toggle group inside is sized to its content rather than stretched.
  return (
    <div>
      <ToggleGroup<BoolMode>
        value={[mode]}
        onValueChange={handleValueChange}
        disabled={disabled}
        variant="outline"
        size="sm"
        aria-label={intl.formatMessage({
          id: "bulk_update_field_mode",
          defaultMessage: "Field update mode",
        })}
      >
        <ToggleGroupItem<BoolMode>
          value="keep"
          aria-label={intl.formatMessage({
            id: "bulk_update_keep_existing_value",
            defaultMessage: "Keep existing value",
          })}
        >
          <BanIcon />
          {intl.formatMessage({ id: "actions.keep", defaultMessage: "Keep" })}
        </ToggleGroupItem>
        <ToggleGroupItem<BoolMode> value="true" aria-label={trueText}>
          <CheckIcon />
          {trueText}
        </ToggleGroupItem>
        <ToggleGroupItem<BoolMode> value="false" aria-label={falseText}>
          <XIcon />
          {falseText}
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
  );
}
