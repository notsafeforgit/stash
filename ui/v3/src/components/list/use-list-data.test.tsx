// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import {
  ApolloClient,
  ApolloLink,
  Observable,
  gql,
  type TypedDocumentNode,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createCache } from "@/core/create-client";
import { removeEntitiesFromCache } from "@/core/client";
import { FilterMode, type FindFilterType } from "@/core/generated-graphql";
import { ListFilterModel } from "@/models/list-filter/filter";
import type { GraphQLDataSource } from "./entity-list-types";
import { SearchInput } from "./search-input";
import { useListData } from "./use-list-data";

type Item = { __typename: "Scene"; id: string };
type Rows = {
  findScenes: { __typename: "FindScenesResultType"; scenes: Item[] };
};
type Count = { result: { __typename: "FindScenesResultType"; count: number } };
type Variables = { filter: FindFilterType };
const rowsQuery: TypedDocumentNode<Rows, Variables> = gql`
  query ListRows($filter: FindFilterType) { findScenes(filter: $filter) { scenes { id } } }
`;
const countQuery: TypedDocumentNode<Count, Variables> = gql`
  query ListCount($filter: FindFilterType) { result: findScenes(filter: $filter) { count } }
`;
const source: GraphQLDataSource<Rows, Item, Variables> = {
  kind: "graphql",
  query: rowsQuery,
  countQuery,
  makeVariables: (filter) => ({ filter: filter.makeFindFilter() }),
  extractResult: (data) => ({ items: data?.findScenes.scenes ?? [] }),
};

function Harness() {
  const [filter, setFilter] = useState(
    () => new ListFilterModel(FilterMode.Scenes),
  );
  const { items, count, loading, error, refetch } = useListData(source, filter);
  return (
    <>
      <SearchInput
        value={filter.searchTerm}
        onChange={(text) => {
          const next = filter.clone();
          next.searchTerm = text;
          next.currentPage = 1;
          setFilter(next);
        }}
      />
      <button
        type="button"
        onClick={() => setFilter(filter.changePage(filter.currentPage + 1))}
      >
        Next
      </button>
      <button
        type="button"
        onClick={() => {
          void refetch();
        }}
      >
        Refresh
      </button>
      <output
        data-loading={loading}
        data-count={count ?? "unknown"}
        data-error={error?.message}
      >
        {items.map((item) => (
          <span key={item.id} data-item={item.id}>
            {item.id}
          </span>
        ))}
      </output>
    </>
  );
}

interface Request {
  name: string;
  search: string;
  page: number;
  rows: (ids: string[]) => void;
  count: (count: number) => void;
  fail: () => void;
}
let requests: Request[];
let root: Root;
let container: HTMLDivElement;
let client: ApolloClient;

beforeEach(async () => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  requests = [];
  client = new ApolloClient({
    cache: createCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          const filter: FindFilterType = operation.variables.filter;
          requests.push({
            name: operation.operationName ?? "",
            search: filter.q ?? "",
            page: filter.page ?? 1,
            rows: (ids) => {
              observer.next({
                data: {
                  findScenes: {
                    __typename: "FindScenesResultType",
                    scenes: ids.map((id) => ({ __typename: "Scene", id })),
                  },
                },
              });
              observer.complete();
            },
            count: (count) => {
              observer.next({
                data: { result: { __typename: "FindScenesResultType", count } },
              });
              observer.complete();
            },
            fail: () => observer.error(new Error("count unavailable")),
          });
        }),
    ),
  });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () =>
    root.render(
      <ApolloProvider client={client}>
        <IntlProvider locale="en">
          <Harness />
        </IntlProvider>
      </ApolloProvider>,
    ),
  );
});

afterEach(async () => {
  await act(async () => root.unmount());
  client.stop();
  container.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function request(name: "ListRows" | "ListCount", search = "", page = 1) {
  const found = requests.findLast(
    (request) =>
      request.name === name &&
      request.search === search &&
      request.page === page,
  );
  if (!found) throw new Error(`Missing ${name} ${search} ${page}`);
  return found;
}

async function type(text: string) {
  const input = container.querySelector("input");
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set;
  if (!input || !setter) throw new Error("Missing input");
  await act(async () => {
    input.focus();
    setter.call(input, text);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  return input;
}

it("renders cards before the exact total and merges either response order without replacing cards", async () => {
  await act(async () => request("ListRows").rows(["1", "2"]));
  const first = container.querySelector('[data-item="1"]');
  expect(first).not.toBeNull();
  expect(container.querySelector("output")?.dataset).toMatchObject({
    loading: "false",
    count: "unknown",
  });
  await act(async () => request("ListCount").count(80));
  expect(container.querySelector('[data-item="1"]')).toBe(first);
  expect(container.querySelector("output")?.dataset.count).toBe("80");
  await act(async () => container.querySelector("button")?.click());
  expect(request("ListRows", "", 2)).toBeDefined();
  await act(async () => request("ListCount", "", 2).count(80));
  await act(async () => request("ListRows", "", 2).rows(["3", "4"]));
  expect(container.querySelector("output")?.dataset).toMatchObject({
    loading: "false",
    count: "80",
  });
  expect(container.querySelector('[data-item="3"]')).not.toBeNull();
});

it("debounces typing once and immediately starts clear, Enter and page requests", async () => {
  const initial = requests.length;
  await type("a");
  await act(async () => {
    vi.advanceTimersByTime(100);
  });
  await type("alpha");
  await act(async () => {
    vi.advanceTimersByTime(299);
  });
  expect(requests).toHaveLength(initial);
  await act(async () => {
    vi.advanceTimersByTime(1);
  });
  expect(request("ListRows", "alpha")).toBeDefined();
  const input = await type("beta");
  await act(async () =>
    input.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Enter", bubbles: true }),
    ),
  );
  expect(request("ListRows", "beta")).toBeDefined();
  await act(async () =>
    container.querySelector<HTMLButtonElement>('[aria-label="Clear"]')?.click(),
  );
  expect(requests.at(-2)?.search).toBe("");
  const next = Array.from(container.querySelectorAll("button")).find(
    (button) => button.textContent === "Next",
  );
  await act(async () => next?.click());
  expect(request("ListRows", "", 2)).toBeDefined();
});

it("never applies the previous filter's late count and preserves usable cards on a count failure", async () => {
  const oldCount = request("ListCount");
  await type("new");
  await act(async () => {
    vi.advanceTimersByTime(300);
  });
  await act(async () => request("ListRows", "new").rows(["7"]));
  await act(async () => oldCount.count(900));
  expect(container.querySelector("output")?.dataset.count).toBe("unknown");
  await act(async () => request("ListCount", "new").fail());
  expect(container.querySelector("output")?.dataset).toMatchObject({
    loading: "false",
    count: "unknown",
    error: "count unavailable",
  });
  expect(container.querySelector('[data-item="7"]')).not.toBeNull();
});

it("keeps the total unknown when deleting a card before the count arrives", async () => {
  await act(async () => request("ListRows").rows(["1", "2"]));
  await act(async () =>
    removeEntitiesFromCache({
      cache: client.cache,
      typename: "Scene",
      listFieldName: "findScenes",
      itemsField: "scenes",
      ids: ["1"],
    }),
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(container.querySelector('[data-item="1"]')).toBeNull();
  expect(container.querySelector('[data-item="2"]')).not.toBeNull();
  expect(container.querySelector("output")?.dataset.count).toBe("unknown");
});
