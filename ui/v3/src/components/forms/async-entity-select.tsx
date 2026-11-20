import { useLabelCache } from "@/hooks/use-label-cache";
import { useRef, useState } from "react";
import {
  Combobox,
  ComboboxChips,
  ComboboxChip,
  ComboboxChipsInput,
  ComboboxClear,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  useComboboxAnchor,
} from "src/components/ui/combobox";

export interface EntityOption {
  id: string;
  name: string;
}

// ── Multi-select ──────────────────────────────────────────────────────────────

export interface EntityMultiSelectProps {
  value: EntityOption[];
  onChange: (items: EntityOption[]) => void;
  /** Search result options */
  options: EntityOption[];
  onSearch: (q: string) => void;
  loading?: boolean;
  placeholder?: string;
  disabled?: boolean;
}

export function EntityMultiSelect({
  value,
  onChange,
  options,
  onSearch,
  loading = false,
  placeholder,
  disabled = false,
}: EntityMultiSelectProps) {
  const anchor = useComboboxAnchor();

  // Accumulate a stable id→name map so chips render correct names even after
  // the options list changes (e.g. user cleared the search input).
  const [knownNames] = useLabelCache(
    [...value, ...options].map((item) => [item.id, item.name]),
  );

  const selectedIds = value.map((v) => v.id);

  function handleValueChange(ids: string[]) {
    const items = ids.map((id) => ({
      id,
      name: knownNames.get(id) ?? id,
    }));
    onChange(items);
  }

  function handleInputValueChange(q: string) {
    onSearch(q);
  }

  function handleOpenChange(open: boolean) {
    if (open) onSearch("");
  }

  return (
    <Combobox<string, true>
      multiple={true}
      filter={false}
      value={selectedIds}
      onValueChange={handleValueChange}
      onInputValueChange={handleInputValueChange}
      onOpenChange={handleOpenChange}
      disabled={disabled}
    >
      <ComboboxChips ref={anchor} className="flex-nowrap items-start">
        <div className="flex flex-1 flex-wrap items-center gap-1 min-w-0">
          {value.map((item) => (
            // Chips are positionally indexed — render in same order as selectedIds
            <ComboboxChip key={item.id} value={item.id}>
              {item.name}
            </ComboboxChip>
          ))}
          <ComboboxChipsInput placeholder={placeholder} />
        </div>
        {value.length > 0 && (
          <div className="shrink-0 self-start">
            <ComboboxClear />
          </div>
        )}
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxList>
          {(() => {
            const filtered = options.filter((o) => !selectedIds.includes(o.id));
            if (filtered.length === 0) {
              return (
                <div className="w-full justify-center py-2 text-center text-sm text-muted-foreground flex">
                  {loading ? "Searching…" : "No results"}
                </div>
              );
            }
            return filtered.map((item) => (
              <ComboboxItem key={item.id} value={item.id}>
                {item.name}
              </ComboboxItem>
            ));
          })()}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

// ── Single-select ─────────────────────────────────────────────────────────────

export interface EntitySingleSelectProps {
  value: EntityOption | null;
  onChange: (item: EntityOption | null) => void;
  options: EntityOption[];
  onSearch: (q: string) => void;
  loading?: boolean;
  placeholder?: string;
  disabled?: boolean;
}

export function EntitySingleSelect({
  value,
  onChange,
  options,
  onSearch,
  loading = false,
  placeholder,
  disabled = false,
}: EntitySingleSelectProps) {
  // inputValue shows the name of the selected entity (or the typed query)
  const [inputValue, setInputValue] = useState(value?.name ?? "");

  // When value changes externally (e.g. form reset), sync the display text.
  // Keyed on id only — we don't want to re-sync every time the name changes.
  // Render-time state adjustment instead of an effect so the id-only keying
  // doesn't need a dependency the effect body never reads.
  const valueId = value?.id;
  const valueName = value?.name;
  const [syncedId, setSyncedId] = useState(valueId);
  if (syncedId !== valueId) {
    setSyncedId(valueId);
    setInputValue(valueName ?? "");
  }

  const [knownNames] = useLabelCache(
    [...(value ? [value] : []), ...options].map((item) => [item.id, item.name]),
  );

  // Choosing an item reports its name through onInputValueChange; skip that
  // one call so the search doesn't rerun for it.
  const justSelectedRef = useRef(false);

  function handleValueChange(id: string | null) {
    if (!id) {
      onChange(null);
      setInputValue("");
      onSearch("");
      return;
    }
    const name = knownNames.get(id) ?? id;
    onChange({ id, name });
    setInputValue(name);
    justSelectedRef.current = true;
  }

  function handleInputValueChange(v: string) {
    if (justSelectedRef.current) {
      justSelectedRef.current = false;
      return;
    }
    setInputValue(v);
    onSearch(v);
  }

  function handleOpenChange(open: boolean) {
    if (open) onSearch("");
  }

  return (
    <Combobox<string>
      value={value?.id ?? null}
      onValueChange={handleValueChange}
      inputValue={inputValue}
      onInputValueChange={handleInputValueChange}
      onOpenChange={handleOpenChange}
      disabled={disabled}
      filter={false}
      // Render a selected id as its known name. Without this the input
      // falls back to the raw id, e.g. when it re-syncs on close.
      itemToStringLabel={(id: string) => knownNames.get(id) ?? id}
    >
      <ComboboxInput placeholder={placeholder} showClear={!!value} />
      <ComboboxContent>
        <ComboboxList>
          {options.length === 0 ? (
            <div className="w-full justify-center py-2 text-center text-sm text-muted-foreground flex">
              {loading ? "Searching…" : "No results"}
            </div>
          ) : (
            options.map((item) => (
              <ComboboxItem key={item.id} value={item.id}>
                {item.name}
              </ComboboxItem>
            ))
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
