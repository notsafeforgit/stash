import type React from "react";

import { Checkbox, type CheckboxProps } from "@/components/ui/checkbox";

interface IndeterminateCheckbox
  extends Omit<CheckboxProps, "checked" | "indeterminate" | "onCheckedChange"> {
  checked: boolean | undefined;
  setChecked: (v: boolean | undefined) => void;
  allowIndeterminate?: boolean;
}

export const IndeterminateCheckbox: React.FC<IndeterminateCheckbox> = ({
  checked,
  setChecked,
  allowIndeterminate,
  ...props
}) => {
  function cycleState() {
    const undefAllowed = allowIndeterminate ?? true;
    if (undefAllowed && checked) {
      return undefined;
    }
    if ((!undefAllowed && checked) || checked === undefined) {
      return false;
    }
    return true;
  }

  return (
    <Checkbox
      checked={checked ?? false}
      indeterminate={checked === undefined}
      onCheckedChange={() => setChecked(cycleState())}
      {...props}
    />
  );
};
