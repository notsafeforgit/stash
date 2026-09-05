# v3 architecture

Current foundation as of 2026-09-05. The [original plan](archive/rewrite-plan.md)
and [early evaluation](archive/rewrite-plan-evaluation.md) are historical snapshots.

## Compatibility boundary

v3 uses React 19, TypeScript, TanStack Router, Apollo Client 4, shadcn components
built on Base UI, Tailwind 4, and Video.js 10. It shares the Go backend with v2.5
and is enabled with `--enable-v3-ui` or `STASH_ENABLE_V3_UI=true`.

v3 route paths may evolve independently. Existing v2.5 clients must retain their
GraphQL operations, response shapes, mutation semantics, and compatible database
representation. Treat `ui/v2.5/` as a read-only reference. Keep GraphQL changes
additive and store fork data in sidecars and idempotent reconcilers, leaving
upstream's numeric migrations and primary schema version unchanged. See
[FORK.md](../../../FORK.md).

## Startup, routing, and data ownership

- `core/client.ts` owns the shared lazy Apollo/WebSocket client. Do not create
  another client for a feature.
- Configuration and system-status gates expose errors and retry actions. Bundled
  locale messages load before optional server overrides.
- Plugin registration is staged and time limited. Only completed registrations
  enter the router; core routes remain available if plugin startup fails. Route
  collisions are rejected explicitly.
- `core/platform-url.ts` derives the deployment prefix from the server's base
  element. Use `getPlatformURL` for backend requests, `applicationHref` for raw
  history writes, and `applicationPath` to convert a public URL to a TanStack
  destination. Router destinations are relative to its configured base path.
- `core/mutation-invalidation.ts` defines affected library query roots.
  `core/entity-job-invalidation.ts` refreshes after bulk jobs finish, including
  partial failures, and survives the edit sheet closing. Unrelated configuration,
  plugin, status, and job queries are excluded from library refreshes.

## Lists

`components/list/entity-list-page.tsx` composes list chrome, selection, sidebar,
error state, and the selected layout. Add a configuration for a new entity rather
than copying a page implementation.

| Module | Responsibility |
| --- | --- |
| `entity-list-types.ts` | Configuration and discriminated `source` contract |
| `use-list-data.ts` | Debounce, GraphQL/local dispatch, normalized query state |
| `use-cached-query-result.ts` | Preserve usable data on failed refreshes without treating another filter's data as a successful result |
| `use-list-page-filter.ts` | Layout preferences and filter/URL synchronization |
| `use-filter-state.ts` | Parse and persist filters, including per-view defaults |
| `use-list-page-refill.ts` | Refill shortened remote pages while preserving scroll |
| `use-list-select.ts` | Selection and stable selection accessors |
| `virtualized-item-list.tsx` | Grid/details row virtualization and skeletons |
| `photo-album-wall.tsx` | Justified wall layout and selection synchronization |
| `entity-data-table.tsx` | Table layout and column preferences |
| `entity-list-configs.tsx` | Reusable per-entity configurations |

Choose exactly one source. A GraphQL source requires `kind: "graphql"`, a typed
`query`, `makeVariables(filter)`, and `extractResult(data)` returning
`{ count, items }`. Carry the generated variables type as the third
`EntityListPageConfig` type parameter. A local source requires `kind: "local"`,
raw `items`, and `filter(items, filterModel)` returning the page slice and total;
it may expose `loading`. Local lists never send a GraphQL list query.

Layout preferences must not change query variables or flash loading states.
Only active embedded panels synchronize shared URL parameters. Wall selection
is synchronized in the DOM for performance, including its accessible state;
preserve both when changing that renderer.

## Player

`components/player/scene-player.tsx` owns the stable player shell.
`use-scene-player-sources.tsx` coordinates selected sources and pending resumes.

