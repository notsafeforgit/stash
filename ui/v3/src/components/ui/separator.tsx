"use client";

import { Separator as SeparatorPrimitive } from "@ui-sdk/react/separator";
import type * as React from "react";

import { cn } from "@/lib/utils";

type SeparatorProps = Omit<
  React.ComponentProps<typeof SeparatorPrimitive>,
  "className" | "children"
> & {
  className?: string;
};

function Separator({
  className,
  orientation = "horizontal",
  ...props
}: SeparatorProps) {
  return (
    <SeparatorPrimitive
      orientation={orientation}
      className={cn("shrink-0", className)}
      {...props}
    />
  );
}

export { Separator, type SeparatorProps };
