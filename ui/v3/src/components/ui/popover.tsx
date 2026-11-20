"use client";

import type * as React from "react";
import {
  Popover as PopoverPrimitive,
  PopoverContent as PopoverContentPrimitive,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from "@ui-sdk/react/popover";

import { cn } from "@/lib/utils";
import {
  type ShortcutOverlayRootProps,
  useOverlayOpenState,
} from "@/components/shortcut-provider";
import {
  type OpenChangeArgs,
  type OverlayFocus,
  overlayFocus,
  useOverlayContainerElement,
} from "./overlay-container";

type PopoverRootProps = React.ComponentProps<typeof PopoverPrimitive> &
  ShortcutOverlayRootProps;

function Popover({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  ...props
}: PopoverRootProps) {
  const container = useOverlayContainerElement();
  const overlayProps = useOverlayOpenState<OpenChangeArgs<PopoverRootProps>>({
    open,
    defaultOpen,
    onOpenChange,
    blocksListShortcuts,
  });

  return (
    <PopoverPrimitive container={container} {...overlayProps} {...props} />
  );
}

type PopoverContentProps = Omit<
  React.ComponentProps<typeof PopoverContentPrimitive>,
  "initialFocus" | "finalFocus"
> & {
  positionerClassName?: string;
  initialFocus?: OverlayFocus;
  finalFocus?: OverlayFocus;
};

function PopoverContent({
  className,
  positionerClassName,
  align = "center",
  alignOffset = 0,
  side = "bottom",
  sideOffset = 4,
  anchor,
  initialFocus,
  finalFocus,
  ...props
}: PopoverContentProps) {
  return (
    <PopoverContentPrimitive
      align={align}
      alignOffset={alignOffset}
      side={side}
      sideOffset={sideOffset}
      anchor={anchor}
      initialFocus={overlayFocus(initialFocus)}
      finalFocus={overlayFocus(finalFocus)}
      className={cn("w-72", positionerClassName, className)}
      {...props}
    />
  );
}

function PopoverHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="popover-header"
      className={cn("flex flex-col gap-0.5 text-sm", className)}
      {...props}
    />
  );
}

export {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
};
