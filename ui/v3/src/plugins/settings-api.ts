import type { ApolloClient } from "@apollo/client";
import * as GQL from "src/core/generated-graphql";

export type PluginSettingsSnapshot = GQL.PluginSettingsQuery["pluginSettings"];

export function createPluginSettingsAPI(
  apollo: ApolloClient,
  pluginId: string,
) {
  return Object.freeze({
    async preview(setting: string, entityId: string): Promise<unknown> {
      const result = await apollo.query({
        query: GQL.PluginSettingPreviewDocument,
        variables: { plugin_id: pluginId, setting, entity_id: entityId },
        fetchPolicy: "no-cache",
      });
      if (!result.data) throw new Error("Missing preview response");
      return result.data.pluginSettingPreviewV3;
    },
    async get(): Promise<PluginSettingsSnapshot> {
      const result = await apollo.query({
        query: GQL.PluginSettingsDocument,
        variables: { plugin_id: pluginId },
        fetchPolicy: "network-only",
      });
      if (!result.data) throw new Error("Missing plugin settings response");
      return result.data.pluginSettings;
    },
    async update(input: Record<string, unknown>, reset: string[] = []) {
      const result = await apollo.mutate({
        mutation: GQL.UpdatePluginSettingsDocument,
        variables: { plugin_id: pluginId, input, reset },
      });
      if (!result.data) throw new Error("Missing plugin settings response");
      const values = result.data.updatePluginSettings;
      const existing = apollo.cache.readQuery({
        query: GQL.ConfigurationDocument,
      });
      if (existing?.configuration) {
        apollo.cache.writeQuery({
          query: GQL.ConfigurationDocument,
          data: {
            configuration: {
              ...existing.configuration,
              plugins: {
                ...existing.configuration.plugins,
                [pluginId]: values,
              },
            },
          },
        });
      }
      apollo.cache.evict({
        id: "ROOT_QUERY",
        fieldName: "pluginSettingsV3",
        args: { plugin_id: pluginId },
      });
      return values;
    },
  });
}

export function createPluginExpressionsAPI(apollo: ApolloClient) {
  return Object.freeze({
    async jq(expression: string, input: unknown): Promise<unknown[]> {
      const result = await apollo.query({
        query: GQL.PluginEvaluateJqDocument,
        variables: { expression, input },
        fetchPolicy: "no-cache",
      });
      if (!result.data) throw new Error("Missing expression response");
      return result.data.pluginEvaluateJQ;
    },
    async map(mappings: Record<string, string>, input: unknown) {
      const result = await apollo.query({
        query: GQL.PluginEvaluateMappingsDocument,
        variables: { mappings, input },
        fetchPolicy: "no-cache",
      });
      if (!result.data) throw new Error("Missing expression response");
      return result.data.pluginEvaluateMappings;
    },
  });
}
