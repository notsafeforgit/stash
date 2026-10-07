import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type { ArchiveActivityAPI } from "@/core/native-archive/activity-api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { PostEmpty } from "../posts/shared";
import {
  ActivityError,
  ActivityState,
  ActivityTime,
  useActivityRead,
} from "./shared";

export function AttemptHistory({
  api,
  type,
  id,
  revision,
}: {
  api: ArchiveActivityAPI;
  type: "jobs" | "runs";
  id: string;
  revision: number;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [before, setBefore] = useState(0);
  const [previous, setPrevious] = useState<number[]>([]);
  const [refresh, setRefresh] = useState(0);
  const load = useCallback(
    (signal: AbortSignal) => api.attempts(type, id, before, signal),
    [api, type, id, before],
  );
  const read = useActivityRead(
    JSON.stringify([type, id, before]),
    load,
    refresh + revision,
  );
  return (
    <div className="flex flex-col gap-4">
      {read.busy && (
        <Spinner
          aria-label={msg("archive_activity.loading", "Loading activity")}
        />
      )}
      {read.error !== undefined && (
        <ActivityError retry={() => setRefresh((n) => n + 1)} />
      )}
      {read.current?.map((row) => (
        <div key={row.number} className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <span>
              {intl.formatMessage(
                {
                  id: "archive_activity.attempt",
                  defaultMessage: "Attempt {number}",
                },
                { number: row.number },
              )}
            </span>
            <ActivityState state={row.outcome} />
          </div>
          <div className="text-sm text-muted-foreground">
            <ActivityTime value={row.started_at} />
            {row.ended_at && (
              <>
                {" "}
                — <ActivityTime value={row.ended_at} />
              </>
            )}
          </div>
          {row.error_code && (
            <code className="wrap-anywhere">{row.error_code}</code>
          )}
        </div>
      ))}
      {!read.busy && !read.error && read.current?.length === 0 && (
        <PostEmpty
          title={msg("archive_activity.no_attempts", "No attempts yet")}
        />
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
              setBefore(row.number);
            }
          }}
        >
          {msg("archive_activity.older", "Older")}
        </Button>
      </div>
    </div>
  );
}
