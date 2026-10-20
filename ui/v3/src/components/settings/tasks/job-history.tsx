import { useState } from "react";
import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import { JobHistoryDocument, JobStatus } from "@/core/generated-graphql";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

const statusMessages = {
  [JobStatus.Ready]: { id: "job.status.ready", defaultMessage: "Queued" },
  [JobStatus.Running]: { id: "job.status.running", defaultMessage: "Running" },
  [JobStatus.Stopping]: {
    id: "job.status.stopping",
    defaultMessage: "Stopping",
  },
  [JobStatus.Finished]: {
    id: "job.status.finished",
    defaultMessage: "Completed",
  },
  [JobStatus.Cancelled]: {
    id: "job.status.cancelled",
    defaultMessage: "Cancelled",
  },
  [JobStatus.Failed]: { id: "job.status.failed", defaultMessage: "Failed" },
  [JobStatus.Paused]: { id: "job.status.paused", defaultMessage: "Paused" },
  [JobStatus.Interrupted]: {
    id: "job.status.interrupted",
    defaultMessage: "Interrupted",
  },
};
const pageSize = 25;

export function JobHistory() {
  const intl = useIntl();
  const [offset, setOffset] = useState(0);
  const { data, loading, error, refetch } = useQuery(JobHistoryDocument, {
    variables: { limit: pageSize, offset },
    fetchPolicy: "network-only",
    pollInterval: 5000,
  });
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-base font-medium">
          {intl.formatMessage({
            id: "job.history",
            defaultMessage: "Recent library tasks",
          })}
        </h2>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void refetch()}
          disabled={loading}
        >
          {intl.formatMessage({
            id: "actions.refresh",
            defaultMessage: "Refresh",
          })}
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        {intl.formatMessage({
          id: "job.history_help",
          defaultMessage:
            "The last 1,000 outcomes are retained. Bulk edits resume after a server restart. Paused bulk edits retain their progress and can be cancelled. Other interrupted tasks must be reviewed before starting again.",
        })}
      </p>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      )}
      <ul className="divide-y rounded-lg border bg-card" aria-busy={loading}>
        {data?.jobHistory.map((job) => (
          <li key={job.id} className="space-y-1 p-3">
            <div className="flex justify-between gap-3 text-sm">
              <span className="break-words">{job.description}</span>
              <span className="shrink-0">
                {intl.formatMessage(statusMessages[job.status])}
              </span>
            </div>
            <time
              dateTime={job.addTime}
              className="text-xs text-muted-foreground"
            >
              {intl.formatDate(job.addTime, {
                dateStyle: "medium",
                timeStyle: "short",
              })}
            </time>
            {job.error && (
              <p className="text-sm text-destructive">{job.error}</p>
            )}
          </li>
        ))}
        {!loading && data?.jobHistory.length === 0 && (
          <li className="p-3 text-sm text-muted-foreground">
            {intl.formatMessage({
              id: "job.history_empty",
              defaultMessage: "No recorded tasks.",
            })}
          </li>
        )}
      </ul>
      <div className="flex justify-end gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={loading || offset === 0}
          onClick={() => setOffset(Math.max(0, offset - pageSize))}
        >
          {intl.formatMessage({
            id: "actions.previous",
            defaultMessage: "Previous",
          })}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={loading || data?.jobHistory.length !== pageSize}
          onClick={() => setOffset(offset + pageSize)}
        >
          {intl.formatMessage({ id: "actions.next", defaultMessage: "Next" })}
        </Button>
      </div>
    </section>
  );
}
