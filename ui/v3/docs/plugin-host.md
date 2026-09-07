# Stash v3 plugin host (host v1)

This document describes the contract for v3 UI plugins — plugins that add new top-level pages, navigation entries, and other React-rendered UI to Stash. Backend plugin features (hooks, scrapers, jobs, packaged plugin sources) are unchanged.

Checked against the implementation on 2026-09-07. The authoritative types are in
[host.ts](../src/plugins/host.ts), startup behavior in
[loader.ts](../src/plugins/loader.ts), and exported components in
[ui-exports.ts](../src/plugins/ui-exports.ts). See the
[documentation index](../../../docs/README.md) for related guides.

## Concept

A v3 plugin is an ESM module loaded at app boot. Stash's frontend dynamic-imports the module, calls its exported `register(host)` function, and the plugin uses the host APIs to add routes, nav entries, etc. Plugin code runs in the same origin and same React tree as Stash itself, with full access to the host's GraphQL client.

The v3 UI uses this host instead of the v2.5 `window.PluginApi`/DOM-patching API.
v3 does not load legacy UI scripts. Existing v2.5 UI plugins can still load in
the v2.5 frontend, and backend plugin behavior remains shared. Declaring a v3
entry does not authorize breaking the v2.5 client contract.

## Plugin manifest

A plugin opts into v3 by declaring `ui.entry` in its `<plugin>.yml`:

```yaml
id: stash-tv
name: StashTV
description: Vertical-scroll TikTok-style scene viewer.
version: 1.0.0
ui:
  # URL path beneath the plugin's assets endpoint.
  # This mapping serves ./dist/index.js as assets/index.js.
  entry: index.js
  assets:
    /: ./dist
```

The backend resolves the entry beneath `/plugin/{id}/assets/`, including the
deployment prefix, and returns it as `Plugin.paths.entry` from the `plugins`
GraphQL query. The entry is an **asset URL path**, not an independently resolved
filesystem path: with `assets: { "/": "./dist" }`, use `entry: index.js`, not
`dist/index.js`. v3 loads only enabled plugins with an entry. Plugins without one
are skipped, so their legacy UI can continue loading in v2.5.

## Entry module

The entry module must default-export a registration function, or provide a named
`register` export when there is no default export:

```js
// dist/index.js
function StashTVPage() {
  return "StashTV";
}

export default function register(host) {
  host.nav.add({
    label: "StashTV",
    to: "/stashtv",
  });
  host.routes.add({
    path: "/stashtv",
    component: StashTVPage,
  });
}
```

`register()` may be `async`; the loader awaits plugins sequentially. Calls to
routes, navigation, filter extras, and saved-filter listeners are **staged until
registration completes successfully**. They may occur after an `await`, provided
the registration promise has not settled or timed out. If module import or
registration rejects or times out, those staged additions are discarded. Calls
through that plugin's host after completion/timeout are ignored.

Import plus registration has a **5-second per-plugin timeout**. Plugin discovery
and load-order queries each have a 5-second timeout, and the overall startup
budget is **30 seconds**, including those queries. Later plugins receive only
the remaining budget. The shared registry freezes after all attempts; the router
is then built. A full page reload is required to load changed plugin code.

These timeouts stop waiting for asynchronous work and prevent late host
registrations. They do not cancel arbitrary plugin network requests or undo
plugin side effects, and cannot preempt JavaScript that blocks the main thread.

## Host API (`host v1`)

```ts
interface StashPluginHost {
  readonly version: "1";
  readonly pluginId: string;            // your plugin's id, useful for logs

  readonly routes: {
    add(r: { path: string; component: ComponentType }): void;
  };

  readonly nav: {
    add(i: {
      label: string | ((intl: IntlShape) => string);
      to: string;
      icon?: ReactNode;
      placement?: "main" | "mobile" | "utility";
      hotkey?: string;
    }): void;
  };

  readonly filters: {
    addExtras(component: ComponentType<{
      filter: ListFilterModel;
      searchTerm: string;
      view?: string;
    }>): void;
  };

  readonly events: {
    onSavedFilterLoaded(listener: (event: {
      savedFilter: SavedFilterDataFragment;
      filter: ListFilterModel;
      source: "dialog" | "sidebar" | "toolbar";
      view?: string;
    }) => void | Promise<void>): void;
  };

  readonly apollo: ApolloClient;        // shared with the host
  readonly intl: IntlShape;             // host's react-intl
  readonly router: { Link, useNavigate }; // stable router primitives
  readonly ui: StashPluginUI;           // curated stable component subset
}
```

