# Theming Stash v3

v3 lets you customize the UI without forking the codebase by injecting your own CSS and JS, plus serving arbitrary static assets from a folder of your choice. This page covers what those hooks are and the public surface you can target safely.

## Files and where they live

Stash looks for two files in its config directory (`~/.stash/` by default, or wherever `STASH_CONFIG_FILE` points):

- `custom.css` — injected as a `<link rel="stylesheet">` near the end of `<head>`. Loads after the bundled stylesheet, so unlayered rules outrank Tailwind utilities at the same specificity.
- `custom.js` — injected as `<script defer>` after the React module script. Runs after the DOM is parsed but is not guaranteed to run after React has mounted; defer your work until you see the elements you need.

For arbitrary assets (images, fonts, additional stylesheets you `@import`), declare folders under `customServedFolders` in the Stash config. Files mapped this way are served at `/custom/<your-path>` and are reachable from `custom.css`/`custom.js` and from any user-provided HTML.

## Enabling

In Settings → Interface, three toggles control all of this:

- **Custom CSS** — gates whether `custom.css` is served. Off by default.
- **Custom JavaScript** — gates whether `custom.js` is served. Off by default.
- **Custom Locales** — gates `/customlocales`, used by the locale provider for string overrides.

When a toggle is off, Stash returns an empty body for the corresponding endpoint so the `<link>` / `<script>` tags load harmlessly. The "Disable customizations" master switch overrides all three.

## CSS: theme via design tokens

The fastest way to reskin Stash is to override the design tokens. Every component pulls colors, radii, font, and sidebar dimensions from CSS custom properties — change the variables and the whole UI follows.

Tokens are declared at `:root` (light mode), under `.dark` (dark mode), and inside `@theme` blocks (Tailwind v4 utility tokens). Override them in `custom.css`:

```css
:root {
  --primary: oklch(0.55 0.2 260);
  --primary-foreground: oklch(0.99 0 0);
  --radius: 0.4rem;
}

.dark {
  --primary: oklch(0.7 0.18 260);
  --background: oklch(0.12 0.02 260);
}

/* Tailwind v4 utility tokens — these power classes like bg-primary, text-foreground */
@theme {
  --color-primary: oklch(0.55 0.2 260);
  --color-primary-foreground: oklch(0.99 0 0);
}
```

### Token reference

| Group | Variables |
|---|---|
| Surface | `--background`, `--foreground`, `--card`, `--card-foreground`, `--popover`, `--popover-foreground`, `--muted`, `--muted-foreground`, `--accent`, `--accent-foreground` |
| Brand | `--primary`, `--primary-foreground`, `--secondary`, `--secondary-foreground`, `--destructive` |
| Form / chrome | `--border`, `--input`, `--ring` |
| Charts | `--chart-1` through `--chart-5` |
| Sidebar | `--sidebar`, `--sidebar-foreground`, `--sidebar-primary`, `--sidebar-primary-foreground`, `--sidebar-accent`, `--sidebar-accent-foreground`, `--sidebar-border`, `--sidebar-ring`, `--sidebar-width`, `--sidebar-width-icon` |
| Geometry | `--radius` (drives `--radius-sm/md/lg/xl/2xl/3xl/4xl` via `calc()`) |
| Typography | `--font-sans`, `--font-heading` |

Set the same variables in light and dark modes (`:root` and `.dark`) if you want both to follow your overrides.

## CSS: targeting components

When tokens aren't enough and you need to style specific components, use the stable hooks below. These are part of the theming surface and should not change between minor versions.

### `data-slot` — shadcn component slots

Every shadcn primitive in `src/components/ui/` carries a `data-slot` attribute identifying its role. Examples:

- `[data-slot="button"]` — every Button
- `[data-slot="card"]`, `[data-slot="card-header"]`, `[data-slot="card-content"]`, `[data-slot="card-footer"]`
- `[data-slot="dialog-content"]`, `[data-slot="dialog-overlay"]`, `[data-slot="dialog-title"]`
- `[data-slot="dropdown-menu-content"]`, `[data-slot="dropdown-menu-item"]`
- `[data-slot="combobox-input"]`, `[data-slot="combobox-list"]`, `[data-slot="combobox-item"]`
- `[data-slot="select-trigger"]`, `[data-slot="select-content"]`, `[data-slot="select-item"]`
- `[data-slot="checkbox"]`, `[data-slot="checkbox-group"]`
- `[data-slot="tooltip-content"]`
- `[data-slot="sheet-content"]`, `[data-slot="bottom-sheet-content"]`, `[data-slot="drawer-content"]`

```css
[data-slot="card"] {
  border: 1px solid color-mix(in oklch, var(--primary), transparent 80%);
}
```

### `data-state`, `data-side`, `data-disabled` — Base UI primitives

