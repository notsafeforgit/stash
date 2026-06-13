import type React from "react";
import { useEffect, useRef, useState } from "react";
import { Minus, Plus } from "lucide-react";
import { useIntl } from "react-intl";
import { Switch } from "src/components/ui/switch";
import { Label } from "src/components/ui/label";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";
import type {
  IHierarchicalLabelValue,
  ILabeledValueListValue,
} from "src/models/list-filter/types";
import {
  ModifierCriterion,
  type IHierarchicalLabeledIdCriterion,
} from "src/models/list-filter/criteria/criterion";
import type { MessageDescriptor } from "react-intl";
import { CriterionModifier } from "src/core/generated-graphql";
import { useDebounce } from "src/hooks/debounce";
import {
  Combobox,
  ComboboxChips,
  ComboboxChip,
  ComboboxClear,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  useComboboxAnchor,
} from "src/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import type { ILabeledId } from "src/models/list-filter/types";

interface MultiSelectFilterProps<
  T extends ModifierCriterion<ILabeledValueListValue | IHierarchicalLabelValue>,
> {
  criterion: T;
  setCriterion: (criterion: T) => void;
  useResults: (query: string) => { results: ILabeledId[]; loading: boolean };
  singleValue?: boolean;
}

export const MultiSelectFilter = <
  T extends ModifierCriterion<ILabeledValueListValue | IHierarchicalLabelValue>,
