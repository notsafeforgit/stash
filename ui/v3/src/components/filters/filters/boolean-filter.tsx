import type React from "react";
import type { BooleanCriterion } from "src/models/list-filter/criteria/criterion";
import { useIntl } from "react-intl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";

interface BooleanFilter {
  criterion: BooleanCriterion;
  setCriterion: (c: BooleanCriterion) => void;
  renderSelect?: (
    options: Array<{ value: string; label: string }>,
    currentValue: string,
    onChange: (value: string) => void,
  ) => React.ReactNode;
}

export const BooleanFilter: React.FC<BooleanFilter> = ({
  criterion,
  setCriterion,
  renderSelect,
}) => {
  const intl = useIntl();

  function onSelect(nextValue: string) {
    const c = criterion.clone() as BooleanCriterion;
    c.value = nextValue;
    setCriterion(c);
  }

  const options = [
    { value: "true", label: intl.formatMessage({ id: "true" }) },
    { value: "false", label: intl.formatMessage({ id: "false" }) },
  ];

  if (renderSelect) {
    return <>{renderSelect(options, criterion.value, onSelect)}</>;
  }

  const currentLabel =
    options.find((o) => o.value === criterion.value)?.label ?? criterion.value;
  return (
    <Select
      value={criterion.value}
      onValueChange={(v) => v !== null && onSelect(v)}
    >
      <SelectTrigger>
        <SelectValue>{currentLabel}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
};
