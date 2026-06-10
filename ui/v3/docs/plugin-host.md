# Stash v3 plugin host (host v1)

This document describes the contract for v3 UI plugins — plugins that add new top-level pages, navigation entries, and other React-rendered UI to Stash. Backend plugin features (hooks, scrapers, jobs, packaged plugin sources) are unchanged.

## Concept

A v3 plugin is an ESM module loaded at app boot. Stash's frontend dynamic-imports the module, calls its exported `register(host)` function, and the plugin uses the host APIs to add routes, nav entries, etc. Plugin code runs in the same origin and same React tree as Stash itself, with full access to the host's GraphQL client.

This is a clean break from the v2.5 plugin model (`window.PluginApi` + DOM patching). v2.5 plugins are not loaded by v3.

## Plugin manifest

A plugin opts into v3 by declaring `ui.entry` in its `<plugin>.yml`:

```yaml
id: stash-tv
name: StashTV
description: Vertical-scroll TikTok-style scene viewer.
version: 1.0.0
ui:
  # Path to the ESM entry module, relative to the plugin's directory.
  # Must be reachable through one of the `assets:` mappings.
  entry: dist/index.js
  assets:
    /: ./dist
```

The backend resolves the entry to `/plugin/{id}/assets/{entry-path}` and returns it as `Plugin.paths.entry` from the `plugins` GraphQL query. v3 only loads plugins whose `entry` is set; plugins missing it are skipped silently (so v2.5-style plugins continue to load on v2.5 without affecting v3).

## Entry module

The entry module must default-export `register`:

```js
// dist/index.js
export default function register(host) {
  host.nav.add({
    label: "StashTV",
    to: "/stashtv",
    icon: TvIcon,
  });
  host.routes.add({
    path: "/stashtv",
    component: StashTVPage,
  });
}
```

`register()` may be `async` — the loader awaits each plugin in turn. Async work inside `register` is fine for setup, but **all `routes.add()` and `nav.add()` calls must be made synchronously** (i.e. before any `await`). The registry freezes after `register` resolves; later additions are warned and ignored.

A named `register` export is also accepted as a fallback if the default export is missing.

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

  readonly apollo: ApolloClient;        // shared with the host
  readonly intl: IntlShape;             // host's react-intl
  readonly router: { Link, useNavigate }; // stable router primitives
  readonly ui: StashPluginUI;           // curated stable component subset
}
```

### Routes

`host.routes.add({ path, component })` mounts a top-level route. Constraints:

- `path` must start with `/`.
- Plugins should namespace their routes under a unique prefix (typically the plugin id) to avoid collisions: `/stashtv`, `/stashtv/settings`, etc.
- `component` is a React component rendered when the route is active. It receives no props from the router; pull params/search via the host's router primitives if needed.

### Navigation

`host.nav.add({ label, to, icon?, placement?, hotkey? })`:

- `placement: "main"` (default) — primary sidebar / mobile bottom-tab overflow.
- `placement: "mobile"` — primary mobile nav (use sparingly; the bottom bar is space-constrained).
- `placement: "utility"` — utility menus only.
- `label` may be a string or a function `(intl) => string` for plugins that ship localized strings.
- `hotkey` follows the same `"g <key>"` chord syntax as built-in nav (`"g s"` for `/scenes`). Built-in hotkeys take precedence; plugin hotkeys cannot shadow them.

### Apollo + intl

`host.apollo` is the same `ApolloClient` instance the rest of Stash uses, with the same auth cookies, cache, and link chain. Plugin-issued queries and mutations participate in cache normalization automatically.

`host.intl` is the host's `IntlShape`. Use `host.intl.formatMessage({...})` for localized strings.

### Router primitives

`host.router.Link` and `host.router.useNavigate` are re-exports from `@tanstack/react-router`. Use these instead of importing the package directly so plugins don't pin a specific router version.

### Curated UI exports (`host.ui`)

A frozen subset of shadcn/Base UI primitives. Components in this set will not break in shape across host v1's lifetime. The current set:

| Group | Exports |
|---|---|
| Buttons | `Button`, `buttonVariants` |
| Cards | `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter` |
| Form | `Input`, `Textarea`, `Label`, `Checkbox` |
| Selects | `Select`, `SelectTrigger`, `SelectValue`, `SelectContent`, `SelectItem` |
| Comboboxes | `Combobox`, `ComboboxTrigger`, `ComboboxInput`, `ComboboxValue`, `ComboboxContent`, `ComboboxItem`, `ComboboxEmpty` |
| Dialogs | `Dialog`, `DialogTrigger`, `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogDescription`, `DialogFooter` |
| Sheets | `Sheet`, `SheetTrigger`, `SheetContent`, `SheetHeader`, `SheetTitle`, `SheetDescription`, `SheetFooter` |
| Menus | `DropdownMenu`, `DropdownMenuTrigger`, `DropdownMenuContent`, `DropdownMenuItem`, `DropdownMenuLabel`, `DropdownMenuSeparator` |
| Misc | `Tooltip`, `TooltipProvider`, `TooltipTrigger`, `TooltipContent`, `Spinner` |

Bring your own libraries for anything else (charts, virtualization, drag-and-drop, etc.) — those don't belong in the host contract.

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

The loader is defensive:

- Errors fetching the plugin list, hook order, or any individual plugin's module are caught and logged. One broken plugin never takes down the app.
- Plugins missing a `default export register(host)` log a warning and are skipped.
- Routes/nav added after the registry freezes are warned and ignored.
- Routes registered at colliding paths are warned and the second registration is dropped.

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
import React from "react";

function HelloPage({ host }) {
  return React.createElement(
    host.ui.Card,
    { className: "m-4 p-6" },
    React.createElement("h1", { className: "text-xl" }, "Hello from a plugin!"),
  );
}

export default function register(host) {
  if (host.version !== "1") return;
  host.nav.add({ label: "Hello", to: "/hello", icon: null });
  host.routes.add({
    path: "/hello",
    component: () => React.createElement(HelloPage, { host }),
  });
}
```

(For real plugins, ship a bundler config that produces an ESM `dist/index.js` so you can write JSX/TSX. The host receives whatever the entry module exports — the host doesn't care how it's built.)

## What this host doesn't (yet) provide

These were intentionally left out of v1; if you need them, file a request and we'll evaluate for v2:

- **Component-slot extension points** (e.g. "add an item to the scene detail tab bar"). v1 only supports whole-page extension via `routes.add`.
- **Plugin-to-plugin APIs.** Plugins can communicate via the GraphQL cache or window events, but there's no formal API.
- **Settings UI integration.** Plugin settings are still configured via the v2.5 settings UI driven by the plugin manifest's `settings:` block.
- **Lazy route loading.** `host.routes.add` accepts a synchronous component. Wrap in `React.lazy` if you want code-splitting (the host won't introspect it).
- **Iframe / Web Component sandboxing.** Out of scope for v1. Same trust model as v2.5.
