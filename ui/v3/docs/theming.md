# Theming Stash v3

v3 supports runtime CSS and JavaScript customization, locale overrides, and
custom static assets. This guide describes the current hooks, checked on
2026-09-07. See the [documentation index](../../../docs/README.md) for the plugin
API and development guides.

## Files, endpoints, and settings

Place customization files alongside the active config file (`~/.stash/` by
default, or the directory containing `STASH_CONFIG_FILE`):

| File | Endpoint relative to the application base | Purpose |
| --- | --- | --- |
| `custom.css` | `css` | Plain CSS loaded through a stylesheet link |
| `custom.js` | `javascript` | Deferred script loaded after the React module script in the HTML |
| `custom-locales.json` | `customlocales` | JSON message overrides |

Settings → Interface has separate Custom CSS, Custom JavaScript, and Custom
Locales toggles. The Disable customizations switch overrides them. Disabled
CSS/JS endpoints return empty bodies; the locale endpoint returns `{}`.

[index.html](../index.html) uses relative endpoint URLs so these hooks work
under a deployment prefix. Custom JavaScript is deferred until parsing completes,
which does not guarantee that React has mounted. Custom CSS is served verbatim
by the [backend handlers](../../../internal/api/server.go); it does **not** pass
through Vite, Tailwind, PostCSS, or a Sass compiler.

## Runtime tokens and the CSS cascade

[globals.css](../src/styles/globals.css) currently has two color token families:

- `--color-primary`, `--color-background`, etc. drive Tailwind utilities and
  several shared components, including toast colors.
- Unprefixed `--primary`, `--background`, etc. remain in some direct CSS uses.
  Keep their overrides aligned when changing the corresponding color.

The source's `@theme` and `@theme inline` directives run at **build time**. Do not
put `@theme` or `@apply` in `custom.css`; browsers do not compile them. Runtime
color tokens are complete CSS colors, usually OKLCH, not HSL channel tuples.

Custom CSS is not guaranteed to appear after the bundled stylesheet. Normal
unlayered rules take precedence over normal rules in Tailwind's cascade layers,
regardless of selector specificity. The bundle also contains **unlayered** root,
dark-mode, and interaction rules; those compete by specificity and source order.
Use `html:root` and `html:root.dark` for token overrides so they beat the existing
`:root` and `.dark` rules even when the custom stylesheet is earlier.

```css
/* custom.css: runtime overrides for both token families */
html:root {
  --color-primary: oklch(0.55 0.18 250);
  --color-primary-foreground: oklch(0.99 0 0);
  --color-ring: oklch(0.65 0.15 250);
  --primary: var(--color-primary);
  --primary-foreground: var(--color-primary-foreground);
  --ring: var(--color-ring);
  --radius: 0.5rem;
}

html:root.dark {
  --color-primary: oklch(0.7 0.16 250);
  --color-primary-foreground: oklch(0.15 0.05 250);
  --color-background: oklch(0.13 0.02 250);
  --color-card: oklch(0.16 0.02 250);
  --background: var(--color-background);
  --card: var(--color-card);
}

[data-slot="card"] {
  box-shadow: 0 1px 3px color-mix(in oklch, var(--color-primary), transparent 92%);
}
```

Aliases on the root in this example follow the dark-mode `--color-*` values.
Inspect the browser's Computed panel to see which family a particular component
uses. Do not assume a late `@layer overrides` declaration will outrank utilities:
layer order is fixed when layers are first declared. Plain unlayered overrides
are usually sufficient; compare specificity for other unlayered rules and check
for inline styles before increasing it.

### Color and geometry reference

Color names in this table have a `--color-` form; the corresponding unprefixed
variables exist for most of them in `globals.css`.

| Group | Color names |
| --- | --- |
| Surface | `background`, `foreground`, `card`, `card-foreground`, `popover`, `popover-foreground`, `muted`, `muted-foreground`, `accent`, `accent-foreground` |
| Brand | `primary`, `primary-foreground`, `secondary`, `secondary-foreground`, `destructive`, `destructive-foreground` |
| Form/chrome | `border`, `input`, `ring` |
| Charts | `chart-1` through `chart-5` |
| Sidebar | `sidebar`, `sidebar-foreground`, `sidebar-primary`, `sidebar-primary-foreground`, `sidebar-accent`, `sidebar-accent-foreground`, `sidebar-border`, `sidebar-ring` |

`--radius` feeds the generated radius utilities. Sidebar width also has
`--sidebar-width` and `--sidebar-width-icon` variables, but component-level values
may override root defaults. Font utilities currently include build-time inline
values; changing `--font-sans` alone is not a universal runtime font switch. Use
plain `font-family` rules on the intended elements and check the computed result.

