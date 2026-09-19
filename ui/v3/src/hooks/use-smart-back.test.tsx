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
  type HistoryState,
} from "@tanstack/react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Button } from "@/components/ui/button";
import { useSmartBack, useTrackBrowsePage } from "./use-smart-back";

let root: Root;
let container: HTMLDivElement;
let base: HTMLBaseElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  base = document.createElement("base");
  document.head.append(base);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  base.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function Shell() {
  useTrackBrowsePage();
  return <Outlet />;
}

function Detail() {
  const back = useSmartBack("/performers");
  return <Button onClick={back}>Back</Button>;
}

async function mount(initial = "/tags", prefix = "") {
  base.href = `${prefix}/`;
  const route = createRootRoute({ component: Shell });
  const router = createRouter({
    basepath: prefix || "/",
    routeTree: route.addChildren([
      ...["/", "/tags", "/performers"].map((path) =>
        createRoute({
          getParentRoute: () => route,
          path,
          component: () => null,
        }),
      ),
      createRoute({
        getParentRoute: () => route,
        path: "/performers/$performerId",
        component: Detail,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: [`${prefix}${initial}`] }),
  });
  await act(async () => {
    root.render(
      <StrictMode>
        <RouterProvider router={router} />
      </StrictMode>,
    );
    await router.load();
  });
  async function visit(href: string, state?: HistoryState) {
    await act(async () => {
      router.history.push(`${prefix}${href}`, state);
      await router.load();
    });
  }
  async function back() {
    const button = container.querySelector("button");
    if (!button) throw new Error("Missing Back button");
    await act(async () => button.click());
    await vi.waitFor(() => expect(router.state.status).toBe("idle"));
  }
  return { router, visit, back };
}

it.each(["", "/stash"])(
  "returns to Home after Tags → Home → performer name under %s",
  async (prefix) => {
    const { router, visit, back } = await mount("/tags?q=example", prefix);
    await visit("/");
    // Performer-name and table links have no explicit returnTo state.
    await visit("/performers/1");
    await back();
    expect(router.history.location.href).toBe(`${prefix}/`);
  },
);

it("retains the exact initial filtered list as a return destination", async () => {
  const href = "/tags?q=example&p=3&sortby=name";
  const { router, visit, back } = await mount(href);
  await visit("/performers/1");
  await back();
  expect(router.state.location.href).toBe(href);
});

it("keeps an explicit queue or nested-entity origin ahead of the browse fallback", async () => {
  const { router, visit, back } = await mount("/");
  await visit("/performers/2", { returnTo: "/performers/1" });
  await back();
  expect(router.state.location.href).toBe("/performers/1");
});

it("does not reuse another router's browse destination for a direct link", async () => {
  await mount("/tags");
  const { router, visit, back } = await mount("/performers/1");
  await visit("/performers/2", { returnTo: "https://outside.example/" });
  await back();
  expect(router.state.location.href).toBe("/performers");
});
