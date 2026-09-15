import { ApolloClient, InMemoryCache } from "@apollo/client";
import { ApolloProvider, useQuery } from "@apollo/client/react";
import type { ReactNode } from "react";
import { GraphQLError } from "graphql";
import { MockLink, type MockedResponse } from "@apollo/client/testing";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  Link,
} from "@tanstack/react-router";
import { TvPage } from "@/components/tv/tv-page";
import { SettingsTvPage } from "@/components/tv/tv-settings";
import { ConfigurationProvider } from "@/hooks/config";
import { MobileNavigationProvider } from "@/components/layout/mobile-navigation";
import { RouteViewport } from "@/components/layout/route-viewport";
import { installRouteTransitions } from "@/core/route-transitions";
import {
  defaultTvSettings,
  tvSettingsSchema,
  type TvSettings,
} from "@/core/tv/settings";
import { createTvAction, type TvRailEntry } from "@/core/tv/action-config";
import { useTvSettings } from "@/hooks/use-tv-settings";
import { SaveIndicatorProvider } from "@/hooks/save-indicator";
import type { TvFeedQuery } from "@/core/tv/feed-query";
import * as GQL from "@/core/generated-graphql";
import { scenes as sourceScenes } from "./scene-lightbox";
import { playerConfiguration } from "./player-configuration";
import { RatingStarPrecision, RatingSystemType } from "@/utils/rating";
import { RatingSystem } from "@/components/ui/rating-system";

