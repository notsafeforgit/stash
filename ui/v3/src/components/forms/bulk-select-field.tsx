import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import {
  BulkFieldModeToggle,
  bulkPlaceholder,
  useBulkFieldMode,
  type BulkFieldMode,
} from "./bulk-field-mode";

export interface BulkSelectOption<T extends string> {
  value: T;
  label: string;
}

interface BulkSelectFieldProps<T extends string> {
  /** undefined = no change, null = clear, T = set */
  value: T | null | undefined;
  onChange: (v: T | null | undefined) => void;
  options: BulkSelectOption<T>[];
  disabled?: boolean;
  placeholder?: string;
}

/**
 * A select field for bulk update dialogs.
 * - `undefined`: keep existing values
 * - `null`: clear the value on every selected item
 * - `T`: set every selected item to this value
 *
 * In Keep/Clear modes a uniform disabled placeholder input is shown; the
 * select only appears in Set mode.
 */
export function BulkSelectField<T extends string>({
  value,
  onChange,
  options,
  disabled,
  placeholder,
}: BulkSelectFieldProps<T>) {
  const intl = useIntl();
  const { mode, setMode, emit } = useBulkFieldMode(value, onChange);

  function handleModeChange(next: BulkFieldMode) {
    if (next === mode) return;
    setMode(next);
    if (next === "keep") emit(undefined);
    else if (next === "clear") emit(null);
    else emit(typeof value === "string" ? value : undefined);
  }

  const selectValue =
    mode === "set" && typeof value === "string" ? value : undefined;
  const selectedLabel = options.find((o) => o.value === selectValue)?.label;

  return (
    <div className="flex flex-col gap-2">
      <BulkFieldModeToggle
        mode={mode}
        onModeChange={handleModeChange}
        disabled={disabled}
      />
      {mode === "set" ? (
        <Select
          value={selectValue ?? ""}
          onValueChange={(v) => emit((v as T) || undefined)}
          disabled={disabled}
        >
          <SelectTrigger className="w-full">
            <SelectValue
              placeholder={
                placeholder ??
                intl.formatMessage({
                  id: "bulk_field_set_select_placeholder",
                  defaultMessage: "Pick a value",
                })
              }
            >
              {selectedLabel}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {options.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
