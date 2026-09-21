import type {} from "@/router";
import { ApolloClient, ApolloLink } from "@apollo/client";
import { ApolloProvider, useQuery } from "@apollo/client/react";
import { useState } from "react";
import { MockLink, type MockedResponse } from "@apollo/client/testing";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { SceneCard } from "@/components/cards/scene-card";
import { SceneRowContextMenu } from "@/components/cards/use-scene-context-menu";
import { ScenePlayer } from "@/components/player/scene-player";
import { Table, TableBody, TableRow, TableCell } from "@/components/ui/table";
import { MobileNavigationProvider } from "@/components/layout/mobile-navigation";
import { Toaster } from "@/components/ui/sonner";
import { Button } from "@/components/ui/button";
import { SceneGenerateDialog } from "@/components/detail/scene-generate-dialog";
import { LibraryTasks } from "@/components/settings/tasks/library-tasks";
import { FilterBuilder } from "@/components/filters/filter-builder";
import { ListFilterModel } from "@/models/list-filter/filter";
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
      regenerations: GQL.SceneRegenerateCoverMutationVariables[];
      generations: GQL.MetadataGenerateMutationVariables[];
      performerUpdates: GQL.PerformerUpdateImageMutationVariables[];
      failPerformerUpdate: boolean;
    };
  }
}
window.coverFixture = {
  finished: false,
  requests: [],
  screenshots: [],
  regenerations: [],
  generations: [],
  performerUpdates: [],
  failPerformerUpdate: false,
};
const base = scenes[0];
if (!base) throw new Error("Missing synthetic scene");
const url = (path: string) => new URL(path, location.href).href;
const performers: GQL.PerformerDataFragment[] = ["1", "2"].map((id) => ({
  __typename: "Performer",
  id,
  name: `Performer ${id}`,
  disambiguation: null,
  image_path: url(`/covers/performer-${id}-old.jpg`),
  urls: [],
  gender: null,
  birthdate: null,
  ethnicity: null,
  country: null,
  eye_color: null,
  height_cm: null,
  measurements: null,
  fake_tits: null,
  penis_length: null,
  circumcised: null,
  career_start: null,
  career_end: null,
  tattoos: null,
  piercings: null,
  aliases: [],
  favorite: false,
  ignore_auto_tag: false,
  ignore_primary_name_auto_tag: false,
  scene_count: 1,
  image_count: 0,
  gallery_count: 0,
  group_count: 0,
  performer_count: 0,
  o_counter: 0,
  created_at: "2026-09-19",
  updated_at: "2026-09-19",
  tags: [],
  stash_ids: [],
  rating100: null,
  details: null,
  death_date: null,
  hair_color: null,
  weight: null,
  custom_fields: {},
}));
function artwork(): Pick<
  GQL.SceneDataFragment,
  "paths" | "preview_image" | "cover_origin"