### Routes

`host.routes.add({ path, component })` mounts a top-level route. Constraints:

- `path` must start with `/` and is relative to the router's configured base path.
  Do not include a deployment prefix such as `/stash` in the registered path.
- Plugins should namespace their routes under a unique prefix (typically the plugin id) to avoid collisions: `/stashtv`, `/stashtv/settings`, etc.
- `component` is a React component rendered when the route is active. It receives
  no injected props; close over `host` when needed. Host v1 exports `Link` and
  `useNavigate`, not a complete set of router state/parameter hooks.
- Core route collisions and equivalent normalized route patterns are rejected
  during router construction. See failure handling below.

### Navigation

`host.nav.add({ label, to, icon?, placement?, hotkey? })`:

- `placement: "main"` (default) — primary sidebar / mobile bottom-tab overflow.
- `placement: "mobile"` — primary mobile nav (use sparingly; the bottom bar is space-constrained).
- `placement: "utility"` — utility menus only.
- `label` may be a string or a function `(intl) => string` for plugins that ship localized strings.
- `hotkey` follows the same `"g <key>"` chord syntax as built-in nav (`"g s"` for `/scenes`). Built-in hotkeys take precedence; plugin hotkeys cannot shadow them.
- `icon` is a rendered React node, not a component function. For JSX source, pass
  `icon: <TvIcon size={16} />`, not `icon: TvIcon`.

### Filter extensions

`host.filters.addExtras(Component)` renders a plugin component below the list toolbar whenever an entity list is active. The component receives:

- `filter` — the complete current v3 `ListFilterModel`, including its canonical Filter AST.
- `searchTerm` — a convenience mirror of `filter.searchTerm`.
- `view` — the persisted Stash view name when the list has one.

Extensions render in plugin load order. Each extension has its own error boundary, so one plugin's render failure is logged and removes only that extension.

### Saved-filter events

`host.events.onSavedFilterLoaded(listener)` runs after a saved filter has been applied. The event includes the saved-filter record, the applied v3 filter model, its load source, and the current view when available. Listener failures are logged without interrupting the filter change or other plugins.

### Apollo + intl

`host.apollo` is the same `ApolloClient` instance the rest of Stash uses, with the same auth cookies, cache, and link chain. Plugin-issued queries and mutations participate in cache normalization automatically.

`host.intl` is a getter for the current host `IntlShape`; it follows language
updates. Read it when formatting instead of retaining the startup instance.
Use `host.intl.formatMessage({...})` for localized strings. Plugin-owned message
catalog loading is the plugin's responsibility.

### Router primitives

`host.router.Link` and `host.router.useNavigate` are re-exports from `@tanstack/react-router`. Use these instead of importing the package directly so plugins don't pin a specific router version.

### Curated UI exports (`host.ui`)

A curated subset of shadcn/Base UI primitives. Their API shape is part of host
v1; internal DOM structure and classes are not an unrestricted extension API.
The current set:

| Group | Exports |
|---|---|
| Buttons | `Button`, `buttonVariants` |
| Cards | `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter` |
| Form | `Input`, `Textarea`, `Label`, `Checkbox` |
| Selects | `Select`, `SelectTrigger`, `SelectValue`, `SelectContent`, `SelectGroup`, `SelectItem` |
| Comboboxes | `Combobox`, `ComboboxTrigger`, `ComboboxInput`, `ComboboxValue`, `ComboboxContent`, `ComboboxItem`, `ComboboxEmpty` |
| Dialogs | `Dialog`, `DialogTrigger`, `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogDescription`, `DialogFooter` |
| Sheets | `Sheet`, `SheetTrigger`, `SheetContent`, `SheetHeader`, `SheetTitle`, `SheetDescription`, `SheetFooter` |
| Menus | `DropdownMenu`, `DropdownMenuTrigger`, `DropdownMenuContent`, `DropdownMenuGroup`, `DropdownMenuItem`, `DropdownMenuLabel`, `DropdownMenuSeparator` |
| Misc | `Tooltip`, `TooltipProvider`, `TooltipTrigger`, `TooltipContent`, `Spinner` |

Other libraries (charts, virtualization, drag-and-drop, etc.) are outside the
host contract. The loader imports a browser ESM URL as-is: it does not compile
JSX/TypeScript or resolve bare npm imports. Host v1 currently provides neither a
React module export nor an import map. A plugin build must arrange resolvable
imports and compatible shared React runtime access before using hooks or React
libraries; marking `react` external alone is not sufficient. The example below
avoids imports and hooks so it can be served directly.

