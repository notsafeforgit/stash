import { useEffect, useMemo, useRef, useState } from "react";
import { useMsg } from "@/hooks/message";
import {
  createAttachmentSelectionAPI,
  selectionApplySchema,
  type SelectionPreview,
} from "@/core/native-archive/attachment-selection-api";
import {
  createAttachmentSelectionOutbox,
  type SavedSelectionReview,
} from "@/core/native-archive/attachment-selection-outbox";
import type { PostSummary } from "@/core/native-archive/source-post-api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { PostSection } from "../posts/shared";
import { SelectionForm } from "./selection-form";
import { SelectionHistory } from "./selection-details";

export function SelectionReview({
  post,
  endpoint,
  onChanged,
}: {
  post: string;
  endpoint: string;
  onChanged: () => void;
}) {
  const msg = useMsg();
  const api = useMemo(() => createAttachmentSelectionAPI(endpoint), [endpoint]);
  const outbox = useMemo(() => createAttachmentSelectionOutbox(api), [api]);
  const [current, setCurrent] = useState<PostSummary>();
  const [postReady, setPostReady] = useState(false);
  const [saved, setSaved] = useState<SavedSelectionReview | null>(null);
  const [storageReady, setStorageReady] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const mounted = useRef(false);
  const operation = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Explicit reload reads only this post and its saved request. Opening never submits a mutation.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    setStorageReady(false);
    setPostReady(false);
    async function load() {
      try {
        const pending = await outbox.read(post);
        if (controller.signal.aborted) return;
        setSaved(pending);
        setStorageReady(true);
        const value = await api.post(post, controller.signal);
        if (!controller.signal.aborted) {
          setCurrent(value);
          setPostReady(true);
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, outbox, post, refresh]);

  function reload() {
    if (busy || operation.current) return;
    setBusy(true);
    setPostReady(false);
    setRefresh((value) => value + 1);
  }
  async function deliver(preview?: SelectionPreview) {
    if (
      busy ||
      operation.current ||
      !storageReady ||
      (preview && (!postReady || saved !== null))
    )
      return;
    operation.current = true;
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (preview) {
        const pending = await outbox.prepare(post, preview);
        if (mounted.current) setSaved(pending);
      }
      // Once saved, delivery may finish after this panel closes. Its immutable
      // receipt and browser journal remain authoritative on the next opening.
      await outbox.deliver(post);
      onChanged();
      if (!mounted.current) return;
      setApplied(true);
      setPostReady(false);
      const value = await api.post(post);
      if (mounted.current) {
        setCurrent(value);
        setPostReady(true);
      }
    } catch (error) {
      if (mounted.current) setError(error);
    } finally {
      if (mounted.current) {
        try {
          const pending = await outbox.read(post);
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
        post,
        selectionApplySchema.parse(JSON.parse(saved.body)).request_uuid,
      );
      if (mounted.current) {
        setPostReady(false);
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
            "attachment_selection.loading",
            "Loading source-list review",
          )}
        />
      )}
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("attachment_selection.saved", "Source-list choice saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "attachment_selection.saved_help",
              "The archive retained this choice and its history. Gallery membership has not changed.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {error !== undefined &&
        (applied ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "attachment_selection.refresh_failed",
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
                  "attachment_selection.pending",
                  "A source-list change needs confirmation",
                )
              : msg("archive_review.changed", "This choice has changed")}
          </AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-3">
              <p>
                {saved.state === "pending"
                  ? msg(
                      "attachment_selection.pending_help",
                      "Check the saved request before making another choice. Stash will recover its receipt or retry the same request.",
                    )
                  : msg(
                      "archive_review.changed_help",
                      "Load a fresh preview before applying this choice.",
                    )}
              </p>
              <Button
                type="button"
                variant="outline"
                disabled={!storageReady}
                onClick={() =>
                  void (saved.state === "pending" ? deliver() : reviewAgain())
                }
              >
                {saved.state === "pending"
                  ? msg(
                      "attachment_selection.recover",
                      "Check and retry saved change",
                    )
                  : msg("attachment_selection.review_again", "Review again")}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
      {current?.state === "forgotten" ? (
        <p className="text-sm text-muted-foreground">
          {msg(
            "attachment_selection.forgotten",
            "This post was forgotten. Its retained source-list history remains available.",
          )}
        </p>
      ) : (
        current && (
          <SelectionForm
            key={`${current.uuid}:${current.revision}:${refresh}`}
            api={api}
            post={post}
            revision={current.revision}
            disabled={busy || !storageReady || !postReady || saved !== null}
            onApply={deliver}
          />
        )
      )}
      <PostSection
        title={msg("attachment_selection.history", "Source-list history")}
      >
        <SelectionHistory
          key={`${post}:${current?.revision ?? 0}`}
          api={api}
          post={post}
        />
      </PostSection>
    </div>
  );
}
