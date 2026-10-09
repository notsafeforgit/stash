import { useCallback, useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type { ArchiveActivityAPI } from "@/core/native-archive/activity-api";
import type {
  JobActivityDetail,
  RunActivityDetail,
} from "@/core/native-archive/activity-schema";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { PostSection, PostURL } from "../posts/shared";
import {
  ActivityError,
  ActivityState,
  ActivityTime,
  useActivityLabels,
  useActivityRead,
} from "./shared";
import { ActivitySubjectLink } from "./subjects";
import { AttemptHistory } from "./attempts";

function RunWindows({ detail }: { detail: RunActivityDetail }) {
  const msg = useMsg();
  const traversal = [
    detail.window,
    ...detail.pending,
    ...detail.completed,
  ].some((window) => window?.basis === "traversal");
  const groups = [
    {
      label: traversal
        ? msg("archive_activity.current_scan", "Current scan")
        : msg("archive_activity.current_window", "Current time window"),
      rows: detail.window ? [detail.window] : [],
    },
    {
      label: traversal
        ? msg("archive_activity.pending_scans", "Pending scans")
        : msg("archive_activity.pending_windows", "Pending time windows"),
      rows: detail.pending,
    },
    {
      label: traversal
        ? msg("archive_activity.completed_scans", "Completed scans")
        : msg("archive_activity.completed_windows", "Completed time windows"),
      rows: detail.completed,
    },
  ];
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        {traversal
          ? msg(
              "archive_activity.scans_help",
              "A scan follows the source profile's filters and stopping rules. Its timestamp is the request time. Completion does not establish publication-date coverage or a complete historical backfill.",
            )
          : msg(
              "archive_activity.windows_help",
              "A window includes its start and stops just before its end. Finishing one window does not finish a run with other windows pending.",
            )}
      </p>
      {groups.map((group) => (
        <div key={group.label} className="flex flex-col gap-2">
          <h3 className="font-medium">{group.label}</h3>
          {group.rows.length ? (
            group.rows.map((row) => (
              <p
                key={`${row.basis}:${row.since}:${row.until}`}
                className="flex flex-wrap items-center gap-2 text-sm"
              >
                {row.basis === "traversal" ? (
                  <Badge variant="secondary">
                    {msg("archive_activity.scan_requested", "Scan requested")}
                  </Badge>
                ) : (
                  <>
                    {row.since ? (
                      <ActivityTime value={row.since} />
                    ) : (
                      msg("archive_activity.all_history", "All earlier history")
                    )}{" "}
                    —{" "}
                  </>
                )}
                <ActivityTime value={row.until} />
              </p>
            ))
          ) : (
            <p className="text-sm text-muted-foreground">
              {msg("archive_activity.none", "None")}
            </p>
          )}
        </div>
      ))}
    </div>
  );
}

