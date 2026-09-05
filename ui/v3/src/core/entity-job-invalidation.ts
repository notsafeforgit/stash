import type { ApolloClient, DocumentNode } from "@apollo/client";
import { FindJobDocument, JobStatus } from "./generated-graphql";
import { affectedActiveQueries } from "./mutation-invalidation";

const monitored = new WeakMap<ApolloClient, Set<string>>();

/** A job mutation acknowledges scheduling, not completed writes. Own the
 * watcher outside the edit sheet so closing it cannot skip invalidation.
 * Polling also recovers when a completion subscription event was missed. */
export function invalidateAfterEntityJob(
  client: ApolloClient,
  mutation: DocumentNode,
  id: string,
): void {
  let ids = monitored.get(client);
  if (!ids) {
    ids = new Set();
    monitored.set(client, ids);
  }
  if (ids.has(id)) return;
  ids.add(id);
  const pending = ids;
  const query = client.watchQuery({
    query: FindJobDocument,
    variables: { input: { id } },
    fetchPolicy: "network-only",
    pollInterval: 1000,
  });
  const subscription = query.subscribe({
    next: (result) => {
      if (result.loading || !result.data || result.dataState !== "complete")
        return;
      const job = result.data.findJob;
      if (
        job &&
        ![JobStatus.Finished, JobStatus.Failed, JobStatus.Cancelled].includes(
          job.status,
        )
      )
        return;
      // Failures and cancellation can still leave successfully updated items.
      // Schedule cleanup after subscribe returns, including synchronous links.
      queueMicrotask(() => {
        if (!pending.delete(id)) return;
        subscription.unsubscribe();
        void client
          .refetchQueries({ include: affectedActiveQueries(client, mutation) })
          .catch((error: unknown) => {
            console.error("Could not refresh library after bulk update", error);
          });
      });
    },
  });
}
