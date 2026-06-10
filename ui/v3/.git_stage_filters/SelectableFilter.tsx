import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { CircleCheck, CircleX, Minus, Plus } from "lucide-react";
import { ClearableInput } from "src/components/ui/clearable-input";
import {
  IHierarchicalLabelValue,
  ILabeledId,
  ILabeledValueListValue,
} from "src/models/list-filter/types";
import {
  ModifierCriterion,
  IHierarchicalLabeledIdCriterion,
} from "src/models/list-filter/criteria/criterion";
import {
  defineMessages,
  FormattedMessage,
  MessageDescriptor,
  useIntl,
} from "react-intl";
import { CriterionModifier } from "src/core/generated-graphql";
import { keyboardClickHandler } from "src/utils/keyboard";
import { useDebounce } from "src/hooks/debounce";
import useFocus from "src/utils/focus";
import { cn } from "src/lib/utils";
import ScreenUtils from "src/utils/screen";

interface SelectedItemProps {
  label: string;
  excluded?: boolean;
  onClick: () => void;
  // true if the object is a special modifier value
  modifier?: boolean;
}

const SelectedItem: React.FC<SelectedItemProps> = ({
  label,
  excluded = false,
  onClick,
  modifier = false,
}) => {
  const iconClassName = excluded ? "exclude-icon" : "include-button";
  const spanClassName = excluded
    ? "excluded-object-label"
    : "selected-object-label";
  const [hovered, setHovered] = useState(false);

  function onMouseOver() {
    setHovered(true);
  }

  function onMouseOut() {
    setHovered(false);
  }

  const IconComponent = hovered || excluded ? CircleX : CircleCheck;

  return (
    <li className={cn("selected-object", { "modifier-object": modifier })}>
      <a
        onClick={() => onClick()}
        onKeyDown={keyboardClickHandler(onClick)}
        onMouseEnter={() => onMouseOver()}
        onMouseLeave={() => onMouseOut()}
        onFocus={() => onMouseOver()}
        onBlur={() => onMouseOut()}
        tabIndex={0}
      >
        <div>
          <IconComponent className={`icon fa-fw ${iconClassName}`} size={16} />
          <span className={spanClassName}>{label}</span>
        </div>
        <div></div>
      </a>
    </li>
  );
};

const UnselectedItem: React.FC<{
  onSelect: (exclude: boolean) => void;
  label: string;
  canExclude: boolean;
  // true if the object is a special modifier value
  modifier?: boolean;
}> = ({ onSelect, label, canExclude, modifier = false }) => {
  const includeIcon = <Plus className="icon fa-fw include-button" size={16} />;
  const excludeIcon = <Minus className="icon fa-fw exclude-icon" size={16} />;

  return (
    <li className={cn("unselected-object", { "modifier-object": modifier })}>
      <a
        onClick={() => onSelect(false)}
        onKeyDown={keyboardClickHandler(() => onSelect(false))}
        tabIndex={0}
      >
        <div>
          {includeIcon}
          <span className="unselected-object-label">{label}</span>
        </div>
        <div>
          {/* TODO item count */}
          {/* <span className="object-count">{p.id}</span> */}
          {canExclude && (
            <Button
              variant="ghost"
              onClick={(e) => {
                e.stopPropagation();
                onSelect(true);
              }}
              onKeyDown={(e) => e.stopPropagation()}
              className="minimal exclude-button"
            >
              <span className="exclude-button-text">
                <FormattedMessage id="actions.exclude_lowercase" />
              </span>
              {excludeIcon}
            </Button>
          )}
        </div>
      </a>
    </li>
  );
};

export interface SelectableFilterProps {
  query: string;
  onQueryChange: (query: string) => void;
  modifier: CriterionModifier;
  showModifierValues: boolean;
  inputFocus: ReturnType<typeof useFocus>;
  canExclude: boolean;
  queryResults: ILabeledId[];
  selected: ILabeledId[];
  excluded: ILabeledId[];
  onSelect: (value: ILabeledId, exclude: boolean) => void;
  onUnselect: (value: ILabeledId) => void;
  onSetModifier: (modifier: CriterionModifier) => void;
  // true if the filter is for a single value
  singleValue?: boolean;
}

type SpecialValue = "any" | "none" | "any_of" | "only";

function modifierValueToModifier(key: SpecialValue): CriterionModifier {
  switch (key) {
    case "any":
      return CriterionModifier.NotNull;
    case "none":
      return CriterionModifier.IsNull;
    case "any_of":
      return CriterionModifier.Includes;
    case "only":
      return CriterionModifier.Equals;
  }
}

