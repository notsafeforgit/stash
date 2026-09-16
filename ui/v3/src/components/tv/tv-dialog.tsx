import type { ReactNode } from "react";
import { FormattedMessage } from "react-intl";
import { Button } from "@/components/ui/button";
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

/** Forms own their scroller and actions; other content gets one Close footer. */
export function TvDialogContent({
  title,
  description,
  className,
  variant = "content",
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  className?: string;
  variant?: "content" | "form";
  children: ReactNode;
}) {
  return (
    <DialogContent
      showCloseButton={false}
      // Portaled form controls still bubble through the player. Its pointer-up
      // focus handler would steal focus and close a tag picker's popup.
      onPointerUp={(event) => event.stopPropagation()}
      className={cn(
        "min-w-0 max-h-[calc(100dvh-2rem)] grid-cols-[minmax(0,1fr)] overflow-hidden sm:max-w-lg",
        variant === "form"
          ? "grid-rows-[auto_minmax(0,1fr)]"
          : "grid-rows-[auto_minmax(0,1fr)_auto]",
        className,
      )}
    >
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        {description && (
          <DialogDescription className="line-clamp-3 [overflow-wrap:anywhere]">
            {description}
          </DialogDescription>
        )}
      </DialogHeader>
      <div
        className={cn(
          "min-h-0 min-w-0 [overflow-wrap:anywhere]",
          variant === "form"
            ? "flex flex-col overflow-hidden"
            : "overflow-y-auto overscroll-contain",
        )}
        data-tv-dialog-body
      >
        {children}
      </div>
      {variant === "content" && (
        <DialogFooter>
          <DialogClose
            render={<Button variant="outline" className="min-h-11" />}
          >
            <FormattedMessage id="actions.close" defaultMessage="Close" />
          </DialogClose>
        </DialogFooter>
      )}
    </DialogContent>
  );
}
