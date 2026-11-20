import type * as React from "react";
import {
  Drawer as DrawerPrimitive,
  DrawerBackdrop as DrawerBackdropPrimitive,
  DrawerClose,
  DrawerContent as DrawerContentPrimitive,
  DrawerDescription,
  DrawerTitle,
  DrawerTrigger,
} from "@ui-sdk/react/drawer";

import { cn } from "@/lib/utils";
import {
  type ShortcutOverlayRootProps,
  useOverlayOpenState,
} from "@/components/shortcut-provider";
import {
  type OpenChangeArgs,
  useOverlayContainerElement,
} from "./overlay-container";

export type DrawerBackdrop = "blurred" | "dimmed";

type DrawerRootProps = React.ComponentProps<typeof DrawerPrimitive> &
  ShortcutOverlayRootProps;

// Pointer dragging keeps the swipe model these sheets were tuned with, where
// scrollers opt out with data-drawer-no-drag. `auto` would switch touch bottom
// sheets to native scroll-snap, which needs device validation first.
function Drawer({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  driver = "pointer",
  ...props
}: DrawerRootProps) {
  const container = useOverlayContainerElement();
  const overlayProps = useOverlayOpenState<OpenChangeArgs<DrawerRootProps>>({
    open,
    defaultOpen,
    onOpenChange,
    blocksListShortcuts,
  });

  return (
    <DrawerPrimitive
      container={container}
      driver={driver}
      {...overlayProps}
      {...props}
    />
  );
}

function DrawerOverlay({
  className,
  appearance = "blurred",
  ...props
}: React.ComponentProps<typeof DrawerBackdropPrimitive> & {
  appearance?: DrawerBackdrop;
}) {
  return (
    <DrawerBackdropPrimitive
      data-appearance={appearance}
      className={className}
      {...props}
    />
  );
}

function DrawerContent({
  className,
  children,
  backdrop = "blurred",
  ...props
}: Omit<React.ComponentProps<typeof DrawerContentPrimitive>, "children"> & {
  children?: React.ReactNode;
  backdrop?: DrawerBackdrop;
}) {
  return (
    <>
      <DrawerOverlay appearance={backdrop} />
      <DrawerContentPrimitive
        className={cn(
          "max-h-[80svh] min-h-0 pb-[max(0.5rem,var(--safe-area-bottom))] pl-[var(--safe-area-left)] pr-[var(--safe-area-right)]",
          className,
        )}
        {...props}
      >
        <div aria-hidden="true" data-slot="drawer-grip" />
        {children}
      </DrawerContentPrimitive>
    </>
  );
}

function DrawerHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="drawer-header"
      className={cn(
        "flex flex-col gap-0.5 p-4 text-center md:text-left",
        className,
      )}
      {...props}
    />
  );
}

function DrawerFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="drawer-footer"
      className={cn("mt-auto flex flex-col gap-2 p-4", className)}
      {...props}
    />
  );
}

export {
  Drawer,
  DrawerOverlay,
  DrawerTrigger,
  DrawerClose,
  DrawerContent,
  DrawerHeader,
  DrawerFooter,
  DrawerTitle,
  DrawerDescription,
};
