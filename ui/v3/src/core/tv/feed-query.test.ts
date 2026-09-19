import { ApolloClient, InMemoryCache } from "@apollo/client";
import { MockLink } from "@apollo/client/testing";
import { describe, expect, it } from "vitest";
import * as GQL from "../generated-graphql";
import { playerConfiguration } from "../../../tests/browser/fixture/player-configuration";
import { resolveTvQuery, tvQueryIdentity } from "./feed-query";
import { decodeTvSettings, defaultTvSettings } from "./settings";

const condition = {
  condition: {
    field: "title",
    value: { modifier: GQL.CriterionModifier.Includes, value: "favorite" },
  },
} as const;

function clientWithFilter(
  mode = GQL.FilterMode.Scenes,
  filter_ast: GQL.SavedFilterDataFragment["filter_ast"] = { root: condition },
) {
  const client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new MockLink([]),
  });
  client.writeQuery({
    query: GQL.FindSavedFilterDocument,
    variables: { id: "1" },
    data: {
      findSavedFilter: {
        __typename: "SavedFilter",
        id: "1",
        name: "Example",
        mode,
        filter_ast,
        ui_options: {},
        find_filter: {
          __typename: "SavedFindFilterType",
          q: "example",
          sort: "date",
          direction: GQL.SortDirectionEnum.Desc,
          page: 1,
          per_page: 20,
        },
      },
    },
  });
  return client;
}

describe("TV query construction", () => {
  it("preserves saved search and AST while composing orientation on the server", async () => {
    const query = await resolveTvQuery(
      clientWithFilter(),
      playerConfiguration,
      { ...defaultTvSettings, orientation: "match" },
      "scenes",
      { kind: "saved", id: "1" },
      37,
      "portrait",
    );
    expect(query.filter).toMatchObject({
      q: "example",
      sort: "date",
      direction: "DESC",
      per_page: 20,
    });
    expect(query.ast).toMatchObject({
      root: {
        group: {
          operator: "AND",
          children: [
            condition,
            {
              condition: {
                field: "orientation",
                value: { value: ["SQUARE", "PORTRAIT"] },
              },
            },
          ],
        },
      },
    });
  });

  it("uses a stable Random sort seed across pages and a new identity on reshuffle", async () => {
    const client = clientWithFilter();
    const query = await resolveTvQuery(
      client,
      playerConfiguration,
      { ...defaultTvSettings, sort: "random" },
      "scenes",
      { kind: "all" },
      42,
      "landscape",
    );
    expect(query.filter.sort).toBe("random_42");
    expect(tvQueryIdentity(query)).not.toBe(
      tvQueryIdentity({ ...query, seed: 43 }),
    );
  });

  it("reads marker defaults from the scene_markers app view", async () => {
    const config = {
      ...playerConfiguration,
      ui: {
        ...playerConfiguration.ui,
        defaultFilters: {
          scene_markers: {
            filter_ast: {
              root: {
                condition: {
                  field: "duration",
                  value: {
                    modifier: GQL.CriterionModifier.GreaterThan,
                    value: { value: 5 },
                  },
                },
              },
            },
          },
        },
      },
    };
    const query = await resolveTvQuery(
      clientWithFilter(),
      config,
      defaultTvSettings,
      "markers",
      { kind: "default" },
      1,
      "landscape",
    );
    expect(query.ast?.root.group?.children).toEqual([
      {
        condition: {
          field: "duration",
          value: { modifier: "GREATER_THAN", value: 5 },
        },
      },
    ]);
  });

  it("fails closed for missing, wrong-mode, conflicting and malformed filters", async () => {
    const settings = defaultTvSettings;
    await expect(
      resolveTvQuery(
        clientWithFilter(GQL.FilterMode.SceneMarkers),
        playerConfiguration,
        settings,
        "scenes",
        { kind: "saved", id: "1" },
        1,
        "portrait",
      ),
    ).rejects.toThrow("another feed");
    await expect(
      resolveTvQuery(
        clientWithFilter(GQL.FilterMode.Scenes, {
          root: {
            condition: condition.condition,
            group: { operator: GQL.FilterGroupOperator.And, children: [] },
          },
        }),
        playerConfiguration,
        settings,
        "scenes",
        { kind: "saved", id: "1" },
        1,
        "portrait",
      ),
    ).rejects.toThrow("invalid criteria");
    const config = {
      ...playerConfiguration,
      ui: {
        ...playerConfiguration.ui,
        forkDefaultFilterState: {
          scenes: { pending_legacy_object_filter: {} },
        },
      },
    };
    await expect(
      resolveTvQuery(
        clientWithFilter(),
        config,
        settings,
        "scenes",
        { kind: "default" },
        1,
        "portrait",
      ),
    ).rejects.toThrow("conflict");
  });

  it("uses the chosen saved filter without resolving retired extra rules", async () => {
    const decoded = decodeTvSettings({
      ...defaultTvSettings,
      version: 2,
      sceneFilter: { kind: "saved", id: "1" },
      rules: [{ kind: "filter", mode: "scenes", filterId: "999" }],
    });
    if (decoded.kind !== "ready") throw new Error(decoded.message);
    const query = await resolveTvQuery(
      clientWithFilter(),
      playerConfiguration,
      decoded.settings,
      "scenes",
      decoded.settings.sceneFilter,
      1,
      "portrait",
    );
    expect(query.filter).toMatchObject({
      q: "example",
      sort: "date",
      direction: "DESC",
    });
    expect(query.ast?.root.group?.children).toEqual([condition]);
  });
});
