import { ApolloClient, ApolloLink, Observable } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { EntityCard } from "@/components/cards/entity-card";
import {
  EntityListPage,
  type EntityListPageConfig,
} from "@/components/list/entity-list-page";
import { createCache } from "@/core/create-client";
import { objectTitle } from "@/core/files";
import * as GQL from "@/core/generated-graphql";
import { scenes } from "./scene-lightbox";

declare global {
  interface Window {
    deferredListFixture: {
      releaseCounts: () => void;
      requests: { operation: string; page: number }[];
    };
  }
}
const pendingCounts: (() => void)[] = [];
window.deferredListFixture = {
  requests: [],
  releaseCounts: () =>
    pendingCounts.splice(0).forEach((respond) => {
      respond();
    }),
};
const scene = scenes[0];
if (!scene) throw new Error("Missing scene fixture");
const items = Array.from({ length: 80 }, (_, index) => ({
  ...scene,
  id: String(index + 1),
  title: index === 1 ? "" : `Card ${index + 1}`,
  files: scene.files.map((file) => ({
    ...file,
    id: `${index + 1}-${file.id}`,
    path: `/library/Scene file ${index + 1}.mp4`,
  })),
}));
const client = new ApolloClient({
  cache: createCache(),
  link: new ApolloLink(
    (operation) =>
      new Observable((observer) => {
        if (operation.operationName === "ShareTargetSearch") {
          // Selected cards must retain their names even outside search results.
          const data: GQL.ShareTargetSearchQuery = {
            findScenes: { __typename: "FindScenesResultType", scenes: [] },
            findImages: { __typename: "FindImagesResultType", images: [] },
            findGalleries: {
              __typename: "FindGalleriesResultType",
              galleries: [],
            },
          };
          observer.next({ data });
          observer.complete();
          return;
        }
        const variables: GQL.FindSceneListQueryVariables = operation.variables;
        const page = variables.filter?.page ?? 1;
        const size = variables.filter?.per_page ?? 40;
        window.deferredListFixture.requests.push({
          operation: operation.operationName ?? "",
          page,
        });
        if (operation.operationName === "FindSceneListCount") {
          const respond = () => {
            observer.next({
              data: {
                result: {
                  __typename: "FindScenesResultType",
                  count: items.length,
                },
              },
            });
            observer.complete();
          };
          pendingCounts.push(respond);
          return () => {
            const index = pendingCounts.indexOf(respond);
            if (index >= 0) pendingCounts.splice(index, 1);
          };
        }
        if (operation.operationName !== "FindSceneList")
          throw new Error(`Unexpected query ${operation.operationName}`);
        observer.next({
          data: {
            findScenes: {
              __typename: "FindScenesResultType",
              scenes: items.slice((page - 1) * size, page * size),
            },
          },
        });
        observer.complete();
      }),
  ),
});
const config: EntityListPageConfig<
  GQL.FindSceneListQuery,
  GQL.SlimSceneDataFragment,
  GQL.FindSceneListQueryVariables
> = {
  filterMode: GQL.FilterMode.Scenes,
  sharing: { kind: GQL.ShareEntityKind.Scene, getTitle: objectTitle },
  sidebarContent: <div />,
  source: {
    kind: "graphql",
    query: GQL.FindSceneListDocument,
    countQuery: GQL.FindSceneListCountDocument,
    makeVariables: (filter) => ({
      filter: filter.makeFindFilter(),
      scene_filter_ast: filter.makeFilterAST(),
    }),
    extractResult: (data) => ({ items: data?.findScenes.scenes ?? [] }),
  },
  renderCard: (item, mobile, selected, onSelectedChanged) => (
    <EntityCard
      id={item.id}
      label={objectTitle(item)}
      isMobile={mobile}
      selected={selected}
      onSelectedChanged={onSelectedChanged}
      destination={{
        to: "/scenes/$sceneId",
        params: { sceneId: item.id },
        search: undefined,
      }}
    >
      <EntityCard.Preview image="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='640' height='360'/%3E" />
      <EntityCard.Body>
        <EntityCard.Title>{objectTitle(item)}</EntityCard.Title>
      </EntityCard.Body>
    </EntityCard>
  ),
};

export function DeferredListFixture() {
  return (
    <ApolloProvider client={client}>
      <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
        <EntityListPage config={config} />
      </div>
    </ApolloProvider>
  );
}
