import type React from "react";
import type {
  ModifierCriterion,
  CriterionValue,
} from "../../../models/list-filter/criteria/criterion";
import { Input } from "src/components/ui/input";

interface PathFilterProps {
  criterion: ModifierCriterion<CriterionValue>;
  onValueChanged: (value: string) => void;
}

export const PathFilter: React.FC<PathFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  return (
    <Input
      type="text"
      onChange={(v) => onValueChanged(v.target.value)}
      value={criterion.value ? criterion.value.toString() : ""}
    />
  );
};