export const SelectableFilter: React.FC<SelectableFilterProps> = ({
  query,
  onQueryChange,
  modifier,
  showModifierValues,
  inputFocus,
  canExclude,
  queryResults,
  selected,
  excluded,
  onSelect,
  onUnselect,
  onSetModifier,
  singleValue,
}) => {
  const intl = useIntl();
  const objects = useMemo(() => {
    if (
      modifier === CriterionModifier.IsNull ||
      modifier === CriterionModifier.NotNull
    ) {
      return [];
    }
    return queryResults.filter(
      (p) =>
        selected.find((s) => s.id === p.id) === undefined &&
        excluded.find((s) => s.id === p.id) === undefined,
    );
  }, [modifier, queryResults, selected, excluded]);

  const includingOnly = modifier == CriterionModifier.Equals;
  const excludingOnly =
    modifier == CriterionModifier.Excludes ||
    modifier == CriterionModifier.NotEquals;

  const modifierValues = useMemo(() => {
    return {
      any: modifier === CriterionModifier.NotNull,
      none: modifier === CriterionModifier.IsNull,
      any_of: !singleValue && modifier === CriterionModifier.Includes,
      only: !singleValue && modifier === CriterionModifier.Equals,
    };
  }, [modifier, singleValue]);

  const defaultModifier = useMemo(() => {
    if (singleValue) {
      return CriterionModifier.Includes;
    }
    return CriterionModifier.IncludesAll;
  }, [singleValue]);

  const availableModifierValues: Record<SpecialValue, boolean> = useMemo(() => {
    return {
      any:
        modifier === defaultModifier &&
        selected.length === 0 &&
        excluded.length === 0,
      none:
        modifier === defaultModifier &&
        selected.length === 0 &&
        excluded.length === 0,
      any_of:
        !singleValue && modifier === defaultModifier && selected.length > 1,
      only:
        !singleValue &&
        modifier === defaultModifier &&
        selected.length > 0 &&
        excluded.length === 0,
    };
  }, [singleValue, defaultModifier, modifier, selected, excluded]);

  function onModifierValueSelect(key: SpecialValue) {
    const m = modifierValueToModifier(key);
    onSetModifier(m);
  }

  function onModifierValueUnselect() {
    onSetModifier(defaultModifier);
  }

  function onEnter() {
    if (objects.length === 1) {
      onSelect(objects[0], false);
    }
  }

  return (
    <div className="selectable-filter">
      <ClearableInput
        focus={inputFocus}
        value={query}
        setValue={(v) => onQueryChange(v)}
        onEnter={onEnter}
        placeholder={`${intl.formatMessage({ id: "actions.search" })}…`}
      />
      <ul>
        {Object.entries(modifierValues).map(([key, value]) => {
          if (!value) {
            return null;
          }

          return (
            <SelectedItem
              key={key}
              onClick={() => onModifierValueUnselect()}
              label={`(${intl.formatMessage({
                id: `criterion_modifier_values.${key}`,
              })})`}
              modifier
            />
          );
        })}
        {selected.map((p) => (
          <SelectedItem
            key={p.id}
            label={p.label}
            excluded={excludingOnly}
            onClick={() => onUnselect(p)}
          />
        ))}
        {excluded.map((p) => (
          <li key={p.id} className="excluded-object">
            <SelectedItem
              label={p.label}
              excluded
              onClick={() => onUnselect(p)}
            />
          </li>
        ))}
        {showModifierValues && (
          <>
            {Object.entries(availableModifierValues).map(([key, value]) => {
              if (!value) {
                return null;
              }

              return (
                <UnselectedItem
                  key={key}
                  onSelect={() => onModifierValueSelect(key as SpecialValue)}
                  label={`(${intl.formatMessage({
                    id: `criterion_modifier_values.${key}`,
                  })})`}
                  canExclude={false}
                  modifier
                />
              );
            })}
          </>
        )}
        {objects.map((p) => (
          <UnselectedItem
            key={p.id}
            onSelect={(exclude) => onSelect(p, exclude)}
            label={p.label}
            canExclude={canExclude && !includingOnly && !excludingOnly}
          />
        ))}
      </ul>
    </div>
  );
};

interface ObjectsFilterProps<
  T extends ModifierCriterion<ILabeledValueListValue>,
> {
  criterion: T;
  setCriterion: (criterion: T) => void;
  useResults: (query: string) => { results: ILabeledId[]; loading: boolean };
  singleValue?: boolean;
}

export const ObjectsFilter = <
  T extends ModifierCriterion<ILabeledValueListValue | IHierarchicalLabelValue>,
