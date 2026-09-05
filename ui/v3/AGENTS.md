# Repository Guidelines

## Scope & Architecture

`ui/v3/` is the active, ground-up React 19/TypeScript rewrite of Stash's UI. It shares the Go backend and GraphQL API at the Git root. Treat `ui/v2.5/` as a read-only reference and fallback; do not implement fixes there. The v3 UI and supporting endpoints are gated by `--enable-v3-ui`/`STASH_ENABLE_V3_UI=true`.

This is a tracking fork of `stashapp/stash`. Read root-level `CLAUDE.md` for current architecture and `FORK.md` before schema changes, migrations, or upstream syncs. Keep fork work in fork-owned files, shared upstream edits small, and GraphQL changes additive.

## Project Structure

File-based TanStack routes live in `src/routes/`; feature components are in `src/components/`, with shadcn/Base UI primitives in `src/components/ui/`. Domain/Apollo helpers live in `src/core/`; shared hooks, models, plugins, utilities, styles, and locales use their matching `src/` directories. Operations are under `graphql/`, static assets under `public/`, and current architecture under `docs/architecture.md`; historical plans are under `docs/archive/`. Backend schemas and services are at root `graphql/schema/`, `internal/`, and `pkg/`.

## Build, Test, and Development Commands

Run integrated workflows from the Git root:

- `make pre-ui-v3` installs locked v3 dependencies.
- `STASH_ENABLE_V3_UI=true make server-start` runs the Go backend on port 9999.
- `VITE_APP_PLATFORM_URL=http://127.0.0.1:9999 make ui-v3-start` runs Vite on port 3002.
- `make ui-v3-only` type-checks and builds the embedded v3 assets.
- `make validate-ui-v3` runs generation, Biome (including accessibility), TypeScript, formatting, locale checks, Vitest, and the pinned v2.5 compatibility check.
- `make lint` runs CI-pinned `golangci-lint` through `go run`; no local install is required.
- `make validate-fork` is the pre-push gate for backend generation, Go lint/integration tests, and v3 validation.
- From `ui/v3`, `pnpm test --run` runs Vitest once and `pnpm gqlgen` refreshes GraphQL types.

## Coding & Testing Conventions

Use strict TypeScript, functional components, two-space indentation, double quotes, and trailing commas; Biome is authoritative. Prefer `@/` imports, kebab-case files, PascalCase components, and `use`-prefixed hooks. Use TanStack Form with Zod, Apollo typed documents, Lucide icons, `cn()` for conditional Tailwind classes, and repository UI wrappers instead of bare interactive HTML elements. Wrap user-visible strings with `react-intl`; edit only `en-GB` locale source.

Co-locate Vitest files as `*.test.ts(x)` and cover regressions and user behavior. For Go integration changes, run focused root tests such as `go test ./ui` plus affected packages. Do not hand-edit generated GraphQL types, `src/routeTree.gen.ts`, or `settings-search-index.gen.ts`.

## Commits & Pull Requests

Use short imperative commit subjects. PRs must address an open issue, stay focused, follow `.github/pull_request_template.md`, describe manual testing, and include before/after screenshots for UI changes. Read `docs/CONTRIBUTING.md` and disclose AI assistance as required by `docs/AI_POLICY.md`.
