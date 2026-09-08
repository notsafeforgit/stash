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
vi.mock("@/core/platform-url", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/core/platform-url")>();
  return {
    getApplicationBasePath: () => "/stash/",
    applicationHref: (href: string) =>
      actual.applicationHref(href, new URL("https://example.test/stash/")),
  };
});

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

it("browser Back and the app's URL-based Back share scroll positions without mixing filters or pages", () => {
  const listHref = "/stash/scenes?q=example&perPage=100&p=2";
  const history = createMemoryHistory({ initialEntries: [listHref] });
  const router = createAppRouter();
  router.update({ history });
  const getKey = router.options.getScrollRestorationKey!;
  const readLocation = () => router.parseLocation(history.location);
  const original = readLocation();
  const key = getKey(original);

  expect(router.options.scrollRestoration).toBe(true);
  expect(key).toBe(listHref);

  history.push("/stash/scenes/123");
  history.back();
  expect(getKey(readLocation())).toBe(key);

  // The in-app action navigates to returnTo, creating a new history entry.
  history.push("/stash/scenes/123");
  history.push(listHref);
  const returned = readLocation();
  expect(returned.state.__TSR_key).not.toBe(original.state.__TSR_key);
  expect(getKey(returned)).toBe(key);

  history.push("/stash/scenes?q=example&perPage=100&p=3");
  expect(getKey(readLocation())).not.toBe(key);
  history.push("/stash/scenes?q=another&perPage=100&p=2");
  expect(getKey(readLocation())).not.toBe(key);
});
