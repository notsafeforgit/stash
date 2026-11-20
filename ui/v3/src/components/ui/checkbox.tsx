import {
  Checkbox as CheckboxPrimitive,
  CheckboxIndicator,
} from "@ui-sdk/react/checkbox";
import type * as React from "react";

import { cn } from "@/lib/utils";

type CheckboxPrimitiveProps = React.ComponentProps<typeof CheckboxPrimitive>;

type CheckboxProps = Omit<
  CheckboxPrimitiveProps,
  "checked" | "onCheckedChange" | "className" | "children"
> & {
  checked?: boolean;
  indeterminate?: boolean;
  onCheckedChange?: (
    checked: boolean,
    details: Parameters<
      NonNullable<CheckboxPrimitiveProps["onCheckedChange"]>
    >[1],
  ) => void;
  className?: string;
};

function Checkbox({
  checked,
  indeterminate,
  onCheckedChange,
  className,
  ...props
}: CheckboxProps) {
  return (
    <CheckboxPrimitive
      checked={indeterminate ? "indeterminate" : checked}
      onCheckedChange={(next, details) =>
        onCheckedChange?.(next === true, details)
      }
      className={cn(
        "relative after:absolute after:-inset-x-3 after:-inset-y-2",
        className,
      )}
      {...props}
    >
      <CheckboxIndicator />
    </CheckboxPrimitive>
  );
}

export { Checkbox, type CheckboxProps };
