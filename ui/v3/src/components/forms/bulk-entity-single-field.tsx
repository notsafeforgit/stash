import { useIntl } from "react-intl";
import { Input } from "src/components/ui/input";
import { EntitySingleSelect, type EntityOption } from "./async-entity-select";
import {
  BulkFieldModeToggle,
  bulkPlaceholder,
  useBulkFieldMode,
  type BulkFieldMode,
} from "./bulk-field-mode";

interface BulkEntitySingleFieldProps {
  /** undefined = no change, null = clear, EntityOption = set */
  value: EntityOption | null | undefined;
  onChange: (v: EntityOption | null | undefined) => void;
  options: EntityOption[];
  onSearch: (q: string) => void;
  loading?: boolean;
  placeholder?: string;
  disabled?: boolean;
}

/**
 * A single-entity select for bulk update dialogs.
 * - `undefined`: keep existing values
 * - `null`: clear the field on every selected item
 * - `EntityOption`: set every selected item to this entity
 *
 * In Keep/Clear modes a uniform disabled placeholder input is shown; the
 * entity picker only appears in Set mode.
 */
export function BulkEntitySingleField({
  value,
  onChange,
  options,
  onSearch,
  loading,
  placeholder,
  disabled,
}: BulkEntitySingleFieldProps) {
  const intl = useIntl();
  const { mode, setMode, emit } = useBulkFieldMode(value, onChange);

  function handleModeChange(next: BulkFieldMode) {
    if (next === mode) return;
    setMode(next);
    if (next === "keep") emit(undefined);
    else if (next === "clear") emit(null);
    else emit(value && typeof value === "object" ? value : undefined);
  }

  const selectValue =
    mode === "set" && value && typeof value === "object" ? value : null;

  return (
    <div className="flex flex-col gap-2">
      <BulkFieldModeToggle
        mode={mode}
        onModeChange={handleModeChange}
        disabled={disabled}
      />
      {mode === "set" ? (
        <EntitySingleSelect
          value={selectValue}
          onChange={(v) => emit(v ?? undefined)}
          options={options}
          onSearch={onSearch}
          loading={loading}
          placeholder={
            placeholder ??
            intl.formatMessage({
              id: "bulk_field_set_entity_placeholder",
              defaultMessage: "Pick an entity",
            })
          }
          disabled={disabled}
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
