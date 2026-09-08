import type { ApolloClient, DocumentNode } from "@apollo/client";
import { FindJobDocument, JobStatus } from "./generated-graphql";
import { affectedActiveQueries } from "./mutation-invalidation";

export type EntityJobAcknowledgment =
  | { kind: "completed" }
  | { kind: "scheduled"; id: string }
  | { kind: "invalid" };

/** The legacy scalar response is retained for v2.5. Only numeric IDs may be
 * sent to findJob; selected-ID operations can finish synchronously. */
export function decodeEntityJobAcknowledgment(
  value: unknown,
): EntityJobAcknowledgment {
  if (value === "sync") return { kind: "completed" };
  if (
    typeof value === "string" &&
    /^[1-9]\d*$/.test(value) &&
    Number.isSafeInteger(Number(value))
  )
    return { kind: "scheduled", id: value };
  return { kind: "invalid" };
}

const monitored = new WeakMap<ApolloClient, Map<string, () => void>>();

/** Owned independently of edit sheets. Transient query failures retry with a
 * bounded backoff; permanent failures dispose the watcher and refresh once,
 * since a failed/cancelled job may still have committed some entities. */
export function invalidateAfterEntityJob(
  client: ApolloClient,
  mutation: DocumentNode,
  id: string,
): () => void {
  const acknowledgment = decodeEntityJobAcknowledgment(id);
  if (acknowledgment.kind !== "scheduled") return () => {};
  let jobs = monitored.get(client);
  if (!jobs) {
    jobs = new Map();
    monitored.set(client, jobs);
  }
  const existing = jobs.get(id);
  if (existing) return existing;
  const pending = jobs;
  let disposed = false;
  let failures = 0;
  let stopAttempt: (() => void) | undefined;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;

  function dispose() {
    if (disposed) return;
    disposed = true;
    pending.delete(id);
    clearTimeout(retryTimer);
    stopAttempt?.();
  }
  function finish() {
    if (disposed) return;
    dispose();
    void client
      .refetchQueries({ include: affectedActiveQueries(client, mutation) })
      .catch((error: unknown) =>
        console.error("Could not refresh library after bulk update", error),
      );
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
          console.error("Could not monitor bulk update", error);
          finish();
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
        queueMicrotask(finish);
      },
      error: failed,
    });
    stopAttempt = () => {
      query.stopPolling();
      subscription.unsubscribe();
    };
  }
  pending.set(id, dispose);
  start();
  return dispose;
}
