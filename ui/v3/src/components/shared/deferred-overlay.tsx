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

export type OverlayPendingProps = Pick<OverlayState, "onClose"> & {
  error?: Error;
};

/** Load on first use or explicit preload, then retain the original component's
 * lifetime (drafts, closing animations, and the scene lightbox's stable player). */
export function deferredOverlay<Props extends object>(
  load: () => Promise<{ default: FC<Props> }>,
  getState: (props: Props) => OverlayState,
  Pending: FC<OverlayPendingProps> = OverlayPending,
) {
  let loaded: FC<Props> | undefined;
  let pending: Promise<{ default: FC<Props> }> | undefined;
  const preload = () => {
    pending ??= Promise.resolve()
      .then(load)
      .then((module) => {
        loaded = module.default;
        return module;
      })
      .catch((error: unknown) => {
        // A failed background preload must still allow a normal open to retry.
        pending = undefined;
        throw error;
      });
    return pending;
  };
  const LazyComponent = lazyComponent(preload);
  function DeferredOverlay(props: Props) {
    const { open, onClose } = getState(props);
    // Pick the component once, when first opened. A completed preload renders
    // synchronously; a cold open retains its lazy identity after resolution so
    // subsequent prop changes and reopening do not remount the player or form.
    const [active, setActive] = useState<{ Component: FC<Props> } | null>(() =>
      open ? { Component: loaded ?? LazyComponent } : null,
    );
    if (open && !active) setActive({ Component: loaded ?? LazyComponent });
    if (!active) return null;
    const { Component } = active;

    return (
      <CatchBoundary
        getResetKey={() => open}
        errorComponent={({ error }) =>
          open ? (
            <Pending
              onClose={onClose}
              error={error instanceof Error ? error : new Error(String(error))}
            />
          ) : null
        }
      >
        <Suspense fallback={open ? <Pending onClose={onClose} /> : null}>
          <Component {...props} />
        </Suspense>
      </CatchBoundary>
    );
  }
  return Object.assign(DeferredOverlay, { preload });
}

function OverlayPending({ onClose, error }: OverlayPendingProps) {
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
