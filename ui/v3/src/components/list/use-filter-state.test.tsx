// @vitest-environment jsdom
import { act, createContext, StrictMode, useContext } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useLocation,
} from "@tanstack/react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { DefaultFilters } from "@/core/config";
import { FilterMode, SortDirectionEnum } from "@/core/generated-graphql";
import { View } from "./views";
import { useFilterState } from "./use-filter-state";

const DefaultsContext = createContext<DefaultFilters>({});
vi.mock("src/hooks/config", () => ({
  useConfigurationContextOptional: () => ({
    configuration: { ui: { defaultFilters: useContext(DefaultsContext) } },
  }),
}));

const lists = [
  { tab: "scenes", view: View.PerformerScenes, mode: FilterMode.Scenes },
  { tab: "images", view: View.PerformerImages, mode: FilterMode.Images },
] as const;
const filters = new Map<View, ReturnType<typeof useFilterState>>();

function List({ tab, view, mode }: (typeof lists)[number]) {
  const location = useLocation();
  const activeTab = new URLSearchParams(location.searchStr).get("tab");
  const state = useFilterState({
    filterMode: mode,
    view,
    useURL: (activeTab ?? "scenes") === tab,
  });
  filters.set(view, state);
  return null;
}

let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  filters.clear();
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function savedSort(
  mode: FilterMode,
  sort: string,
  direction: SortDirectionEnum,
) {
  return { mode, find_filter: { sort, direction } };
}

async function mount(defaults: DefaultFilters, search = "?tab=scenes") {
  const route = createRootRoute({ component: Outlet });
  const detail = createRoute({
    getParentRoute: () => route,
    path: "/performers/$performerId",
    component: () => lists.map((list) => <List key={list.view} {...list} />),
  });
  const router = createRouter({
    routeTree: route.addChildren([detail]),
    history: createMemoryHistory({
      initialEntries: [`/performers/1${search}`],
    }),
  });
  async function render(nextDefaults = defaults) {
    await act(async () => {
      root.render(
        <StrictMode>
          <DefaultsContext value={nextDefaults}>
            <RouterProvider router={router} />
          </DefaultsContext>
        </StrictMode>,
      );
      await router.load();
    });
  }
  await render();
  async function visit(nextSearch: string) {
    await act(async () => {
      router.history.push(`/performers/1${nextSearch}`);
      await router.load();
    });
  }
  return { router, render, visit };
}

function sort(view: View) {
  const state = filters.get(view);
  if (!state) throw new Error(`Missing ${view} filter`);
  return [state.filter.sortBy, state.filter.sortDirection];
}

it("applies saved sorts on initial tab-only URLs and when revisiting tabs", async () => {
  const defaults = {
    [View.PerformerScenes]: savedSort(
      FilterMode.Scenes,
      "title",
      SortDirectionEnum.Desc,
    ),
    [View.PerformerImages]: savedSort(
      FilterMode.Images,
      "date",
      SortDirectionEnum.Desc,
    ),
  };
  const { visit } = await mount(defaults);
  expect(sort(View.PerformerScenes)).toEqual(["title", SortDirectionEnum.Desc]);
  await visit("?tab=images");
  expect(sort(View.PerformerImages)).toEqual(["date", SortDirectionEnum.Desc]);
  await visit("?tab=scenes");
  expect(sort(View.PerformerScenes)).toEqual(["title", SortDirectionEnum.Desc]);
  await visit("?tab=images");
  expect(sort(View.PerformerImages)).toEqual(["date", SortDirectionEnum.Desc]);
});

it("keeps saved defaults independent after saving either tab's new sort", async () => {
  let defaults: DefaultFilters = {
    [View.PerformerScenes]: savedSort(
      FilterMode.Scenes,
      "date",
      SortDirectionEnum.Desc,
    ),
    [View.PerformerImages]: savedSort(
      FilterMode.Images,
      "date",
      SortDirectionEnum.Desc,
    ),
  };
  const { render, visit } = await mount(defaults);
  for (const list of lists) {
    await visit(`?tab=${list.tab}&sortby=title&sortdir=desc`);
    defaults = {
      ...defaults,
      [list.view]: savedSort(list.mode, "title", SortDirectionEnum.Desc),
    };
    await render(defaults);
    for (const other of lists) {
      await visit(`?tab=${other.tab}`);
      expect(sort(other.view)).toEqual([
        defaults[other.view]?.find_filter?.sort,
        SortDirectionEnum.Desc,
      ]);
    }
  }
});

it("honours explicit URL sorts and restores saved defaults on browser Back", async () => {
  const defaults = {
    [View.PerformerImages]: savedSort(
      FilterMode.Images,
      "date",
      SortDirectionEnum.Desc,
    ),
  };
  const { router, visit } = await mount(defaults, "?tab=images");
  await visit("?tab=images&sortby=path");
  expect(sort(View.PerformerImages)).toEqual(["path", SortDirectionEnum.Asc]);
  await act(async () => {
    router.history.back();
    await router.load();
  });
  expect(sort(View.PerformerImages)).toEqual(["date", SortDirectionEnum.Desc]);
});

it("does not serialize a saved default just because a non-filter parameter changes", async () => {
  const defaults = {
    [View.PerformerImages]: savedSort(
      FilterMode.Images,
      "date",
      SortDirectionEnum.Desc,
    ),
  };
  const { router, visit } = await mount(defaults, "?tab=images");
  expect(router.state.location.searchStr).toBe("?tab=images");
  await visit("?tab=images&returnTo=%2Fperformers");
  expect(sort(View.PerformerImages)).toEqual(["date", SortDirectionEnum.Desc]);
  expect(router.state.location.searchStr).toBe(
    "?tab=images&returnTo=%2Fperformers",
  );
});
