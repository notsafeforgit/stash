import { useCallback } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  AlbumJob,
  AlbumReviewAPI,
} from "@/core/native-archive/album-review-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { PostEmpty, PostSection } from "../posts/shared";
import { useAlbumPages } from "./read";
import { AlbumReviewError, useAlbumLabels } from "./review-shared";

function AttemptHistory({ api, job }: { api: AlbumReviewAPI; job: AlbumJob }) {
  const msg = useMsg(),
    intl = useIntl(),
    labels = useAlbumLabels();
  const load = useCallback(
    async (after: number | undefined, signal: AbortSignal) => {
      const rows = await api.attempts(job.job_uuid, after, signal);
      return {
        signature: job.job_uuid,
        items: rows,
        header: null,
        next:
          rows.length === api.pageLimit ? (rows.at(-1)?.fence ?? null) : null,
      };
    },
    [api, job.job_uuid],
  );
  const result = useAlbumPages(load);
  return (
    <div className="flex flex-col gap-3">
      {result.busy && (
        <Spinner
          aria-label={msg("album_review.loading", "Loading album operations")}
        />
      )}
      {result.error !== undefined && (
        <AlbumReviewError error={result.error} retry={result.reload} />
      )}
      {result.data?.items.length === 0 && (
        <PostEmpty
          title={msg("album_review.no_attempts", "No worker attempts yet")}
        />
      )}
      {result.data?.items.map((attempt) => (
        <Card size="sm" key={attempt.fence}>
          <CardHeader>
            <CardTitle>
              {intl.formatMessage(
                {
                  id: "album_review.attempt",
                  defaultMessage: "Attempt {number, number}",
                },
                { number: attempt.fence },
              )}
            </CardTitle>
            <CardDescription>{labels.state[attempt.outcome]}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2 text-sm">
            <p>
              {intl.formatDate(attempt.started_at, {
                dateStyle: "medium",
                timeStyle: "short",
              })}
            </p>
            {attempt.error_code && (
              <code data-selectable-text className="wrap-anywhere text-xs">
                {attempt.error_code}
              </code>
            )}
          </CardContent>
        </Card>
      ))}
      {result.data?.next != null && (
        <Button
          type="button"
          variant="outline"
          disabled={result.busy || result.error !== undefined}
          onClick={() => void result.more()}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}

export function AlbumJobStatus({
  job,
  api,
  disabled,
  onRefresh,
  onCancel,
  onRetry,
}: {
  job: AlbumJob;
  api: AlbumReviewAPI;
  disabled: boolean;
  onRefresh: () => void;
  onCancel: () => void;
  onRetry: () => void;
}) {
  const msg = useMsg(),
    intl = useIntl(),
    labels = useAlbumLabels();
  const publication = job.publication;
  const active = job.state === "queued" || job.state === "running";
  const changed =
    publication &&
    (publication.created ||
      publication.selected > 0 ||
      publication.added > 0 ||
      publication.removed > 0);
  return (
    <Card data-album-job>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          {intl.formatMessage(
            {
              id: "album_review.job",
              defaultMessage: "Album job {number, number}",
            },
            { number: job.sequence },
          )}
          <Badge variant={job.state === "failed" ? "destructive" : "outline"}>
            {labels.state[job.state]}
          </Badge>
        </CardTitle>
        <CardDescription>{labels.policy[job.policy]}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {publication ? (
          <Alert>
            <AlertTitle>
              {changed
                ? msg("album_review.saved", "Album changes saved")
                : msg(
                    "album_review.saved_noop",
                    "No album changes were needed",
                  )}
            </AlertTitle>
            <AlertDescription className="flex flex-col gap-2">
              <p>
                {intl.formatMessage(
                  {
                    id: "album_review.published_counts",
                    defaultMessage:
                      "{selected, number} attachment links saved; {added, number} gallery members added; {removed, number} removed.",
                  },
                  publication,
                )}
              </p>
              {(publication.review > 0 || publication.unavailable > 0) && (
                <p>
                  {intl.formatMessage(
                    {
                      id: "album_review.unresolved_counts",
                      defaultMessage:
                        "{review, number} attachments still need review; {unavailable, number} have no current match.",
                    },
                    publication,
                  )}
                </p>
              )}
              <p>
                {job.hooks_finished
                  ? msg(
                      "album_review.notifications_done",
                      "Notification delivery finished.",
                    )
                  : active
                    ? msg(
                        "album_review.notifications_pending",
                        "Plugin notifications are still pending. Saved changes remain in the library.",
                      )
                    : msg(
                        "album_review.notifications_stopped",
                        "Notification delivery did not finish. Retry resumes delivery without applying the gallery changes again.",
                      )}
              </p>
            </AlertDescription>
          </Alert>
        ) : (
          <p className="text-sm text-muted-foreground">
            {msg(
              "album_review.not_committed",
              "This job has not committed album changes.",
            )}
          </p>
        )}
        {job.error_code === "album_preview_changed" && (
          <p className="text-sm text-muted-foreground">
            {msg(
              "album_review.job_stale",
              "Source evidence or library choices changed before this job applied. Create a fresh preview to review the current changes.",
            )}
          </p>
        )}
        {active && (
          <p className="text-sm text-muted-foreground">
            {msg(
              "album_review.cancel_help",
              "Cancelling stops pending work. It does not undo saved changes or notifications already delivered.",
            )}
          </p>
        )}
        <p className="text-sm text-muted-foreground">
          {intl.formatMessage(
            {
              id: "album_review.attempt_count",
              defaultMessage:
                "{count, number} worker attempts; {failures, number} failures out of {limit, number} allowed.",
            },
            {
              count: job.attempts,
              failures: job.failures,
              limit: job.max_attempts,
            },
          )}
        </p>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={disabled}
            onClick={onRefresh}
          >
            {msg("album_review.refresh", "Refresh album status")}
          </Button>
          {active && (
            <Button
              type="button"
              variant="outline"
              disabled={disabled}
              onClick={onCancel}
            >
              {msg("album_review.cancel_job", "Cancel pending work")}
            </Button>
          )}
          {(job.state === "failed" || job.state === "cancelled") && (
            <Button
              type="button"
              variant="outline"
              disabled={disabled}
              onClick={onRetry}
            >
              {publication
                ? msg(
                    "album_review.retry_notifications",
                    "Retry notification delivery",
                  )
                : msg("album_review.retry_job", "Retry album job")}
            </Button>
          )}
        </div>
        <PostSection title={msg("album_review.attempts", "Worker attempts")}>
          <AttemptHistory
            key={`${job.job_uuid}:${job.revision}`}
            api={api}
            job={job}
          />
        </PostSection>
        <PostSection title={msg("album_review.job_details", "Job details")}>
          <dl className="grid gap-2 text-sm">
            <dt className="text-muted-foreground">
              {msg("album_review.updated", "Last updated")}
            </dt>
            <dd>
              {intl.formatDate(job.updated_at, {
                dateStyle: "medium",
                timeStyle: "short",
              })}
            </dd>
            <dt className="text-muted-foreground">
              {msg("album_review.job_id", "Job identity")}
            </dt>
            <dd className="wrap-anywhere text-xs" data-selectable-text>
              {job.job_uuid}
            </dd>
            {job.error_code && (
              <>
                <dt className="text-muted-foreground">
                  {msg("album_review.failure_code", "Failure code")}
                </dt>
                <dd className="wrap-anywhere text-xs" data-selectable-text>
                  {job.error_code}
                </dd>
              </>
            )}
          </dl>
        </PostSection>
      </CardContent>
    </Card>
  );
}

export function AlbumJobHistory({
  api,
  post,
  disabled,
  onChoose,
}: {
  api: AlbumReviewAPI;
  post: string;
  disabled: boolean;
  onChoose: (job: AlbumJob) => void;
}) {
  const msg = useMsg(),
    intl = useIntl(),
    labels = useAlbumLabels();
  const load = useCallback(
    async (after: number | undefined, signal: AbortSignal) => {
      const rows = await api.history(post, after, signal);
      return {
        signature: post,
        items: rows,
        header: null,
        next:
          rows.length === api.pageLimit
            ? (rows.at(-1)?.sequence ?? null)
            : null,
      };
    },
    [api, post],
  );
  const result = useAlbumPages(load);
  return (
    <div className="flex flex-col gap-3">
      {result.busy && (
        <Spinner
          aria-label={msg("album_review.loading", "Loading album operations")}
        />
      )}
      {result.error !== undefined && (
        <AlbumReviewError error={result.error} retry={result.reload} />
      )}
      {result.data?.items.length === 0 && (
        <PostEmpty
          title={msg("album_review.no_jobs", "No album jobs for this post")}
        />
      )}
      {result.data?.items.map((job) => (
        <Card key={job.job_uuid} size="sm">
          <CardHeader>
            <CardTitle>
              {intl.formatMessage(
                {
                  id: "album_review.job",
                  defaultMessage: "Album job {number, number}",
                },
                { number: job.sequence },
              )}
            </CardTitle>
            <CardDescription>
              {labels.state[job.state]} ·{" "}
              {intl.formatDate(job.created_at, {
                dateStyle: "medium",
                timeStyle: "short",
              })}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button
              type="button"
              variant="outline"
              disabled={disabled}
              onClick={() => onChoose(job)}
            >
              {msg("album_review.inspect_job", "Inspect job")}
            </Button>
          </CardContent>
        </Card>
      ))}
      {result.data?.next != null && (
        <Button
          type="button"
          variant="outline"
          disabled={result.busy || result.error !== undefined}
          onClick={() => void result.more()}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
      <Button
        type="button"
        variant="outline"
        disabled={result.busy}
        onClick={result.reload}
      >
        {msg("album_review.refresh_history", "Refresh job history")}
      </Button>
    </div>
  );
}
