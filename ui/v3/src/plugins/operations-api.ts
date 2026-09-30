import type { ApolloClient } from "@apollo/client";
import * as GQL from "src/core/generated-graphql";

/** No caching of review results: plugins own their preview/apply concurrency checks. */
export function createPluginOperationsAPI(
  apollo: ApolloClient,
  pluginId: string,
) {
  return Object.freeze({
    async query(
      operation: string,
      input: Record<string, unknown> = {},
    ): Promise<unknown> {
      const result = await apollo.query({
        query: GQL.PluginQueryV3Document,
        variables: { plugin_id: pluginId, operation, input },
        fetchPolicy: "no-cache",
      });
      if (!result.data) throw new Error("Missing plugin operation response");
      return result.data.pluginQueryV3;
    },
    async mutate(
      operation: string,
      input: Record<string, unknown> = {},
    ): Promise<unknown> {
      const result = await apollo.mutate({
        mutation: GQL.PluginMutationV3Document,
        variables: { plugin_id: pluginId, operation, input },
      });
      if (!result.data) throw new Error("Missing plugin operation response");
      return result.data.pluginMutationV3;
    },
  });
}