declare global {
  interface Window {
    tvFixtureRequests: { name: string; variables: unknown }[];
    tvFixtureSaveAttempts: GQL.ConfigureUiSettingMutationVariables[];
    tvFixtureVideo?: HTMLVideoElement;
    tvFixtureNativeFullscreen: number;
  }
}
window.tvFixtureRequests = [];
window.tvFixtureSaveAttempts = [];
window.tvFixtureNativeFullscreen = 0;
const params = new URLSearchParams(location.search);
const base = sourceScenes[0];
if (!base) throw new Error("Missing synthetic scene");
const scenes: GQL.SceneDataFragment[] = Array.from(
  { length: 100 },
  (_, index) => {
    const id = String(index + 1);
    const scene = {
      ...base,
      id,
      o_counter: Number(params.get("count") ?? 0),
      resume_time: params.has("resume") ? 4 : base.resume_time,
      files: params.has("portrait")
        ? base.files.map((file) => ({ ...file, width: 180, height: 320 }))
        : base.files,
      title: params.has("long-info")
        ? `Scene ${id} ${"VeryLongUnbrokenTitle".repeat(25)}`
        : `Scene ${id}`,
      details: params.has("long-info")
        ? "Long description. ".repeat(100)
        : params.has("metadata")
          ? "An unhurried study in colour and motion, captured in a single take."
          : base.details,
      tags:
        params.has("long-info") || params.has("metadata")
          ? Array.from(
              { length: params.has("long-info") ? 60 : 4 },
              (_, tagIndex) => ({
                __typename: "Tag" as const,
                id: `tag-${tagIndex}`,
                name: params.has("long-info")
                  ? `Tag ${tagIndex} ${tagIndex === 0 ? "UnbrokenTagName".repeat(30) : "A long tag label"}`
                  : (["Colour study", "Portrait", "One take", "Motion"][
                      tagIndex
                    ] ?? "Tag"),
                sort_name: null,
                aliases: [],
                image_path: null,
                parent_count: 0,
                child_count: 0,
                stash_ids: [],
              }),
            )
          : base.tags,
      paths: {
        ...base.paths,
        caption: new URL(`/scene/${id}/caption`, location.href).href,
      },
      sceneStreams: [
        {
          url: new URL(`/scene/${id}/stream`, location.href).href,
          mime_type: "video/mp4",
          label: "Direct stream",
        },
        {
          url: new URL(
            `/scene/${id}/stream.master.m3u8?resolution=LOW`,
            location.href,
          ).href,
          mime_type: "application/vnd.apple.mpegurl",
          label: "HLS (240p)",
        },
      ],
    };
    return {
      ...scene,
      scene_markers: scene.scene_markers.map((marker, markerIndex) => ({
        ...marker,
        id: String((index + 1) * 10 + markerIndex),
        scene,
      })),
    };
  },
);
const markers = scenes.flatMap((scene) => scene.scene_markers);
const mode = params.has("markers") ? "markers" : "scenes";
const start = tvSettingsSchema.shape.start.safeParse(params.get("start"));
const settings: TvSettings = {
  ...defaultTvSettings,
  leftHanded: params.has("left-handed"),
  rail: defaultTvSettings.rail.flatMap<TvRailEntry>((entry) => {
    if (entry.type === "action" && entry.action.kind === "counter") {
      if (params.get("counter") === "folder") return [];
      return [{ ...entry, pinned: params.get("counter") === "pinned" }];
    }
    if (
      entry.type === "folder" &&
      entry.id === "edit" &&
      params.get("counter") === "folder"
    )
      return [
        {
          ...entry,
          actions: [createTvAction("counter", "counter"), ...entry.actions],
        },
      ];
    return [entry];
  }),
  mode,
  pageSize: 5,
  autoplay: !params.has("paused"),
  startMuted: !params.has("unmuted"),
  start: start.success ? start.data : defaultTvSettings.start,
  window:
    params.get("window") === "fixed"
      ? { kind: "fixed", seconds: 3 }
      : params.get("window") === "random"
        ? { kind: "random", min: 2, max: 3 }
        : { kind: "full" },
  completion: params.has("advance") ? "advance" : "normal",
  defaultQuality: params.has("low")
    ? { kind: "fixed", resolution: GQL.StreamingResolutionEnum.Low }
    : { kind: "best" },
};
const legacyRail: TvRailEntry[] = [
  {
    type: "action",
    pinned: true,
    action: createTvAction("settings", "settings"),
  },
  ...settings.rail,
];
const configuration: GQL.ConfigDataFragment = {
  ...playerConfiguration,
  __typename: "ConfigResult",
  ui: {
    ...playerConfiguration.ui,
    ratingSystemOptions: {
      type: params.has("decimal")
        ? RatingSystemType.Decimal
        : RatingSystemType.Stars,
      starPrecision:
        Object.values(RatingStarPrecision).find(
          (value) => value === params.get("precision"),
        ) ?? RatingStarPrecision.Full,
    },
    tv: params.has("legacy-shuffle")
      ? {
          ...settings,
          version: 1,
          rail: legacyRail,
          shuffle: true,
          sort: "created_at",
          rules: [],
        }
      : params.has("legacy-rules")
        ? {
            ...settings,
            version: 2,
            rail: legacyRail,
            sceneFilter: { kind: "saved", id: "1" },
            markerFilter: { kind: "saved", id: "2" },
            rules: [{ kind: "filter", mode: "scenes", filterId: "999" }],
          }
        : params.has("legacy-gear")
          ? { ...settings, version: 3, rail: legacyRail }
          : settings,
    trackActivity: params.has("activity"),
    minimumPlayPercent: 0,
  },
  interface: {
    ...playerConfiguration.interface,
    autostartVideo: !params.has("paused"),
  },
};
const record = (name: string, variables: unknown) =>
  window.tvFixtureRequests.push({ name, variables });
const scenePage: MockedResponse<GQL.TvScenesQuery, GQL.TvScenesQueryVariables> =
  {
    request: { query: GQL.TvScenesDocument, variables: () => true },
    maxUsageCount: Infinity,
    delay: params.has("feed-loading") ? 5000 : 0,
    result: (variables) => {
      record("TvScenes", variables);
      const page = variables.filter?.page ?? 1;
      const size = variables.filter?.per_page ?? 5;
      return {
        data: {
          findScenes: {
            count: params.has("empty") ? 0 : scenes.length,
            scenes: params.has("empty")
              ? []
              : scenes.slice((page - 1) * size, page * size),
          },
        },
      };
    },
  };
const markerPage: MockedResponse<
  GQL.TvMarkersQuery,
  GQL.TvMarkersQueryVariables
