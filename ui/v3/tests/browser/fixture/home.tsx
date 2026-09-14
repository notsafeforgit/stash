import { ApolloClient, InMemoryCache } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { MockLink, type MockedResponse } from "@apollo/client/testing";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { FrontPage } from "@/components/frontpage/front-page";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { ConfigurationProvider } from "@/hooks/config";
import type { FrontPageContent } from "@/core/config";
import * as GQL from "@/core/generated-graphql";
import { playerConfiguration } from "./player-configuration";
import { installRouteTransitions } from "@/core/route-transitions";
import { MobileNavigationProvider } from "@/components/layout/mobile-navigation";
import { RouteViewport } from "@/components/layout/route-viewport";
import { preloadFrontPage } from "@/components/frontpage/preload-front-page";

declare global {
  interface Window {
    homeFixtureQueries: string[];
    homeFixturePreloaded: boolean;
    homeFixtureDefinitions: string[];
  }
}
window.homeFixtureQueries = [];
window.homeFixturePreloaded = false;
window.homeFixtureDefinitions = [];

const rows = ["name", "random", "updated_at", "scenes_count", "rating"].map(
  (sortBy, index): FrontPageContent => ({
    __typename: "CustomFilter",
    mode: GQL.FilterMode.Studios,
    sortBy,
    direction: GQL.SortDirectionEnum.Asc,
    title: `Row ${index + 1}`,
  }),
);
rows[1] = { __typename: "SavedFilter", savedFilterId: "2" };

const studios: GQL.StudioDataFragment[] = Array.from(
  { length: 25 },
  (_, index) => ({
    __typename: "Studio",
    id: String(index),
    name: `Studio ${index + 1}`,
    url: "",
    urls: [],
    parent_studio: null,
    child_studios: [],
    ignore_auto_tag: false,
    organized: false,
    image_path: "",
    scene_count: 1,
    scene_count_all: 1,
    image_count: 0,
    image_count_all: 0,
    gallery_count: 0,
    gallery_count_all: 0,
    scene_marker_count: 0,
    performer_count: 0,
    performer_count_all: 0,
    group_count: 0,
    group_count_all: 0,
    stash_ids: [],
    details: "",
    rating100: null,
    favorite: false,
    aliases: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    tags: [],
    o_counter: 0,
    o_counter_all: 0,
    custom_fields: {},
  }),
);

const studioMock: MockedResponse<
  GQL.FindStudiosQuery,
  GQL.FindStudiosQueryVariables
> = {
  request: { query: GQL.FindStudiosDocument, variables: () => true },
  result: (variables) => {
    window.homeFixtureQueries.push(variables.filter?.sort ?? "studios");
    return { data: { findStudios: { count: studios.length, studios } } };
  },
  delay: 150,
  maxUsageCount: Number.POSITIVE_INFINITY,
};
const filtersMock: MockedResponse<GQL.FindSavedFiltersQuery> = {
  request: { query: GQL.FindSavedFiltersDocument, variables: {} },
  result: () => {
    window.homeFixtureQueries.push("saved-filters");
    return { data: { findSavedFilters: [] } };
  },
  maxUsageCount: Number.POSITIVE_INFINITY,
};
const studioPrefetchMock: MockedResponse<
  GQL.FindStudioQuery,
  GQL.FindStudioQueryVariables
> = {
  request: { query: GQL.FindStudioDocument, variables: () => true },
  result: ({ id }) => ({
    data: { findStudio: studios.find((studio) => studio.id === id) ?? null },
  }),
  maxUsageCount: Number.POSITIVE_INFINITY,
};

const definitionMock: MockedResponse<
  GQL.FindSavedFilterQuery,
  GQL.FindSavedFilterQueryVariables
> = {
  request: { query: GQL.FindSavedFilterDocument, variables: { id: "2" } },
  result: () => {
    window.homeFixtureDefinitions.push("2");
    return {
      data: {
        findSavedFilter: {
          __typename: "SavedFilter",
          id: "2",
          name: "Row 2",
          mode: GQL.FilterMode.Studios,
          filter_ast: null,
          ui_options: null,
          find_filter: {
            __typename: "SavedFindFilterType",
            q: null,
            page: 1,
            per_page: 25,
            sort: "random",
            direction: GQL.SortDirectionEnum.Asc,
          },
        },
      },
    };
  },
  delay: 150,
  maxUsageCount: Number.POSITIVE_INFINITY,
};
const client = new ApolloClient({
  cache: new InMemoryCache(),
  link: new MockLink([
    studioMock,
    filtersMock,
    studioPrefetchMock,
    definitionMock,
  ]),
});

const root = createRootRoute({
  component: () => (
    <ApolloProvider client={client}>
      <ConfigurationProvider
        configuration={{
          ...playerConfiguration,
          ui: { frontPageContent: rows },
        }}
      >
        <MobileNavigationProvider>
          <div
            data-app-viewport
            className="flex h-dvh flex-col overflow-hidden"
          >
            <RouteViewport>
              <Outlet />
            </RouteViewport>
            <BottomTabBar />
          </div>
        </MobileNavigationProvider>
      </ConfigurationProvider>
    </ApolloProvider>
  ),
});
const router = createRouter({
  basepath: "/home-fixture",
  defaultPreload: "intent",
  routeTree: root.addChildren([
    createRoute({
      getParentRoute: () => root,
      path: "/",
      loader: ({ preload }) => {
        if (preload) window.homeFixturePreloaded = true;
        return preloadFrontPage(client, rows);
      },
      component: FrontPage,
    }),
    ...["scenes", "images", "groups"].map((path) =>
      createRoute({
        getParentRoute: () => root,
        path,
        component: () => <h1 className="p-4">{path}</h1>,
      }),
    ),
  ]),
});
installRouteTransitions(router);

export function HomeFixture() {
  return <RouterProvider router={router} />;
}
