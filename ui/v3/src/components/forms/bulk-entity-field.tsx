import { useCallback, useMemo, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { MinusIcon, PencilIcon, PlusIcon } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";
import { EntityMultiSelect, type EntityOption } from "./async-entity-select";

interface BulkEntityFieldProps {
  value: GQL.BulkUpdateIds;
  onChange: (v: GQL.BulkUpdateIds) => void;
  options: EntityOption[];
  onSearch: (q: string) => void;
  loading?: boolean;
  placeholder?: string;
  disabled?: boolean;
  /**
   * IDs present on every selected entity. Adding one is a no-op (already
   * there everywhere), so these are filtered out of the Add-mode dropdown.
   * Also used to seed the field when switching to Set mode.
   */
  intersectionIds?: string[];
  /**
   * IDs present on at least one selected entity. Removing an ID not in
   * this set is a no-op, so the Remove-mode dropdown only shows these.
   */
  unionIds?: string[];
  /** Map of id→name for resolving id-only references to display labels. */
  existingNames?: Record<string, string>;
}

/**
 * A relationship field for bulk update dialogs.
 * Renders an Add / Remove / Set toggle group above an EntityMultiSelect.
 */
export function BulkEntityField({
  value,
  onChange,
  options,
  onSearch,
  loading,
  placeholder,
  disabled,
  intersectionIds,
  unionIds,
  existingNames,
}: BulkEntityFieldProps) {
  const intl = useIntl();

  function handleModeChange(mode: GQL.BulkUpdateIdMode) {
    if (mode === value.mode) return;
    // Add and Remove are operations against the existing set — the field
    // shows only what to add or remove, so it starts empty. Set is a full
    // replacement, so we seed the field with the IDs every entity already
    // has in common to give the user a starting point.
    const ids =
      mode === GQL.BulkUpdateIdMode.Set ? (intersectionIds ?? []) : [];
    onChange({ mode, ids });
  }

  function handleToggleChange(values: GQL.BulkUpdateIdMode[]) {
    if (values.length === 0) return;
    handleModeChange(values[0]);
  }

  // Maintain a stable id→name map across renders so chips keep their labels
  // even after the search results change (e.g. user cleared the search input
  // after picking a new item).
  const knownNamesRef = useRef<Record<string, string>>({});
  for (const opt of options) knownNamesRef.current[opt.id] = opt.name;
  if (existingNames) Object.assign(knownNamesRef.current, existingNames);

  function handleSelectionChange(items: EntityOption[]) {
    for (const item of items) {
      knownNamesRef.current[item.id] = item.name;
    }
    onChange({ ...value, ids: items.map((i) => i.id) });
  }

  const selected: EntityOption[] = (value.ids ?? []).map((id) => ({
    id,
    name: knownNamesRef.current[id] ?? id,
  }));

  // Local query state for Remove mode — we filter the union client-side
  // rather than hitting the server, since the eligible candidates are
  // exactly the items already on the selected entities and we already
  // know all of them.
  const [removeQuery, setRemoveQuery] = useState("");

  const handleSearch = useCallback(
    (q: string) => {
      if (value.mode === GQL.BulkUpdateIdMode.Remove) {
        setRemoveQuery(q);
        return;
      }
      onSearch(q);
    },
    [value.mode, onSearch],
  );

  // Build the dropdown options based on mode:
  //  - Add: server search results, with intersection items hidden
  //    (adding an item already on every entity is a no-op).
  //  - Remove: union items resolved via existingNames, locally filtered
  //    by the typed query — server search isn't useful since only items
  //    already on at least one entity are removable.
  //  - Set: server search results untouched (full replacement, anything
  //    in the catalogue is meaningful).
  const filteredOptions = useMemo(() => {
    if (value.mode === GQL.BulkUpdateIdMode.Add && intersectionIds?.length) {
      const skip = new Set(intersectionIds);
      return options.filter((o) => !skip.has(o.id));
    }
    if (value.mode === GQL.BulkUpdateIdMode.Remove) {
      let items: EntityOption[] = (unionIds ?? []).map((id) => ({
        id,
        name: existingNames?.[id] ?? id,
      }));
      if (removeQuery) {
        const q = removeQuery.toLowerCase();
        items = items.filter((it) => it.name.toLowerCase().includes(q));
      }
      return items;
    }
    return options;
  }, [
    value.mode,
    options,
    intersectionIds,
    unionIds,
    existingNames,
    removeQuery,
  ]);

  return (
    <div className="flex flex-col gap-2">
      <div>
        <ToggleGroup<GQL.BulkUpdateIdMode>
          value={[value.mode]}
          onValueChange={handleToggleChange}
          disabled={disabled}
          variant="outline"
          size="sm"
          aria-label={intl.formatMessage({
            id: "bulk_update_field_mode",
            defaultMessage: "Field update mode",
          })}
        >
          <ToggleGroupItem<GQL.BulkUpdateIdMode>
            value={GQL.BulkUpdateIdMode.Add}
          >
            <PlusIcon />
            {intl.formatMessage({ id: "actions.add", defaultMessage: "Add" })}
          </ToggleGroupItem>
          <ToggleGroupItem<GQL.BulkUpdateIdMode>
            value={GQL.BulkUpdateIdMode.Remove}
          >
            <MinusIcon />
            {intl.formatMessage({
              id: "actions.remove",
              defaultMessage: "Remove",
            })}
          </ToggleGroupItem>
          <ToggleGroupItem<GQL.BulkUpdateIdMode>
            value={GQL.BulkUpdateIdMode.Set}
          >
            <PencilIcon />
            {intl.formatMessage({ id: "actions.set", defaultMessage: "Set" })}
          </ToggleGroupItem>
        </ToggleGroup>
      </div>
      <EntityMultiSelect
        value={selected}
        onChange={handleSelectionChange}
        options={filteredOptions}
        onSearch={handleSearch}
        loading={loading}
        placeholder={placeholder}
        disabled={disabled}
      />
    </div>
  );
}