| Module | Responsibility |
| --- | --- |
| `scene-player-sources.ts` | Source eligibility, quality preferences, initial resume |
| `scene-player-transitions.ts` | Pure seek/restart decisions and resume plans |
| `scene-player-source-url.ts` | Stream URLs, clip bounds, fragments, reload nonce |
| `hls.ts` | HLS timeline policy and engine helpers |
| `use-player-transition-feedback.tsx` | Freeze frame, loading feedback, seek readiness |
| `use-player-transcode-session.ts` | Release transcodes and keep paused sessions alive |
| `use-player-recovery.ts` | Native fullscreen seeking and stalled-playback recovery |

Transition plans consume plain buffered/seekable state and return an in-place
seek, engine restart, or source reload. Browser effects apply the plan; keep DOM
operations out of the planner so it remains testable without a media element.

Preserve these invariants:

- Scene time is absolute. A clip's media time is relative to its segment-aligned
  origin; convert only at the media seek boundary.
- The player root survives source changes. Retain the playhead, paused state,
  and playback rate through the pending-resume path.
- Buffered seeks stay in place. Distant desktop HLS seeks can flush the engine;
  iOS ManagedMediaSource and clipped playlists use source reloads.
- Every forced reload changes the URL, even a repeated target at zero.
- Clip URL bounds stay fixed through quality changes so earlier portions remain
  reachable. Retain freeze-frame masking and native fullscreen behavior.

Root [CLAUDE.md](../../../CLAUDE.md) describes the backend HLS constraints.

## Backend extension points

`internal/api/resolver_mutation_bulk_*.go` owns fork bulk-job resolvers and their
per-entity operations. Shared resolvers retain v2.5 synchronous adapters with
explicit IDs. `internal/api/bulk_update.go` holds common selection/enqueue
helpers. Background jobs commit each successful item atomically, report failures,
and execute enabled post-hooks after successful writes.

Saved/default filters retain a v2.5 projection alongside canonical v3 state.
`configureDefaultFilter` updates one view atomically on the server and resolves
legacy conflicts against current state. Do not replace the entire UI configuration
to change one default. Complex conflicts preserve both versions for user review.

`internal/api/job_subscription.go` owns cancel-aware forwarding. Lifecycle events
remain lossless, progress can be dropped under backpressure, and cancellation
releases blocked sends.

## Interaction and accessibility

Use repository controls, descriptive action names, native keyboard activation,
and labels linked to unique control IDs. Cards expose navigation, preview, and
selection separately. Do not nest interactive controls. Preserve focus across
dialogs and viewers.

Biome's recommended accessibility rules are enabled. Narrow suppressions explain
render props, layout wrappers, and pointer event delegation at their call sites.
Decorative previews do not require invented captions; media controls retain
keyboard actions.

General page pinch zoom and double-tap zoom remain disabled by product choice.
Keep `index.html`, `lib/prevent-page-pinch-zoom.ts`, and the global touch policy
consistent. Custom image/video zoom is independent and remains available.

Interface text is non-selectable by default. Inputs, textareas, editable regions,
and technical `code`/`pre` output retain selection and the touch copy menu.
Use `data-selectable-text` on other copyable values (paths, URLs, hashes, IDs,
logs, diagnostic messages), not their labels or surrounding controls.
`MetaRow` and `textColumn` expose `selectableText` for this purpose;
`SettingDisplay` values are copyable readouts. Keep this shared policy in
`styles/globals.css` instead of adding selection overrides to whole panels.

## Validation

`make validate-ui-v3` runs generation, lint, TypeScript, formatting, locale checks,
Vitest, and the pinned v2.5 compatibility check. `make validate-fork` adds backend
generation, Go lint, and integration tests. `make ui-v3-only` verifies the bundle.

The compatibility checker validates mainline operations, additive schema changes,
argument defaults, and the migration track. SQLite fixtures cover v3 close,
mainline writes, and v3 reopen. These checks do not replace testing real v2.5
clients or media gestures on physical iOS devices when changing those contracts.
