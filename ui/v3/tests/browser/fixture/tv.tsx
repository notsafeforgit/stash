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
import { defaultTvSettings, type TvSettings } from "@/core/tv/settings";
import type { TvFeedQuery } from "@/core/tv/feed-query";
import * as GQL from "@/core/generated-graphql";
import { scenes as sourceScenes } from "./scene-lightbox";
import { playerConfiguration } from "./player-configuration";

declare global {
  interface Window {
    tvFixtureRequests: { name: string; variables: unknown }[];
    tvFixtureVideo?: HTMLVideoElement;
    tvFixtureNativeFullscreen: number;
  }
}
window.tvFixtureRequests = [];
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
      title: `Scene ${id}`,
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
const settings: TvSettings = {
  ...defaultTvSettings,
  mode,
  pageSize: 5,
  autoplay: !params.has("paused"),
  completion: params.has("advance") ? "advance" : "normal",
  defaultQuality: params.has("low")
    ? { kind: "fixed", resolution: GQL.StreamingResolutionEnum.Low }
    : { kind: "best" },
};
const configuration: GQL.ConfigDataFragment = {
  ...playerConfiguration,
  __typename: "ConfigResult",
  ui: {
    ...playerConfiguration.ui,
    tv: settings,
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
    delay: 0,
    result: (variables) => {
      record("TvScenes", variables);
      const page = variables.filter?.page ?? 1;
      const size = variables.filter?.per_page ?? 5;
      return {
        data: {
          findScenes: {
            count: scenes.length,
            scenes: scenes.slice((page - 1) * size, page * size),
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
          findScene: scenes.find((scene) => scene.id === variables.id) ?? null,
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
const savedFilters: MockedResponse<
  GQL.FindSavedFiltersQuery,
  GQL.FindSavedFiltersQueryVariables
> = {
  request: { query: GQL.FindSavedFiltersDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: { data: { findSavedFilters: [] } },
};
let saveFailed = false;
const configure: MockedResponse<
  GQL.ConfigureUiSettingMutation,
  GQL.ConfigureUiSettingMutationVariables
> = {
  request: { query: GQL.ConfigureUiSettingDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 50,
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
const root = createRootRoute({
  component: () => (
    <ApolloProvider client={client}>
      <FixtureConfiguration>
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
      </FixtureConfiguration>
    </ApolloProvider>
  ),
});
const router = createRouter({
  basepath: "/tv-fixture",
  routeTree: root.addChildren([
    createRoute({
      getParentRoute: () => root,
      path: "/tv",
      component: () => (
        <TvPage
          query={query}
          settings={settings}
          seed={37}
          search={{ seed: 37 }}
        />
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
