import { Toggle as TogglePrimitive } from "@ui-sdk/react/toggle";
import type * as React from "react";

type ToggleVariant = "default" | "outline";

type ToggleSize = "default" | "sm" | "lg";

type ToggleProps<Value extends string = string> = Omit<
  React.ComponentProps<typeof TogglePrimitive>,
  "value"
> & {
  value?: Value;
  variant?: ToggleVariant;
  size?: ToggleSize;
};

function Toggle<Value extends string = string>({
  className,
  variant = "default",
  size = "default",
  ...props
}: ToggleProps<Value>) {
  return (
    <TogglePrimitive
      data-variant={variant}
      data-size={size}
      className={className}
      {...props}
    />
  );
}

export { Toggle, type ToggleProps, type ToggleSize, type ToggleVariant };
