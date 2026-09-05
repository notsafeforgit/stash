import {
  createRoute,
  createRouter,
  type AnyRoute,
  type RouteComponent,
} from "@tanstack/react-router";
import { Route as rootRoute } from "@/routes/__root";
import { routeTree as fileRouteTree } from "./routeTree.gen";
import { getRegisteredRoutes } from "@/plugins/registry";
import { getApplicationBasePath } from "@/core/platform-url";

const coreRoutes = [
  ...((fileRouteTree as unknown as { children?: AnyRoute[] }).children ?? []),
];

function routePattern(path: string): string {
  return (
    path
      .replace(/\/+/g, "/")
      .replace(/\/$/, "")
      .replace(/\$[^/]+/g, "$") || "/"
  );
}

function coreRoutePatterns(routes: AnyRoute[], parent = ""): string[] {
  return routes.flatMap((route) => {
    const segment = "path" in route.options ? route.options.path : "";
    const path = `${parent}/${segment ?? ""}`;
    return [
      routePattern(path),
      ...coreRoutePatterns(route.children ?? [], path),
    ];
  });
}

/**
 * Build the runtime route tree by appending plugin-registered routes
 * to the file-based tree. Must be called after `loadPlugins()` has
 * resolved (i.e. after `freezeRegistry()` is set), so plugin
 * registrations are stable. The returned tree is passed to
 * `createRouter`.
 */
function buildRouteTree(includePlugins: boolean) {
  const paths = new Set(coreRoutePatterns(coreRoutes));
  const pluginRoutes = (includePlugins ? getRegisteredRoutes() : []).map(
    (r) => {
      const path = routePattern(r.path);
      if (paths.has(path))
        throw new Error(
          `Plugin route conflicts with an existing route: ${r.path}`,
        );
      paths.add(path);
      return createRoute({
        getParentRoute: () => rootRoute,
        path: r.path,
        // TanStack Router's RouteComponent has a richer shape than
        // ComponentType (preload metadata etc.); a plain function
        // component is structurally compatible at runtime.
        component: r.component as RouteComponent,
      });
    },
  );

  const combined = [...coreRoutes, ...pluginRoutes];
  fileRouteTree.addChildren(combined);
  return fileRouteTree;
}

export function createAppRouter(includePlugins = true) {
  return createRouter({
    routeTree: buildRouteTree(includePlugins),
    basepath: getApplicationBasePath(),
    defaultPreload: "intent",
  });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
