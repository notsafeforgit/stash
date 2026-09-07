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
| `make validate-ui-v3` | Biome lint (including accessibility), generation, TypeScript, formatting, locales, Vitest, and pinned v2.5 compatibility |
| `make validate-fork` | Backend generation, v3 validation, Go lint, and Go unit/integration tests |
| `make lint` | CI-pinned golangci-lint via `go run` |
| `make it` | Go tests with `sqlite_stat4 sqlite_math_functions integration` build tags |
| `pnpm --dir ui/v3 test --run` | Generate v3 GraphQL types and run Vitest once |
| `make fmt-ui-v3` | Format v3 source with Biome |
| `make validate` | Upstream/v2.5 UI validation plus backend checks; does not validate v3 |

During iteration, choose checks for the changed contracts. The
[compatibility checker](../scripts/check-compatibility.mjs) validates pinned
mainline operations, additive schema behavior, argument defaults, and upstream
migrations. Follow [FORK.md](../../../FORK.md) when updating its baseline after
an upstream sync. Do not modify `ui/v2.5/` for fork feature work.

For routing changes, exercise a deployment under a path prefix as well as `/`.
For player or gesture changes, check target browsers and physical iOS devices;
unit tests cannot verify native fullscreen or touch behavior. Preserve the
[zoom, keyboard, focus, and text-selection policies](architecture.md#interaction-and-accessibility).

## Publishing

Follow the [deployment runbook](../../../docs/v3-deployment.md) after the intended
source commit is pushed. It covers the two image builds and verifying what the
local container actually runs.
