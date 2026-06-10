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
  type PluginModule,
  type PluginNavItem,
  type PluginRegister,
  type PluginRouteOptions,
  type StashPluginHost,
} from "./host";
import { freezeRegistry, recordNavItem, recordRoute } from "./registry";
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

function buildHost(
  pluginId: string,
  apollo: ApolloClient,
  intl: IntlShape,
): StashPluginHost {
  return Object.freeze({
    version: HOST_VERSION,
    pluginId,
    routes: Object.freeze({
      add: (route: PluginRouteOptions) => recordRoute(pluginId, route),
    }),
    nav: Object.freeze({
      add: (item: PluginNavItem) => recordNavItem(pluginId, item),
    }),
    apollo,
    intl,
    router: Object.freeze({ Link, useNavigate }),
    ui,
  }) as StashPluginHost;
}

export interface LoadPluginsOpts {
  apollo: ApolloClient;
  intl: IntlShape;
}

let loadPromise: Promise<void> | null = null;

/**
 * Memoized entry point — safe to call multiple times (e.g. from React
 * 18 strict-mode double-invocation). Only the first call performs the
 * fetch + register cycle; subsequent calls return the same promise.
 */
export function ensurePluginsLoaded(opts: LoadPluginsOpts): Promise<void> {
  if (!loadPromise) {
    loadPromise = loadPlugins(opts);
  }
  return loadPromise;
}

export async function loadPlugins(opts: LoadPluginsOpts): Promise<void> {
  const { apollo, intl } = opts;

  let pluginsList: PluginToLoad[] = [];
  try {
    const result = await apollo.query<GQL.PluginsQuery>({
      query: GQL.PluginsDocument,
      fetchPolicy: "network-only",
    });
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
    return;
  }

  if (pluginsList.length === 0) {
    freezeRegistry();
    return;
  }

  let order: readonly string[] = [];
  try {
    const result = await apollo.query<GQL.PluginHookOrderQuery>({
      query: GQL.PluginHookOrderDocument,
      fetchPolicy: "network-only",
    });
    const entry = result.data?.pluginHookOrder?.find(
      (e) => e.hook === UI_LOAD_HOOK,
    );
    if (entry) order = entry.plugin_ids;
  } catch (err) {
    console.warn("[stash-plugins] failed to query hook order", err);
  }

  const ordered = applyOrder(pluginsList, order);

  for (const p of ordered) {
    try {
      const mod = (await import(/* @vite-ignore */ p.entry)) as PluginModule;
      const register: PluginRegister | undefined = mod.default ?? mod.register;
      if (typeof register !== "function") {
        console.warn(
          `[stash-plugin:${p.id}] entry module did not export a default register(host) function; skipped.`,
        );
        continue;
      }
      const host = buildHost(p.id, apollo, intl);
      await register(host);
    } catch (err) {
      console.error(`[stash-plugin:${p.id}] failed to load`, err);
    }
  }

  freezeRegistry();
}
