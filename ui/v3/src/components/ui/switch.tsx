"use client";

import { Switch as SwitchPrimitive, SwitchThumb } from "@ui-sdk/react/switch";
import type * as React from "react";

import { cn } from "@/lib/utils";

type SwitchProps = Omit<
  React.ComponentProps<typeof SwitchPrimitive>,
  "className" | "children"
> & {
  className?: string;
  size?: "sm" | "default";
};

function Switch({ className, size = "default", ...props }: SwitchProps) {
  return (
    <SwitchPrimitive
      data-size={size}
      className={cn(
        "relative after:absolute after:-inset-x-3 after:-inset-y-2",
        className,
      )}
      {...props}
    >
      <SwitchThumb />
    </SwitchPrimitive>
  );
}

export { Switch, type SwitchProps };
