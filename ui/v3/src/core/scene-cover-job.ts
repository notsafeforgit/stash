import type { ApolloClient } from "@apollo/client";
import { FindSceneCoversDocument } from "./generated-graphql";
import { monitorJobCompletion, type JobCompletion } from "./monitor-job";

/** Update normalized Scene artwork wherever it is displayed, including lists
 * reopened before the job finishes. Never fetch streams, files or form fields:
 * signed stream URLs change on refetch and would interrupt active playback.
 * Failed/cancelled bulk jobs can still have published some of their covers. */
export async function refreshSceneCoversAfterJob(
  client: ApolloClient,
  sceneIds: readonly string[],
  jobId: string,
): Promise<JobCompletion> {
  const ids = [...new Set(sceneIds)];
  const result = await new Promise<JobCompletion>((resolve) => {
    monitorJobCompletion(client, jobId, resolve);
  });
  // Bound each response even when generating an entire selection of covers.
  for (let offset = 0; offset < ids.length; offset += 100) {
    await client.query({
      query: FindSceneCoversDocument,
      variables: { ids: ids.slice(offset, offset + 100) },
      fetchPolicy: "network-only",
    });
  }
  return result;
}
