import { useMemo, useState } from "react";
import { useRouter, useSearch } from "@tanstack/react-router";
import { ApolloClient, ApolloLink, Observable } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { EntityListPage } from "@/components/list/entity-list-page";
import { View } from "@/components/list/views";
import {
  useImageListConfig,
  useSceneListConfig,
} from "@/components/list/entity-list-configs";
import { createCache } from "@/core/create-client";
import type * as GQL from "@/core/generated-graphql";
import { FilterMode } from "@/core/generated-graphql";
import { savedFiltersLink } from "./saved-filters";
import { Toaster } from "@/components/ui/sonner";
import { ConfigurationProvider } from "@/hooks/config";
import { images } from "./image-lightbox";
import { scenes } from "./scene-lightbox";
import { playerConfiguration } from "./player-configuration";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { DetailTabs } from "@/components/detail/detail-tabs";

declare global {
  interface Window {
    lightboxListRequests: string[];
  }
}

const image = images[0];
const scene = scenes[0];
if (!image || !scene) throw new Error("Missing media fixtures");
const ids = Array.from(
  { length: 80 },
  (_, i) => `00000000-0000-4000-8000-${String(i + 1).padStart(12, "0")}`,
);
const imageItems = ids.map((id, i) => ({
  ...image,
  id,
  title: `Image ${i + 1}`,
  paths: { ...image.paths, thumbnail: `${image.paths.thumbnail}#thumbnail` },
  performers: [],
  galleries: [],
  created_at: "2026-10-13",
  updated_at: "2026-10-13",
  custom_fields: {},
}));
const sceneItems = ids.map((id, i) => ({
  ...scene,
  id,
  title: `Scene ${i + 1}`,
  scene_markers: [],
}));

function makeClient() {
  window.lightboxListRequests = [];
  return new ApolloClient({
    cache: createCache(),
    link: ApolloLink.from([
      savedFiltersLink(
        new URLSearchParams(location.search).get("kind") === "scene"
          ? FilterMode.Scenes
          : FilterMode.Images,
      ),
      new ApolloLink(
        (operation) =>
          new Observable((observer) => {
            const name = operation.operationName;
            window.lightboxListRequests.push(name ?? "");
            const variables: GQL.FindSceneListQueryVariables & { id?: string } =
              operation.variables;
            const size = variables.filter?.per_page ?? 40;
            const start = ((variables.filter?.page ?? 1) - 1) * size;
            let data: object;
            switch (name) {
              case "FindSavedFilters":
                data = { findSavedFilters: [] };
                break;
              case "FindImageList":
                data = {
                  findImages: {
                    __typename: "FindImagesResultType",
                    images: imageItems.slice(start, start + size),
                  },
                };
                break;
              case "FindSceneList":
                data = {
                  findScenes: {
                    __typename: "FindScenesResultType",
                    scenes: sceneItems.slice(start, start + size),
                  },
                };
                break;
              case "FindImageListCount":
              case "FindSceneListCount":
                data = {
                  result: {
                    __typename:
                      name === "FindImageListCount"
                        ? "FindImagesResultType"
                        : "FindScenesResultType",
                    count: ids.length,
                  },
                };
                break;
              case "FindImage":
                data = {
                  findImage: imageItems.find(
                    (item) => item.id === variables.id,
                  ),
                };
                break;
              case "ImageRotate": {
                const item = imageItems.find(
                  (item) => item.id === variables.id,
                );
                if (!item) throw new Error("Unknown image to rotate");
                data = {
                  imageRotate: {
                    ...item,
                    paths: {
                      ...item.paths,
                      image: `${item.paths.image}#rotated`,
                    },
                  },
                };
                break;
              }
              case "FindScene":
                data = {
                  findScene: sceneItems.find(
                    (item) => item.id === variables.id,
                  ),
                };
                break;
              case "SceneSaveActivity":
                data = { sceneSaveActivity: true };
                break;
              case "SceneAddPlay":
                data = {
                  sceneAddPlay: {
                    __typename: "HistoryMutationResult",
                    count: 1,
                    history: [],
                  },
                };
                break;
              default:
                throw new Error(`Unexpected lightbox list query: ${name}`);
            }
            observer.next({ data });
            observer.complete();
          }),
      ),
    ]),
  });
}

const onEdit = () => {};
function Images() {
  const { config, lightboxElement, lightboxOpen } = useImageListConfig(onEdit);
  return (
    <>
      <EntityListPage
        config={config}
        view={View.Images}
        keyboardShortcutsDisabled={lightboxOpen}
      />
      {lightboxElement}
    </>
  );
}
function Scenes() {
  const { config, lightboxElement, lightboxOpen } = useSceneListConfig(onEdit);
  return (
    <>
      <EntityListPage
        config={config}
        view={View.Scenes}
        keyboardShortcutsDisabled={lightboxOpen}
      />
      {lightboxElement}
    </>
  );
}

function MediaTabs() {
  const { tab = "scenes" } = useSearch({ strict: false });
  const router = useRouter();
  return (
    <CollectionDetailLayout title="Example performer" onBack={() => {}}>
      <div className="md:flex md:h-full">
        <aside className="h-96 md:w-72 md:shrink-0">Performer details</aside>
        <DetailTabs
          activeTab={tab}
          onTabChange={(tab) =>
            router.navigate({
              href: `/lightbox-list?tab=${tab}`,
              replace: true,
            })
          }
          tabs={[
            { id: "scenes", label: "Scenes", content: <Scenes /> },
            { id: "images", label: "Images", content: <Images /> },
          ]}
        />
      </div>
    </CollectionDetailLayout>
  );
}

export function LightboxListFixture() {
  const client = useMemo(makeClient, []);
  const [tabbed] = useState(() =>
    new URLSearchParams(location.search).has("tabs"),
  );
  return (
    <ApolloProvider client={client}>
      <ConfigurationProvider configuration={playerConfiguration}>
        <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
          {tabbed ? (
            <MediaTabs />
          ) : new URLSearchParams(location.search).get("kind") === "scene" ? (
            <Scenes />
          ) : (
            <Images />
          )}
          <Toaster />
        </div>
      </ConfigurationProvider>
    </ApolloProvider>
  );
}
