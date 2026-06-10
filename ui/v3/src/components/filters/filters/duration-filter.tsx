import type React from "react";
import { useIntl } from "react-intl";
import { CriterionModifier } from "src/core/generated-graphql";
import { DurationInput } from "src/components/ui/duration-input";
import type { INumberValue } from "src/models/list-filter/types";
import type { ModifierCriterion } from "src/models/list-filter/criteria/criterion";

interface DurationFilterProps {
  criterion: ModifierCriterion<INumberValue>;
  onValueChanged: (value: INumberValue) => void;
}

export const DurationFilter: React.FC<DurationFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  const intl = useIntl();

  function onChanged(v: number | null, property: "value" | "value2") {
    onValueChanged({ ...criterion.value, [property]: v ?? undefined });
  }

  function renderTop() {
    let placeholder: string;
    if (
      criterion.modifier === CriterionModifier.GreaterThan ||
      criterion.modifier === CriterionModifier.Between ||
      criterion.modifier === CriterionModifier.NotBetween
    ) {
      placeholder = intl.formatMessage({ id: "criterion.greater_than" });
    } else if (criterion.modifier === CriterionModifier.LessThan) {
      placeholder = intl.formatMessage({ id: "criterion.less_than" });
    } else {
      placeholder = intl.formatMessage({ id: "criterion.value" });
    }

    return (
      <DurationInput
        value={criterion.value?.value}
        setValue={(v) => onChanged(v, "value")}
        placeholder={placeholder}
      />
    );
  }

  function renderBottom() {
    if (
      criterion.modifier !== CriterionModifier.Between &&
      criterion.modifier !== CriterionModifier.NotBetween
    ) {
      return;
    }

    return (
      <DurationInput
        value={criterion.value?.value2}
        setValue={(v) => onChanged(v, "value2")}
        placeholder={intl.formatMessage({ id: "criterion.less_than" })}
      />
    );
  }

  return (
    <>
      {renderTop()}
      {renderBottom()}
    </>
  );
};
