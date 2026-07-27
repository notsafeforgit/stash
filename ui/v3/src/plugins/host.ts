/**
 * Stash v3 plugin host — public contract for v3 UI plugins.
 *
 * A v3 plugin is an ESM module whose default export is a `register`
 * function. At app boot, the loader fetches the list of enabled plugins
 * from the backend, dynamic-imports each plugin's entry module, and
 * calls `register(host)`. The plugin uses the host APIs to register
 * routes, nav items, etc.
 *
 * Plugin authors: import the host type for typing purposes only. Do
 * not bundle this file into your plugin — the host instance is passed
 * in at runtime.
 */

import type { ApolloClient } from "@apollo/client";
import type { ComponentType, ReactNode } from "react";
import type { IntlShape } from "react-intl";
import type { Link, useNavigate } from "@tanstack/react-router";
import type { SavedFilterDataFragment } from "src/core/generated-graphql";
import type { ListFilterModel } from "src/models/list-filter/filter";
import type { StashPluginUI } from "./ui-exports";

export const HOST_VERSION = "1" as const;

export type NavPlacement = "main" | "mobile" | "utility";

export interface PluginRouteOptions {
  /**
   * Absolute URL path the plugin route should mount at, e.g. `/stashtv`.
   * Must start with `/`. Plugins should namespace their routes under a
   * unique prefix (typically the plugin id) to avoid collisions.
   */
  path: string;
  /**
   * The React component rendered when the route is active.
   */
  component: ComponentType;
}

export interface PluginNavItem {
  /**
   * Display label. Either a static string or a function returning a
   * localized string from the host's intl shape.
   */
  label: string | ((intl: IntlShape) => string);
  /**
   * Path to navigate to. Should match a registered plugin route or a
   * built-in route.
   */
  to: string;
  /**
   * Optional icon — any React node, but a `lucide-react` icon at
   * `size={16}` matches the built-in nav items.
   */
  icon?: ReactNode;
  /**
   * Where to surface the nav item:
   * - `main` (default): primary sidebar / desktop nav.
   * - `mobile`: mobile bottom-tab bar (limit to high-priority items).
   * - `utility`: secondary menus only (overflow on desktop, drawer on mobile).
   */
  placement?: NavPlacement;
  /**
   * Optional hotkey string ("g x"). Same syntax as built-in nav items.
   */
  hotkey?: string;
}

export interface PluginFilterExtrasProps {
  /** Current list filter, including the canonical v3 Filter AST. */
  filter: ListFilterModel;
  /** Convenience mirror of `filter.searchTerm`. */
  searchTerm: string;
  /** Current list view, when the list maps to a persisted Stash view. */
  view?: string;
}

export type PluginFilterExtrasComponent =
  ComponentType<PluginFilterExtrasProps>;

export interface PluginSavedFilterLoadedEvent {
  savedFilter: SavedFilterDataFragment;
  filter: ListFilterModel;
  source: "dialog" | "sidebar" | "toolbar";
  view?: string;
}

export type PluginSavedFilterLoadedListener = (
  event: PluginSavedFilterLoadedEvent,
) => void | Promise<void>;

export interface StashPluginHost {
  /**
   * Host API contract version. Plugins should check this matches their
   * expected major version and bail out otherwise — the host will
   * never break compatibility within a major version.
   */
  readonly version: typeof HOST_VERSION;

  /**
   * Identity of the plugin currently registering. Useful for logging,
   * scoping local storage keys, etc.
   */
  readonly pluginId: string;

  /**
   * Add a top-level route. Must be called synchronously inside
   * `register()`; routes added after the router has mounted are
   * ignored.
   */
  readonly routes: {
    add(route: PluginRouteOptions): void;
  };

  /**
   * Add a navigation entry. Must be called synchronously inside
   * `register()`.
   */
  readonly nav: {
    add(item: PluginNavItem): void;
  };

  /**
   * Add content below the list filter toolbar. The component receives the
   * current search term, view, and complete v3 filter model.
   */
  readonly filters: {
    addExtras(component: PluginFilterExtrasComponent): void;
  };

  /**
   * Subscribe to UI lifecycle events. Saved-filter listeners run after the
   * selected filter has been applied to the list.
   */
  readonly events: {
    onSavedFilterLoaded(listener: PluginSavedFilterLoadedListener): void;
  };

  /**
   * The same Apollo client the host uses. Plugins can run any query or
   * mutation declared in the GraphQL schema, including subscriptions
   * over the `/graphql` WebSocket.
   */
  readonly apollo: ApolloClient;

  /**
   * The host's react-intl instance, mirroring the user's selected
   * language. Use this for plugin-internal strings; plugins that want
   * to ship their own translations should manage them themselves.
   */
  readonly intl: IntlShape;

  /**
   * Stable router primitives. Use these instead of importing from
   * `@tanstack/react-router` directly so plugins don't pin to a
   * specific router version.
   */
  readonly router: {
    Link: typeof Link;
    useNavigate: typeof useNavigate;
  };

  /**
   * Curated stable subset of shadcn/Base UI primitives. The set is
   * frozen for the lifetime of host major version 1.
   */
  readonly ui: StashPluginUI;
}

export type PluginRegister = (host: StashPluginHost) => void | Promise<void>;

export interface PluginModule {
  default?: PluginRegister;
  register?: PluginRegister;
}