> = {
  request: { query: GQL.TvMarkersDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: (variables) => {
    record("TvMarkers", variables);
    const page = variables.filter?.page ?? 1;
    const size = variables.filter?.per_page ?? 5;
    return {
      data: {
        findSceneMarkers: {
          count: markers.length,
          scene_markers: markers.slice((page - 1) * size, page * size),
        },
      },
    };
  },
};
const detail: MockedResponse<GQL.FindSceneQuery, GQL.FindSceneQueryVariables> =
  {
    request: { query: GQL.FindSceneDocument, variables: () => true },
    maxUsageCount: Infinity,
    delay: params.has("slow") ? 500 : 0,
    result: (variables) => {
      record("FindScene", variables);
      return {
        data: {
          findScene: params.has("missing")
            ? null
            : (scenes.find((scene) => scene.id === variables.id) ?? null),
        },
      };
    },
  };
const activity: MockedResponse<
  GQL.SceneSaveActivityMutation,
  GQL.SceneSaveActivityMutationVariables
> = {
  request: { query: GQL.SceneSaveActivityDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: (variables) => {
    record("SceneSaveActivity", variables);
    return { data: { sceneSaveActivity: true } };
  },
};
const play: MockedResponse<
  GQL.SceneAddPlayMutation,
  GQL.SceneAddPlayMutationVariables
> = {
  request: { query: GQL.SceneAddPlayDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: (variables) => {
    record("SceneAddPlay", variables);
    return { data: { sceneAddPlay: { count: 1, history: [] } } };
  },
};
const cache = new InMemoryCache();
function targetScene(id: string) {
  const scene = scenes.find((scene) => scene.id === id);
  if (!scene) throw new Error("Missing mutation target");
  return scene;
}
const updateScene: MockedResponse<
  GQL.SceneUpdateMutation,
  GQL.SceneUpdateMutationVariables
> = {
  request: { query: GQL.SceneUpdateDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 100,
  result: (variables) => {
    record("SceneUpdate", variables);
    const scene = targetScene(variables.input.id);
    if (variables.input.rating100 !== undefined)
      scene.rating100 = variables.input.rating100;
    return { data: { sceneUpdate: scene } };
  },
};
let counterFailed = false;
const addO: MockedResponse<
  GQL.SceneAddOMutation,
  GQL.SceneAddOMutationVariables
> = {
  request: { query: GQL.SceneAddODocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 100,
  result: (variables) => {
    record("SceneAddO", variables);
    if (params.has("counter-error") && !counterFailed) {
      counterFailed = true;
      return { errors: [new GraphQLError("Counter unavailable")] };
    }
    const scene = targetScene(variables.id);
    scene.o_counter = (scene.o_counter ?? 0) + 1;
    return { data: { sceneAddO: { count: scene.o_counter, history: [] } } };
  },
};
const deleteO: MockedResponse<
  GQL.SceneDeleteOMutation,
  GQL.SceneDeleteOMutationVariables
> = {
  request: { query: GQL.SceneDeleteODocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 100,
  result: (variables) => {
    record("SceneDeleteO", variables);
    const scene = targetScene(variables.id);
    scene.o_counter = Math.max(0, (scene.o_counter ?? 0) - 1);
    return { data: { sceneDeleteO: { count: scene.o_counter, history: [] } } };
  },
};
const resetO: MockedResponse<
  GQL.SceneResetOMutation,
  GQL.SceneResetOMutationVariables
> = {
  request: { query: GQL.SceneResetODocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 100,
  result: (variables) => {
    record("SceneResetO", variables);
    targetScene(variables.id).o_counter = 0;
    return { data: { sceneResetO: 0 } };
  },
};
const savedFilters: MockedResponse<
  GQL.FindSavedFiltersQuery,
  GQL.FindSavedFiltersQueryVariables
> = {
  request: { query: GQL.FindSavedFiltersDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: (variables) => ({
    data: {
      findSavedFilters: [
        {
          __typename: "SavedFilter" as const,
          id: "1",
          name: "Scene picks",
          mode: GQL.FilterMode.Scenes,
          find_filter: null,
          filter_ast: null,
          ui_options: null,
        },
        {
          __typename: "SavedFilter" as const,
          id: "2",
          name: "Marker picks",
          mode: GQL.FilterMode.SceneMarkers,
          find_filter: null,
          filter_ast: null,
          ui_options: null,
        },
      ].filter((filter) => !variables.mode || filter.mode === variables.mode),
    },
  }),
};
let saveFailed = false;
const configure: MockedResponse<
  GQL.ConfigureUiSettingMutation,
  GQL.ConfigureUiSettingMutationVariables
> = {
  request: {
    query: GQL.ConfigureUiSettingDocument,
    variables: (variables) => {
      window.tvFixtureSaveAttempts.push(variables);
      return true;
    },
  },
  maxUsageCount: Infinity,
  delay: params.has("slow-save") ? 500 : 50,
  result: (variables) => {
    record("ConfigureUISetting", variables);
    if (params.has("save-error") && !saveFailed) {
      saveFailed = true;
      return { errors: [new GraphQLError("Save unavailable")] };
    }
    return {
      data: {
        configureUISetting: { ...configuration.ui, tv: variables.value },
      },
    };
  },
};
cache.writeQuery({ query: GQL.ConfigurationDocument, data: { configuration } });
const client = new ApolloClient({
  cache,
  link: new MockLink([
    scenePage,
    markerPage,
    detail,
    activity,
    play,
    savedFilters,
    configure,
    updateScene,
    addO,
    deleteO,
    resetO,
  ]),
});
function FixtureConfiguration({ children }: { children: ReactNode }) {
  const { data } = useQuery(GQL.ConfigurationDocument);
  if (!data) throw new Error("Missing fixture configuration");
  return (
    <ConfigurationProvider configuration={data.configuration}>
      {children}
    </ConfigurationProvider>
  );
}
const query: TvFeedQuery = {
  seed: 37,
  mode,
  filter: { sort: "title", direction: GQL.SortDirectionEnum.Asc },
  pageSize: 5,
  prefetch: 2,
  itemLimit: null,
};
function FixtureTvPage() {
  const { result } = useTvSettings();
  if (result.kind !== "ready") throw new Error("Invalid fixture TV settings");
  return (
    <TvPage
      query={query}
      settings={result.settings}
      seed={37}
      search={{ seed: 37 }}
    />
  );
}
function FixtureRating() {
  const { data } = useQuery(GQL.FindSceneDocument, { variables: { id: "1" } });
  return (
    <div className="m-4 w-64">
      <RatingSystem
        value={data?.findScene?.rating100}
        onSetRating={(rating100) => {
          void client.mutate({
            mutation: GQL.SceneUpdateDocument,
            variables: { input: { id: "1", rating100 } },
          });
        }}
      />
    </div>
  );
}
const root = createRootRoute({
  component: () => (
    <ApolloProvider client={client}>
      <FixtureConfiguration>
        <SaveIndicatorProvider>
          <MobileNavigationProvider>
            <div
              data-app-viewport
              className="flex h-dvh flex-col overflow-hidden"
            >
              <RouteViewport>
                <Outlet />
              </RouteViewport>
            </div>
          </MobileNavigationProvider>
        </SaveIndicatorProvider>
      </FixtureConfiguration>
    </ApolloProvider>
  ),
});
const router = createRouter({
  basepath: "/tv-fixture",
  routeTree: root.addChildren([
    createRoute({
      getParentRoute: () => root,
      path: "/rating",
      component: FixtureRating,
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/tv",
      component: FixtureTvPage,
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/settings",
      component: () => (
        <div>
          <h1>Settings</h1>
          <Link to="/settings/tv">TV</Link>
        </div>
      ),
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/settings/tv",
      component: () => (
        <div className="h-full overflow-y-auto">
          <h1>TV settings</h1>
          <Link to="/tv">Return to TV</Link>
          <SettingsTvPage />
        </div>
      ),
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/scenes",
      component: () => (
        <div>
          <h1>Scenes</h1>
          <Link to="/tv">Return to TV</Link>
        </div>
      ),
    }),
  ]),
});
installRouteTransitions(router);
export function TvFixture() {
  return <RouterProvider router={router} />;
}
