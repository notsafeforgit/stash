import type * as React from "react";
import { FormattedMessage } from "react-intl";
import { XIcon } from "lucide-react";
import { DialogContent as DialogContentPrimitive } from "@ui-sdk/react/dialog";

import { cn } from "@/lib/utils";
import {
  Dialog,
  DialogClose,
  DialogDescription,
  DialogOverlay,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { type OverlayFocus, overlayFocus } from "./overlay-container";

// A sheet is a modal dialog placed at an edge, so it uses the Dialog primitive.
// ui-sdk's Drawer takes its edge on the root, which would move the side API off
// SheetContent, and adds swipe-to-dismiss that side sheets never had.
const Sheet = Dialog;

type SheetContentProps = Omit<
  React.ComponentProps<typeof DialogContentPrimitive>,
  "children" | "initialFocus" | "finalFocus"
> & {
  children?: React.ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  showCloseButton?: boolean;
  overlayClassName?: string;
  initialFocus?: OverlayFocus;
  finalFocus?: OverlayFocus;
};

function SheetContent({
  className,
  children,
  side = "right",
  showCloseButton = true,
  overlayClassName,
  initialFocus,
  finalFocus,
  ...props
}: SheetContentProps) {
  return (
    <>
      <DialogOverlay className={overlayClassName} />
      <DialogContentPrimitive
        data-side={side}
        className={className}
        initialFocus={overlayFocus(initialFocus)}
        finalFocus={overlayFocus(finalFocus)}
        {...props}
      >
        {children}
        {showCloseButton && (
          <DialogClose
            data-variant="ghost"
            data-size="icon-sm"
            className="absolute top-[calc(0.75rem+var(--safe-area-top))] right-[max(0.75rem,var(--safe-area-right))]"
          >
            <XIcon />
            <span className="sr-only">
              <FormattedMessage id="actions.close" defaultMessage="Close" />
            </span>
          </DialogClose>
        )}
      </DialogContentPrimitive>
    </>
  );
}

function SheetHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="sheet-header"
      className={cn("flex flex-col gap-0.5 p-4", className)}
      {...props}
    />
  );
}

function SheetFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="sheet-footer"
      className={cn("mt-auto flex flex-col gap-2 p-4", className)}
      {...props}
    />
  );
}

export {
  Sheet,
  DialogTrigger as SheetTrigger,
  DialogClose as SheetClose,
  SheetContent,
  SheetHeader,
  SheetFooter,
  DialogTitle as SheetTitle,
  DialogDescription as SheetDescription,
};
