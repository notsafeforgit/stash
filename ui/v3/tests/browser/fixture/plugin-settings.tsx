import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import {
  PluginSettingsForm,
  type Setting,
} from "@/components/settings/plugin-settings-form";
import {
  PluginSettingEditorV3,
  PluginSettingTypeV3,
  PluginPreviewEntityV3,
} from "@/core/generated-graphql";
import "./style.css";
import { PluginLoadErrors } from "@/components/settings/plugin-load-errors";

declare global {
  interface Window {
    mappingRequests: string[];
  }
}
window.mappingRequests = [];
const setting: Setting = {
  __typename: "PluginSettingV3",
  name: "scene_import_mappings",
  display_name: "Scene import mappings",
  description:
    "Choose a supported Stash metadata field and enter its jq expression.",
  type: PluginSettingTypeV3.Json,
  editor: PluginSettingEditorV3.JqMap,
  default_value: {},
  options: [],
  preview: {
    __typename: "PluginSettingPreviewV3",
    entity: PluginPreviewEntityV3.Scene,
    description: "Tests custom mappings only; nothing is written.",
  },
  mapping_targets: [
    {
      __typename: "PluginMappingTargetV3",
      name: "title",
      label: "Title",
      type: "String",
      description: "The item title.",
    },
    {
      __typename: "PluginMappingTargetV3",
      name: "performer_ids",
      label: "Performers",
      type: "[ID!]",
      description: "Existing Stash performer IDs, not names.",
    },
  ],
};
const client = new ApolloClient({
  cache: new InMemoryCache(),
  link: new ApolloLink(
    (operation) =>
      new Observable((observer) => {
        window.mappingRequests.push(operation.operationName ?? "");
        const data =
          operation.operationName === "PluginPreviewScenes"
            ? {
                findScenes: {
                  __typename: "FindScenesResultType",
                  scenes: [
                    {
                      __typename: "Scene",
                      id: "42",
                      title: "Example scene",
                      files: [
                        { __typename: "VideoFile", path: "/media/example.mp4" },
                      ],
                    },
                  ],
                },
              }
            : operation.operationName === "PluginSettingPreview"
              ? {
                  pluginSettingPreviewV3: {
                    stash: { id: "42", title: "Example scene" },
                    catalog: { title: "Catalog title" },
                    observations: [],
                  },
                }
              : operation.operationName === "PluginEvaluateMappings"
                ? { pluginEvaluateMappings: { title: "Catalog title" } }
                : { updatePluginSettings: operation.variables.input };
        observer.next({ data });
        observer.complete();
      }),
  ),
});
const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
createRoot(root).render(
  <ApolloProvider client={client}>
    <IntlProvider locale="en-GB" defaultLocale="en-GB">
      <main className="mx-auto h-dvh max-w-4xl overflow-y-auto p-4">
        <PluginLoadErrors
          errors={[
            {
              __typename: "PluginLoadErrorV3",
              path: "community/legacy-plugin.yml",
              message:
                "plugin manifest requires apiVersion: 3; unversioned plugins are no longer supported",
            },
          ]}
        />
        <PluginSettingsForm
          pluginId="catalogMetadata"
          settings={[setting]}
          saved={{ scene_import_mappings: { title: ".catalog.title" } }}
        />
      </main>
    </IntlProvider>
  </ApolloProvider>,
);
