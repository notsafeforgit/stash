import type React from "react";
import { Checkbox as CheckboxPrimitive } from "@base-ui/react/checkbox";
import { CheckIcon, MinusIcon } from "lucide-react";
import { cn } from "src/lib/utils";

interface IndeterminateCheckbox
  extends Omit<CheckboxPrimitive.Root.Props, "checked" | "onCheckedChange"> {
  checked: boolean | undefined;
  setChecked: (v: boolean | undefined) => void;
  allowIndeterminate?: boolean;
}

export const IndeterminateCheckbox: React.FC<IndeterminateCheckbox> = ({
  checked,
  setChecked,
  allowIndeterminate,
  className,
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
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      checked={checked ?? false}
      indeterminate={checked === undefined}
      onCheckedChange={() => setChecked(cycleState())}
      className={cn(
        "peer relative flex size-4 shrink-0 items-center justify-center rounded-[4px] border border-input transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 data-checked:border-primary data-checked:bg-primary data-checked:text-primary-foreground data-indeterminate:border-primary data-indeterminate:bg-primary data-indeterminate:text-primary-foreground dark:bg-input/30 dark:data-checked:bg-primary",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current transition-none [&>svg]:size-3.5"
      >
        {checked === undefined ? <MinusIcon /> : <CheckIcon />}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
};
