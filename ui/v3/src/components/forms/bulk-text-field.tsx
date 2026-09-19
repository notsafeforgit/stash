import { useIntl } from "react-intl";
import { cn } from "src/lib/utils";
import { Input } from "src/components/ui/input";
import { Textarea } from "src/components/ui/textarea";
import {
  BulkFieldModeToggle,
  bulkPlaceholder,
  useBulkFieldMode,
  type BulkFieldMode,
} from "./bulk-field-mode";

interface BulkTextFieldProps {
  id?: string;
  /** undefined = no change, null = clear, string = set */
  value: string | null | undefined;
  onChange: (v: string | null | undefined) => void;
  multiline?: boolean;
  disabled?: boolean;
  className?: string;
}

/**
 * A text input for bulk update dialogs.
 * - `undefined`: keep existing values
 * - `null`: clear the field on every selected item
 * - `string`: set every selected item to this value
 *
 * Mode is driven by the Keep/Clear/Set toggle above the input.
 */
export function BulkTextField({
  id,
  value,
  onChange,
  multiline = false,
  disabled,
  className,
}: BulkTextFieldProps) {
  const intl = useIntl();
  const { mode, setMode, emit } = useBulkFieldMode(value, onChange);

  function handleModeChange(next: BulkFieldMode) {
    if (next === mode) return;
    setMode(next);
    if (next === "keep") emit(undefined);
    else if (next === "clear") emit(null);
    else emit(typeof value === "string" ? value : undefined);
  }

  const inputValue = mode === "set" && typeof value === "string" ? value : "";
  const inputDisabled = disabled || mode !== "set";

  const placeholder =
    mode === "set"
      ? intl.formatMessage({
          id: "bulk_field_set_placeholder",
          defaultMessage: "Enter a new value…",
        })
      : bulkPlaceholder(mode, intl);

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <BulkFieldModeToggle
        mode={mode}
        onModeChange={handleModeChange}
        disabled={disabled}
      />
      {multiline ? (
        <Textarea
          id={id}
          value={inputValue}
          placeholder={placeholder}
          onChange={(e) => emit(e.target.value)}
          disabled={inputDisabled}
          rows={3}
        />
      ) : (
        <Input
          id={id}
          value={inputValue}
          placeholder={placeholder}
          onChange={(e) => emit(e.target.value)}
          disabled={inputDisabled}
        />
      )}
    </div>
  );
}
