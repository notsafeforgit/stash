// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot } from "react-dom/client";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { z } from "zod";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { FilterMode, CriterionModifier } from "@/core/generated-graphql";
import { ListFilterModel } from "@/models/list-filter/filter";
import { useDuplicateFilter, useDuplicateSelection } from "./controller";
import { groupValueTints, selectAllButRetained } from "./groups";

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function filterURL(mode: FilterMode, title: string) {
  const filter = new ListFilterModel(mode);
  filter.configureFromSavedFilter({
    filter_ast: {
      root: {
        condition: {
          field: "title",
          value: { value: title, modifier: CriterionModifier.Includes },
        },
      },
    },
  });
  return filter.getEncodedParams().fa ?? undefined;
}

it.each([
  FilterMode.Scenes,
  FilterMode.Images,
])("follows same-route filters and Back/Forward in %s without remounting", async (mode) => {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const path =
    mode === FilterMode.Scenes
      ? "/scene-duplicate-checker"
      : "/image-duplicate-checker";
  const history = createMemoryHistory({ initialEntries: [path] });
  const parent = createRootRoute({ component: Outlet });
  const route = createRoute({
    getParentRoute: () => parent,
    path,
    validateSearch: z.object({
      fa: z.string().optional(),
      page: z.number().optional(),
    }),
    component: () => {
      const search = route.useSearch();
      const navigate = route.useNavigate();
      const { filterModel, setFilter } = useDuplicateFilter(
        mode,
        search.fa,
        (next) => {
          void navigate({
            search: (prev) => ({ ...prev, ...next }),
            replace: true,
          });
        },
      );
      const [checked, setChecked] = useDuplicateSelection(
        JSON.stringify(search),
      );
      return (
        <>
          <output>
            {JSON.stringify({
              ast: filterModel.makeFilterAST(),
              checked,
              page: search.page,
            })}
          </output>
          <button type="button" onClick={() => setChecked({ "1": true })}>
            Select
          </button>
          <button
            type="button"
            onClick={() => setFilter(filterModel.clearCriteria())}
          >
            Clear
          </button>
        </>
      );
    },
  });
  const router = createRouter({
    routeTree: parent.addChildren([route]),
    history,
  });
  try {
    await act(async () => {
      await router.load();
      root.render(
        <StrictMode>
          <RouterProvider router={router} />
        </StrictMode>,
      );
    });
    await act(async () =>
      router.navigate({
        to: path,
        search: { fa: filterURL(mode, "日本語 🎬"), page: 2 },
      }),
    );
    expect(container.textContent).toContain("日本語 🎬");
    await act(async () => container.querySelector("button")?.click());
    expect(container.textContent).toContain('"1":true');
    await act(async () =>
      router.navigate({
        to: path,
        search: { fa: filterURL(mode, "edited"), page: 3 },
      }),
    );
    expect(container.textContent).toContain("edited");
    expect(container.textContent).toContain('"checked":{}');
    await act(async () => {
      history.back();
      await router.load();
    });
    expect(container.textContent).toContain("日本語 🎬");
    await act(async () => {
      history.forward();
      await router.load();
    });
    expect(container.textContent).toContain("edited");
    await act(async () => container.querySelectorAll("button")[1]?.click());
    expect(router.state.location.search).toEqual({
      fa: undefined,
      page: undefined,
    });
    expect(container.textContent).not.toContain("edited");
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
});

it("keeps one valid retained entity per safe group and groups equal values by tint", () => {
  const a = { id: "1", value: "equal" },
    b = { id: "2", value: "equal" },
    c = { id: "3", value: "different" };
  expect(
    selectAllButRetained(
      [[a, b], [c]],
      (group) => group[0],
      (group) => group.length > 1,
    ),
  ).toEqual([b]);
  expect(selectAllButRetained([[a, b]], () => c)).toEqual([]);
  const tints = groupValueTints([[a, b, c]], "value", (item) => item.value);
  expect(tints.get(a.id)).toBe(tints.get(b.id));
  expect(tints.get(a.id)).not.toBe(tints.get(c.id));
});
