import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import { RatingSystem } from "src/components/ui/rating-system";
import {
  BulkFieldModeToggle,
  bulkPlaceholder,
  useBulkFieldMode,
  type BulkFieldMode,
} from "./bulk-field-mode";

interface BulkRatingFieldProps {
  /** undefined = no change, null = clear, number = set */
  value: number | null | undefined;
  onChange: (v: number | null | undefined) => void;
  disabled?: boolean;
}

/**
 * A rating field for bulk update dialogs.
 * - `undefined`: keep existing values
 * - `null`: clear the rating on every selected item
 * - `number`: set every selected item to this rating
 *
 * In Keep/Clear modes a uniform disabled placeholder input is shown; the
 * star picker only appears in Set mode.
 */
export function BulkRatingField({
  value,
  onChange,
  disabled,
}: BulkRatingFieldProps) {
  const intl = useIntl();
  const { mode, setMode, emit } = useBulkFieldMode(value, onChange);

  function handleModeChange(next: BulkFieldMode) {
    if (next === mode) return;
    setMode(next);
    if (next === "keep") emit(undefined);
    else if (next === "clear") emit(null);
    else emit(typeof value === "number" ? value : undefined);
  }

  const ratingValue = typeof value === "number" ? value : null;

  return (
    <div className="flex flex-col gap-2">
      <BulkFieldModeToggle
        mode={mode}
        onModeChange={handleModeChange}
        disabled={disabled}
      />
      {mode === "set" ? (
        <div className="flex h-8 items-center">
          <RatingSystem
            value={ratingValue}
            onSetRating={(v) => emit(v)}
            disabled={disabled}
            clickToRate
            withoutContext
          />
        </div>
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
