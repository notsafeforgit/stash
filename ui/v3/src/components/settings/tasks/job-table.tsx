import { useEffect, useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { useMutation, useQuery, useSubscription } from "@apollo/client/react";
import { Ban, Check, CircleAlert, Cog, Hourglass, X } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Progress } from "src/components/ui/progress";
import { ScrollArea } from "src/components/ui/scroll-area";
import { cn } from "src/lib/utils";
import { humanizeSeconds } from "src/utils/duration";

type JobFragment = GQL.JobDataFragment;

function statusIcon(status: GQL.JobStatus) {
  switch (status) {
    case GQL.JobStatus.Ready:
      return <Hourglass className="size-4 shrink-0" />;
    case GQL.JobStatus.Running:
    case GQL.JobStatus.Stopping:
      return <Cog className="size-4 shrink-0 animate-spin" />;
    case GQL.JobStatus.Finished:
      return <Check className="size-4 shrink-0 text-green-500" />;
    case GQL.JobStatus.Cancelled:
      return <Ban className="size-4 shrink-0 text-muted-foreground" />;
    case GQL.JobStatus.Failed:
      return <CircleAlert className="size-4 shrink-0 text-destructive" />;
  }
}

function Task({ job }: { job: JobFragment }) {
  const [stopping, setStopping] = useState(false);
  const [fadeState, setFadeState] = useState<"in" | "out" | null>(null);
  const [stopJob] = useMutation(GQL.StopJobDocument);

  useEffect(() => {
    const t = window.setTimeout(() => setFadeState("in"), 0);
    return () => window.clearTimeout(t);
  }, []);

  useEffect(() => {
    if (
      job.status === GQL.JobStatus.Cancelled ||
      job.status === GQL.JobStatus.Failed ||
      job.status === GQL.JobStatus.Finished
    ) {
      const t = window.setTimeout(() => setFadeState("out"), 9800);
      return () => window.clearTimeout(t);
    }
  }, [job.status]);

  const canStop =
    !stopping &&
    (job.status === GQL.JobStatus.Ready ||
      job.status === GQL.JobStatus.Running);

  async function onStop() {
    setStopping(true);
    await stopJob({ variables: { job_id: job.id } });
  }

  const rawProgress =
    job.status === GQL.JobStatus.Running &&
    job.progress !== undefined &&
    job.progress !== null
      ? Math.max(0, Math.min(1, job.progress))
      : null;
  const progressPct =
    rawProgress !== null ? Math.min(99, Math.floor(rawProgress * 100)) : null;
  const finalizing = rawProgress !== null && rawProgress >= 1;

  let eta: string | null = null;
  if (
    job.status === GQL.JobStatus.Running &&
    job.startTime &&
    job.progress !== null &&
    job.progress !== undefined &&
    job.progress > 0
  ) {
    const nowMs = Date.now();
    const startMs = new Date(job.startTime).valueOf();
    if (job.progress < 1 && startMs <= nowMs) {
      const elapsedMs = nowMs - startMs;
      const remainingMs = (elapsedMs * (1 - job.progress)) / job.progress;
      eta = humanizeSeconds(remainingMs / 1000);
    }
  }

  const showSubtasks =
    job.status === GQL.JobStatus.Running ||
    job.status === GQL.JobStatus.Stopping;

  return (
    <li
      className={cn(
        "border-b px-3 py-2 transition-opacity duration-500 last:border-b-0",
        fadeState === null && "opacity-0",
        fadeState === "in" && "opacity-100",
        fadeState === "out" && "opacity-0",
      )}
    >
      {/* Title row — X + status icon + description share one items-center
          flex so they line up vertically regardless of the button's
          intrinsic height. */}
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={onStop}
          disabled={!canStop}
          aria-label="Stop job"
        >
          <X className="size-4" />
        </Button>
        {statusIcon(job.status)}
        <span
          className="min-w-0 flex-1 truncate text-sm"
          title={job.description}
        >
          {job.description}
        </span>
        {finalizing ? (
          <span className="shrink-0 text-xs text-muted-foreground">
            <FormattedMessage id="job.finalizing" defaultMessage="Finalizing" />
          </span>
        ) : eta ? (
          <span className="shrink-0 text-xs text-muted-foreground">
            <FormattedMessage id="eta" defaultMessage="ETA" />: {eta}
          </span>
        ) : null}
      </div>

      {/* Progress + subtasks + error sit full-width below the title row.
          (Earlier they were indented past the X button via `pl-9`, but
          that left the scroll area asymmetric — pl-9 on one side, only
          the li's px-3 on the other — so subtle visual lopsidedness.) */}
      {(progressPct !== null ||
        showSubtasks ||
        (job.status === GQL.JobStatus.Failed && job.error)) && (
        <div className="mt-1 space-y-1">
          {progressPct !== null && (
            <div className="flex items-center gap-2">
              <Progress value={progressPct} className="flex-1" />
              <span className="w-10 text-right text-xs tabular-nums text-muted-foreground">
                {progressPct}%
              </span>
            </div>
          )}
          {/* Fixed-height scrollable box: worker subtasks come and go
              while the job is running, and at variable text lengths.
              Containing them inside a constant-height region keeps the
              outer queue layout stable instead of reflowing on every
              tick. */}
          {showSubtasks && (
            <ScrollArea className="h-64 rounded-md bg-muted/30">
              <div className="space-y-1 p-2">
                {(job.subTasks ?? []).map((t, i) => (
                  <div
                    key={i}
                    className="text-xs break-all text-muted-foreground"
                    title={t}
                  >
                    {t}
                  </div>
                ))}
              </div>
            </ScrollArea>
          )}
          {job.status === GQL.JobStatus.Failed && job.error && (
            <div className="text-xs text-destructive">{job.error}</div>
          )}
        </div>
      )}
    </li>
  );
}

