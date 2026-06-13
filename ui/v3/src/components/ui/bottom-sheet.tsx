import type * as React from "react";
import { cn } from "@/lib/utils";
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerTitle,
} from "@/components/ui/drawer";

export function BottomSheetHeader({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="bottom-sheet-header"
      className={cn("flex flex-col gap-0.5 p-4", className)}
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
}: BottomSheetProps) {
  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      blocksListShortcuts={blocksListShortcuts}
    >
      <DrawerContent className={cn("bg-background outline-none", className)}>
        {children}
      </DrawerContent>
    </Drawer>
  );
}
