import { FormattedMessage } from "react-intl";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { QueryError } from "@/components/query-error";
import { PlayerCloseButton } from "@/components/player/player-close-button";
import type { OverlayPendingProps } from "@/components/shared/deferred-overlay";

/** Match the lightbox surface when opened before its background preload ends. */
export function LightboxPending({ onClose, error }: OverlayPendingProps) {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        data-lightbox-pending=""
        showCloseButton={false}
        className="top-0 left-0 h-dvh max-w-none translate-x-0 translate-y-0 rounded-none p-0 ring-0 sm:max-w-none data-open:animate-none data-closed:animate-none"
      >
        <DialogTitle className="sr-only">
          {error ? (
            <FormattedMessage id="errors.load_failed" />
          ) : (
            <FormattedMessage id="loading.generic" defaultMessage="Loading…" />
          )}
        </DialogTitle>
        <div className="absolute inset-0 flex items-center justify-center bg-black">
          {error ? (
            <div className="safe-area-viewport max-w-sm bg-popover p-4 text-popover-foreground">
              <QueryError
                error={error}
                retry={async () => window.location.reload()}
              />
            </div>
          ) : (
            <Spinner className="size-10 text-white/70" />
          )}
        </div>
        <div className="viewport-controls absolute inset-x-0 bottom-0 flex justify-end">
          <PlayerCloseButton onClose={onClose} />
        </div>
      </DialogContent>
    </Dialog>
  );
}
