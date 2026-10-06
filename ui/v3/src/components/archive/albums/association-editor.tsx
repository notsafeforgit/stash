import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type ComponentType,
} from "react";
import { useMsg } from "@/hooks/message";
import type {
  AssociationInput,
  AssociationRequest,
  AssociationPreview,
  AssociationReceipt,
  AssociationProtocol,
} from "@/core/native-archive/association-review-protocol";
import {
  createAssociationReviewOutbox,
  type SavedAssociationReview,
} from "@/core/native-archive/association-review-outbox";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { PostSection } from "../posts/shared";

export type AssociationEditorFormProps<Context, Preview, API> = {
  api: API;
  current: Context;
  disabled: boolean;
  onApply: (preview: Preview) => Promise<void>;
  generation: number;
};

export function AssociationEditor<
  Input extends AssociationInput,
  Apply extends Input & AssociationRequest,
  Preview extends AssociationPreview<Input>,
  Receipt extends AssociationReceipt<Apply>,
  Context,
>({
  api,
  scope,
  onChanged,
  Form,
  history,
}: {
  api: AssociationProtocol<Input, Apply, Preview, Receipt> & {
    context: (scope: string, signal?: AbortSignal) => Promise<Context>;
  };
  scope: string;
  onChanged: () => void | Promise<void>;
  Form: ComponentType<
    AssociationEditorFormProps<
      Context,
      Preview,
      Pick<
        AssociationProtocol<Input, Apply, Preview, Receipt>,
        "endpoint" | "preview"
      >
    >
  >;
  history: ReactNode;
}) {
  const msg = useMsg();
  const outbox = useMemo(() => createAssociationReviewOutbox(api), [api]);
  const [current, setCurrent] = useState<Context>();
  const [ready, setReady] = useState(false);
  const [saved, setSaved] = useState<SavedAssociationReview | null>(null);
  const [storageReady, setStorageReady] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [formGeneration, setFormGeneration] = useState(0);
  const [historyRevision, setHistoryRevision] = useState(0);
  const mounted = useRef(false),
    operation = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Reload replaces this scope's context and saved intent; it never delivers a request.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    setStorageReady(false);
    setReady(false);
    async function load() {
      try {
        const pending = await outbox.read(scope);
        if (controller.signal.aborted) return;
        setSaved(pending);
        setStorageReady(true);
        const value = await api.context(scope, controller.signal);
        if (!controller.signal.aborted) {
          setCurrent(value);
          setReady(true);
          setFormGeneration((value) => value + 1);
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, outbox, scope, refresh]);
  function reload() {
    if (busy || operation.current) return;
    setBusy(true);
    setReady(false);
    setRefresh((value) => value + 1);
  }
  async function deliver(preview?: Preview) {
    if (
      busy ||
      operation.current ||
      !storageReady ||
      (preview && (!ready || saved !== null))
    )
      return;
    operation.current = true;
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (preview) {
        const pending = await outbox.prepare(scope, preview);
        if (mounted.current) setSaved(pending);
      }
      // A saved operation can finish after closing. Recover through the same
      // receipt next time, and refresh the owning card even after unmount.
      await outbox.deliver(scope);
      if (mounted.current) {
        setApplied(true);
        setReady(false);
        setHistoryRevision((value) => value + 1);
      }
      await onChanged();
      if (!mounted.current) return;
      const value = await api.context(scope);
      if (mounted.current) {
        setCurrent(value);
        setReady(true);
        setFormGeneration((value) => value + 1);
      }
    } catch (error) {
      if (mounted.current) setError(error);
    } finally {
      if (mounted.current) {
        try {
          const pending = await outbox.read(scope);
          if (mounted.current) {
            setSaved(pending);
            setStorageReady(true);
          }
        } catch (error) {
          if (mounted.current) {
            setStorageReady(false);
            setError(error);
          }
        }
        if (mounted.current) setBusy(false);
      }
      operation.current = false;
    }
  }
  async function reviewAgain() {
    if (
      busy ||
      operation.current ||
      !storageReady ||
      saved?.state !== "rejected"
    )
      return;
    operation.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await outbox.forgetRejected(
        scope,
        api.parseApply(JSON.parse(saved.body)).request_uuid,
      );
      if (mounted.current) {
        setReady(false);
        setRefresh((value) => value + 1);
      }
    } catch (error) {
      if (mounted.current) {
        setError(error);
        setBusy(false);
      }
    } finally {
      operation.current = false;
    }
  }
  return (
    <div className="flex flex-col gap-4">
      {busy && (
        <Spinner
          aria-label={msg(
            "association_review.loading",
            "Loading association review",
          )}
        />
      )}
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("association_review.saved", "Association choice saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "association_review.saved_help",
              "The choice and its history are saved. Gallery members, metadata and files have not changed.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {error !== undefined &&
        (applied ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "association_review.refresh_failed",
                "The choice is saved, but this view could not be refreshed",
              )}
            </AlertTitle>
            <AlertDescription>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={reload}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <ReviewError error={error} retry={!busy ? reload : undefined} />
        ))}
      {saved && !busy && (
        <Alert>
          <AlertTitle>
            {saved.state === "pending"
              ? msg(
                  "association_review.pending",
                  "An association change needs confirmation",
                )
              : msg(
                  "association_review.stale",
                  "This association preview changed",
                )}
          </AlertTitle>
          <AlertDescription>
            <p>
              {saved.state === "pending"
                ? msg(
                    "association_review.pending_help",
                    "Recover the original result before making another choice. Opening this panel does not resend it.",
                  )
                : msg(
                    "association_review.stale_help",
                    "This request was rejected before saving. Load the current association and review a new choice.",
                  )}
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={busy || !storageReady}
              onClick={() =>
                void (saved.state === "pending" ? deliver() : reviewAgain())
              }
            >
              {saved.state === "pending"
                ? msg("association_review.recover", "Recover saved request")
                : msg("association_review.review_again", "Review again")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {current !== undefined && ready && storageReady && !saved && (
        <Form
          api={api}
          current={current}
          disabled={busy}
          onApply={deliver}
          generation={formGeneration}
        />
      )}
      <PostSection
        title={msg("association_review.history", "Association history")}
      >
        <div key={`${scope}:${historyRevision}`}>{history}</div>
      </PostSection>
    </div>
  );
}
