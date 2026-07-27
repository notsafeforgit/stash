import { useCallback, useEffect, useRef } from "react";
import { useQuery, useSubscription } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";

export type MonitoredJob = Pick<
  GQL.Job,
  "id" | "status" | "subTasks" | "description" | "progress" | "error"
>;

function isTerminalStatus(status: GQL.JobStatus) {
  return (
    status === GQL.JobStatus.Cancelled ||
    status === GQL.JobStatus.Failed ||
    status === GQL.JobStatus.Finished
  );
}

export function useMonitorJob(
  jobId: string | null,
  onComplete: (job?: MonitoredJob) => void | Promise<void>,
) {
  const completedJobIdRef = useRef<string | null>(null);

  useEffect(() => {
    if (jobId) completedJobIdRef.current = null;
  }, [jobId]);

  const finish = useCallback(
    (job?: MonitoredJob) => {
      if (!jobId || completedJobIdRef.current === jobId) return;
      completedJobIdRef.current = jobId;
      void onComplete(job);
    },
    [jobId, onComplete],
  );

  const { data, loading } = useQuery(GQL.FindJobDocument, {
    variables: { input: { id: jobId ?? "" } },
    skip: !jobId,
    fetchPolicy: "network-only",
    pollInterval: jobId ? 1000 : 0,
  });

  useEffect(() => {
    if (!jobId || loading || !data) return;
    const job = data.findJob;
    if (!job || isTerminalStatus(job.status)) finish(job ?? undefined);
  }, [data, finish, jobId, loading]);

  useSubscription(GQL.JobsLifecycleSubscribeDocument, {
    skip: !jobId,
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsLifecycleSubscribe;
      if (!event || event.job.id !== jobId) return;
      if (
        event.type === GQL.JobStatusUpdateType.Remove ||
        isTerminalStatus(event.job.status)
      ) {
        finish(event.job);
      }
    },
  });

  useSubscription(GQL.JobsProgressSubscribeDocument, {
    skip: !jobId,
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsProgressSubscribe;
      if (event?.job.id === jobId && isTerminalStatus(event.job.status)) {
        finish(event.job);
      }
    },
  });
}
