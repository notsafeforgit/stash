import type * as React from "react";
import { cn } from "@/lib/utils";
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerTitle,
  type DrawerBackdrop,
} from "@/components/ui/drawer";

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

export {
  DrawerTitle as BottomSheetTitle,
  DrawerDescription as BottomSheetDescription,
};

export interface BottomSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
  /** Extra classes applied to the content panel (e.g. height constraints). */
  className?: string;
  /** Whether this sheet should suppress background entity-list shortcuts. */
  blocksListShortcuts?: boolean;
  onOpenChangeComplete?: (open: boolean) => void;
  backdrop?: DrawerBackdrop;
}

/**
 * A mobile-optimised bottom sheet backed by ui-sdk Drawer.
 * Slides in with CSS transitions; drag the panel down to dismiss.
 */
export function BottomSheet({
  open,
  onOpenChange,
  children,
  className,
  blocksListShortcuts,
  onOpenChangeComplete,
  backdrop,
}: BottomSheetProps) {
  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      blocksListShortcuts={blocksListShortcuts}
      onOpenChangeComplete={onOpenChangeComplete}
    >
      <DrawerContent backdrop={backdrop} className={className}>
        {children}
      </DrawerContent>
    </Drawer>
  );
}
