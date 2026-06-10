import type React from "react";
import { useIntl } from "react-intl";
import {
  type CriterionValue,
  type ModifierCriterion,
  resolveOption,
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

  const { options, sortOptions } = criterion.modifierCriterionOption();
  const currentValue = (criterion.value as string) ?? "";
  const selectOptions = (options ?? []).map((o) => {
    const { value, messageID } = resolveOption(o);
    return {
      value,
      label: intl.formatMessage({ id: messageID, defaultMessage: value }),
    };
  });
  if (sortOptions) {
    selectOptions.sort((a, b) => a.label.localeCompare(b.label, intl.locale));
  }

  if (renderSelect) {
    return <>{renderSelect(selectOptions, currentValue, onSelect)}</>;
  }

  const currentLabel =
    selectOptions.find((o) => o.value === currentValue)?.label ?? currentValue;
  return (
    <Select
      value={currentValue}
      onValueChange={(v) => v !== null && onSelect(v)}
    >
      <SelectTrigger>
        <SelectValue>{currentLabel}</SelectValue>
      </SelectTrigger>
      <SelectContent visibleItems={7}>
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

  const { options, sortOptions } = criterion.modifierCriterionOption();
  const value = criterion.value as string[];

  const items = (options ?? []).map((o) => {
    const { value: optValue, messageID } = resolveOption(o);
    return {
      value: optValue,
      label: intl.formatMessage({ id: messageID, defaultMessage: optValue }),
    };
  });
  if (sortOptions) {
    items.sort((a, b) => a.label.localeCompare(b.label, intl.locale));
  }

  return (
    <div className="option-list-filter">
      {items.map((o) => {
        const id = `${criterion.getId()}-${o.value}`;
        return (
          <div key={o.value} className="option-list-filter-item">
            <Checkbox
              id={id}
              checked={value.includes(o.value)}
              onCheckedChange={() => onSelect(o.value)}
            />
            <label htmlFor={id}>{o.label}</label>
          </div>
        );
      })}
    </div>
  );
};