export function JobTable() {
  const intl = useIntl();
  const { data } = useQuery(GQL.JobQueueDocument, {
    fetchPolicy: "cache-and-network",
  });
  const [queue, setQueue] = useState<JobFragment[]>([]);

  useEffect(() => {
    if (!data?.jobQueue) return;
    // Merge rather than replace. In normal use the initial query fires
    // once and seeds the queue; the subscriptions take over from there.
    // But if some future caller triggers a refetch of JobQueue (Apollo
    // cache invalidation, navigation re-mount, etc.) we don't want to
    // wipe out "ghost" entries — terminal-status jobs still inside the
    // 10 s fade-out window after their lifecycle Remove arrived.
    const serverJobs = data.jobQueue;
    setQueue((q) => {
      const serverIds = new Set(serverJobs.map((j) => j.id));
      const ghosts = q.filter((j) => !serverIds.has(j.id));
      return [...serverJobs, ...ghosts];
    });
  }, [data]);

  // Two-subscription design (mirrors the backend split):
  //
  //   - JobsLifecycleSubscribe streams ADD / REMOVE only. The server
  //     guarantees these are never dropped: they ride a dedicated
  //     pipeline that can't be starved by progress traffic. Anything
  //     correctness-sensitive (does this job exist? is it gone?) reads
  //     from here.
  //   - JobsProgressSubscribe streams UPDATE only. The server drops
  //     these under backpressure by design — each tick supersedes the
  //     prior one, so a missed tick just means the next one re-syncs us.
  //
  // `onData` is used (not the hook's `data` return) because Apollo
  // coalesces `data` to the latest event between renders, which would
  // lose intermediate events arriving back-to-back. `onData` fires once
  // per subscription payload.
  useSubscription(GQL.JobsLifecycleSubscribeDocument, {
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsLifecycleSubscribe;
      if (!event) return;
      setQueue((q) => {
        switch (event.type) {
          case GQL.JobStatusUpdateType.Add:
            if (q.some((j) => j.id === event.job.id)) return q;
            return q.concat([event.job as JobFragment]);
          case GQL.JobStatusUpdateType.Remove: {
            // Apply the final status immediately so the row can
            // render its terminal icon (✓ / ✗ / 🚫), then evict
            // after 10 s so the user has a moment to read the
            // outcome before it disappears.
            const next = q.map((j) =>
              j.id === event.job.id ? (event.job as JobFragment) : j,
            );
            window.setTimeout(() => {
              setQueue((current) =>
                current.filter((j) => j.id !== event.job.id),
              );
            }, 10000);
            return next;
          }
          default:
            // The server filters out UPDATE on this channel, but be
            // defensive: drop any unexpected type silently rather
            // than crashing the reducer.
            return q;
        }
      });
    },
  });

  useSubscription(GQL.JobsProgressSubscribeDocument, {
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsProgressSubscribe;
      if (!event) return;
      setQueue((q) =>
        // Only patch jobs we already know about. If the lifecycle
        // ADD for this id hasn't landed yet (or it landed and was
        // since removed), ignore the tick — the next ADD will be
        // sourced from the lifecycle stream, not from here.
        q.map((j) => (j.id === event.job.id ? (event.job as JobFragment) : j)),
      );
    },
  });

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      {queue.length === 0 ? (
        <div className="px-4 py-6 text-center text-sm text-muted-foreground">
          {intl.formatMessage({
            id: "config.tasks.empty_queue",
            defaultMessage: "No tasks are currently running.",
          })}
        </div>
      ) : (
        <ScrollArea viewportClassName="max-h-[60vh]">
          <ul className="divide-y">
            {queue.map((j) => (
              <Task key={j.id} job={j} />
            ))}
          </ul>
        </ScrollArea>
      )}
    </div>
  );
}