>({
  criterion,
  setCriterion,
  useResults,
}: MultiSelectFilterProps<T>) => {
  const intl = useIntl();
  const anchor = useComboboxAnchor();
  const [comboboxOpen, setComboboxOpen] = useState(false);
  const [inputValue, setInputValue] = useState("");
  const [serverQuery, setServerQuery] = useState("");
  const debouncedSetServerQuery = useDebounce(setServerQuery, 100);
  const { results } = useResults(serverQuery);

  // Preserve labels for items that may leave query results
  const labelMapRef = useRef(new Map<string, string>());

  useEffect(() => {
    results.forEach((item) => {
      labelMapRef.current.set(item.id, item.label);
    });
  }, [results]);

  useEffect(() => {
    criterion.value.items.forEach((item) => {
      labelMapRef.current.set(item.id, item.label);
    });
  }, [criterion.value.items]);

  const modifierOptions = criterion.modifierCriterionOption().modifierOptions;
  const modifierSelectItems = modifierOptions.map((m) => ({
    value: m,
    label: ModifierCriterion.getModifierOptionLabel(intl, m),
  }));

  function onModifierChange(m: CriterionModifier) {
    const newCriterion = criterion.clone() as T;
    newCriterion.modifier = m;
    setCriterion(newCriterion);
  }

  const isNullOrNotNull =
    criterion.modifier === CriterionModifier.IsNull ||
    criterion.modifier === CriterionModifier.NotNull;

  const selectedIds = criterion.value.items.map((i) => i.id);

  function onValueChange(newIds: string[]) {
    const newCriterion = criterion.clone() as T;
    newCriterion.value = {
      ...newCriterion.value,
      items: newIds.map((id) => ({
        id,
        label: labelMapRef.current.get(id) ?? id,
      })),
    };
    setCriterion(newCriterion);
    debouncedSetServerQuery.cancel();
    setInputValue("");
    setServerQuery("");
  }

  function onInputKeyDownCapture(e: React.KeyboardEvent<HTMLInputElement>) {
    if (
      e.key !== "Escape" ||
      e.ctrlKey ||
      e.metaKey ||
      e.altKey ||
      e.shiftKey
    ) {
      return;
    }

    e.preventDefault();
    e.stopPropagation();
    e.nativeEvent.stopImmediatePropagation();
    debouncedSetServerQuery.cancel();
    setInputValue("");
    setServerQuery("");
    setComboboxOpen(false);
    e.currentTarget.blur();
  }

  return (
    <div className="flex flex-col gap-2">
      {modifierOptions.length > 1 && (
        <Select
          value={criterion.modifier}
          onValueChange={(v) => onModifierChange(v as CriterionModifier)}
          items={modifierSelectItems}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {modifierSelectItems.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {!isNullOrNotNull && (
        <Combobox<string, true>
          multiple
          open={comboboxOpen}
          onOpenChange={setComboboxOpen}
          inputValue={inputValue}
          autoHighlight={inputValue.length > 0}
          value={selectedIds}
          onValueChange={onValueChange}
          onInputValueChange={(v: string) => {
            setInputValue(v);
            debouncedSetServerQuery(v);
          }}
          itemToStringLabel={(id: string | null) =>
            labelMapRef.current.get(id ?? "") ?? id ?? ""
          }
        >
          <ComboboxChips ref={anchor}>
            {criterion.value.items.map((item) => (
              <ComboboxChip key={item.id}>
                {item.label || labelMapRef.current.get(item.id) || item.id}
              </ComboboxChip>
            ))}
            <ComboboxChipsInput
              placeholder={`${intl.formatMessage({ id: "actions.search" })}…`}
              aria-invalid={selectedIds.length === 0 ? "true" : undefined}
              onKeyDownCapture={onInputKeyDownCapture}
            />
            {selectedIds.length > 0 && <ComboboxClear />}
          </ComboboxChips>
          <ComboboxContent anchor={anchor}>
            {inputValue && results.length === 0 && (
              <div className="w-full py-2 text-center text-sm text-muted-foreground">
                {intl.formatMessage({ id: "filter_no_results" })}
              </div>
            )}
            <ComboboxList>
              {results.map((item) => (
                <ComboboxItem key={item.id} value={item.id}>
                  {item.label}
                </ComboboxItem>
              ))}
            </ComboboxList>
          </ComboboxContent>
        </Combobox>
      )}
    </div>
  );
};

export interface ObjectsFilterProps<
  T extends ModifierCriterion<ILabeledValueListValue>,
> {
  criterion: T;
  setCriterion: (criterion: T) => void;
  useResults: (query: string) => { results: ILabeledId[]; loading: boolean };
  singleValue?: boolean;
}

const DepthInput: React.FC<{
  id?: string;
  value: number; // -1 = unlimited, N ≥ 1 = N levels
  onChange: (v: number) => void;
}> = ({ id, value, onChange }) => {
  const unlimited = value < 1;
  const [str, setStr] = useState(unlimited ? "∞" : String(value));
  const strRef = useRef(str);
  strRef.current = str;

  useEffect(() => {
    const next = value < 1 ? "∞" : String(value);
    if (strRef.current !== next) setStr(next);
  }, [value]);

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    const raw = e.target.value;
    if (raw !== "" && !/^\d+$/.test(raw)) return;
    setStr(raw);
    const n = parseInt(raw, 10);
    if (!Number.isNaN(n) && n > 0) onChange(n);
  }

  function handleBlur() {
    const n = parseInt(str, 10);
    if (!Number.isNaN(n) && n > 0) {
      onChange(n);
      setStr(String(n));
    } else {
      onChange(-1);
      setStr("∞");
    }
  }

  return (
    <div className="flex gap-1">
      <Input
        id={id}
        type="text"
        inputMode="numeric"
        value={str}
        onChange={handleChange}
        onBlur={handleBlur}
        onFocus={(e) => e.target.select()}
      />
      <Button
        type="button"
        variant="outline"
        size="icon"
        tabIndex={-1}
        disabled={unlimited}
        onClick={() => onChange(value === 1 ? -1 : value - 1)}
      >
        <Minus size={14} />
      </Button>
      <Button
        type="button"
        variant="outline"
        size="icon"
        tabIndex={-1}
        onClick={() => onChange(unlimited ? 1 : value + 1)}
      >
        <Plus size={14} />
      </Button>
    </div>
  );
};

export const DepthSelector: React.FC<{
  depth: number | undefined;
  onDepthChanged: (depth: number) => void;
  id: string;
  label?: React.ReactNode;
  disabled?: boolean;
}> = ({ depth, onDepthChanged, id, label, disabled }) => {
  const intl = useIntl();
  const checked = depth !== 0;
  const depthInputId = `${id}-depth`;
  return (
    <div className="flex flex-col gap-2">
      {/* `pl-2.5` matches the inset of the surrounding select / input
          controls' text so the toggle and "Levels" label visually align
          with the dropdown labels above and below them in the card. */}
      <div className="flex items-center gap-2 pl-2.5">
        <Switch
          id={id}
          checked={checked}
          onCheckedChange={(v) => onDepthChanged(v ? -1 : 0)}
          disabled={disabled}
        />
        <Label htmlFor={id}>{label}</Label>
      </div>
      {checked && (
        <div className="flex flex-col gap-1">
          <Label htmlFor={depthInputId} className="pl-2.5">
            {intl.formatMessage({ id: "depth_levels" })}
          </Label>
          <DepthInput
            id={depthInputId}
            value={depth !== undefined && depth > 0 ? depth : -1}
            onChange={onDepthChanged}
          />
        </div>
      )}
    </div>
  );
};

type HierarchicalObjectsFilterProps<T extends IHierarchicalLabeledIdCriterion> =
  {
    criterion: T;
    setCriterion: (criterion: T) => void;
    useResults: (query: string) => { results: ILabeledId[]; loading: boolean };
    singleValue?: boolean;
  };

export const HierarchicalObjectsFilter = <
  T extends IHierarchicalLabeledIdCriterion,
>(
  props: HierarchicalObjectsFilterProps<T>,
) => {
  const intl = useIntl();
  const { criterion, setCriterion } = props;

  const isNullOrNotNull =
    criterion.modifier === CriterionModifier.IsNull ||
    criterion.modifier === CriterionModifier.NotNull;

  function onDepthChanged(depth: number) {
    const newCriterion: T = criterion.clone() as T;
    newCriterion.value.depth = depth;
    setCriterion(newCriterion);
  }

  function criterionOptionTypeToIncludeID(): string {
    if (criterion.criterionOption.type === "studios") {
      return "include-sub-studios";
    }
    if (criterion.criterionOption.type === "children") {
      return "include-parent-tags";
    }
    return "include-sub-tags";
  }

  function criterionOptionTypeToIncludeUIString(): MessageDescriptor {
    const optionType =
      criterion.criterionOption.type === "studios"
        ? "include_sub_studios"
        : criterion.criterionOption.type === "children"
          ? "include_parent_tags"
          : "include_sub_tags";
    return {
      id: optionType,
    };
  }

  return (
    <div className="hierarchical-objects-filter flex flex-col gap-2">
      {!isNullOrNotNull && (
        <DepthSelector
          depth={criterion.value.depth}
          onDepthChanged={onDepthChanged}
          id={criterionOptionTypeToIncludeID()}
          label={intl.formatMessage(criterionOptionTypeToIncludeUIString())}
        />
      )}
      <MultiSelectFilter {...props} />
    </div>
  );
};
