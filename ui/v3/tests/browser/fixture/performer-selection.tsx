import {
  ApolloClient,
  ApolloLink,
  Observable,
  type OperationVariables,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { DetailTabs } from "@/components/detail/detail-tabs";
import { EntityCard } from "@/components/cards/entity-card";
import {
  EntityListPage,
  type EntityListPageConfig,
} from "@/components/list/entity-list-page";
import { Toaster } from "@/components/ui/sonner";
import { createCache } from "@/core/create-client";
import * as GQL from "@/core/generated-graphql";
import { objectTitle } from "@/core/files";
import { ListFilterModel } from "@/models/list-filter/filter";
import { PerformersCriterion } from "@/models/list-filter/criteria/performers";
import {
  createASTConditionFromCriterion,
  createASTGroup,
} from "@/models/list-filter/filter-ast";
import { libraryID } from "../../fixtures/library";
import { scenes } from "./scene-lightbox";

declare global {
  interface Window {
    performerSelectionFixture: {
      requests: { name: string; variables: OperationVariables }[];
      matching: "success" | "error" | "limit" | "partial" | "pending";
      releaseMatching?: () => void;
      shares: GQL.MediaShareCreateMutationVariables[];
    };
  }
}
const state: Window["performerSelectionFixture"] = {
  requests: [],
  matching: "success",
  shares: [],
};
window.performerSelectionFixture = state;
const scene = scenes[0]!;
const items = Array.from({ length: 80 }, (_, index) => ({
  ...scene,
  id: libraryID(index + 1),
  title: `Video ${index + 1}`,
  files: scene.files.map((file) => ({
    ...file,
    id: libraryID(index + 100),
    path: `/library/video-${index + 1}.mp4`,
  })),
}));
const client = new ApolloClient({
  cache: createCache(),
  link: new ApolloLink(
    (operation) =>
      new Observable((observer) => {
        const name = operation.operationName ?? "";
        state.requests.push({ name, variables: operation.variables });
        const respond = (data: object) => {
          observer.next({ data });
          observer.complete();
        };
        if (name === "FindSceneList") {
          const variables: GQL.FindSceneListQueryVariables =
            operation.variables;
          const page = variables.filter?.page ?? 1;
          const size = variables.filter?.per_page ?? 40;
          respond({
            findScenes: {
              __typename: "FindScenesResultType",
              scenes: items.slice((page - 1) * size, page * size),
            },
          });
        } else if (name === "FindSceneListCount") {
          respond({
            result: { __typename: "FindScenesResultType", count: items.length },
          });
        } else if (name === "ShareTargetSearch") {
          respond({
            findScenes: { scenes: [] },
            findImages: { images: [] },
            findGalleries: { galleries: [] },
          });
        } else if (name === "ShareMatchingTargets") {
          const send = () => {
            if (state.matching === "error")
              observer.error(new Error("Matching query failed"));
            else
              respond({
                sharingConfiguration: { max_items: 2000 },
                findScenes: {
                  count: state.matching === "limit" ? 2001 : items.length,
                  scenes:
                    state.matching === "partial" ? items.slice(0, 40) : items,
                },
              });
          };
          if (state.matching === "pending") state.releaseMatching = send;
          else send();
        } else if (name === "MediaShareCreate") {
          const variables =
            operation.variables as GQL.MediaShareCreateMutationVariables;
          state.shares.push(variables);
          const data: GQL.MediaShareCreateMutation = {
            mediaShareCreate: {
              __typename: "MediaShareCreated",
              url: "https://shares.test/share/ABCDEFGHIJKLMNOPQRSTUV?key=example",
              share: {
                __typename: "MediaShare",
                id: "ABCDEFGHIJKLMNOPQRSTUV",
                url: "https://shares.test/share/ABCDEFGHIJKLMNOPQRSTUV?key=example",
                label: variables.input.label,
                created_at: new Date().toISOString(),
                expires_at: variables.input.expires_at,
                revoked_at: null,
                allow_download: false,
                show_metadata: false,
                access_count: 0,
                last_accessed_at: null,
                media_count: variables.input.targets.length,
                items: variables.input.targets.map((target) => ({
                  ...target,
                  __typename: "MediaShareEntry",
                  title: target.id,
                  media_count: 1,
                })),
              },
            },
          };
          respond(data);
        } else throw new Error(`Unexpected query ${name}`);
      }),
  ),
});

const filter = new ListFilterModel(GQL.FilterMode.Scenes);
const performer = new PerformersCriterion();
performer.modifier = GQL.CriterionModifier.IncludesAll;
performer.value = {
  items: [{ id: libraryID(999), label: "Example performer" }],
  excluded: [],
};
filter.lockedFilterAst = createASTGroup(
  GQL.FilterMode.Scenes,
  GQL.FilterGroupOperator.And,
  [createASTConditionFromCriterion(GQL.FilterMode.Scenes, performer)],
);
const config: EntityListPageConfig<
  GQL.FindSceneListQuery,
  GQL.SlimSceneDataFragment,
  GQL.FindSceneListQueryVariables
> = {
  filterMode: GQL.FilterMode.Scenes,
  sharing: { kind: GQL.ShareEntityKind.Scene, getTitle: objectTitle },
  sidebarContent: <div />,
  // Include the real delete action's footprint to test narrow-screen overflow.
  onDeleteSelection: () => {},
  source: {
    kind: "graphql",
    query: GQL.FindSceneListDocument,
    countQuery: GQL.FindSceneListCountDocument,
    makeVariables: (value) => ({
      filter: value.makeFindFilter(),
      scene_filter_ast: value.makeFilterAST(),
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

export function PerformerSelectionFixture() {
  return (
    <ApolloProvider client={client}>
      <CollectionDetailLayout title="Example performer" onBack={() => {}}>
        <DetailTabs
          activeTab="scenes"
          onTabChange={() => {}}
          tabs={[
            {
              id: "scenes",
              label: "Videos",
              content: (
                <EntityListPage
                  config={config}
                  defaultFilter={filter}
                  mobileChromeFixed
                />
              ),
            },
          ]}
        />
      </CollectionDetailLayout>
      <Toaster />
    </ApolloProvider>
  );
}
