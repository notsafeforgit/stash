import * as React from "react";
import { ToggleGroup as ToggleGroupPrimitive } from "@ui-sdk/react/toggle";

import { cn } from "@/lib/utils";
import {
  Toggle,
  type ToggleProps,
  type ToggleSize,
  type ToggleVariant,
} from "@/components/ui/toggle";

type ToggleGroupContextValue = {
  variant?: ToggleVariant;
  size?: ToggleSize;
};

const ToggleGroupContext = React.createContext<ToggleGroupContextValue>({});

type ToggleGroupProps<Value extends string = string> = Omit<
  React.ComponentProps<typeof ToggleGroupPrimitive<Value>>,
  "children" | "className"
> &
  ToggleGroupContextValue & {
    children?: React.ReactNode;
    className?: string;
  };

function ToggleGroup<Value extends string = string>({
  className,
  variant,
  size,
  children,
  ...props
}: ToggleGroupProps<Value>) {
  return (
    <ToggleGroupPrimitive<Value>
      data-variant={variant}
      data-size={size}
      className={cn("w-fit", className)}
      {...props}
    >
      <ToggleGroupContext.Provider value={{ variant, size }}>
        {children}
      </ToggleGroupContext.Provider>
    </ToggleGroupPrimitive>
  );
}

function ToggleGroupItem<Value extends string = string>({
  variant,
  size,
  ...props
}: ToggleProps<Value>) {
  const context = React.useContext(ToggleGroupContext);
  return (
    <Toggle<Value>
      variant={context.variant ?? variant}
      size={context.size ?? size}
      {...props}
    />
  );
}

export { ToggleGroup, ToggleGroupItem };
