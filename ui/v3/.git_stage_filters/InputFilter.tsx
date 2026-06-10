import React from "react";
import {
  ModifierCriterion,
  CriterionValue,
} from "../../../models/list-filter/criteria/criterion";
import { Input } from "src/components/ui/input";

interface InputFilterProps {
  criterion: ModifierCriterion<CriterionValue>;
  onValueChanged: (value: string) => void;
}

export const InputFilter: React.FC<InputFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  return (
    <Input
      type={criterion.modifierCriterionOption().inputType}
      onChange={(e) => onValueChanged(e.target.value)}
      value={criterion.value ? criterion.value.toString() : ""}
    />
  );
};