Base UI carries open/closed, side, and interactive state on the rendered element:

- `[data-state="open"]` / `[data-state="closed"]`
- `[data-side="top|right|bottom|left"]`
- `[data-disabled]`, `[data-highlighted]`, `[data-selected]`

```css
[data-slot="dropdown-menu-content"][data-state="open"] {
  animation: my-fade-in 120ms ease;
}
```

### Stash-specific class hooks

A handful of Stash-defined classes that paper over Tailwind have stable names; you can target them safely:

- `.entity-card`, `.entity-card-rating-ribbon[data-rating="1..5"]`, `.entity-card-checkbox`, `.entity-card-select-overlay`, `.entity-card-wall-circle`
- `.filter-group-card`, `.filter-condition-card`, `.filter-depth-{0..5}`, `.filter-pill`, `.filter-pill-{muted,success,danger}`
- `body.mobile-list-view` — body class while the mobile list bar is mounted
- `.lightbox-mobile-toolbar-bottom`, `.image-lightbox`, `.image-lightbox.chrome-hidden`
- `.bottom-tab-bar` — the global mobile tab bar

What you should *not* target:

- Tailwind utility classes themselves (`text-sm`, `bg-card`, etc.) — they're an implementation detail and may change.
- Auto-generated identifiers (anything that looks like a hash).

### Specificity and `@layer`

Tailwind utilities live in the `utilities` cascade layer. Plain unlayered CSS in `custom.css` outranks them at equal specificity, so most overrides "just work." If you find yourself fighting a utility, increase specificity via the data-slot selector (`[data-slot="..."].your-class`) or wrap rules in a later layer:

```css
@layer overrides {
  [data-slot="button"] {
    text-transform: uppercase;
  }
}
```

## Static assets via `/custom/`

To serve images, fonts, or additional CSS files alongside the UI, register a folder under `customServedFolders` in Stash's config:

```yaml
customServedFolders:
  /banners: /home/me/stash-theme/banners
  /fonts: /home/me/stash-theme/fonts
```

You can then reference them from `custom.css`:

```css
@font-face {
  font-family: "MyFont";
  src: url("/custom/fonts/myfont.woff2") format("woff2");
}

.entity-card-rating-ribbon[data-rating="5"]::after {
  background: url("/custom/banners/gold.png");
}
```

Or load an entire stylesheet:

```css
@import url("/custom/themes/midnight.css");
```

## JavaScript caveats

`custom.js` runs in the same window as the React app. You can:

- Add global event listeners (`document.addEventListener("keydown", ...)`).
- Open WebSockets, do `fetch()`, observe `IntersectionObserver`/`MutationObserver`.
- Append your own DOM nodes to `document.body` (overlays, debug panels) — React only owns `#root`.
- Read configuration via the GraphQL endpoint at `/graphql`.

What will hurt:

- **Mutating Stash's DOM nodes inside `#root`.** React will reconcile your changes away on the next render. If you must annotate a Stash-rendered element, observe rather than mutate (e.g. write data to a side channel and react to events), or use CSS instead.
- **Assuming elements exist at boot.** The script loads `defer`, but React is async — wrap your work in a `MutationObserver` or `requestAnimationFrame` loop until the elements you want are present.
- **Loading large libraries synchronously.** Use `import()` for anything heavy so you don't block paint.

## Worked example

A minimal "blue accent + rounded corners" theme:

```css
/* custom.css */
:root {
  --primary: oklch(0.55 0.18 250);
  --primary-foreground: oklch(0.99 0 0);
  --ring: oklch(0.65 0.15 250);
  --radius: 0.5rem;
}

.dark {
  --primary: oklch(0.7 0.16 250);
  --primary-foreground: oklch(0.15 0.05 250);
  --background: oklch(0.13 0.02 250);
  --card: oklch(0.16 0.02 250);
}

@theme {
  --color-primary: oklch(0.55 0.18 250);
  --color-primary-foreground: oklch(0.99 0 0);
}

[data-slot="card"] {
  box-shadow: 0 1px 3px color-mix(in oklch, var(--primary), transparent 92%);
}
```

A minimal "log every navigation" custom.js:

```js
// custom.js
let lastPath = location.pathname;
new MutationObserver(() => {
  if (location.pathname !== lastPath) {
    console.log("nav", location.pathname);
    lastPath = location.pathname;
  }
}).observe(document.body, { childList: true, subtree: true });
```

## Tips

- Use the browser inspector's **Computed** panel to find which token a color is coming from — overriding the token is almost always cleaner than overriding the rule.
- Keep `custom.css` short. The whole file reloads on every page load, so big imported stylesheets show up in the critical path.
- Stash's settings page is itself styled with these tokens; once your theme is right there, the rest of the app follows automatically.
