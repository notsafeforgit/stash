import { Suspense, useState, type FC } from "react";
import { FormattedMessage } from "react-intl";
import { CatchBoundary } from "@tanstack/react-router";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { QueryError } from "@/components/query-error";
import { lazyComponent } from "@/utils/lazy-component";

interface OverlayState {
  open: boolean;
  onClose: () => void;
}

/** Defer code until first use, then retain the original component's lifetime
 * (drafts, closing animations, and the scene lightbox's stable player). */
export function deferredOverlay<Props extends object>(
  load: () => Promise<{ default: FC<Props> }>,
  getState: (props: Props) => OverlayState,
) {
  const Component = lazyComponent(load);
  return function DeferredOverlay(props: Props) {
    const { open, onClose } = getState(props);
    const [activated, setActivated] = useState(open);
    if (open && !activated) setActivated(true);
    if (!activated) return null;

    return (
      <CatchBoundary
        getResetKey={() => open}
        errorComponent={({ error }) =>
          open ? (
            <OverlayPending
              onClose={onClose}
              error={error instanceof Error ? error : new Error(String(error))}
            />
          ) : null
        }
      >
        <Suspense fallback={open ? <OverlayPending onClose={onClose} /> : null}>
          <Component {...props} />
        </Suspense>
      </CatchBoundary>
    );
  };
}

function OverlayPending({
  onClose,
  error,
}: Pick<OverlayState, "onClose"> & { error?: Error }) {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent>
        <DialogTitle className={error ? "sr-only" : undefined}>
          {error ? (
            <FormattedMessage id="errors.load_failed" />
          ) : (
            <FormattedMessage id="loading.generic" defaultMessage="Loading…" />
          )}
        </DialogTitle>
        {error ? (
          <QueryError
            error={error}
            retry={async () => window.location.reload()}
          />
        ) : (
          <Spinner className="size-8" />
        )}
      </DialogContent>
    </Dialog>
  );
}

export function deferredDialog<
  Props extends {
    open: boolean;
    onOpenChange: (open: boolean) => void;
  },
>(load: () => Promise<{ default: FC<Props> }>) {
  return deferredOverlay(load, (props) => ({
    open: props.open,
    onClose: () => props.onOpenChange(false),
  }));
}

export function deferredEditSheet<
  Props extends {
    id: string | null;
    onClose: () => void;
  },
>(load: () => Promise<{ default: FC<Props> }>) {
  return deferredOverlay(load, (props) => ({
    open: !!props.id,
    onClose: props.onClose,
  }));
}
