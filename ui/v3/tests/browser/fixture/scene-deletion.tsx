import type {} from "@/router";
import { ApolloClient, ApolloLink } from "@apollo/client";
import { ApolloProvider, useQuery } from "@apollo/client/react";
import { MockLink, type MockedResponse } from "@apollo/client/testing";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { SceneCard } from "@/components/cards/scene-card";
import { FrontPage } from "@/components/frontpage/front-page";
import { MobileNavigationProvider } from "@/components/layout/mobile-navigation";
import { RouteViewport } from "@/components/layout/route-viewport";
import { Toaster } from "@/components/ui/sonner";
import { ConfigurationProvider } from "@/hooks/config";
import { Route as SceneDetailRoute } from "@/routes/scenes/$sceneId";
import { createCache } from "@/core/create-client";
import { installRouteTransitions } from "@/core/route-transitions";
import { useTrackBrowsePage } from "@/hooks/use-smart-back";
import { useLibraryRestore } from "@/hooks/use-library-restore";
import * as GQL from "@/core/generated-graphql";
import { playerConfiguration } from "./player-configuration";
import { scenes } from "./scene-lightbox";

declare global {
  interface Window {
    deletionFixture: { requests: string[]; deleted: string[] };
  }
}
window.deletionFixture = { requests: [], deleted: [] };
const remaining = () =>
  scenes.filter(
    (scene) =>
      scene.id !== "slow" && !window.deletionFixture.deleted.includes(scene.id),
  );
const listResult = (): { data: GQL.FindScenesQuery } => ({
  data: {
    findScenes: {
      __typename: "FindScenesResultType",
      count: remaining().length,
      filesize: 1,
      duration: remaining().length * 12,
      scenes: remaining(),
    },
  },
});
const homeMock: MockedResponse<
  GQL.FindScenesQuery,
  GQL.FindScenesQueryVariables
> = {
  request: { query: GQL.FindScenesDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: listResult,
};
const listMock: MockedResponse<
  GQL.FindScenesMobileQuery,
  GQL.FindScenesMobileQueryVariables
> = {
  request: { query: GQL.FindScenesMobileDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: listResult,
};
const detailMock: MockedResponse<
  GQL.FindSceneQuery,
  GQL.FindSceneQueryVariables
> = {
  request: { query: GQL.FindSceneDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: ({ id }) => ({
    data: { findScene: remaining().find((scene) => scene.id === id) ?? null },
  }),
};
const deleteMock: MockedResponse<
  GQL.SceneDestroyMutation,
  GQL.SceneDestroyMutationVariables
> = {
  request: { query: GQL.SceneDestroyDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: ({ id }) => {
    window.deletionFixture.deleted.push(id);
    return { data: { sceneDestroy: true } };
  },
};
const client = new ApolloClient({
  cache: createCache(),
  link: ApolloLink.from([
    new ApolloLink((operation, forward) => {
      window.deletionFixture.requests.push(operation.operationName ?? "");
      return forward(operation);
    }),
    new MockLink([
      homeMock,
      listMock,
      detailMock,
      deleteMock,
      {
        request: { query: GQL.ListSceneScrapersDocument },
        maxUsageCount: Infinity,
        delay: 0,
        result: { data: { listScrapers: [] } },
      },
      {
        request: { query: GQL.ServerCapabilitiesDocument },
        maxUsageCount: Infinity,
        delay: 0,
        result: { data: { serverCapabilities: { downloadFormats: [] } } },
      },
      {
        request: { query: GQL.ConfigurationDocument },
        maxUsageCount: Infinity,
        delay: 0,
        result: {
          data: {
            configuration: {
              ...playerConfiguration,
              __typename: "ConfigResult",
            },
          },
        },
      },
    ]),
  ]),
});
function SceneList() {
  const { data } = useQuery(GQL.FindScenesMobileDocument);
  return (
    <div className="flex gap-3 p-4">
      {data?.findScenes.scenes.map((scene) => (
        <div key={scene.id} className="w-40">
          <SceneCard scene={scene} />
        </div>
      ))}
    </div>
  );
}
function Shell() {
  useTrackBrowsePage();
  useLibraryRestore();
  return (
    <MobileNavigationProvider>
      <nav className="flex gap-4 p-2">
        <Link to="/">Home</Link>
        <Link to="/scenes">Scenes</Link>
      </nav>
      <RouteViewport>
        <Outlet />
      </RouteViewport>
    </MobileNavigationProvider>
  );
}
const root = createRootRoute({ component: Shell });
const router = createRouter({
  basepath: "/scene-deletion-fixture",
  defaultPreload: "intent",
  routeTree: root.addChildren([
    createRoute({
      getParentRoute: () => root,
      path: "/",
      component: FrontPage,
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/scenes",
      component: SceneList,
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/scenes/$sceneId",
      validateSearch: SceneDetailRoute.options.validateSearch,
      loader: ({ params }) =>
        client
          .query({
            query: GQL.FindSceneDocument,
            variables: { id: params.sceneId },
            fetchPolicy: "cache-first",
          })
          .then(() => undefined),
      component: SceneDetailRoute.options.component,
    }),
  ]),
});
installRouteTransitions(router);
export function SceneDeletionFixture() {
  return (
    <ApolloProvider client={client}>
      <base href="/scene-deletion-fixture/" />
      <ConfigurationProvider
        configuration={{
          ...playerConfiguration,
          interface: {
            ...playerConfiguration.interface,
            autostartVideo: false,
          },
          ui: {
            ...playerConfiguration.ui,
            frontPageContent: [
              {
                __typename: "CustomFilter",
                mode: GQL.FilterMode.Scenes,
                sortBy: "created_at",
                direction: GQL.SortDirectionEnum.Desc,
                title: "Recent scenes",
              },
            ],
          },
        }}
      >
        <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
          <RouterProvider router={router} />
        </div>
        <Toaster />
      </ConfigurationProvider>
    </ApolloProvider>
  );
}
