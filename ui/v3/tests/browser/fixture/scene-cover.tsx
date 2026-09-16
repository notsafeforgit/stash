import type {} from "@/router";
import { ApolloClient, ApolloLink } from "@apollo/client";
import { ApolloProvider, useQuery } from "@apollo/client/react";
import { MockLink, type MockedResponse } from "@apollo/client/testing";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { SceneCard } from "@/components/cards/scene-card";
import { MobileNavigationProvider } from "@/components/layout/mobile-navigation";
import { Toaster } from "@/components/ui/sonner";
import { ConfigurationProvider } from "@/hooks/config";
import { Route as SceneDetailRoute } from "@/routes/scenes/$sceneId";
import { createCache } from "@/core/create-client";
import * as GQL from "@/core/generated-graphql";
import { playerConfiguration } from "./player-configuration";
import { scenes } from "./scene-lightbox";

declare global {
  interface Window {
    coverFixture: {
      finished: boolean;
      requests: string[];
      screenshots: GQL.SceneGenerateScreenshotMutationVariables[];
      generations: GQL.MetadataGenerateMutationVariables[];
    };
  }
}
window.coverFixture = {
  finished: false,
  requests: [],
  screenshots: [],
  generations: [],
};
const base = scenes[0];
if (!base) throw new Error("Missing synthetic scene");
const url = (path: string) => new URL(path, location.href).href;
function artwork(): Pick<GQL.SceneDataFragment, "paths" | "preview_image"> {
  const revision = window.coverFixture.finished ? "new" : "old";
  const fallback = url(`/covers/${revision}.jpg`);
  return {
    paths: { ...base?.paths, screenshot: fallback },
    preview_image: {
      __typename: "PreviewImage",
      fallback,
      sources: [
        {
          __typename: "PreviewImageSource",
          url: url(`/covers/${revision}.avif`),
          mime_type: "image/avif",
          dynamic_range: GQL.PreviewImageDynamicRange.Adaptive,
          width: 160,
          height: 90,
        },
      ],
    },
  };
}
const scene: GQL.SceneDataFragment = {
  ...base,
  ...artwork(),
  captions: [],
  scene_markers: [],
};
let streamRevision = 0;
const detailMock: MockedResponse<
  GQL.FindSceneQuery,
  GQL.FindSceneQueryVariables
> = {
  request: { query: GQL.FindSceneDocument, variables: { id: scene.id } },
  maxUsageCount: Infinity,
  delay: 0,
  result: () => ({
    data: {
      findScene: {
        ...scene,
        ...artwork(),
        // A real authenticated refetch issues a fresh signature, even for the same file.
        sceneStreams: [
          {
            url: url(`/scene/1/stream?signature=${++streamRevision}`),
            mime_type: "video/mp4",
            label: "Direct stream",
          },
        ],
      },
    },
  }),
};
const listMock: MockedResponse<
  GQL.FindScenesMobileQuery,
  GQL.FindScenesMobileQueryVariables
> = {
  request: { query: GQL.FindScenesMobileDocument, variables: {} },
  maxUsageCount: Infinity,
  delay: 0,
  result: () => ({
    data: {
      findScenes: {
        __typename: "FindScenesResultType",
        count: 1,
        filesize: 1,
        duration: 12,
        scenes: [{ ...scene, ...artwork() }],
      },
    },
  }),
};
const screenshotMock: MockedResponse<
  GQL.SceneGenerateScreenshotMutation,
  GQL.SceneGenerateScreenshotMutationVariables
> = {
  request: {
    query: GQL.SceneGenerateScreenshotDocument,
    variables: () => true,
  },
  maxUsageCount: Infinity,
  delay: 0,
  result: (variables) => {
    window.coverFixture.screenshots.push(variables);
    return { data: { sceneGenerateScreenshot: "7" } };
  },
};
const generateMock: MockedResponse<
  GQL.MetadataGenerateMutation,
  GQL.MetadataGenerateMutationVariables
