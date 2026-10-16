// @vitest-environment jsdom
import {
  ApolloClient,
  ApolloLink,
  Observable,
  gql,
  type TypedDocumentNode,
} from "@apollo/client";
import { afterEach, expect, it, vi } from "vitest";
import { createCache } from "./create-client";
import { refetchAfterEntityMutation } from "./entity-job-invalidation";

type Variables = { page: number; excluded: string };
type Galleries = {
  findGalleries: { count: number; galleries: { id: string }[] };
};
const galleries: TypedDocumentNode<Galleries, Variables> = gql`
  query CachedGalleries($page: Int!, $excluded: ID!) {
    findGalleries(filter: { page: $page, q: "example" }, gallery_filter_ast: {
      root: { condition: { field: "performer_tags",
        value: { value: [$excluded], excludes: [], modifier: EXCLUDES, depth: -1 } } }
    }) { count galleries { id } }
  }
`;
const scenes = gql`query CachedScenes { findScenes { scenes { id } } }`;
const count = gql`query CachedSceneCount { result: findScenes { count } }`;
const configuration = gql`query UnrelatedConfig { configuration { ui } }`;
const performerEdit = gql`
  mutation EditPerformer { performerUpdate(input: {}) { id } }
`;
const tagEdit = gql`mutation EditTag { tagUpdate(input: {}) { id } }`;
const variables = { page: 2, excluded: "parent-tag" };
const clients: ApolloClient[] = [];

afterEach(() => {
  for (const client of clients.splice(0)) client.stop();
});

function setup() {
  let excluded = false;
  let reject = false;
  const requests: string[] = [];
  const client = new ApolloClient({
    cache: createCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          const name = operation.operationName ?? "";
          requests.push(name);
          if (name === "EditPerformer" || name === "EditTag") {
            if (reject) {
              observer.next({ errors: [{ message: "Edit rejected" }] });
            } else {
              excluded = !excluded;
              const field =
                name === "EditPerformer" ? "performerUpdate" : "tagUpdate";
              observer.next({ data: { [field]: { id: "edited" } } });
            }
          } else if (name === "CachedGalleries") {
            observer.next({
              data: {
                findGalleries: {
                  __typename: "FindGalleriesResultType",
                  count: excluded ? 0 : 3,
                  galleries: excluded
                    ? []
                    : [{ __typename: "Gallery", id: "album" }],
                },
              },
            });
          } else if (name === "CachedScenes") {
            observer.next({
              data: {
                findScenes: {
                  __typename: "FindScenesResultType",
                  scenes: excluded
                    ? []
                    : [{ __typename: "Scene", id: "scene" }],
                },
              },
            });
          } else if (name === "CachedSceneCount") {
            observer.next({
              data: {
                result: {
                  __typename: "FindScenesResultType",
                  count: excluded ? 0 : 3,
                },
              },
            });
          } else if (name === "UnrelatedConfig") {
            observer.next({ data: { configuration: { ui: {} } } });
          } else throw new Error(`Unexpected operation ${name}`);
          observer.complete();
        }),
    ),
  });
  clients.push(client);
  return {
    client,
    requests,
    reject: () => {
      reject = true;
    },
    edit: (mutation = performerEdit) =>
      client.mutate({
        mutation,
        awaitRefetchQueries: true,
        refetchQueries: (result) =>
          refetchAfterEntityMutation(client, mutation, result.data),
      }),
    search: (page = 2) =>
      client.query({ query: galleries, variables: { ...variables, page } }),
  };
}

it.each([performerEdit, tagEdit])(
  "rechecks inactive dependent searches and every cached page after an edit",
  async (mutation) => {
    const { client, requests, search, edit } = setup();
    await search(1);
    await search(2);
    await client.query({ query: configuration });
    // A normal return without editing uses the same cached page.
    expect((await search(2)).data?.findGalleries.count).toBe(3);
    expect(requests.filter((name) => name === "CachedGalleries")).toHaveLength(
      2,
    );

    await edit(mutation);
    // An inactive list is invalidated without eagerly downloading its pages.
    expect(requests.filter((name) => name === "CachedGalleries")).toHaveLength(
      2,
    );
    for (const page of [1, 2]) {
      expect((await search(page)).data?.findGalleries).toMatchObject({
        count: 0,
        galleries: [],
      });
    }
    expect(requests.filter((name) => name === "CachedGalleries")).toHaveLength(
      4,
    );
    await client.query({ query: configuration });
    expect(requests.filter((name) => name === "UnrelatedConfig")).toHaveLength(
      1,
    );
    expect(client.cache.extract()).toMatchObject({
      "Gallery:album": { id: "album" },
    });
    // Removing the exclusion also needs a search: the old empty result cannot
    // discover newly eligible entities through normalized entity updates.
    await edit(mutation);
    expect((await search()).data?.findGalleries.count).toBe(3);
  },
);

it("refreshes both inactive split rows and totals after related edits", async () => {
  const { client, edit, requests } = setup();
  for (const query of [scenes, count]) await client.query({ query });
  await edit();
  expect(requests).toEqual([
    "CachedScenes",
    "CachedSceneCount",
    "EditPerformer",
  ]);
  expect((await client.query({ query: scenes })).data).toMatchObject({
    findScenes: { scenes: [] },
  });
  expect((await client.query({ query: count })).data).toMatchObject({
    result: { count: 0 },
  });
});

it("refreshes a mounted affected list once and leaves unrelated active queries alone", async () => {
  const { client, requests, edit, search } = setup();
  await search();
  await client.query({ query: configuration });
  const gallery = client.watchQuery({ query: galleries, variables });
  const subscription = gallery.subscribe({});
  const config = client.watchQuery({ query: configuration }).subscribe({});
  await edit();
  await vi.waitFor(() =>
    expect(gallery.getCurrentResult()?.data).toMatchObject({
      findGalleries: { count: 0 },
    }),
  );
  expect(requests.filter((name) => name === "CachedGalleries")).toHaveLength(2);
  expect(requests.filter((name) => name === "UnrelatedConfig")).toHaveLength(1);
  subscription.unsubscribe();
  config.unsubscribe();
  // A completed active refetch is reusable when returning from details.
  expect((await search()).data?.findGalleries.count).toBe(0);
  expect(requests.filter((name) => name === "CachedGalleries")).toHaveLength(2);
});

it("keeps cached searches after a rejected edit", async () => {
  const { requests, reject, edit, search } = setup();
  await search();
  reject();
  await expect(edit()).rejects.toThrow("Edit rejected");
  expect((await search()).data?.findGalleries.count).toBe(3);
  expect(requests.filter((name) => name === "CachedGalleries")).toHaveLength(1);
});
