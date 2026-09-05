import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createMemoryHistory } from "@tanstack/react-router";

const plugins = vi.hoisted(() => ({ paths: ["/plugin-page"] }));

// This app runs in a browser. Disable TanStack's server-only global tree cache
// while exercising multiple router constructions in the Node test runner.
beforeEach(() => {
  vi.stubEnv("NODE_ENV", "development");
  plugins.paths = ["/plugin-page"];
});
afterEach(() => vi.unstubAllEnvs());

vi.mock("@/routes/__root", async () => {
  const { createRootRoute } = await import("@tanstack/react-router");
  return { Route: createRootRoute() };
});
vi.mock("./routeTree.gen", async () => {
  const { createRoute } = await import("@tanstack/react-router");
  const { Route } = await import("@/routes/__root");
  return {
    routeTree: Route.addChildren([
      createRoute({ getParentRoute: () => Route, path: "/scenes" }),
    ]),
  };
});
vi.mock("@/plugins/registry", () => ({
  getRegisteredRoutes: () =>
    plugins.paths.map((path) => ({ path, component: () => null })),
}));
vi.mock("@/core/platform-url", () => ({
  getApplicationBasePath: () => "/stash/",
}));

import { createAppRouter } from "./router";

it("rebuilding the app router adds plugin routes once and preserves the public prefix", () => {
  const first = createAppRouter();
  const second = createAppRouter();
  second.update({
    history: createMemoryHistory({ initialEntries: ["/stash/scenes"] }),
  });
  expect(Object.keys(first.routesById).sort()).toEqual([
    "/plugin-page",
    "/scenes",
    "__root__",
  ]);
  expect(Object.keys(second.routesById).sort()).toEqual(
    Object.keys(first.routesById).sort(),
  );
  expect(second.buildLocation({ to: "/scenes" }).href).toBe("/stash/scenes");
});

it("can start the core app after a plugin registers a conflicting route", () => {
  plugins.paths = ["/scenes"];
  expect(() => createAppRouter()).toThrow();
  const router = createAppRouter(false);
  expect(Object.keys(router.routesById).sort()).toEqual([
    "/scenes",
    "__root__",
  ]);
});
