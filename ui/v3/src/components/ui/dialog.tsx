import type * as React from "react";
import { FormattedMessage } from "react-intl";
import { XIcon } from "lucide-react";
import {
  Dialog as DialogPrimitive,
  DialogBackdrop,
  DialogClose,
  DialogContent as DialogContentPrimitive,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@ui-sdk/react/dialog";

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

type DialogRootProps = React.ComponentProps<typeof DialogPrimitive> &
  ShortcutOverlayRootProps;

function Dialog({
  open,
  defaultOpen,
  onOpenChange,
  blocksListShortcuts,
  ...props
}: DialogRootProps) {
  const container = useOverlayContainerElement();
  const overlayProps = useOverlayOpenState<OpenChangeArgs<DialogRootProps>>({
    open,
    defaultOpen,
    onOpenChange,
    blocksListShortcuts,
  });

  return <DialogPrimitive container={container} {...overlayProps} {...props} />;
}

function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DialogBackdrop>) {
  return <DialogBackdrop className={className} {...props} />;
}

type DialogContentProps = Omit<
  React.ComponentProps<typeof DialogContentPrimitive>,
  "children" | "initialFocus" | "finalFocus"
> & {
  children?: React.ReactNode;
  // Disable when the content already provides Cancel or Close.
  showCloseButton?: boolean;
  overlayClassName?: string;
  initialFocus?: OverlayFocus;
  finalFocus?: OverlayFocus;
};

function DialogContent({
  className,
  overlayClassName,
  children,
  showCloseButton = true,
  initialFocus,
  finalFocus,
  ...props
}: DialogContentProps) {
  return (
    <>
      <DialogOverlay className={overlayClassName} />
      <DialogContentPrimitive
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
            className="absolute top-2 right-2"
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

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex flex-col gap-2", className)}
      {...props}
    />
  );
}

function DialogFooter({
  className,
  showCloseButton = false,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  showCloseButton?: boolean;
}) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        "flex flex-col-reverse gap-2 sm:flex-row sm:justify-end",
        className,
      )}
      {...props}
    >
      {children}
      {showCloseButton && (
        <DialogClose data-variant="outline">
          <FormattedMessage id="actions.close" defaultMessage="Close" />
        </DialogClose>
      )}
    </div>
  );
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogTitle,
  DialogTrigger,
};