## Targeting components

Prefer semantic `data-slot` attributes and deliberate Stash class hooks over
Tailwind utility names or generated identifiers. The owning wrappers under
[src/components/ui/](../src/components/ui/) are the source of truth for rendered
attributes. Some wrappers delegate to another component and do not create a
separate DOM slot.

Examples present in the current wrappers:

- `[data-slot="button"]`
- `[data-slot="card"]`, `[data-slot="card-header"]`, `[data-slot="card-content"]`
- `[data-slot="dialog-content"]`, `[data-slot="dialog-overlay"]`
- `[data-slot="dropdown-menu-content"]`, `[data-slot="dropdown-menu-item"]`
- `[data-slot="combobox-trigger"]`, `[data-slot="combobox-list"]`
- `[data-slot="select-trigger"]`, `[data-slot="select-content"]`
- `[data-slot="checkbox"]`, `[data-slot="tooltip-content"]`
- `[data-slot="sheet-content"]`, `[data-slot="drawer-content"]`

`ComboboxInput` renders through the input-group wrappers, so it does not add a
`combobox-input` slot of its own.

### Base UI state attributes

State attributes depend on the primitive. For example, dropdown popups use
`[data-open]` / `[data-closed]`, submenu triggers can use `[data-popup-open]`, and
items expose attributes such as `[data-disabled]` or `[data-highlighted]`.
Positioned popups use `data-side` values such as `top`, `bottom`, `left`, or `right`
(and logical sides where supported).

```css
[data-slot="dropdown-menu-content"][data-open] {
  outline: 1px solid var(--color-border);
}
```

Do not assume Radix-style `[data-state="open"]` applies to Base UI popups. Some
Stash wrappers define their own `data-state`, so inspect the actual component
before writing a state selector.

### Stash hooks and interaction policy

Useful hooks include `.entity-card`, `.entity-card-rating-ribbon[data-rating]`,
`.entity-card-checkbox`, `.entity-card-select-overlay`, `.entity-card-wall-circle`,
`.filter-group-card`, `.filter-condition-card`, `.filter-pill`, `.image-lightbox`,
and `.bottom-tab-bar`. Preserve keyboard focus indicators and readable selected,
disabled, and error states when changing their appearance.

The [interaction policy](architecture.md#interaction-and-accessibility) disables
general page pinch/double-tap zoom while preserving custom image/video zoom.
Interface text is non-selectable by default. Inputs, editable regions, `code`,
`pre`, and values marked `data-selectable-text` remain copyable. Keep these
exceptions on values such as paths, URLs, hashes, and logs; do not enable selection
or alter touch behavior across whole panels as part of a theme.

## Static assets and deployment prefixes

Register directories in the Stash config:

```yaml
customServedFolders:
  /banners: /home/me/stash-theme/banners
  /fonts: /home/me/stash-theme/fonts
  /themes: /home/me/stash-theme/themes
```

These mappings appear under the application's `custom/` endpoint. In
`custom.css`, URLs relative to the `css` endpoint preserve the deployment prefix:

```css
/* @import must precede ordinary style rules. */
@import url("custom/themes/midnight.css");

@font-face {
  font-family: "MyFont";
  src: url("custom/fonts/myfont.woff2") format("woff2");
}

.entity-card-rating-ribbon[data-rating="5"]::after {
  background-image: url("custom/banners/gold.png");
}
```

For an app mounted at `/stash/`, `custom/fonts/...` in `/stash/css` resolves to
`/stash/custom/fonts/...`. A leading `/custom/...` would bypass that prefix.
URLs inside an imported stylesheet resolve relative to **that stylesheet's**
location, so a font reference from `custom/themes/midnight.css` would instead
use `../fonts/myfont.woff2`.

## JavaScript and plugins

`custom.js` runs in the application's window with the user's session. Use it
for small event listeners or observers. Resolve backend URLs from the base
element, even when the browser is displaying a nested SPA route:

```js
const graphqlURL = new URL("graphql", document.baseURI);
const themeAssetURL = new URL("custom/banners/gold.png", document.baseURI);
```

React owns the app's rendered UI, including portaled dialogs outside `#root`.
Directly mutating those nodes competes with reconciliation. If a script depends
on an element existing, use an observer with a defined cleanup point; do not
assume the deferred script runs after application startup. Avoid blocking startup
on large scripts.

For React-rendered pages, navigation, list filter extras, or saved-filter events,
use the [v3 plugin host](plugin-host.md). Its registration lifecycle and shared
Apollo/UI APIs provide the supported integration points.
