// @vitest-environment jsdom

import {
  act,
  createContext,
  startTransition,
  StrictMode,
  Suspense,
  useContext,
  useState,
} from "react";
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
import { getScrollRestorationKey } from "@/core/scroll-restoration";
import { useListScrollRestoration } from "./use-list-scroll-restoration";

const id = "restoration-test-list";
const listURL = "/stash/scenes?q=example&perPage=100&p=2#cards";
const renderAttempt = vi.fn();
const ContentContext = createContext<{
  ready: boolean;
  key: string;
  suspend?: Promise<void>;
}>({ ready: true, key: "page-2" });

function List() {
  const [element, setElement] = useState<HTMLDivElement | null>(null);
  const content = useContext(ContentContext);
  const restoration = useListScrollRestoration(
    id,
    element,
    content.ready,
    content.key,
  );
  renderAttempt(restoration);
  if (content.suspend) throw content.suspend;
  return (
    <div
      ref={setElement}
      data-scroll-restoration-id={id}
      data-restoration-key={restoration.restorationKey}
      data-initial-offset={restoration.initialOffset}
    />
  );
}

let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  renderAttempt.mockClear();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  document.head.innerHTML = '<base href="/stash/">';
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  document.head.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function scroller() {
  return container.querySelector<HTMLDivElement>(
    `[data-scroll-restoration-id="${id}"]`,
  )!;
}

function scrollTo(top: number, left = 0) {
  scroller().scrollTop = top;
  scroller().scrollLeft = left;
  scroller().dispatchEvent(new Event("scroll"));
}

