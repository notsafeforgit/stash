import type * as React from "react";
import { useIntl } from "react-intl";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerDescription,
  DrawerTitle,
} from "@/components/ui/drawer";

/** A consistent exit within thumb reach, independent of drawer scrolling. */
export function BottomSheetCloseFooter() {
  const intl = useIntl();
  return (
    <div className="flex shrink-0 justify-end border-t px-3 pt-1 pb-[max(0.5rem,env(safe-area-inset-bottom,0px))]">
      <DrawerClose
        render={<Button variant="ghost" className="h-11 min-w-11" />}
      >
        {intl.formatMessage({ id: "actions.close", defaultMessage: "Close" })}
      </DrawerClose>
    </div>
  );
}

export function BottomSheetHeader({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="bottom-sheet-header"
      className={cn("flex shrink-0 flex-col gap-0.5 p-4", className)}
      {...props}
    />
  );
}

export function BottomSheetTitle({
  className,
  ...props
}: React.ComponentProps<typeof DrawerTitle>) {
  return (
    <DrawerTitle
      data-slot="bottom-sheet-title"
      className={className}
      {...props}
    />
  );
}

export function BottomSheetDescription({
  className,
  ...props
}: React.ComponentProps<typeof DrawerDescription>) {
  return (
    <DrawerDescription
      data-slot="bottom-sheet-description"
      className={className}
      {...props}
    />
  );
}

export interface BottomSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
  /** Extra classes applied to the content panel (e.g. height constraints). */
  className?: string;
  /** Whether this sheet should suppress background entity-list shortcuts. */
  blocksListShortcuts?: boolean;
  showCloseButton?: boolean;
}

/**
 * A mobile-optimised bottom sheet backed by Base UI Drawer.
 * Slides in with CSS transitions; drag the panel down to dismiss.
 */
export function BottomSheet({
  open,
  onOpenChange,
  children,
  className,
  blocksListShortcuts,
  showCloseButton = true,
}: BottomSheetProps) {
  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      blocksListShortcuts={blocksListShortcuts}
    >
      <DrawerContent className={cn("bg-background outline-none", className)}>
        {children}
        {showCloseButton && <BottomSheetCloseFooter />}
      </DrawerContent>
    </Drawer>
  );
}
