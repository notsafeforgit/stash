/**
 * Module-singleton registry that collects plugin registrations during
 * boot. Plugins call `host.routes.add(...)` / `host.nav.add(...)`
 * synchronously inside their `register(host)` function; those calls
 * land here. After all plugins have registered, the registry is read
 * once by the router builder and the nav.
 *
 * The registry is intentionally not reactive — plugins are loaded once
 * at boot before the router mounts. Hot-reloading a plugin requires a
 * full page reload.
 */

import type {
  PluginFilterExtrasComponent,
  PluginNavItem,
  PluginRouteOptions,
  PluginSavedFilterLoadedEvent,
  PluginSavedFilterLoadedListener,
} from "./host";

interface RegisteredRoute extends PluginRouteOptions {
  pluginId: string;
}

interface RegisteredNavItem extends PluginNavItem {
  pluginId: string;
}

export interface RegisteredFilterExtras {
  pluginId: string;
  component: PluginFilterExtrasComponent;
}

interface RegisteredSavedFilterLoadedListener {
  pluginId: string;
  listener: PluginSavedFilterLoadedListener;
}

const routes: RegisteredRoute[] = [];
const navItems: RegisteredNavItem[] = [];
const filterExtras: RegisteredFilterExtras[] = [];
const savedFilterLoadedListeners: RegisteredSavedFilterLoadedListener[] = [];
let frozen = false;

export function recordRoute(pluginId: string, route: PluginRouteOptions) {
  if (frozen) {
    console.warn(
      `[stash-plugin:${pluginId}] routes.add() called after host froze; ignored. Routes must be registered synchronously inside register().`,
    );
    return;
  }
  if (!route.path.startsWith("/")) {
    console.warn(
      `[stash-plugin:${pluginId}] route path must start with '/' (got ${JSON.stringify(route.path)}); skipped.`,
    );
    return;
  }
  if (routes.some((r) => r.path === route.path)) {
    console.warn(
      `[stash-plugin:${pluginId}] route ${route.path} already registered; skipped.`,
    );
    return;
  }
  routes.push({ ...route, pluginId });
}

export function recordNavItem(pluginId: string, item: PluginNavItem) {
  if (frozen) {
    console.warn(
      `[stash-plugin:${pluginId}] nav.add() called after host froze; ignored. Nav items must be registered synchronously inside register().`,
    );
    return;
  }
  navItems.push({ ...item, pluginId });
}

export function recordFilterExtras(
  pluginId: string,
  component: PluginFilterExtrasComponent,
) {
  if (frozen) {
    console.warn(
      `[stash-plugin:${pluginId}] filters.addExtras() called after host froze; ignored. Filter extensions must be registered synchronously inside register().`,
    );
    return;
  }
  filterExtras.push({ pluginId, component });
}

export function recordSavedFilterLoadedListener(
  pluginId: string,
  listener: PluginSavedFilterLoadedListener,
) {
  if (frozen) {
    console.warn(
      `[stash-plugin:${pluginId}] events.onSavedFilterLoaded() called after host froze; ignored. Event listeners must be registered synchronously inside register().`,
    );
    return;
  }
  savedFilterLoadedListeners.push({ pluginId, listener });
}

export function notifySavedFilterLoaded(event: PluginSavedFilterLoadedEvent) {
  for (const { pluginId, listener } of savedFilterLoadedListeners) {
    try {
      void Promise.resolve(listener(event)).catch((err) => {
        console.error(
          `[stash-plugin:${pluginId}] SavedFilter.Loaded listener failed`,
          err,
        );
      });
    } catch (err) {
      console.error(
        `[stash-plugin:${pluginId}] SavedFilter.Loaded listener failed`,
        err,
      );
    }
  }
}

/**
 * Mark the registry as frozen. Subsequent register/nav adds will be
 * ignored with a warning. Called after all plugins have run their
 * `register()` function.
 */
export function freezeRegistry() {
  frozen = true;
}

export function isRegistryFrozen() {
  return frozen;
}

export function getRegisteredRoutes(): readonly RegisteredRoute[] {
  return routes;
}

export function getRegisteredNavItems(): readonly RegisteredNavItem[] {
  return navItems;
}

export function getRegisteredFilterExtras(): readonly RegisteredFilterExtras[] {
  return filterExtras;
}
