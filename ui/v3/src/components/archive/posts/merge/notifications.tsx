import { useCallback, useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { NativeArchiveError } from "@/core/native-archive/client";
import type {
  PostConsolidationAPI,
  MergeNotification,
} from "@/core/native-archive/post-consolidation-api";
import { createMergeNotificationOutbox } from "@/core/native-archive/post-consolidation-outbox";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { usePostRead, PostRows } from "../read";
import { PostSection } from "../shared";
import { MergeError } from "./shared";

function NotificationState({ job }: { job: MergeNotification }) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = {
    queued: msg("album_review.queued", "Queued"),
    running: msg("album_review.running", "Running"),
    succeeded: msg("album_review.succeeded", "Finished"),
    failed: msg("album_review.failed", "Failed"),
    cancelled: msg("album_review.cancelled", "Cancelled"),
  };
  return (
    <div className="flex flex-col gap-1">
      <p>
        {labels[job.state]} ·{" "}
        {intl.formatDate(job.updated_at, {
          dateStyle: "medium",
          timeStyle: "short",
        })}
      </p>
      {job.error_code && (
        <code data-selectable-text className="wrap-anywhere text-xs">
          {job.error_code}
        </code>
      )}
    </div>
  );
}

function OlderNotifications({
  api,
  review,
  before,
}: {
  api: PostConsolidationAPI;
  review: string;
  before: number;
}) {
  const msg = useMsg();
  const load = useCallback(
    (cursor?: string, signal?: AbortSignal) =>
      api.notifications(review, cursor ? Number(cursor) : before, signal),
    [api, review, before],
  );
  return (
    <PostRows
      load={load}
      pageLimit={api.pageLimit}
      rowKey={(job) => String(job.sequence)}
      renderRow={(job) => <NotificationState job={job} />}
      empty={msg(
        "post_merge.no_older_notifications",
        "No earlier notification jobs",
      )}
    />
  );
}

export function MergeNotifications({
  api,
  review,
}: {
  api: PostConsolidationAPI;
  review: string;
}) {
  const msg = useMsg();
  const [box] = useState(() => createMergeNotificationOutbox(api));
  const load = useCallback(
    async (signal: AbortSignal) => {
      const [jobs, saved] = await Promise.all([
        api.notifications(review, 0, signal),
        box.read(review),
      ]);
      if (!jobs.length) throw new NativeArchiveError(0, "invalid_response");
      return { job: jobs[0], saved };
    },
    [api, box, review],
  );
  const result = usePostRead(load);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const { job, saved } = result.value ?? {};
  const { busy: reading, retry } = result;
  useEffect(() => {
    if (reading || (job?.state !== "queued" && job?.state !== "running"))
      return;
    const timer = setTimeout(retry, 5000);
    return () => clearTimeout(timer);
  }, [reading, retry, job?.state]);
  async function act(mode: "cancel" | "retry" | "recover") {
    if (busy || reading) return;
    setBusy(true);
    setError(undefined);
    try {
      if (mode === "cancel" && job) await api.cancelNotification(job);
      else {
        if (mode === "retry" && job) await box.prepare(job);
        await box.deliver(review);
      }
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
      retry();
    }
  }
  return (
    <div className="flex flex-col gap-3">
      <p>{msg("post_merge.notifications", "Plugin notifications")}</p>
      {reading && (
        <Spinner
          aria-label={msg(
            "post_merge.loading_notifications",
            "Loading notification delivery",
          )}
        />
      )}
      {result.error !== undefined && (
        <MergeError error={result.error} retry={retry} />
      )}
      {error !== undefined && <MergeError error={error} />}
      {job && (
        <>
          <NotificationState job={job} />
          <p>
            {job.hooks_finished
              ? msg(
                  "album_review.notifications_done",
                  "Notification delivery finished.",
                )
              : job.state === "queued" || job.state === "running"
                ? msg(
                    "album_review.notifications_pending",
                    "Plugin notifications are still pending. Saved changes remain in the library.",
                  )
                : msg(
                    "album_review.notifications_stopped",
                    "Notification delivery did not finish. Retry resumes delivery without applying the gallery changes again.",
                  )}
          </p>
          {saved ? (
            <Alert>
              <AlertTitle>
                {msg(
                  "post_merge.pending_notification",
                  "A notification retry needs confirmation",
                )}
              </AlertTitle>
              <AlertDescription>
                <p>
                  {msg(
                    "post_merge.pending_notification_help",
                    "Recover the saved retry request before starting another. This does not repeat the post merge.",
                  )}
                </p>
                <Button
                  variant="outline"
                  disabled={busy || reading || result.error !== undefined}
                  onClick={() => void act("recover")}
                >
                  {msg(
                    "post_merge.recover_notification",
                    "Recover notification retry",
                  )}
                </Button>
              </AlertDescription>
            </Alert>
          ) : (
            <div className="flex flex-wrap gap-2">
              {(job.state === "failed" || job.state === "cancelled") && (
                <Button
                  variant="outline"
                  disabled={busy || reading || result.error !== undefined}
                  onClick={() => void act("retry")}
                >
                  {msg(
                    "album_review.retry_notifications",
                    "Retry notification delivery",
                  )}
                </Button>
              )}
              {(job.state === "queued" || job.state === "running") && (
                <Button
                  variant="outline"
                  disabled={busy || reading || result.error !== undefined}
                  onClick={() => void act("cancel")}
                >
                  {msg(
                    "post_merge.cancel_notifications",
                    "Cancel notification delivery",
                  )}
                </Button>
              )}
            </div>
          )}
          {(job.state === "queued" || job.state === "running") && (
            <p className="text-sm text-muted-foreground">
              {msg(
                "album_review.cancel_help",
                "Cancelling stops pending work. It does not undo saved changes or notifications already delivered.",
              )}
            </p>
          )}
          <PostSection
            title={msg(
              "post_merge.notification_history",
              "Earlier notification jobs",
            )}
          >
            <OlderNotifications
              key={job.sequence}
              api={api}
              review={review}
              before={job.sequence}
            />
          </PostSection>
        </>
      )}
      <Button variant="outline" disabled={busy || reading} onClick={retry}>
        {msg("post_merge.refresh_notifications", "Refresh notification status")}
      </Button>
    </div>
  );
}
