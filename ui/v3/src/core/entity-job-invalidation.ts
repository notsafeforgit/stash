import type { ApolloClient, DocumentNode } from "@apollo/client";
import { monitorJobCompletion } from "./monitor-job";
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
  const dispose = monitorJobCompletion(client, id, (result) => {
    pending.delete(id);
    if (result.kind === "unavailable")
      console.error("Could not monitor bulk update", result.error);
    void client
      .refetchQueries({ include: affectedActiveQueries(client, mutation) })
      .catch((error: unknown) =>
        console.error("Could not refresh library after bulk update", error),
      );
  });
  const stop = () => {
    if (pending.get(id) === stop) pending.delete(id);
    dispose();
  };
  pending.set(id, stop);
  return stop;
}
