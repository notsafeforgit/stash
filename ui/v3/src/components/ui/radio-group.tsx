import {
  Radio,
  RadioGroup as RadioGroupPrimitive,
  RadioIndicator,
} from "@ui-sdk/react/radio";
import type * as React from "react";

import { cn } from "@/lib/utils";

type RadioGroupProps = Omit<
  React.ComponentProps<typeof RadioGroupPrimitive>,
  "className" | "children"
> & {
  className?: string;
  children?: React.ReactNode;
};

function RadioGroup({ className, ...props }: RadioGroupProps) {
  return <RadioGroupPrimitive className={cn("w-full", className)} {...props} />;
}

type RadioGroupItemProps = Omit<
  React.ComponentProps<typeof Radio>,
  "className" | "children"
> & {
  className?: string;
};

function RadioGroupItem({ className, ...props }: RadioGroupItemProps) {
  return (
    <Radio
      className={cn(
        "relative after:absolute after:-inset-x-3 after:-inset-y-2",
        className,
      )}
      {...props}
    >
      <RadioIndicator />
    </Radio>
  );
}

export { RadioGroup, RadioGroupItem };
