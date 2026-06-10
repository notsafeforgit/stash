import { useIntl } from "react-intl";
import { DateInput } from "src/components/ui/date-input";
import { Input } from "src/components/ui/input";
import {
  BulkFieldModeToggle,
  bulkPlaceholder,
  useBulkFieldMode,
  type BulkFieldMode,
} from "./bulk-field-mode";

interface BulkDateFieldProps {
  /** undefined = no change, null = clear, "YYYY-MM-DD" = set */
  value: string | null | undefined;
  onChange: (v: string | null | undefined) => void;
  disabled?: boolean;
}

/**
 * A date field for bulk update dialogs.
 * - `undefined`: keep existing values
 * - `null`: clear the date on every selected item
 * - `"YYYY-MM-DD"`: set every selected item to this date
 *
 * In Keep/Clear modes a uniform disabled placeholder input is shown; the
 * date picker only appears in Set mode.
 */
export function BulkDateField({
  value,
  onChange,
  disabled,
}: BulkDateFieldProps) {
  const intl = useIntl();
  const { mode, setMode, emit } = useBulkFieldMode(value, onChange);

  function handleModeChange(next: BulkFieldMode) {
    if (next === mode) return;
    setMode(next);
    if (next === "keep") emit(undefined);
    else if (next === "clear") emit(null);
    else emit(typeof value === "string" ? value : undefined);
  }

  const dateValue = mode === "set" && typeof value === "string" ? value : "";

  return (
    <div className="flex flex-col gap-2">
      <BulkFieldModeToggle
        mode={mode}
        onModeChange={handleModeChange}
        disabled={disabled}
      />
      {mode === "set" ? (
        <DateInput
          value={dateValue}
          onValueChange={(v) => emit(v || undefined)}
          disabled={disabled}
          placeholder={intl.formatMessage({
            id: "bulk_field_set_date_placeholder",
            defaultMessage: "Pick a date",
          })}
        />
      ) : (
        <Input
          value=""
          placeholder={bulkPlaceholder(mode, intl)}
          disabled
          readOnly
        />
      )}
    </div>
  );
}
