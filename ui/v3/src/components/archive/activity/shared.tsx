import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

export function useActivityLabels() {
  const msg = useMsg();
  return {
    kinds: {
      "media.verify": msg("archive_activity.verify", "Import media"),
      "album.backfill": msg("archive_activity.album", "Build source album"),
      "text.translate": msg(
        "archive_activity.translate",
        "Translate source text",
      ),
      "post.enrich": msg("archive_activity.enrich", "Fetch post metadata"),
      "account.list_page": msg(
        "archive_activity.list",
        "Search source history",
      ),
      "post.verify_candidate": msg(
        "archive_activity.verify_candidate",
        "Check post match",
      ),
      "post.merge_notify": msg(
        "archive_activity.notify_merge",
        "Deliver post-merge notifications",
      ),
    },
    states: {
      queued: msg("archive_activity.queued", "Queued"),
      running: msg("archive_activity.running", "Running"),
      succeeded: msg("archive_activity.succeeded", "Finished"),
      failed: msg("archive_activity.failed", "Failed"),
      cancelled: msg("archive_activity.cancelled", "Cancelled"),
      deferred: msg("archive_activity.deferred", "Deferred"),
      retry: msg("archive_activity.retry", "Retry scheduled"),
      expired: msg("archive_activity.expired", "Worker lease expired"),
    },
  };
}

export function ActivityState({
  state,
}: {
  state: keyof ReturnType<typeof useActivityLabels>["states"];
}) {
  const labels = useActivityLabels();
  return (
    <Badge variant={state === "failed" ? "destructive" : "secondary"}>
      {labels.states[state]}
    </Badge>
  );
}

export function ActivityTime({ value }: { value: string }) {
  const intl = useIntl();
  return (
    <time dateTime={value}>
      {intl.formatDate(value, { dateStyle: "medium", timeStyle: "short" })}
    </time>
  );
}

export function ActivityError({ retry }: { retry: () => void }) {
  const msg = useMsg();
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {msg("archive_activity.read_failed", "Could not load activity")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {msg(
            "archive_activity.read_failed_help",
            "Check your connection and access to Stash, then retry.",
          )}
        </p>
        <Button variant="outline" size="sm" onClick={retry}>
          {msg("actions.retry", "Retry")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

// Keep a successful same-scope read visible during refresh. A different filter
// or selected item must never inherit the previous response or late errors.
export function useActivityRead<T>(
  key: string,
  load: (signal: AbortSignal) => Promise<T>,
  refresh: number,
) {
  const [data, setData] = useState<{ key: string; value: T }>();
  const [request, setRequest] = useState<{
    key: string;
    busy: boolean;
    error?: unknown;
  }>({ key, busy: true });
  // biome-ignore lint/correctness/useExhaustiveDependencies: Refresh deliberately reloads the same resource.
  useEffect(() => {
    const controller = new AbortController();
    setRequest({ key, busy: true });
    void load(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) {
          setData({ key, value });
          setRequest({ key, busy: false });
        }
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setRequest({ key, busy: false, error });
      });
    return () => controller.abort();
  }, [key, load, refresh]);
  return {
    current: data?.key === key ? data.value : undefined,
    busy: request.key !== key || request.busy,
    error: request.key === key ? request.error : undefined,
  };
}
