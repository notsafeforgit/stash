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
import { SceneDestroyDocument } from "./generated-graphql";

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
