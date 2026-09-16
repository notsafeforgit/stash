import type { ApolloClient } from "@apollo/client";
import {
  FindJobDocument,
  JobStatus,
  type FindJobQuery,
} from "./generated-graphql";

export type JobCompletion =
  | { kind: "complete"; job: FindJobQuery["findJob"] }
  | { kind: "unavailable"; error: unknown };

/** Job-owned, not component-owned: navigation must not abandon cache updates.
 * Retry transient failures, then release polling/timers on every terminal path.
 * The caller can explicitly dispose a monitor it no longer needs. */
export function monitorJobCompletion(
  client: ApolloClient,
  id: string,
  onComplete: (result: JobCompletion) => void,
): () => void {
  let disposed = false;
  let failures = 0;
  let stopAttempt: (() => void) | undefined;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;

  function dispose() {
    if (disposed) return;
    disposed = true;
    clearTimeout(retryTimer);
    stopAttempt?.();
  }
  function finish(result: JobCompletion) {
    if (disposed) return;
    dispose();
    onComplete(result);
  }
  function start() {
    if (disposed) return;
    let settled = false;
    const query = client.watchQuery({
      query: FindJobDocument,
      variables: { input: { id } },
      fetchPolicy: "network-only",
      pollInterval: 1000,
    });
    function failed(error: unknown) {
      if (disposed || settled) return;
      settled = true;
      queueMicrotask(() => {
        if (disposed) return;
        stopAttempt?.();
        failures++;
        if (failures >= 3) {
          finish({ kind: "unavailable", error });
        } else retryTimer = setTimeout(start, 1000 * 2 ** (failures - 1));
      });
    }
    const subscription = query.subscribe({
      next: (result) => {
        if (disposed || settled || result.loading) return;
        if (result.error) {
          failed(result.error);
          return;
        }
        if (!result.data || result.dataState !== "complete") {
          failed(new Error("Incomplete job response"));
          return;
        }
        failures = 0;
        const job = result.data.findJob;
        if (
          job &&
          ![JobStatus.Finished, JobStatus.Failed, JobStatus.Cancelled].includes(
            job.status,
          )
        )
          return;
        settled = true;
        queueMicrotask(() => finish({ kind: "complete", job }));
      },
      error: failed,
    });
    stopAttempt = () => {
      query.stopPolling();
      subscription.unsubscribe();
    };
  }
  start();
  return dispose;
}
