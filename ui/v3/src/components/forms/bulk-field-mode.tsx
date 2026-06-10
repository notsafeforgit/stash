import React from "react";
import { useIntl, type IntlShape } from "react-intl";
import { BanIcon, EraserIcon, PencilIcon } from "lucide-react";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";

export type BulkFieldMode = "keep" | "clear" | "set";

/**
 * The placeholder text used inside disabled inputs to communicate the current
 * bulk-field mode. Wrapped in `< … >` brackets so it reads as meta text rather
 * than as a literal value.
 */
export function bulkPlaceholder(
  mode: Exclude<BulkFieldMode, "set">,
  intl: IntlShape,
): string {
  if (mode === "keep") {
    return `< ${intl.formatMessage({
      id: "bulk_field_keep_placeholder",
      defaultMessage: "existing value kept",
    })} >`;
  }
  return `< ${intl.formatMessage({
    id: "bulk_field_clear_placeholder",
    defaultMessage: "value will be cleared",
  })} >`;
}

interface BulkFieldModeProps {
  mode: BulkFieldMode;
  onModeChange: (m: BulkFieldMode) => void;
  disabled?: boolean;
  /** Hide the "Clear" option for fields where clearing is meaningless. */
  hideClear?: boolean;
}

/**
 * Tri-state mode selector used by all scalar bulk-edit fields.
 * - Keep:   leave existing values untouched (no change is sent)
 * - Clear:  clear the value on every selected item
 * - Set:    write the chosen value to every selected item
 */
export function BulkFieldModeToggle({
  mode,
  onModeChange,
  disabled,
  hideClear,
}: BulkFieldModeProps) {
  const intl = useIntl();

  function handleValueChange(values: BulkFieldMode[]) {
    // ToggleGroup is single-select but allows deselect; ignore that and keep current.
    if (values.length === 0) return;
    onModeChange(values[0]);
  }

  return (
    <ToggleGroup<BulkFieldMode>
      value={[mode]}
      onValueChange={handleValueChange}
      disabled={disabled}
      variant="outline"
      size="sm"
      className="self-start"
      aria-label={intl.formatMessage({
        id: "bulk_update_field_mode",
        defaultMessage: "Field update mode",
      })}
    >
      <ToggleGroupItem<BulkFieldMode>
        value="keep"
        aria-label={intl.formatMessage({
          id: "bulk_update_keep_existing_value",
          defaultMessage: "Keep existing value",
        })}
      >
        <BanIcon />
        {intl.formatMessage({ id: "actions.keep", defaultMessage: "Keep" })}
      </ToggleGroupItem>
      {!hideClear && (
        <ToggleGroupItem<BulkFieldMode>
          value="clear"
          aria-label={intl.formatMessage({
            id: "actions.clear",
            defaultMessage: "Clear",
          })}
        >
          <EraserIcon />
          {intl.formatMessage({ id: "actions.clear", defaultMessage: "Clear" })}
        </ToggleGroupItem>
      )}
      <ToggleGroupItem<BulkFieldMode>
        value="set"
        aria-label={intl.formatMessage({
          id: "actions.set",
          defaultMessage: "Set",
        })}
      >
        <PencilIcon />
        {intl.formatMessage({ id: "actions.set", defaultMessage: "Set" })}
      </ToggleGroupItem>
    </ToggleGroup>
  );
}

/** Derive the visible mode from a tri-state value. */
export function deriveBulkMode<T>(value: T | null | undefined): BulkFieldMode {
  if (value === undefined) return "keep";
  if (value === null) return "clear";
  return "set";
}

/**
 * Helper hook: keeps an internal `mode` state that stays in sync with the
 * controlled `value` when the parent resets it (e.g. on sheet open or when
 * the apply-to-all switch is toggled). Callers should use the returned
 * `setMode` instead of writing to `mode` directly.
 *
 * The hook also returns an `emit` wrapper around `onChange` that records the
 * emitted value so subsequent value props matching it are recognised as
 * internally-driven and don't reset the mode.
 */
export function useBulkFieldMode<T>(
  value: T | null | undefined,
  onChange: (v: T | null | undefined) => void,
) {
  const [mode, setMode] = React.useState<BulkFieldMode>(() =>
    deriveBulkMode(value),
  );
  const lastEmittedRef = React.useRef<T | null | undefined>(value);

  React.useEffect(() => {
    if (value !== lastEmittedRef.current) {
      lastEmittedRef.current = value;
      setMode(deriveBulkMode(value));
    }
  }, [value]);

  const emit = React.useCallback(
    (v: T | null | undefined) => {
      lastEmittedRef.current = v;
      onChange(v);
    },
    [onChange],
  );

  return { mode, setMode, emit };
}
