import { gql } from "@apollo/client";
import { describe, expect, it } from "vitest";
import { createCache } from "./create-client";
import { MAX_INACTIVE_LIST_QUERIES, retainListQuery } from "./list-query-cache";

const query = gql`
  query Page($paging: FindFilterType) {
    items: findScenes(filter: $paging) { scenes { id title } }
  }
`;

describe("list query retention", () => {
  it("bounds pages across filters and remounts, preserves active queries and recent Back results", () => {
    const cache = createCache();
    const put = (page: number) => {
      const variables = { paging: { page, q: `filter-${page % 3}` } };
      const release = retainListQuery(cache, query, variables);
      cache.writeQuery({
        query,
        variables,
        data: {
          items: {
            __typename: "FindScenesResultType",
            scenes: [
              { __typename: "Scene", id: String(page), title: `Scene ${page}` },
            ],
          },
        },
      });
      return { variables, release };
    };
    const active = put(0);
    const selected = cache.readQuery<{
      items: { scenes: { id: string; title: string }[] };
    }>({ query, variables: active.variables })?.items.scenes[0];
    for (let page = 1; page <= 40; page++) put(page).release();
    expect(
      cache.readQuery({ query, variables: active.variables }),
    ).not.toBeNull();
    expect(
      cache.readQuery({
        query,
        variables: { paging: { page: 1, q: "filter-1" } },
      }),
    ).toBeNull();
    expect(
      cache.readQuery({
        query,
        variables: { paging: { page: 40, q: "filter-1" } },
      }),
    ).not.toBeNull();
    const stored = cache.extract();
    expect(
      Object.keys(stored.ROOT_QUERY ?? {}).filter((key) =>
        key.startsWith("findScenes("),
      ),
    ).toHaveLength(MAX_INACTIVE_LIST_QUERIES + 1);
    expect(
      Object.keys(stored).filter((key) => key.startsWith("Scene:")),
    ).toHaveLength(MAX_INACTIVE_LIST_QUERIES + 1);
    active.release();
    expect(cache.readQuery({ query, variables: active.variables })).toBeNull();
    // Selection owns its objects independently of normalized-cache lifetime.
    expect(selected).toMatchObject({ id: "0", title: "Scene 0" });
  });

  it("shares retention for aliases and operations with different variable names", () => {
    const cache = createCache();
    const other = gql`query Other($filter: FindFilterType) { findScenes(filter: $filter) { count } }`;
    const variables = { paging: { page: 1 } };
    const release = retainListQuery(cache, query, variables);
    const releaseOther = retainListQuery(cache, other, {
      filter: variables.paging,
    });
    cache.writeQuery({
      query: other,
      variables: { filter: variables.paging },
      data: { findScenes: { __typename: "FindScenesResultType", count: 10 } },
    });
    release();
    for (let page = 2; page < 30; page++)
      retainListQuery(cache, query, { paging: { page } })();
    expect(
      cache.readQuery({
        query: other,
        variables: { filter: variables.paging },
      }),
    ).not.toBeNull();
    releaseOther();
    expect(
      cache.readQuery({
        query: other,
        variables: { filter: variables.paging },
      }),
    ).toBeNull();
  });
});
