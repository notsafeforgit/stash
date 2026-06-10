import {
  createRoute,
  createRouter,
  type AnyRoute,
  type RouteComponent,
} from "@tanstack/react-router";
import { Route as rootRoute } from "@/routes/__root";
import { routeTree as fileRouteTree } from "./routeTree.gen";
import { getRegisteredRoutes } from "@/plugins/registry";

/**
 * Build the runtime route tree by appending plugin-registered routes
 * to the file-based tree. Must be called after `loadPlugins()` has
 * resolved (i.e. after `freezeRegistry()` is set), so plugin
 * registrations are stable. The returned tree is passed to
 * `createRouter`.
 */
function buildRouteTree() {
  const pluginRoutes = getRegisteredRoutes().map((r) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path: r.path,
      // TanStack Router's RouteComponent has a richer shape than
      // ComponentType (preload metadata etc.); a plain function
      // component is structurally compatible at runtime.
      component: r.component as RouteComponent,
    }),
  );

  if (pluginRoutes.length === 0) return fileRouteTree;

  // The file tree is rootRoute with children mutated in place.
  // Read those children, append plugin routes, and re-attach.
  const existing = (fileRouteTree as unknown as { children?: AnyRoute[] })
    .children;
  const combined = [...(existing ?? []), ...pluginRoutes];
  fileRouteTree.addChildren(combined);
  return fileRouteTree;
}

export function createAppRouter() {
  return createRouter({
    routeTree: buildRouteTree(),
    defaultPreload: "intent",
  });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