> {
  if (!base) throw new Error("Missing cover fixture scene");
  const revision = window.coverFixture.finished ? "new" : "old";
  const fallback = url(`/covers/${revision}.jpg`);
  return {
    cover_origin: {
      __typename: "SceneCoverOrigin",
      at: new URLSearchParams(location.search).has("unknown") ? null : 1.125,
      source_file_id: "1",
      status: new URLSearchParams(location.search).has("unknown")
        ? GQL.SceneCoverOriginStatus.Unknown
        : new URLSearchParams(location.search).has("stale")
          ? GQL.SceneCoverOriginStatus.Changed
          : GQL.SceneCoverOriginStatus.Available,
    },
    paths: { ...base.paths, screenshot: fallback },
    preview_image: {
      thumbnail:
        revision === "new"
          ? {
              __typename: "PreviewImage",
              fallback: url("/covers/new-thumbnail.jpg"),
              sources: [
                {
                  __typename: "PreviewImageSource",
                  url: url("/covers/new-thumbnail.avif"),
                  mime_type: "image/avif",
                  dynamic_range: GQL.PreviewImageDynamicRange.Adaptive,
                  width: 80,
                  height: 45,
                },
              ],
            }
          : null,
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
  performers,
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
            __typename: "SceneStreamEndpoint" as const,
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
const regenerateMock: MockedResponse<
  GQL.SceneRegenerateCoverMutation,
  GQL.SceneRegenerateCoverMutationVariables
> = {
  request: { query: GQL.SceneRegenerateCoverDocument, variables: { id: "1" } },
  maxUsageCount: Infinity,
  delay: 0,
  result: (variables) => {
    window.coverFixture.regenerations.push(variables);
    return { data: { sceneRegenerateCover: "7" } };
  },
};
const scenePerformersMock: MockedResponse<GQL.FindSceneImagePerformersQuery> = {
  request: {
    query: GQL.FindSceneImagePerformersDocument,
    variables: { id: scene.id },
  },
  maxUsageCount: Infinity,
  delay: 0,
  result: () => ({
    data: { findScene: { __typename: "Scene", id: scene.id, performers } },
  }),
};
const performerImageMock: MockedResponse<
  GQL.PerformerUpdateImageMutation,
  GQL.PerformerUpdateImageMutationVariables
> = {
  request: { query: GQL.PerformerUpdateImageDocument, variables: () => true },
  maxUsageCount: Infinity,
  delay: 300,
  result: (variables) => {
    window.coverFixture.performerUpdates.push(variables);
    if (window.coverFixture.failPerformerUpdate)
      return { errors: [{ message: "Image update failed" }] };
    return {
      data: {
        performerUpdate: {
          __typename: "Performer",
          id: variables.id,
          image_path: url(`/covers/performer-${variables.id}-new.jpg`),
        },
      },
    };
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
  result: {
    data: {
      serverCapabilities: {
        __typename: "ServerCapabilities",
        downloadFormats: [],
      },
    },
  },
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
const savedFiltersMock: MockedResponse<
  GQL.FindSavedFiltersQuery,
  GQL.FindSavedFiltersQueryVariables
> = {
  request: {
    query: GQL.FindSavedFiltersDocument,
    variables: { mode: GQL.FilterMode.Scenes },
  },
  maxUsageCount: Infinity,
  delay: 0,
  result: { data: { findSavedFilters: [] } },
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
      regenerateMock,
      scenePerformersMock,
      performerImageMock,
      generateMock,
      jobMock,
      coversMock,
      scrapersMock,
      capabilitiesMock,
      configurationMock,
      savedFiltersMock,
    ]),
  ]),
});
function SceneList() {
  const { data } = useQuery(GQL.FindScenesMobileDocument);
  const { data: imageData } = useQuery(GQL.FindSceneImagePerformersDocument, {
    variables: { id: scene.id },
  });
  const params = new URL(location.href).searchParams;
  const target = params.has("performer") ? "1" : undefined;
  return (
    <div className="p-4">
      <h1>Scenes</h1>
      {imageData?.findScene?.performers.map((performer) => (
        <img
          key={performer.id}
          data-testid={`performer-image-${performer.id}`}
          src={performer.image_path ?? undefined}
          alt={performer.name}
          width={48}
          height={48}
        />
      ))}
      <div className="max-w-80" data-testid="scene-card">
        {data?.findScenes.scenes.map((item) => (
          <SceneCard
            key={item.id}
            scene={item}
            performerImageTargetId={target}
          />
        ))}
      </div>
      <Table>
        <TableBody>
          {data?.findScenes.scenes.map((item) => (
            <SceneRowContextMenu
              key={item.id}
              scene={item}
              performerImageTargetId={target}
            >
              <TableRow data-testid="scene-row">
                <TableCell>{item.title}</TableCell>
              </TableRow>
            </SceneRowContextMenu>
          ))}
        </TableBody>
      </Table>
      {params.has("playing") && <PlayingScene />}
    </div>
  );
}
function PlayingScene() {
  const { data } = useQuery(GQL.FindSceneDocument, {
    variables: { id: scene.id },
  });
  return data?.findScene ? (
    <ScenePlayer scene={data.findScene} autostartEnabled={false} />
  ) : null;
}
const root = createRootRoute({
  component: () => (
    <MobileNavigationProvider>
      <Outlet />
    </MobileNavigationProvider>
  ),
});

function GenerateSelectedScenes() {
  const [open, setOpen] = useState(false);
  const matching = new URLSearchParams(location.search).has("matching");
  const [filter, setFilter] = useState(
    () => new ListFilterModel(GQL.FilterMode.Scenes),
  );
  return (
    <>
      {matching && (
        <div className="max-w-md">
          <FilterBuilder
            mode={GQL.FilterMode.Scenes}
            filter={filter}
            setFilter={setFilter}
            root={
              filter.filterAst?.kind === "group" ? filter.filterAst : undefined
            }
            onChange={(root) => {
              const next = filter.clone();
              next.filterAst = root;
              setFilter(next);
            }}
            onCurrentSavedFilterChange={() => {}}
          />
        </div>
      )}
      <Button onClick={() => setOpen(true)}>Generate selected scenes</Button>
      <SceneGenerateDialog
        open={open}
        onOpenChange={setOpen}
        sceneIds={
          new URLSearchParams(location.search).has("empty") ? [] : ["1", "2"]
        }
        totalCount={matching ? 200 : undefined}
        applyToAllTarget={
          matching
            ? {
                findFilter: filter.makeFindFilter(),
                filterAST: filter.makeFilterAST(),
              }
            : undefined
        }
      />
    </>
  );
}

const router = createRouter({
  basepath: "/scene-cover-fixture",
  routeTree: root.addChildren([
    createRoute({
      getParentRoute: () => root,
      path: "/generate",
      component: GenerateSelectedScenes,
    }),
    createRoute({
      getParentRoute: () => root,
      path: "/tasks",
      component: LibraryTasks,
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
