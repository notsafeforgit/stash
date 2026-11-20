import type * as React from "react";
import { createContext, useContext, useMemo } from "react";
import {
  Tooltip as TooltipPrimitive,
  TooltipContent as TooltipContentPrimitive,
  TooltipTrigger,
} from "@ui-sdk/react/tooltip";

import { cn } from "@/lib/utils";
import { useOverlayContainerElement } from "./overlay-container";

type TooltipTiming = { delay?: number; closeDelay?: number };

const TooltipTimingContext = createContext<TooltipTiming>({});

function TooltipProvider({
  delay = 0,
  closeDelay,
  children,
}: TooltipTiming & { children?: React.ReactNode }) {
  const timing = useMemo(() => ({ delay, closeDelay }), [delay, closeDelay]);
  return (
    <TooltipTimingContext.Provider value={timing}>
      {children}
    </TooltipTimingContext.Provider>
  );
}

function Tooltip(props: React.ComponentProps<typeof TooltipPrimitive>) {
  const timing = useContext(TooltipTimingContext);
  return (
    <TooltipPrimitive
      delay={timing.delay}
      closeDelay={timing.closeDelay}
      {...props}
    />
  );
}

// The content keeps pointer-events-none, so hovering a tooltip never holds it open
// and it can't cover whatever sits underneath.
function TooltipContent({
  className,
  side = "top",
  sideOffset = 4,
  align = "center",
  alignOffset = 0,
  container,
  ...props
}: React.ComponentProps<typeof TooltipContentPrimitive>) {
  const overlay = useOverlayContainerElement();
  return (
    <TooltipContentPrimitive
      container={container ?? overlay}
      side={side}
      sideOffset={sideOffset}
      align={align}
      alignOffset={alignOffset}
      className={cn(
        "pointer-events-none select-none inline-flex w-fit max-w-xs items-center gap-1.5 whitespace-pre-wrap [overflow-wrap:anywhere] has-data-[slot=kbd]:pr-1.5 **:data-[slot=kbd]:relative **:data-[slot=kbd]:isolate **:data-[slot=kbd]:z-50 **:data-[slot=kbd]:rounded-sm",
        className,
      )}
      {...props}
    />
  );
}

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider };
