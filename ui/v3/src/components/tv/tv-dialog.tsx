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

/** Keep long content in one bounded scroller and dismissal within reach. */
export function TvDialogContent({
  title,
  description,
  className,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <DialogContent
      showCloseButton={false}
      className={cn(
        "min-w-0 max-h-[calc(100%-2rem)] grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-lg",
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
        className="min-h-0 min-w-0 overflow-y-auto overscroll-contain [overflow-wrap:anywhere]"
        data-tv-dialog-body
      >
        {children}
      </div>
      <DialogFooter>
        <DialogClose render={<Button variant="outline" className="min-h-11" />}>
          <FormattedMessage id="actions.close" defaultMessage="Close" />
        </DialogClose>
      </DialogFooter>
    </DialogContent>
  );
}