>({
  criterion,
  setCriterion,
  useResults,
  singleValue,
}: ObjectsFilterProps<T>) => {
  const [query, setQuery] = useState("");
  const [displayQuery, setDisplayQuery] = useState(query);

  const debouncedSetQuery = useDebounce(setQuery, 250);
  const onQueryChange = useCallback(
    (input: string) => {
      setDisplayQuery(input);
      debouncedSetQuery(input);
    },
    [debouncedSetQuery, setDisplayQuery],
  );

  const [queryResults, setQueryResults] = useState<ILabeledId[]>([]);
  const { results, loading: resultsLoading } = useResults(query);
  useEffect(() => {
    if (!resultsLoading) {
      setQueryResults(results);
    }
  }, [results, resultsLoading]);

  const inputFocus = useFocus();
  const [, setInputFocus] = inputFocus;

  function onSelect(value: ILabeledId, newExclude: boolean) {
    const newCriterion: T = criterion.clone() as T;

    if (newExclude) {
      if (newCriterion.value.excluded) {
        newCriterion.value.excluded.push(value);
      } else {
        newCriterion.value.excluded = [value];
      }
    } else {
      newCriterion.value.items.push(value);
    }

    setCriterion(newCriterion);

    // reset filter query after selecting
    debouncedSetQuery.cancel();
    setQuery("");
    setDisplayQuery("");

    // focus the input box
    // don't do this on touch devices, as it's annoying
    if (!ScreenUtils.isTouch()) {
      setInputFocus();
    }
  }

  const onUnselect = useCallback(
    (value: ILabeledId) => {
      if (!criterion) return;

      const newCriterion: T = criterion.clone() as T;

      newCriterion.value.items = criterion.value.items.filter(
        (v) => v.id !== value.id,
      );
      newCriterion.value.excluded = criterion.value.excluded.filter(
        (v) => v.id !== value.id,
      );

      setCriterion(newCriterion);

      // focus the input box
      setInputFocus();
    },
    [criterion, setCriterion, setInputFocus],
  );

  const onSetModifier = useCallback(
    (modifier: CriterionModifier) => {
      const newCriterion: T = criterion.clone();
      newCriterion.modifier = modifier;
      setCriterion(newCriterion);
    },
    [criterion, setCriterion],
  );

  const sortedSelected = useMemo(() => {
    const ret = criterion.value.items.slice();
    ret.sort((a, b) => a.label.localeCompare(b.label));
    return ret;
  }, [criterion]);

  const sortedExcluded = useMemo(() => {
    if (!criterion.value.excluded) return [];
    const ret = criterion.value.excluded.slice();
    ret.sort((a, b) => a.label.localeCompare(b.label));
    return ret;
  }, [criterion]);

  // if excludes is not a valid modifierOption then we can use `value.excluded`
  const canExclude =
    criterion
      .modifierCriterionOption()
      .modifierOptions.find((m) => m === CriterionModifier.Excludes) ===
    undefined;

  return (
    <SelectableFilter
      query={displayQuery}
      onQueryChange={onQueryChange}
      modifier={criterion.modifier}
      showModifierValues={!query}
      inputFocus={inputFocus}
      canExclude={canExclude}
      selected={sortedSelected}
      queryResults={queryResults}
      onSelect={onSelect}
      onUnselect={onUnselect}
      excluded={sortedExcluded}
      onSetModifier={onSetModifier}
      singleValue={singleValue}
    />
  );
};

export const DepthSelector: React.FC<{
  depth: number | undefined;
  onDepthChanged: (depth: number) => void;
  id: string;
  label?: React.ReactNode;
  placeholder?: string;
  disabled?: boolean;
}> = ({ depth, onDepthChanged, id, label, disabled, placeholder }) => {
  return (
    <div className="hierarchical-depth-selector">
      <div className="hierarchical-toggle-switch">
        <button
          id={id}
          type="button"
          role="switch"
          aria-checked={depth !== 0}
          className={
            depth !== 0
              ? "hierarchical-toggle-switch-button active"
              : "hierarchical-toggle-switch-button"
          }
          onClick={() => onDepthChanged(depth !== 0 ? 0 : -1)}
          disabled={disabled}
        >
          <span className="hierarchical-toggle-switch-knob" />
        </button>
        <label className="hierarchical-toggle-switch-label" htmlFor={id}>
          {label}
        </label>
      </div>
      <div
        className={
          depth !== 0
            ? "hierarchical-depth-levels"
            : "hierarchical-depth-levels hierarchical-depth-levels-hidden"
        }
        aria-hidden={depth === 0}
      >
        <Input
          type="text"
          inputMode="numeric"
          className="btn-secondary"
          placeholder={placeholder}
          value={depth !== undefined && depth !== -1 ? String(depth) : ""}
          onChange={(e) => {
            const raw = e.target.value.replace(/[^0-9]/g, "");
            onDepthChanged(raw === "" ? -1 : Math.max(1, parseInt(raw, 10)));
          }}
          disabled={depth === 0 || disabled}
        />
      </div>
    </div>
  );
};

type HierarchicalObjectsFilterProps<T extends IHierarchicalLabeledIdCriterion> =
  ObjectsFilterProps<T>;

export const HierarchicalObjectsFilter = <
  T extends IHierarchicalLabeledIdCriterion,
>(
  props: HierarchicalObjectsFilterProps<T>,
) => {
  const intl = useIntl();
  const { criterion, setCriterion } = props;

  const messages = defineMessages({
    studio_depth: {
      id: "studio_depth",
      defaultMessage: "Levels (empty for all)",
    },
  });

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
    <div className="hierarchical-objects-filter">
      <DepthSelector
        depth={criterion.value.depth}
        onDepthChanged={onDepthChanged}
        id={criterionOptionTypeToIncludeID()}
        label={intl.formatMessage(criterionOptionTypeToIncludeUIString())}
        placeholder={intl.formatMessage(messages.studio_depth)}
      />
      <ObjectsFilter {...props} />
    </div>
  );
};
