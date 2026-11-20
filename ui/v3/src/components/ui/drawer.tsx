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

// Pointer dragging, not touch scroll-snap: scrolling a field into view would move
// the sheet. ui-sdk's 0.34 s spring opens about 100 ms behind the 200 ms ease-out
// these sheets had; 0.2 s keeps pace with it.
const SHEET_PHYSICS = { response: 0.2 };

function Drawer({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  driver = "pointer",
  physics = SHEET_PHYSICS,
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
      physics={physics}
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
          "max-h-[min(80svh,var(--drawer-max-height))] min-h-0 pb-[max(0.5rem,var(--safe-area-bottom))] pl-[var(--safe-area-left)] pr-[var(--safe-area-right)]",
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
