import { useCallback, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { useSourceTracking } from "@/hooks/use-source-tracking";
import { createArchiveActivityAPI } from "@/core/native-archive/activity-api";
import {
  archivePageInfoPath,
  createArchivePaginationAPI,
} from "@/core/native-archive/pagination-api";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import {
  ActivityError,
  ActivityTime,
  useActivityLabels,
  useActivityRead,
} from "@/components/archive/activity/shared";
import { useActivityPolling } from "@/components/archive/activity/use-activity-polling";

const states = ["running", "queued", "deferred"] as const;

function ScrapeTaskStatus() {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useActivityLabels();
  const [api] = useState(() => createArchiveActivityAPI());
  const [counts] = useState(() => createArchivePaginationAPI());
  const [refresh, setRefresh] = useState(0);
  const reload = useCallback(() => setRefresh((n) => n + 1), []);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const [running, ...totals] = await Promise.all([
        api.runs({ collection: "", state: "running" }, 0, signal),
        ...states.map((state) =>
          counts(archivePageInfoPath({ resource: "runs", state }), signal),
        ),
      ]);
      return { running: running.slice(0, 5), totals };
    },
    [api, counts],
  );
  const read = useActivityRead("scrape-tasks", load, refresh);
  useActivityPolling(true, read.busy, read.error !== undefined, reload);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{msg("archive_activity.runs", "Scrape runs")}</CardTitle>
        <CardDescription>
          {msg(
            "scrape_tasks.description",
            "Follow gallery-dl work submitted by scripts and scheduled scrapes. Open a run to see its progress and any errors.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {read.busy && !read.current && (
          <Spinner
            aria-label={msg("archive_activity.loading", "Loading activity")}
          />
        )}
        {read.error !== undefined && <ActivityError retry={reload} />}
        {read.current && (
          <>
            <div className="flex flex-wrap gap-2">
              {states.map((state, index) => (
                <Link
                  key={state}
                  to="/archive-activity"
                  search={{ view: "runs", state }}
                  className={buttonVariants({ variant: "outline", size: "sm" })}
                >
                  {intl.formatMessage(
                    {
                      id: "scrape_tasks.count",
                      defaultMessage: "{state}: {count, number}",
                    },
                    {
                      state: labels.states[state],
                      count: read.current?.totals[index]?.total ?? 0,
                    },
                  )}
                </Link>
              ))}
            </div>
            {read.current.running.length > 0 && (
              <div className="flex flex-col gap-3">
                {read.current.running.map((run) => (
                  <div key={run.uuid} className="flex flex-col gap-1">
                    <Link
                      to="/archive-activity"
                      search={{ view: "runs", item: run.uuid }}
                      className="underline underline-offset-4 wrap-anywhere"
                    >
                      {run.collection_label}
                    </Link>
                    <p className="text-sm text-muted-foreground">
                      {msg("archive_activity.updated", "Last updated")}:{" "}
                      <ActivityTime value={run.updated_at} />
                    </p>
                  </div>
                ))}
                <p className="text-sm text-muted-foreground">
                  {intl.formatMessage(
                    {
                      id: "scrape_tasks.showing",
                      defaultMessage:
                        "Showing {shown, number} of {total, number} running scrapes.",
                    },
                    {
                      shown: read.current.running.length,
                      total: read.current.totals[0]?.total ?? 0,
                    },
                  )}
                </p>
              </div>
            )}
            {read.current.running.length === 0 && (
              <p className="text-sm text-muted-foreground">
                {msg(
                  "scrape_tasks.idle",
                  "No scrape is currently running. Open all scrape runs for queued work and completed runs.",
                )}
              </p>
            )}
          </>
        )}
      </CardContent>
      <CardFooter className="flex flex-wrap gap-2">
        <Link
          to="/archive-activity"
          search={{ view: "runs" }}
          className={buttonVariants({ variant: "outline" })}
        >
          {msg("scrape_tasks.open", "View all scrape runs")}
        </Link>
        <Button variant="outline" onClick={reload} disabled={read.busy}>
          {msg("archive_activity.refresh", "Refresh")}
        </Button>
      </CardFooter>
    </Card>
  );
}

export function ScrapeTasks() {
  return useSourceTracking() ? <ScrapeTaskStatus /> : null;
}
