import { withTimeout } from "@/utils/with-timeout";
/**
 * Plugin loader — runs once at app boot, before the TanStack Router is
 * built and React renders. Fetches the list of enabled plugins from
 * the backend, filters to those declaring a v3 entry module, and
 * dynamic-imports each one in the user-configured order, calling
 * `register(host)` on each.
 *
 * Errors loading any single plugin are caught and logged. One broken
 * plugin must never take down the app.
 */

import type { ApolloClient } from "@apollo/client";
import { Link, useNavigate } from "@tanstack/react-router";
import type { IntlShape } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import {
  HOST_VERSION,
  type PluginFilterExtrasComponent,
  type PluginModule,
  type PluginNavItem,
  type PluginRegister,
  type PluginRouteOptions,
  type PluginSavedFilterLoadedListener,
  type StashPluginHost,
} from "./host";
import {
  freezeRegistry,
  recordFilterExtras,
  recordNavItem,
  recordRoute,
  recordSavedFilterLoadedListener,
} from "./registry";
import * as ui from "./ui-exports";

interface PluginToLoad {
  id: string;
  name: string;
  entry: string;
}

const UI_LOAD_HOOK = "ui-load";

function applyOrder(
  plugins: PluginToLoad[],
  order: readonly string[],
): PluginToLoad[] {
  if (!order || order.length === 0) {
    return [...plugins].sort((a, b) => a.id.localeCompare(b.id));
  }
  const byId = new Map(plugins.map((p) => [p.id, p]));
  const seen = new Set<string>();
  const ordered: PluginToLoad[] = [];
  for (const id of order) {
    const p = byId.get(id);
    if (p && !seen.has(id)) {
      ordered.push(p);
      seen.add(id);
    }
  }
  const remainder = plugins
    .filter((p) => !seen.has(p.id))
    .sort((a, b) => a.id.localeCompare(b.id));
  return [...ordered, ...remainder];
}

let currentIntl: IntlShape | undefined;

export function updatePluginIntl(intl: IntlShape) {
  currentIntl = intl;
}

function buildHost(
  pluginId: string,
  apollo: ApolloClient,
  intl: IntlShape,
  stage: (registration: () => void) => void,
): StashPluginHost {
  return Object.freeze({
    version: HOST_VERSION,
    pluginId,
    routes: Object.freeze({
      add: (route: PluginRouteOptions) =>
        stage(() => recordRoute(pluginId, route)),
    }),
    nav: Object.freeze({
      add: (item: PluginNavItem) => stage(() => recordNavItem(pluginId, item)),
    }),
    filters: Object.freeze({
      addExtras: (component: PluginFilterExtrasComponent) =>
        stage(() => recordFilterExtras(pluginId, component)),
    }),
    events: Object.freeze({
      onSavedFilterLoaded: (listener: PluginSavedFilterLoadedListener) =>
        stage(() => recordSavedFilterLoadedListener(pluginId, listener)),
    }),
    apollo,
    get intl() {
      return currentIntl ?? intl;
    },
    router: Object.freeze({ Link, useNavigate }),
    ui,
  }) as StashPluginHost;
}

export interface LoadPluginsOpts {
  apollo: ApolloClient;
  intl: IntlShape;
}

export const PLUGIN_TIMEOUT_MS = 5000;
export const PLUGIN_BOOT_TIMEOUT_MS = 30000;
let loadPromise: Promise<PluginLoadIssue[]> | null = null;

export interface PluginLoadIssue {
  pluginId?: string;
  error: unknown;
}

/** Stage registrations so a rejected or timed-out plugin cannot leave behind
 * partial routes, or register later after the router has already mounted. */
export async function registerPlugin(
  plugin: PluginToLoad,
  opts: LoadPluginsOpts,
  timeout = PLUGIN_TIMEOUT_MS,
  importModule: (entry: string) => Promise<PluginModule> = (entry) =>
    import(/* @vite-ignore */ entry),
): Promise<void> {
  const registrations: (() => void)[] = [];
  let accepting = true;
  const host = buildHost(plugin.id, opts.apollo, opts.intl, (registration) => {
    if (accepting) registrations.push(registration);
  });
  try {
    await withTimeout(
      (async () => {
        const mod = await importModule(plugin.entry);
        if (!accepting) return;
        const register: PluginRegister | undefined =
          mod.default ?? mod.register;
        if (typeof register !== "function")
          throw new Error("Plugin must export register(host)");
        await register(host);
      })(),
      timeout,
      `Plugin ${plugin.id}`,
    );
    for (const commit of registrations) commit();
  } finally {
    accepting = false;
  }
}

/**
 * Memoized entry point — safe to call multiple times (e.g. from React
 * 18 strict-mode double-invocation). Only the first call performs the
 * fetch + register cycle; subsequent calls return the same promise.
 */
export function ensurePluginsLoaded(
  opts: LoadPluginsOpts,
): Promise<PluginLoadIssue[]> {
  if (!loadPromise) {
    loadPromise = loadPlugins(opts);
  }
  return loadPromise;
}

export async function loadPlugins(
  opts: LoadPluginsOpts,
): Promise<PluginLoadIssue[]> {
  const { apollo } = opts;
  const issues: PluginLoadIssue[] = [];
  const deadline = Date.now() + PLUGIN_BOOT_TIMEOUT_MS;

  let pluginsList: PluginToLoad[] = [];
  try {
    const result = await withTimeout(
      apollo.query<GQL.PluginsQuery>({
        query: GQL.PluginsDocument,
        fetchPolicy: "network-only",
      }),
      PLUGIN_TIMEOUT_MS,
      "Plugin list",
    );
    const plugins = result.data?.plugins ?? [];
    pluginsList = plugins
      .filter((p) => p.enabled && p.paths.entry)
      .map((p) => ({
        id: p.id,
        name: p.name,
        entry: p.paths.entry as string,
      }));
  } catch (err) {
    console.error("[stash-plugins] failed to query plugin list", err);
    freezeRegistry();
    return [{ error: err }];
  }

  if (pluginsList.length === 0) {
    freezeRegistry();
    return issues;
  }

  let order: readonly string[] = [];
  try {
    const result = await withTimeout(
      apollo.query<GQL.PluginHookOrderQuery>({
        query: GQL.PluginHookOrderDocument,
        fetchPolicy: "network-only",
      }),
      PLUGIN_TIMEOUT_MS,
      "Plugin order",
    );
    const entry = result.data?.pluginHookOrder?.find(
      (e) => e.hook === UI_LOAD_HOOK,
    );
    if (entry) order = entry.plugin_ids;
  } catch (err) {
    console.warn("[stash-plugins] failed to query hook order", err);
    issues.push({ error: err });
  }

  const ordered = applyOrder(pluginsList, order);

  for (const p of ordered) {
    try {
      const remaining = deadline - Date.now();
      if (remaining <= 0) throw new Error("Plugin startup deadline exceeded");
      await registerPlugin(p, opts, Math.min(PLUGIN_TIMEOUT_MS, remaining));
    } catch (err) {
      console.error(`[stash-plugin:${p.id}] failed to load`, err);
      issues.push({ pluginId: p.id, error: err });
    }
  }

  freezeRegistry();
  return issues;
}
