import type { ApolloClient } from "@apollo/client";
import { z } from "zod";
import { FindSceneCoversDocument } from "./generated-graphql";
import { monitorJobCompletion, type JobCompletion } from "./monitor-job";

const cachedScenes = z.record(
  z.string(),
  z
    .object({ __typename: z.literal("Scene"), id: z.string() })
    .optional()
    .catch(undefined),
);

const filterNode = z.object({
  condition: z.object({ field: z.string() }).nullish(),
  group: z.object({ children: z.array(z.unknown()) }).nullish(),
});
const filterVariables = z.object({
  scene_filter_ast: z.object({ root: z.unknown() }).nullish(),
});

function usesCoverFrame(value: unknown): boolean {
  const node = filterNode.safeParse(value);
  return (
    node.success &&
    (node.data.condition?.field === "cover_frame" ||
      !!node.data.group?.children.some(usesCoverFrame))
  );
}

async function refreshCoverFilteredLists(client: ApolloClient) {
  // Only the scene list's card/count operations: these never request signed
  // sceneStreams. A reset may remove scenes from the active cover filter.
  const queries = [...client.getObservableQueries("active")].filter((query) => {
    if (
      query.queryName !== "FindSceneList" &&
      query.queryName !== "FindSceneListCount"
    )
      return false;
    const variables = filterVariables.safeParse(query.variables);
    return (
      variables.success && usesCoverFrame(variables.data.scene_filter_ast?.root)
    );
  });
  await Promise.all(queries.map((query) => query.refetch()));
}

/** Update normalized Scene artwork wherever it is displayed, including lists
 * reopened before the job finishes. Never refetch scene detail/stream queries:
 * signed stream URLs change on refetch and would interrupt active playback.
 * Failed/cancelled bulk jobs can still have published some of their covers. */
export async function refreshSceneCoversAfterJob(
  client: ApolloClient,
  sceneIds: readonly string[] | "cached",
  jobId: string,
): Promise<JobCompletion> {
  const result = await new Promise<JobCompletion>((resolve) => {
    monitorJobCompletion(client, jobId, resolve);
  });
  // A library-wide job refreshes only scenes the client has loaded, including
  // those opened while it was running. Never download the entire library.
  const ids = [
    ...new Set(
      sceneIds === "cached"
        ? Object.values(cachedScenes.parse(client.cache.extract())).flatMap(
            (item) => (item ? [item.id] : []),
          )
        : sceneIds,
    ),
  ];
  // Bound each response even when generating an entire selection of covers.
  for (let offset = 0; offset < ids.length; offset += 100) {
    await client.query({
      query: FindSceneCoversDocument,
      variables: { ids: ids.slice(offset, offset + 100) },
      fetchPolicy: "network-only",
    });
  }
  await refreshCoverFilteredLists(client);
  return result;
}
