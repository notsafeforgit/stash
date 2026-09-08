// @vitest-environment jsdom
import { gql, type TypedDocumentNode } from "@apollo/client";
import { expect, it } from "vitest";
import { createClient } from "./create-client";
import { getFrontPageContent } from "./config";
import {
  type ConfigDataFragment,
  FilterMode,
  SortDirectionEnum,
} from "./generated-graphql";

const configurationQuery: TypedDocumentNode<{
  configuration: Pick<ConfigDataFragment, "__typename" | "ui">;
}> = gql`
  query HomeScreenConfiguration {
    configuration {
      ui
    }
  }
`;

it.each([
  { format: "numeric", id: (value: number) => value },
  { format: "string", id: (value: number) => String(value) },
  {
    format: "mixed",
    id: (value: number) => (value % 2 === 0 ? value : String(value)),
  },
])("retains a persisted $format-ID Home Screen through cache updates", async ({
  id,
}) => {
  // Existing configurations contain both JSON numbers and persisted strings.
  // Include custom carousels between saved filters to catch whole-layout loss.
  const frontPageContent = [
    { __typename: "SavedFilter", savedFilterId: id(10), extension: "keep" },
    { __typename: "SavedFilter", savedFilterId: id(11) },
    ...[FilterMode.Scenes, FilterMode.Galleries, FilterMode.Images].map(
      (mode) => ({
        __typename: "CustomFilter",
        message: {
          id: "recently_added_objects",
          values: { objects: mode },
        },
        mode,
        sortBy: "created_at",
        direction: SortDirectionEnum.Desc,
      }),
    ),
    { __typename: "SavedFilter", savedFilterId: id(12) },
  ];
  const { client, cache, wsClient } = createClient();
  try {
    // Setting mutations write the complete returned UI config back to this
    // same cache boundary. Unrelated edits must retain the existing layout.
    for (const advancedMode of [false, true]) {
      cache.writeQuery({
        query: configurationQuery,
        data: {
          configuration: {
            __typename: "ConfigResult",
            ui: { frontPageContent, advancedMode },
          },
        },
      });
      const ui = cache.readQuery({ query: configurationQuery })?.configuration
        .ui;
      expect(ui?.advancedMode).toBe(advancedMode);
      expect(ui?.frontPageContent).toEqual(frontPageContent);
      expect(getFrontPageContent(ui)).toEqual(frontPageContent);
    }
  } finally {
    client.stop();
    await wsClient.dispose();
  }
});