### Settings

Manifest `settings:` entries already appear in **v3 Settings → Plugins**.
Boolean, number, and string settings use the shared setting controls and persist
through `configurePlugin`. This is separate from registering arbitrary React
content inside the settings pages, which host v1 does not provide.

## Load order

Plugins load in the configured order for the `ui-load` hook (see Settings → Plugin hook order). Plugins not listed in the override load alphabetically by id, after the explicitly-ordered ones. The same persistence model applies as for backend hooks; configured ids that no longer match an installed plugin are silently skipped.

A plugin should not assume any other plugin has loaded first unless the user has configured the order. Cross-plugin dependencies are not part of host v1; coordinate via shared GraphQL state if you need to.

## Trust model

Plugins run in Stash's origin context. They have full access to:

- The user's auth cookies and session
- localStorage / sessionStorage / IndexedDB
- The full GraphQL schema (every query and mutation Stash exposes)
- The DOM, including non-Stash regions

There is no sandboxing. Same as v2.5 plugins, same as VSCode extensions: the user is responsible for vetting plugins before installing. The benefit is that plugins integrate seamlessly — they share the host's Apollo cache, render with the host's design tokens, and feel native.

## Versioning

`host.version` is `"1"` for the current contract. Plugins should check it on entry and bail out if it doesn't match what they were built against:

```js
export default function register(host) {
  if (host.version !== "1") {
    console.warn(`[stashtv] expected host v1, got ${host.version}; skipping.`);
    return;
  }
  // ... register routes/nav
}
```

When the host gains a new field within v1, existing plugins keep working. Breaking shape changes — removing or changing the type of an existing field — bump to host v2 and live alongside v1 for at least one release cycle.

## Failure handling

The loader and router handle host-managed failures as follows:

- Discovery failure yields an empty frozen registry. Load-order failure falls
  back to alphabetical order. Both are logged and surfaced in the startup warning.
- An import/registration error, missing callable export, or timeout skips that
  plugin's staged registrations. Other attempts continue within the startup budget.
- Exact duplicate plugin paths are warned and skipped by the registry. Router
  construction additionally normalizes repeated/trailing slashes and parameter
  names to detect collisions with core or other plugin routes.
- If plugin routes prevent router construction, the app warns and retries with
  **core routes only**. This fallback omits all plugin routes for that page load,
  not just the colliding route.
- Filter-extra components have individual render error boundaries; saved-filter
  listener exceptions/rejections are logged without stopping other listeners.

Late host calls are ignored. Reload after correcting an entry path, plugin
error, or collision; registration is not a live update mechanism.

## Worked example: a tiny plugin

`my-plugin.yml` in the plugins folder:

```yaml
id: hello-world
name: Hello World
version: 0.1.0
ui:
  entry: index.js
  assets:
    /: .
```

`index.js` next to the yml:

```js
export default function register(host) {
  if (host.version !== "1") return;
  host.nav.add({ label: "Hello", to: "/hello", icon: null });
  host.routes.add({
    path: "/hello",
    component: () => "Hello from a plugin!",
  });
}
```

This minimal React component returns text, so the entry has no module
dependencies. For a bundled plugin, produce browser-ready ESM, expose all chunks
through the asset mappings, and verify imports against the deployed app as well
as a development server. Plugin CSS is not generated by the host's Tailwind
build; ship any styles the existing bundle does not contain. Use the
[theming guide](theming.md) for runtime tokens and preserve the shared
[interaction policies](architecture.md#interaction-and-accessibility).

## What this host doesn't (yet) provide

These were intentionally left out of v1; if you need them, file a request and we'll evaluate for v2:

- **Arbitrary component-slot extension points** (e.g. "add an item to the scene detail tab bar"). v1 currently provides the list filter extras slot plus whole-page extension via `routes.add`.
- **Plugin-to-plugin APIs.** Plugins can communicate via the GraphQL cache or window events, but there's no formal API.
- **Arbitrary settings-page components.** Manifest boolean/number/string settings
  already work in v3; there is no host API to inject custom settings controls.
- **Lazy route loading.** `host.routes.add` accepts a synchronous component. Wrap in `React.lazy` if you want code-splitting (the host won't introspect it).
- **Iframe / Web Component sandboxing.** Out of scope for v1. Same trust model as v2.5.