export function ActivityDetail({
  api,
  type,
  id,
  onBack,
}: {
  api: ArchiveActivityAPI;
  type: "jobs" | "runs";
  id: string;
  onBack: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useActivityLabels();
  const [refresh, setRefresh] = useState(0);
  const load = useCallback(
    (signal: AbortSignal): Promise<JobActivityDetail | RunActivityDetail> =>
      type === "jobs" ? api.job(id, signal) : api.run(id, signal),
    [api, type, id],
  );
  const read = useActivityRead(JSON.stringify([type, id]), load, refresh);
  const current = read.current;
  const summary = current?.summary;
  const running = summary?.state === "running" || summary?.state === "queued";
  // biome-ignore lint/correctness/useExhaustiveDependencies: Schedule another read only after the preceding refresh completes.
  useEffect(() => {
    if (!running || read.busy || read.error !== undefined) return;
    const timer = setTimeout(() => setRefresh((n) => n + 1), 5000);
    return () => clearTimeout(timer);
  }, [running, read.busy, read.error, refresh]);
  const job = current && "subjects" in current ? current : undefined;
  const run = current && "pending" in current ? current : undefined;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={onBack}>
          {msg("archive_activity.back", "Back to activity")}
        </Button>
        <Button
          variant="outline"
          disabled={read.busy}
          onClick={() => setRefresh((n) => n + 1)}
        >
          {msg("archive_activity.refresh", "Refresh")}
        </Button>
      </div>
      {read.busy && (
        <Spinner
          aria-label={msg("archive_activity.loading", "Loading activity")}
        />
      )}
      {read.error !== undefined && (
        <ActivityError retry={() => setRefresh((n) => n + 1)} />
      )}
      {summary && (
        <Card>
          <CardHeader>
            <CardTitle>
              {job
                ? labels.kinds[job.summary.kind]
                : run?.summary.collection_label}
            </CardTitle>
            <CardDescription>
              <ActivityTime value={summary.created_at} />
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-2">
              <ActivityState state={summary.state} />
              <span className="text-sm">
                {intl.formatMessage(
                  {
                    id: "archive_activity.attempt_count",
                    defaultMessage:
                      "{count, plural, one {# attempt} other {# attempts}}",
                  },
                  { count: summary.attempt_count },
                )}
              </span>
            </div>
            {summary.error_code && (
              <Alert variant="destructive">
                <AlertTitle>
                  {msg("archive_activity.last_error", "Last reported error")}
                </AlertTitle>
                <AlertDescription>
                  <code className="wrap-anywhere">{summary.error_code}</code>
                </AlertDescription>
              </Alert>
            )}
            {job && (
              <>
                {job.relative_path && (
                  <p data-selectable-text className="wrap-anywhere">
                    {job.relative_path}
                  </p>
                )}
                {!job.context_available && (
                  <Alert>
                    <AlertTitle>
                      {msg(
                        "archive_activity.context_unavailable",
                        "Some job details are unavailable",
                      )}
                    </AlertTitle>
                    <AlertDescription>
                      {msg(
                        "archive_activity.context_help",
                        "Status and attempt history are still available. The original job may use a format this screen cannot describe.",
                      )}
                    </AlertDescription>
                  </Alert>
                )}
                <div className="flex flex-wrap items-start gap-2">
                  {job.subjects.map((subject) => (
                    <ActivitySubjectLink
                      key={`${subject.kind}:${subject.uuid}:${subject.revision}`}
                      subject={subject}
                    />
                  ))}
                </div>
              </>
            )}
            {run && (
              <>
                <PostURL value={run.summary.target_url} />
                <p>
                  {run.summary.operation === "download"
                    ? msg("archive_activity.download", "Download media")
                    : msg(
                        "archive_activity.metadata_only",
                        "Fetch metadata only",
                      )}
                </p>
                <p>
                  {intl.formatMessage(
                    {
                      id: "archive_activity.run_progress",
                      defaultMessage:
                        "{items} items seen · {files} files completed",
                    },
                    {
                      items: run.progress.items_seen,
                      files: run.progress.files_completed,
                    },
                  )}
                </p>
                <div>
                  <Link
                    className={buttonVariants({ variant: "outline" })}
                    to="/collections"
                    search={{
                      q: "",
                      state: "",
                      kind: "",
                      collection:
                        run.summary.canonical_collection_uuid ??
                        run.summary.collection_uuid,
                    }}
                  >
                    {msg("archive_activity.open_collection", "Open collection")}
                  </Link>
                </div>
                <PostSection
                  title={msg("archive_activity.coverage", "Scrape coverage")}
                >
                  <RunWindows detail={run} />
                </PostSection>
              </>
            )}
            <PostSection
              title={msg("archive_activity.attempt_history", "Attempt history")}
            >
              <AttemptHistory
                api={api}
                type={type}
                id={id}
                revision={summary.revision}
              />
            </PostSection>
            <PostSection title={msg("archive_activity.details", "Job details")}>
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
                <dt>{msg("archive_activity.id", "Archive ID")}</dt>
                <dd>
                  <code className="wrap-anywhere">{summary.uuid}</code>
                </dd>
                <dt>{msg("archive_activity.updated", "Last updated")}</dt>
                <dd>
                  <ActivityTime value={summary.updated_at} />
                </dd>
                <dt>{msg("archive_activity.available", "Available after")}</dt>
                <dd>
                  <ActivityTime value={summary.available_at} />
                </dd>
                {summary.lease_until && (
                  <>
                    <dt>
                      {msg("archive_activity.lease", "Worker lease ends")}
                    </dt>
                    <dd>
                      <ActivityTime value={summary.lease_until} />
                    </dd>
                  </>
                )}
                {job && (
                  <>
                    <dt>
                      {msg("archive_activity.attempt_limit", "Attempt limit")}
                    </dt>
                    <dd>{job.summary.max_attempts}</dd>
                  </>
                )}
                {run?.path_prefix && (
                  <>
                    <dt>
                      {msg(
                        "archive_activity.destination",
                        "Destination folder",
                      )}
                    </dt>
                    <dd data-selectable-text className="wrap-anywhere">
                      {run.path_prefix}
                    </dd>
                  </>
                )}
              </dl>
            </PostSection>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
