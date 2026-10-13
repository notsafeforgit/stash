// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CriterionModifier, FilterMode } from "@/core/generated-graphql";
import { createASTGroup } from "@/models/list-filter/filter-ast";
import { DisplayMode } from "@/models/list-filter/types";
import { useListPageFilter } from "./use-list-page-filter";

let state: ReturnType<typeof useListPageFilter>;
let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  localStorage.clear();
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

async function mount(useURL = true) {
  function List() {
    state = useListPageFilter({
      filterMode: FilterMode.Scenes,
      isActive: true,
      useURL,
    });
    return null;
  }
  const route = createRootRoute({ component: Outlet });
  const list = createRoute({
    getParentRoute: () => route,
    path: "/scenes",
    component: List,
  });
  const router = createRouter({
    routeTree: route.addChildren([list]),
    history: createMemoryHistory({ initialEntries: ["/scenes"] }),
  });
  await act(async () => {
    root.render(
      <StrictMode>
        <RouterProvider router={router} />
      </StrictMode>,
    );
    await router.load();
  });
  return router;
}

it.each([true, false])(
  "retains unfinished conditions without navigating (URL sync: %s)",
  async (useURL) => {
    const router = await mount(useURL);
    const navigate = vi.spyOn(router, "navigate");
    const draft = createASTGroup(FilterMode.Scenes);
    await act(async () => {
      const next = state.filter.clone();
      next.filterAst = draft;
      state.setFilter(next);
    });
    expect(state.filter.filterAst).toEqual(draft);
    expect(state.filter.makeFilterAST()).toBeUndefined();
    expect(navigate).not.toHaveBeenCalled();

    await act(async () => {
      const next = state.filter.clone();
      const condition =
        next.filterAst?.kind === "group" && next.filterAst.children[0];
      if (!condition || condition.kind !== "condition")
        throw new Error("Missing draft condition");
      condition.criterion.fromDecodedParams({
        modifier: CriterionModifier.Includes,
        value: "sample title",
      });
      state.setFilter(next);
    });
    expect(state.filter.count()).toBe(1);
    expect(state.filter.makeFilterAST()).toBeDefined();
    expect(navigate).toHaveBeenCalledTimes(useURL ? 1 : 0);
    expect(router.state.location.searchStr.includes("fa=")).toBe(useURL);
  },
);

it("keeps draft nodes through display and rapid zoom changes without navigating", async () => {
  const router = await mount();
  const navigate = vi.spyOn(router, "navigate");
  const draft = createASTGroup(FilterMode.Scenes);
  await act(async () => {
    const next = state.filter.clone();
    next.filterAst = draft;
    state.setFilter(next);
  });
  const zoom = state.filter.zoomIndex;
  await act(async () => {
    state.setFilter((prev) => prev.setZoom(prev.zoomIndex + 1));
    state.setFilter((prev) => prev.setZoom(prev.zoomIndex + 1));
  });
  expect(state.filter.zoomIndex).toBe(zoom + 2);
  await act(async () => {
    state.setFilter((prev) => prev.setDisplayMode(DisplayMode.Table));
  });
  expect(state.filter.displayMode).toBe(DisplayMode.Table);
  expect(state.filter.zoomIndex).toBe(zoom + 2);
  expect(state.filter.filterAst).toEqual(draft);
  expect(navigate).not.toHaveBeenCalled();
  expect(router.state.location.href).toBe("/scenes");
});
