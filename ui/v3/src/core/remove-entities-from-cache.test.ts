// @vitest-environment jsdom
import {
  ApolloClient,
  ApolloLink,
  Observable,
  gql,
  type TypedDocumentNode,
} from "@apollo/client";
import { expect, it } from "vitest";
import { createCache } from "./create-client";
import { removeEntitiesFromCache } from "./client";
import { SceneDestroyDocument, type FindFilterType } from "./generated-graphql";

interface SceneList {
  findScenes: {
    __typename: "FindScenesResultType";
    count: number;
    scenes: { __typename: "Scene"; id: string; title: string }[];
  };
}
const scenesQuery: TypedDocumentNode<SceneList, { sort: string }> = gql`
  query CachedScenes($sort: String!) {
    findScenes(filter: { sort: $sort }) { count scenes { id title } }
  }
`;

it("removes a successfully deleted scene from inactive Home and list queries", async () => {
  const client = new ApolloClient({
    cache: createCache(),
    link: new ApolloLink(
      () =>
        new Observable((observer) => {
          observer.next({ data: { sceneDestroy: true } });
          observer.complete();
        }),
    ),
  });
  for (const sort of ["created_at", "date", "random_37"]) {
    client.writeQuery({
      query: scenesQuery,
      variables: { sort },
      data: {
        findScenes: {
          __typename: "FindScenesResultType",
          count: 2,
          scenes: ["1", "2"].map((id) => ({
            __typename: "Scene",
            id,
            title: `Scene ${id}`,
          })),
        },
      },
    });
  }
  await client.mutate({
    mutation: SceneDestroyDocument,
    variables: { id: "1" },
    update(cache) {
      removeEntitiesFromCache({
        cache,
        typename: "Scene",
        listFieldName: "findScenes",
        itemsField: "scenes",
        ids: ["1"],
      });
    },
  });
  for (const sort of ["created_at", "date", "random_37"]) {
    expect(
      client.readQuery({ query: scenesQuery, variables: { sort } })?.findScenes,
    ).toMatchObject({ count: 1, scenes: [{ id: "2" }] });
  }
  client.stop();
});

const pageQuery: TypedDocumentNode<SceneList, { filter: FindFilterType }> = gql`
  query CachedScenePage($filter: FindFilterType) {
    findScenes(filter: $filter) { count scenes { id title } }
  }
`;
const countQuery: TypedDocumentNode<
  { findScenes: Pick<SceneList["findScenes"], "__typename" | "count"> },
  { filter: FindFilterType }
> = gql`
  query CachedSceneCount($filter: FindFilterType) {
    findScenes(filter: $filter) { count }
  }
`;

it.each([
  { name: "another page", items: ["1", "2"], search: "" },
  { name: "partially overlapping page", items: ["2", "3"], search: "" },
  { name: "another filter", items: ["1", "2"], search: "other" },
])(
  "invalidates the total for $name when deleted membership is unknown",
  ({ items, search }) => {
    const cache = createCache();
    const variables = { filter: { page: 1, per_page: 2, q: search } };
    cache.writeQuery({
      query: pageQuery,
      variables,
      data: {
        findScenes: {
          __typename: "FindScenesResultType",
          count: 4,
          scenes: items.map((id) => ({ __typename: "Scene", id, title: id })),
        },
      },
    });
    removeEntitiesFromCache({
      cache,
      typename: "Scene",
      listFieldName: "findScenes",
      itemsField: "scenes",
      ids: ["3", "4"],
    });

    expect(cache.readQuery({ query: countQuery, variables })).toBeNull();
    expect(
      cache
        .readQuery({ query: pageQuery, variables, returnPartialData: true })
        ?.findScenes.scenes.map((scene) => scene.id),
    ).toEqual(items.filter((id) => id !== "3"));
  },
);

it("invalidates a cached total even when its rows have not been fetched", () => {
  const cache = createCache();
  const variables = { filter: { page: 1, per_page: 2 } };
  cache.writeQuery({
    query: countQuery,
    variables,
    data: { findScenes: { __typename: "FindScenesResultType", count: 4 } },
  });
  removeEntitiesFromCache({
    cache,
    typename: "Scene",
    listFieldName: "findScenes",
    itemsField: "scenes",
    ids: ["3", "4"],
  });
  expect(cache.readQuery({ query: countQuery, variables })).toBeNull();
});
