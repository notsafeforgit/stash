import type React from "react";
import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { CriterionModifier } from "src/core/generated-graphql";
import { ModifierCriterion } from "src/models/list-filter/criteria/criterion";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";

export const ModifierSelect: React.FC<{
  options: CriterionModifier[];
  value: CriterionModifier;
  onChanged: (m: CriterionModifier) => void;
}> = ({ options, value, onChanged }) => {
  const intl = useIntl();
  const selectOptions = useMemo(
    () =>
      options.map((m) => ({
        value: m,
        label: ModifierCriterion.getModifierOptionLabel(intl, m),
      })),
    [intl, options],
  );
  const currentLabel =
    selectOptions.find((o) => o.value === value)?.label ?? value;
  return (
    <Select
      value={value}
      onValueChange={(v) => onChanged(v as CriterionModifier)}
    >
      <SelectTrigger className="w-full">
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
