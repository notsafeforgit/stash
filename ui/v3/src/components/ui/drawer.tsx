import { createContext, useContext } from "react";
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

import { installAnimationStartTimeSync } from "@/lib/animation-start-time";
import { cn } from "@/lib/utils";
import {
  type ShortcutOverlayRootProps,
  useOverlayOpenState,
} from "@/components/shortcut-provider";
import {
  type OpenChangeArgs,
  useOverlayContainerElement,
} from "./overlay-container";

installAnimationStartTimeSync();

export type DrawerBackdrop = "blurred" | "dimmed";

type DrawerRootProps = React.ComponentProps<typeof DrawerPrimitive> &
  ShortcutOverlayRootProps;

// Pointer dragging, not touch scroll-snap: scrolling a field into view would move
// the sheet. ui-sdk's 0.34 s spring opens about 100 ms behind the 200 ms ease-out
// these sheets had; 0.2 s keeps pace with it.
const SHEET_PHYSICS = { response: 0.2 };

const DrawerModalContext = createContext(true);

// A fully modal drawer sets `inert` on the page, which restyles all of it as the
// sheet opens and closes; on a long list that outlasts the slide, so the sheet
// pops in. Trapping focus avoids it, and the content still declares aria-modal.
function Drawer({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  driver = "pointer",
  physics = SHEET_PHYSICS,
  modal = "trap-focus",
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
    <DrawerModalContext value={modal !== false}>
      <DrawerPrimitive
        container={container}
        driver={driver}
        physics={physics}
        modal={modal}
        {...overlayProps}
        {...props}
      />
    </DrawerModalContext>
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

// Our sheets open from their own buttons, which ui-sdk counts as a code open and
// focuses the first field, raising the iOS keyboard. Focus the sheet instead.
function DrawerContent({
  className,
  children,
  backdrop = "blurred",
  initialFocus = "content",
  ...props
}: Omit<React.ComponentProps<typeof DrawerContentPrimitive>, "children"> & {
  children?: React.ReactNode;
  backdrop?: DrawerBackdrop;
}) {
  const modal = useContext(DrawerModalContext);
  return (
    <>
      <DrawerOverlay appearance={backdrop} />
      <DrawerContentPrimitive
        aria-modal={modal || undefined}
        initialFocus={initialFocus}
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
