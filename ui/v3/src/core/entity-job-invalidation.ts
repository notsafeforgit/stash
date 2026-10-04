import type { ApolloClient, DocumentNode } from "@apollo/client";
import { monitorJobCompletion } from "./monitor-job";
import {
  affectedActiveQueries,
  rootFieldSelections,
} from "./mutation-invalidation";

export type EntityJobAcknowledgment =
  | { kind: "completed" }
  | { kind: "scheduled"; id: string }
  | { kind: "invalid" };

function isJobID(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[1-9]\d*$/.test(value) &&
    Number.isSafeInteger(Number(value))
  );
}

/** Queue admission is distinct from a committed edit. Reject incomplete or
 * contradictory acknowledgments instead of presenting them as success. */
export function decodeEntityJobAcknowledgment(
  value: unknown,
): EntityJobAcknowledgment {
  if (!value || typeof value !== "object") return { kind: "invalid" };
  const result = value as Record<string, unknown>;
  if (
    typeof result.selected_count !== "number" ||
    !Number.isSafeInteger(result.selected_count) ||
    result.selected_count < 0 ||
    !Array.isArray(result.updated_ids) ||
    !result.updated_ids.every((id) => typeof id === "string" && id.length > 0)
  )
    return { kind: "invalid" };
  if (
    result.status === "COMPLETED" &&
    result.job_id === null &&
    result.updated_ids.length === result.selected_count
  )
    return { kind: "completed" };
  if (
    result.status === "QUEUED" &&
    isJobID(result.job_id) &&
    result.updated_ids.length === 0
  )
    return { kind: "scheduled", id: result.job_id };
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
  if (!isJobID(id)) return () => {};
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

/** Select immediate refreshes and retain queued-job monitors beyond form lifetime. */
export function refetchAfterEntityMutation(
  client: ApolloClient,
  document: DocumentNode,
  data: unknown,
): DocumentNode[] {
  const fields = rootFieldSelections(document);
  const bulkFields = fields.filter((field) =>
    /^bulk(Scene|SceneMarker|Image|Gallery|Performer|Studio|Tag|Group)Update$/.test(
      field.name.value,
    ),
  );
  if (!bulkFields.length) return affectedActiveQueries(client, document);
  // Mixed mutations also refresh their non-bulk edits immediately.
  let refreshNow = fields.some(
    (field) => field.name.value !== "__typename" && !bulkFields.includes(field),
  );
  const values =
    data && typeof data === "object" ? (data as Record<string, unknown>) : {};
  for (const field of bulkFields) {
    const key = field.alias?.value ?? field.name.value;
    const acknowledgment = decodeEntityJobAcknowledgment(values[key]);
    if (acknowledgment.kind === "scheduled")
      invalidateAfterEntityJob(client, document, acknowledgment.id);
    else refreshNow = true;
  }
  return refreshNow ? affectedActiveQueries(client, document) : [];
}
