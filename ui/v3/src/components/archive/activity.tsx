import { useCallback, useState } from "react";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import { createArchiveActivityAPI } from "@/core/native-archive/activity-api";
import {
  activitySearchSchema,
  jobActivityFilterSchema,
  runActivityFilterSchema,
  type ActivitySearch,
  type JobActivity,
  type RunActivity,
} from "@/core/native-archive/activity-schema";
import { useListScrollRestoration } from "@/components/list/use-list-scroll-restoration";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { PostEmpty } from "./posts/shared";
import { ActivityFilters } from "./activity/filters";
import { ActivityDetail } from "./activity/detail";
import {
  ActivityError,
  ActivityState,
  ActivityTime,
  useActivityLabels,
  useActivityRead,
} from "./activity/shared";

export function ArchiveActivity({
  search,
  onChange,
}: {
  search: ActivitySearch;
  onChange: (value: ActivitySearch) => void;
}) {
  const msg = useMsg();
  const labels = useActivityLabels();
  const [api] = useState(() => createArchiveActivityAPI());
  const [before, setBefore] = useState(0);
  const [previous, setPrevious] = useState<number[]>([]);
  const [refresh, setRefresh] = useState(0);
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const { view, kind, state, collection, item } = search;
  const key = JSON.stringify([view, kind, state, collection, before]);
  const load = useCallback(
    (signal: AbortSignal): Promise<Array<JobActivity | RunActivity>> =>
      view === "jobs"
        ? api.jobs(
            jobActivityFilterSchema.parse({ kind, state }),
            before,
            signal,
          )
        : api.runs(
            runActivityFilterSchema.parse({ collection, state }),
            before,
            signal,
          ),
    [api, view, kind, state, collection, before],
  );
  const read = useActivityRead(key, load, refresh);
  useListScrollRestoration(
    "archive-activity",
    scroller,
    !item && !!read.current && !read.busy,
    key,
  );
  return (
    <div
      ref={setScroller}
      className="min-h-0 flex-1 overflow-y-auto"
      data-scroll-restoration-id="archive-activity"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        <header className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold">
            {msg("archive_activity.title", "Archive activity")}
          </h1>
          <p className="text-muted-foreground">
            {msg(
              "archive_activity.description",
              "Inspect scrape runs and background work, including pending steps and retry history.",
            )}
          </p>
        </header>
        <ToggleGroup
          aria-label={msg("archive_activity.view", "Activity view")}
          variant="outline"
          value={[view]}
          onValueChange={(values) => {
            const view = values[0];
            if (view) onChange(activitySearchSchema.parse({ view }));
          }}
        >
          <ToggleGroupItem value="jobs">
            {msg("archive_activity.jobs", "Background jobs")}
          </ToggleGroupItem>
          <ToggleGroupItem value="runs">
            {msg("archive_activity.runs", "Scrape runs")}
          </ToggleGroupItem>
        </ToggleGroup>
        {item && (
          <ActivityDetail
            key={`${view}:${item}`}
            api={api}
            type={view}
            id={item}
            onBack={() => onChange({ ...search, item: undefined })}
          />
        )}
        <div className={cn("flex flex-col gap-6", item && "hidden")}>
          <ActivityFilters value={search} onChange={onChange} />
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-sm text-muted-foreground">
              {msg("archive_activity.newest_first", "Newest first")}
            </p>
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
          {read.current?.map((row) => (
            <Card key={row.uuid}>
              <CardHeader>
                <CardTitle
                  data-selectable-text={
                    "collection_label" in row ? true : undefined
                  }
                >
                  {"kind" in row
                    ? labels.kinds[row.kind]
                    : row.collection_label}
                </CardTitle>
                <CardDescription>
                  <ActivityTime value={row.created_at} />
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                <div>
                  <ActivityState state={row.state} />
                </div>
                {"operation" in row && (
                  <p className="text-sm">
                    {row.operation === "download"
                      ? msg("archive_activity.download", "Download media")
                      : msg(
                          "archive_activity.metadata_only",
                          "Fetch metadata only",
                        )}
                  </p>
                )}
                {row.error_code && (
                  <code className="wrap-anywhere">{row.error_code}</code>
                )}
              </CardContent>
              <CardFooter>
                <Button
                  variant="outline"
                  onClick={() => onChange({ ...search, item: row.uuid })}
                >
                  {msg("archive_activity.open", "Inspect activity")}
                </Button>
              </CardFooter>
            </Card>
          ))}
          {!read.busy &&
            read.error === undefined &&
            read.current?.length === 0 && (
              <PostEmpty
                title={msg("archive_activity.empty", "No matching activity")}
              >
                {msg(
                  "archive_activity.empty_help",
                  "Try another status, job type or source collection.",
                )}
              </PostEmpty>
            )}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={read.busy || !previous.length}
              onClick={() => {
                setBefore(previous.at(-1) ?? 0);
                setPrevious((values) => values.slice(0, -1));
              }}
            >
              {msg("archive_activity.newer", "Newer")}
            </Button>
            <Button
              variant="outline"
              disabled={
                read.busy ||
                read.error !== undefined ||
                read.current?.length !== api.pageLimit
              }
              onClick={() => {
                const row = read.current?.at(-1);
                if (row) {
                  setPrevious((values) => [...values, before]);
                  setBefore(row.sequence);
                }
              }}
            >
              {msg("archive_activity.older", "Older")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