> = {
  request: { query: GQL.MetadataGenerateDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 0,
  result: (variables) => {
    window.coverFixture.generations.push(variables);
    return { data: { metadataGenerate: "7" } };
  },
};
const jobMock: MockedResponse<GQL.FindJobQuery, GQL.FindJobQueryVariables> = {
  request: { query: GQL.FindJobDocument, variables: { input: { id: "7" } } },
  maxUsageCount: Infinity,
  delay: 0,
  result: () => ({
    data: {
      findJob: {
        __typename: "Job",
        id: "7",
        status: window.coverFixture.finished
          ? GQL.JobStatus.Finished
          : GQL.JobStatus.Running,
        description: "Generate cover",
        addTime: "2026-09-16T00:00:00Z",
        progress: 0,
        subTasks: [],
        startTime: null,
        endTime: null,
        error: null,
      },
    },
  }),
};
const coversMock: MockedResponse<
  GQL.FindSceneCoversQuery,
  GQL.FindSceneCoversQueryVariables
> = {
  request: {
    query: GQL.FindSceneCoversDocument,
    variables: { ids: [scene.id] },
  },
  maxUsageCount: Infinity,
  delay: 0,
  result: () => ({
    data: {
      findScenes: {
        __typename: "FindScenesResultType",
        scenes: [{ __typename: "Scene", id: scene.id, ...artwork() }],
      },
    },
  }),
};
const scrapersMock: MockedResponse<
  GQL.ListSceneScrapersQuery,
  GQL.ListSceneScrapersQueryVariables
> = {
  request: { query: GQL.ListSceneScrapersDocument },
  maxUsageCount: Infinity,
  delay: 0,
  result: { data: { listScrapers: [] } },
};
const capabilitiesMock: MockedResponse<GQL.ServerCapabilitiesQuery> = {
  request: { query: GQL.ServerCapabilitiesDocument },
  maxUsageCount: Infinity,
  delay: 0,
  result: { data: { serverCapabilities: { downloadFormats: [] } } },
};
const configurationMock: MockedResponse<GQL.ConfigurationQuery> = {
  request: { query: GQL.ConfigurationDocument },
  maxUsageCount: Infinity,
  delay: 0,
  result: {
    data: {
      configuration: { ...playerConfiguration, __typename: "ConfigResult" },
    },
  },
};
const client = new ApolloClient({
  cache: createCache(),
  link: ApolloLink.from([
    new ApolloLink((operation, forward) => {
      window.coverFixture.requests.push(operation.operationName ?? "");
      return forward(operation);
    }),
    new MockLink([
      detailMock,
      listMock,
      screenshotMock,
      generateMock,
      jobMock,
      coversMock,
      scrapersMock,
      capabilitiesMock,
      configurationMock,
    ]),
  ]),
});
function SceneList() {
  const { data } = useQuery(GQL.FindScenesMobileDocument);
  return (
    <div className="p-4">
      <h1>Scenes</h1>
      <div className="max-w-80" data-testid="scene-card">
        {data?.findScenes.scenes.map((item) => (
          <SceneCard key={item.id} scene={item} />
        ))}
      </div>
    </div>
  );
}
const root = createRootRoute({
  component: () => (
    <MobileNavigationProvider>
      <Outlet />
    </MobileNavigationProvider>
  ),
});
const router = createRouter({
  basepath: "/scene-cover-fixture",
  routeTree: root.addChildren([
    createRoute({
      getParentRoute: () => root,
      path: "/scenes",
      component: SceneList,
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/scenes/$sceneId",
      validateSearch: SceneDetailRoute.options.validateSearch,
      component: SceneDetailRoute.options.component,
    }),
  ]),
});
export function SceneCoverFixture() {
  return (
    <ApolloProvider client={client}>
      <ConfigurationProvider
        configuration={{
          ...playerConfiguration,
          interface: {
            ...playerConfiguration.interface,
            autostartVideo: false,
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
