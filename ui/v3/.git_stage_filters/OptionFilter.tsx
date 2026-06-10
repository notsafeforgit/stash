import React from "react";
import { useIntl } from "react-intl";
import {
  CriterionValue,
  ModifierCriterion,
} from "src/models/list-filter/criteria/criterion";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import { Checkbox } from "src/components/ui/checkbox";

interface OptionsFilterProps {
  criterion: ModifierCriterion<CriterionValue>;
  setCriterion: (c: ModifierCriterion<CriterionValue>) => void;
  renderSelect?: (
    options: Array<{ value: string; label: string }>,
    currentValue: string,
    onChange: (value: string) => void,
  ) => React.ReactNode;
}

export const OptionFilter: React.FC<OptionsFilterProps> = ({
  criterion,
  setCriterion,
  renderSelect,
}) => {
  const intl = useIntl();

  function onSelect(v: string) {
    const c = criterion.clone() as ModifierCriterion<CriterionValue>;
    c.value = v;
    setCriterion(c);
  }

  const { options } = criterion.modifierCriterionOption();
  const currentValue = (criterion.value as string) ?? "";
  const selectOptions = (options ?? []).map((o) => ({
    value: o.toString(),
    label: intl.formatMessage({
      id: o.toString(),
      defaultMessage: o.toString(),
    }),
  }));

  if (renderSelect) {
    return <>{renderSelect(selectOptions, currentValue, onSelect)}</>;
  }

  const currentLabel =
    selectOptions.find((o) => o.value === currentValue)?.label ?? currentValue;
  return (
    <Select value={currentValue} onValueChange={onSelect}>
      <SelectTrigger>
        <SelectValue>{currentLabel}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {selectOptions.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
};

interface OptionsListFilterProps {
  criterion: ModifierCriterion<CriterionValue>;
  setCriterion: (c: ModifierCriterion<CriterionValue>) => void;
}

export const OptionListFilter: React.FC<OptionsListFilterProps> = ({
  criterion,
  setCriterion,
}) => {
  const intl = useIntl();

  function onSelect(v: string) {
    const c = criterion.clone() as ModifierCriterion<CriterionValue>;
    const cv = c.value as string[];
    if (cv.includes(v)) {
      c.value = cv.filter((x) => x !== v);
    } else {
      c.value = [...cv, v];
    }

    setCriterion(c);
  }

  const { options } = criterion.modifierCriterionOption();
  const value = criterion.value as string[];

  return (
    <div className="option-list-filter">
      {options?.map((o) => (
        <div key={o.toString()} className="option-list-filter-item">
          <Checkbox
            id={`${criterion.getId()}-${o.toString()}`}
            checked={value.includes(o.toString())}
            onCheckedChange={() => onSelect(o.toString())}
          />
          <label htmlFor={`${criterion.getId()}-${o.toString()}`}>
            {intl.formatMessage({
              id: o.toString(),
              defaultMessage: o.toString(),
            })}
          </label>
        </div>
      ))}
    </div>
  );
};
