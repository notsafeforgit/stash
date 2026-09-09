# Developing v3

Use these instructions for the active rewrite. The root
[development guide](../../../docs/DEVELOPMENT.md) retains upstream platform setup
and v2.5 instructions. See the [documentation index](../../../docs/README.md) for
architecture, compatibility, feature guides, and operations.

## Prerequisites

Run the commands below from the Git root. Use Go matching
[go.mod](../../../go.mod), a C compiler for SQLite/CGO, Make, Git, FFmpeg/ffprobe,
and Node.js 24 (the compiler image's Node major). Use pnpm 10, matching the
version pinned in [the v2.5 package manifest](../../v2.5/package.json); both UI
trees have their own lockfiles. `make lint` runs the CI-pinned Go linter through
`go run`, so a separate golangci-lint installation is unnecessary.

## First checkout and local development

Install both UI dependency trees, generate shared bindings, and build both
embedded bundles:

```bash
make pre-ui
make pre-ui-v3
make generate
make ui
make ui-v3-only
```

Both bundles are embedded in the Go binary. `make generate` creates placeholder
build directories when necessary, but placeholders do not satisfy the embedded
asset tests. Keep the build steps in this order on a clean checkout.

In one terminal, start a development backend:

```bash
STASH_PORT=9999 STASH_ENABLE_V3_UI=true make server-start
```

This runs from `.local/` with `.local/config.yml`. Complete setup using a test
library on first launch. Restart this command after backend changes.

In a second terminal, start v3:

```bash
VITE_APP_PLATFORM_URL=http://127.0.0.1:9999 make ui-v3-start
```

Open `http://localhost:3002/`. Vite regenerates v3 GraphQL types and the settings
search index before starting, then hot reloads frontend changes. Set the backend
URL explicitly: v3's Vite proxy defaults to port **8010**, whereas this quickstart
uses **9999**. `make ui-start` runs the separate v2.5 dev server on port 3000.

The backend flag selects the embedded v3 UI and enables its additional HTTP
endpoints. v2.5 GraphQL clients continue to use the shared API. For compatibility
checks, use the v2.5 dev UI or a mainline client against that backend; restart
with `STASH_ENABLE_V3_UI=false` to exercise the embedded v2.5 fallback.

## Generation and builds

| Command | Scope |
| --- | --- |
| `make generate-backend` | Go GraphQL bindings |
| `make generate` | Go bindings and **v2.5** GraphQL types |
| `pnpm --dir ui/v3 gqlgen` | v3 GraphQL types and typed operation documents |
| `pnpm --dir ui/v3 check` | v3 GraphQL/settings generation and TypeScript |
| `make ui` | v2.5 bundle and login locales |
| `make ui-v3-only` | v3 generation, TypeScript, and production bundle |
| `make stash` | Main binary, embedding the bundles already on disk |

After schema changes, run `make generate` and regenerate v3 as well. v3's `dev`,
`build`, and `check` scripts already include its generators. TanStack's Vite
plugin generates `src/routeTree.gen.ts` during dev/build; rebuild after route-file
changes before standalone type checks if the generated tree is stale. Never
hand-edit generated bindings, route trees, or the settings search index.

## Validation

The full fork gate, after installing dependencies, is:

```bash
make generate
make ui
make ui-v3-only
make validate-fork
```

Build the UIs **before** `make validate-fork` or full Go tests. In particular,
[ui/ui_v3_test.go](../../../ui/ui_v3_test.go) checks that the binary embeds
compressed underscore-prefixed route chunks. An empty/stale build directory
can fail this check even when frontend lint and type checks pass. The
[container publisher](../../../.github/workflows/ghcr-publish.yml) uses the same
generate → build UIs → validate → compile order.

| Command | Checks |
| --- | --- |
| `make validate-ui-v3` | Biome lint (including accessibility), React purity/type-contract lint, generation, TypeScript, formatting, locales, Vitest, and pinned v2.5 compatibility |
| `make validate-fork` | Backend generation, v3 validation, Go lint, and Go unit/integration tests |
| `make lint` | CI-pinned golangci-lint via `go run` |
| `make it` | Go tests with `sqlite_stat4 sqlite_math_functions integration` build tags |
| `pnpm --dir ui/v3 test --run` | Generate v3 GraphQL types and run Vitest once |
| `make test-ui-v3-browser` | Generate/check v3 and run the Chromium/WebKit toolbar regression suite |
| `make fmt-ui-v3` | Format v3 source with Biome |
| `make validate` | Upstream/v2.5 UI validation plus backend checks; does not validate v3 |

During iteration, choose checks for the changed contracts. The
[compatibility checker](../scripts/check-compatibility.mjs) validates pinned
mainline operations, additive schema behavior, argument defaults, and upstream
migrations. Follow [FORK.md](../../../FORK.md) when updating its baseline after
an upstream sync. Do not modify `ui/v2.5/` for fork feature work.

TypeScript enables `strict` and `noUncheckedIndexedAccess`: check lookup results
or iterate actual entries. Do not add blanket non-null assertions to satisfy the
compiler. `exactOptionalPropertyTypes` remains a separate migration because
omitted and explicitly cleared configuration/API values have different meanings.

Biome owns formatting, general lint and effect dependencies. It knows that
`useCommittedRef` has a stable result. `pnpm --dir ui/v3 lint:contracts` adds
React's `refs`/`purity` rules and rejects `as never` and double assertions.
These checks run through the normal `lint` command. Public browser/plugin
boundaries should narrow unknown inputs or expose checked capabilities.

The app root enables React Strict Mode. Lifecycle regression tests use real
React roots and, for navigation, real TanStack memory routers. Type contract
tests include expected compiler failures for mismatched routes, configuration
keys, list providers, and insufficient media projections.

Offline migration tests use `fake-indexeddb` for database transactions and
controlled file/lock fixtures for quota failures, cancellation, ownership,
receipts, and concurrent imports. Changes to browser storage also need a real
browser check with two backend prefixes on one origin, multiple tabs on one
prefix, and legacy/previous-prefix recovery. Use synthetic files in a fresh
browser profile so these checks do not alter existing downloads.

For routing changes, exercise a deployment under a path prefix as well as `/`.
For player or gesture changes, check target browsers and physical iOS devices;
unit tests cannot verify native fullscreen or touch behavior. Preserve the
[zoom, keyboard, focus, and text-selection policies](architecture.md#interaction-and-accessibility).

## Browser regression suite

Install the browsers once after installing the locked UI dependencies:

```bash
pnpm --dir ui/v3 exec playwright install chromium webkit
make test-ui-v3-browser
```

The [browser workflow](../../../.github/workflows/v3-browser-tests.yml) installs
the browser system libraries with `playwright install --with-deps chromium webkit`
on Ubuntu. It runs for relevant pushes and pull requests and saves traces and
screenshots on failure. To inspect a local failure, run
`pnpm --dir ui/v3 exec playwright show-report`.

[tests/browser](../tests/browser) starts and stops its own Vite server on
`127.0.0.1:3025`. It renders the real collection/media and settings layouts,
tabs, lists, toolbar, popovers, drawers, and forms with synthetic data; backend-dependent
navigation and default-filter integrations are substituted. No backend, credentials, or
existing library is required. Unexpected requests and browser errors fail the
tests. These are shared-component integration tests, not complete entity-route
or backend tests.

The suite covers 320–1280px layouts, search/selection replacement rows, page
validation, section state, direct drawer access, flat entity actions and desktop
submenus, drawer drag and outside-tap dismissal, editor dismissal, keyboard
navigation, and viewport lifting. Settings checks cover bottom navigation,
section routes and browser Back, search highlight links, keyboard result
selection, and preserving unsaved form state across breakpoint changes.
Browser sources are strictly type-checked by
the normal validation command; `.browser.ts` tests run separately from Vitest. Virtual viewport
resizing models keyboard geometry, but physical iOS keyboard and gesture checks
remain necessary for changes to those interactions.

## Publishing

Follow the [deployment runbook](../../../docs/v3-deployment.md) after the intended
source commit is pushed. It covers the two image builds and verifying what the
local container actually runs.