async function mount(initialEntry = listURL) {
  const route = createRootRoute({ component: Outlet });
  let loadDetail = () => Promise.resolve();
  const list = createRoute({
    getParentRoute: () => route,
    path: "/scenes/",
    component: List,
  });
  const detail = createRoute({
    getParentRoute: () => route,
    path: "/scenes/$sceneId",
    loader: () => loadDetail(),
    component: () => <div data-scroll-restoration-id={id} />,
  });
  const embedded = createRoute({
    getParentRoute: () => route,
    path: "/performers/$performerId",
    component: List,
  });
  const router = createRouter({
    routeTree: route.addChildren([list, detail, embedded]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
    basepath: "/stash",
    scrollRestoration: true,
    getScrollRestorationKey,
    defaultPendingMs: 60_000,
  });
  async function render(ready = true, key = "page-2", suspend?: Promise<void>) {
    await act(async () => {
      const update = () =>
        root.render(
          <StrictMode>
            <Suspense fallback={<div data-pending />}>
              <ContentContext value={{ ready, key, suspend }}>
                <RouterProvider router={router} />
              </ContentContext>
            </Suspense>
          </StrictMode>,
        );
      if (suspend) startTransition(update);
      else update();
    });
  }
  await render();
  return {
    router,
    render,
    setDetailLoader(loader: () => Promise<void>) {
      loadDetail = loader;
    },
  };
}

it("keeps the outgoing list's key and scroll entry while the detail route loads", async () => {
  const { router, setDetailLoader } = await mount();
  scrollTo(720, 45);
  await act(async () => {
    await router.navigate({
      to: "/scenes/$sceneId",
      params: { sceneId: "17" },
    });
  });
  // A destination can have an entry for the same shared scroll container ID.
  scrollTo(30, 10);
  await act(async () => {
    await router.navigate({
      to: "/scenes",
      search: { q: "example", perPage: 100, p: 2 },
      hash: "cards",
    });
  });
  expect(scroller().scrollTop).toBe(720);
  const key = scroller().dataset.restorationKey;
  expect(JSON.parse(key!)[1]).toBe(listURL);
  scrollTo(860, 60);
  router.clearCache();

  let enter!: () => void;
  let release!: () => void;
  const entered = new Promise<void>((resolve) => {
    enter = resolve;
  });
  setDetailLoader(() => {
    enter();
    return new Promise<void>((resolve) => {
      release = resolve;
    });
  });
  let navigation: Promise<void>;
  await act(async () => {
    navigation = router.navigate({
      to: "/scenes/$sceneId",
      params: { sceneId: "17" },
    });
    await entered;
  });

  try {
    expect(router.state.location.pathname).toBe("/scenes/17");
    expect(scroller().dataset.restorationKey).toBe(key);
    expect(scroller().dataset.initialOffset).toBe("860");
    expect(scroller().scrollTop).toBe(860);
    expect(scroller().scrollLeft).toBe(60);
  } finally {
    await act(async () => {
      release();
      await navigation;
    });
  }
});

it("restores once when returning data is ready, then permits normal scrolling", async () => {
  const { router, render } = await mount();
  scrollTo(950, 75);
  await act(async () => {
    await router.navigate({
      to: "/scenes/$sceneId",
      params: { sceneId: "18" },
    });
  });
  await render(false);
  await act(async () => router.history.back());
  // Simulate the loading content clamping the router's early DOM restoration.
  scroller().scrollTop = 0;
  scroller().scrollLeft = 0;
  await render(true);
  expect(scroller().scrollTop).toBe(950);
  expect(scroller().scrollLeft).toBe(75);

  scrollTo(420, 20);
  await render(true);
  expect(scroller().scrollTop).toBe(420);
  expect(scroller().scrollLeft).toBe(20);
});

it("updates page keys and follows the owning route when embedded entity params change", async () => {
  const { router, render } = await mount("/stash/performers/1?tab=scenes&p=2");
  const original = scroller().dataset.restorationKey;
  await act(async () => {
    router.history.push("/stash/performers/1?tab=scenes&p=3");
  });
  await render(true, "page-3");
  const nextPage = scroller().dataset.restorationKey;
  expect(nextPage).not.toBe(original);
  expect(JSON.parse(nextPage!)[1]).toBe("/stash/performers/1?tab=scenes&p=3");

  await act(async () => {
    await router.navigate({
      to: "/performers/$performerId",
      params: { performerId: "2" },
      search: true,
    });
  });
  expect(JSON.parse(scroller().dataset.restorationKey!)[1]).toBe(
    "/stash/performers/2?tab=scenes&p=3",
  );
});

it("does not restore again when a suspended render is abandoned", async () => {
  const { router, render } = await mount();
  scrollTo(950, 75);
  await act(async () => {
    await router.navigate({
      to: "/scenes/$sceneId",
      params: { sceneId: "19" },
    });
  });
  await act(async () => router.history.back());
  expect(scroller().scrollTop).toBe(950);
  scrollTo(420, 20);

  // React starts rendering a different filter, but suspends before committing
  // it. An urgent update then abandons that work and retains the current list.
  const suspended = new Promise<void>(() => {});
  renderAttempt.mockClear();
  await render(true, "abandoned-filter", suspended);
  expect(renderAttempt).toHaveBeenCalledWith({
    restorationKey: JSON.stringify([id, listURL, "abandoned-filter"]),
    initialOffset: 950,
  });
  expect(scroller().scrollTop).toBe(420);
  await render();
  // A later refresh of the same list must not revive its consumed restoration.
  await render(false);
  await render(true);
  expect(scroller().scrollTop).toBe(420);
  expect(scroller().scrollLeft).toBe(20);
});

it("retains the list when a pending detail navigation is superseded", async () => {
  const { router, setDetailLoader } = await mount();
  scrollTo(680, 25);
  const key = scroller().dataset.restorationKey;
  let enter!: () => void;
  let release!: () => void;
  const entered = new Promise<void>((resolve) => {
    enter = resolve;
  });
  setDetailLoader(() => {
    enter();
    return new Promise<void>((resolve) => {
      release = resolve;
    });
  });
  let navigation: Promise<void>;
  await act(async () => {
    navigation = router.navigate({
      to: "/scenes/$sceneId",
      params: { sceneId: "20" },
    });
    await entered;
  });
  try {
    await act(async () => {
      await router.navigate({
        to: "/scenes",
        search: { q: "example", perPage: 100, p: 2 },
        hash: "cards",
      });
    });
    expect(scroller().dataset.restorationKey).toBe(key);
    expect(scroller().scrollTop).toBe(680);
    expect(scroller().scrollLeft).toBe(25);
  } finally {
    await act(async () => {
      release();
      await navigation;
    });
  }
});
