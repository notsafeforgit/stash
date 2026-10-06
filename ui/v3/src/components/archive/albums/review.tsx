import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMsg } from "@/hooks/message";
import {
  createAlbumReviewAPI,
  type AlbumJob,
  type AlbumPreview,
} from "@/core/native-archive/album-review-api";
import {
  createAlbumReviewOutbox,
  type SavedAlbumReview,
} from "@/core/native-archive/album-review-outbox";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { PostSection } from "../posts/shared";
import { AlbumPreviewForm } from "./preview";
import { AlbumJobHistory, AlbumJobStatus } from "./jobs";
import { AlbumReviewError } from "./review-shared";

export function AlbumReview({
  post,
  endpoint,
  onPublished,
}: {
  post: string;
  endpoint?: string;
  onPublished?: () => void | Promise<void>;
}) {
  const msg = useMsg();
  const api = useMemo(() => createAlbumReviewAPI(endpoint), [endpoint]);
  const outbox = useMemo(() => createAlbumReviewOutbox(api), [api]);
  const [saved, setSaved] = useState<SavedAlbumReview | null>(null);
  const [job, setJob] = useState<AlbumJob>();
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [refreshFailed, setRefreshFailed] = useState(false);
  const [loadVersion, setLoadVersion] = useState(0);
  const [formVersion, setFormVersion] = useState(0);
  const [historyVersion, setHistoryVersion] = useState(0);
  const locked = useRef(false);
  const mounted = useRef(false);
  const notified = useRef(new Set<string>());
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const acceptJob = useCallback(
    async (result: AlbumJob) => {
      if (result.post_uuid !== post)
        throw new NativeArchiveError(0, "invalid_response");
      if (!mounted.current) return;
      setJob(result);
      const event = result.publication?.event_uuid;
      if (event && !notified.current.has(event)) {
        notified.current.add(event);
        try {
          await onPublished?.();
          if (mounted.current) setRefreshFailed(false);
        } catch {
          notified.current.delete(event);
          if (mounted.current) setRefreshFailed(true);
        }
      }
    },
    [post, onPublished],
  );
  // biome-ignore lint/correctness/useExhaustiveDependencies: Explicit refresh rereads storage and an admitted job, never delivers a pending request.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setReady(false);
    setError(undefined);
    async function load() {
      try {
        const record = await outbox.read(post);
        if (controller.signal.aborted) return;
        setSaved(record);
        setJob(undefined);
        if (record?.state === "admitted") {
          const current = await outbox.inspectAdmitted(
            record,
            controller.signal,
          );
          if (controller.signal.aborted) return;
          await acceptJob(current);
        }
        if (!controller.signal.aborted) setReady(true);
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [post, outbox, acceptJob, loadVersion]);

  // Poll only the visible, admitted/inspected job. This performs no admission or
  // recovery POSTs; any error leaves an explicit refresh action.
  useEffect(() => {
    if (
      !job ||
      (job.state !== "queued" && job.state !== "running") ||
      busy ||
      !ready ||
      error !== undefined
    )
      return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const jobID = job.job_uuid;
    async function poll() {
      if (document.hidden) {
        timer = setTimeout(() => void poll(), 5000);
        return;
      }
      try {
        const current = await api.job(jobID, controller.signal);
        if (!controller.signal.aborted) await acceptJob(current);
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      }
    }
    timer = setTimeout(() => void poll(), 5000);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [api, job, busy, ready, error, acceptJob]);

  async function run(action: () => Promise<AlbumJob | undefined>) {
    if (locked.current) return;
    locked.current = true;
    setBusy(true);
    setError(undefined);
    try {
      const current = await action();
      if (mounted.current) {
        if (current) await acceptJob(current);
        setFormVersion((value) => value + 1);
        setHistoryVersion((value) => value + 1);
      }
    } catch (error) {
      if (mounted.current) setError(error);
    } finally {
      try {
        const record = await outbox.read(post);
        if (mounted.current) {
          setSaved(record);
          setReady(true);
        }
      } catch (error) {
        if (mounted.current) {
          setReady(false);
          setError(error);
        }
      }
      locked.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  function apply(preview: AlbumPreview) {
    return run(async () => {
      const record = await outbox.prepare(preview);
      if (mounted.current) setSaved(record);
      return outbox.deliver(post);
    });
  }
  function changeJob(action: "retry" | "cancel", current: AlbumJob) {
    return run(async () => {
      const record = await (action === "retry"
        ? outbox.prepareRetry(current)
        : outbox.prepareCancel(current));
      if (mounted.current) setSaved(record);
      return outbox.deliver(post);
    });
  }
  function refresh() {
    setBusy(true);
    setFormVersion((value) => value + 1);
    setLoadVersion((value) => value + 1);
  }
  const pending = saved && saved.state !== "admitted";
  const disabled = busy || !ready || !!pending;
  return (
    <div className="flex flex-col gap-4" data-album-review>
      <p className="text-sm text-muted-foreground">
        {msg(
          "album_review.help",
          "Match this post's source attachments to files already registered in Stash, then review the gallery changes. This does not download media.",
        )}
      </p>
      {busy && (
        <Spinner
          aria-label={msg("album_review.loading", "Loading album operations")}
        />
      )}
      {error !== undefined && (
        <AlbumReviewError error={error} retry={busy ? undefined : refresh} />
      )}
      {refreshFailed && (
        <Alert variant="destructive">
          <AlertTitle>
            {msg(
              "album_review.refresh_failed",
              "Changes are saved, but the gallery view could not refresh",
            )}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "album_review.refresh_failed_help",
              "Refresh the album status to reload the affected gallery.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {pending && !busy && (
        <Alert>
          <AlertTitle>
            {saved.state === "pending"
              ? msg(
                  "album_review.pending",
                  "An album request needs confirmation",
                )
              : msg("album_review.rejected", "The saved request was rejected")}
          </AlertTitle>
          <AlertDescription className="flex flex-col gap-3">
            <p>
              {saved.state === "pending"
                ? msg(
                    "album_review.pending_help",
                    "Check the saved request before making another change. Recovery looks up its result and only resends the same request if needed.",
                  )
                : msg(
                    "album_review.rejected_help",
                    "You can clear this rejected request and review the current album again.",
                  )}
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={busy || !ready}
              onClick={() =>
                void run(async () => {
                  if (saved.state === "pending") return outbox.deliver(post);
                  await outbox.forgetRejected(post, saved.request_uuid);
                  return saved.previous
                    ? api.job(saved.previous.job_uuid)
                    : undefined;
                })
              }
            >
              {saved.state === "pending"
                ? msg("album_review.recover", "Check and retry saved request")
                : msg("album_review.review_again", "Review album again")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {job && (
        <AlbumJobStatus
          job={job}
          api={api}
          disabled={disabled}
          onRefresh={() => void run(() => api.job(job.job_uuid))}
          onCancel={() => void changeJob("cancel", job)}
          onRetry={() => void changeJob("retry", job)}
        />
      )}
      <AlbumPreviewForm
        key={formVersion}
        api={api}
        post={post}
        disabled={disabled}
        onApply={apply}
      />
      <PostSection title={msg("album_review.history", "Album job history")}>
        <AlbumJobHistory
          key={historyVersion}
          api={api}
          post={post}
          disabled={busy || !ready}
          onChoose={(selected) => void run(() => api.job(selected.job_uuid))}
        />
      </PostSection>
    </div>
  );
}
