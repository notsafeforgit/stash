import type * as React from "react";
import { Input as InputPrimitive } from "@ui-sdk/react/input";

type InputProps = Omit<
  React.ComponentProps<typeof InputPrimitive>,
  "value" | "defaultValue"
> & {
  value?: string | number;
  defaultValue?: string | number;
};

function Input({ value, defaultValue, ...props }: InputProps) {
  return (
    <InputPrimitive
      value={value === undefined ? undefined : String(value)}
      defaultValue={
        defaultValue === undefined ? undefined : String(defaultValue)
      }
      {...props}
    />
  );
}

export { Input };
