# v3 architecture

Current foundation, checked against the implementation on 2026-09-07.
Use the [development guide](development.md) for setup and validation and the
[documentation index](../../../docs/README.md) for feature and operations guides.
The [original plan](archive/rewrite-plan.md)
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
  Core cards/table links use the checked descriptors in `core/navigation.ts`
  (`to`, `params`, and `search`). Stored return URLs pass through
  `localNavigationHref` before the router's public `href` navigation API.
- `core/mutation-invalidation.ts` defines affected library query roots.
  `core/entity-job-invalidation.ts` refreshes after bulk jobs finish, including
  partial failures, and survives the edit sheet closing. The legacy `"sync"`
  acknowledgment causes an immediate refresh; only numeric job IDs are monitored.
  Query failures retry with bounded backoff; three consecutive failures dispose
  the watcher and refresh once. Unrelated configuration,
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
| `use-list-scroll-restoration.ts` | Apply TanStack's cached position once the active list's content is ready |
| `list-virtualizer-measurements.ts` | Bound and validate reusable row geometry for returning virtualized lists |
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
it may expose `loading`, `error`, and `refresh`. Local lists never send a GraphQL
list query. Remote list extraction receives complete generated operation data;
partial Apollo results are not asserted to be complete.

Layout preferences must not change query variables or flash loading states.
Only active embedded panels synchronize shared URL parameters. Selection stores
IDs and derives selected objects from current list data. `list-provider.tsx`
shares selection capabilities; `entity-list-items.ts` binds each item provider
and consumer to one generated entity projection. Set `ItemsProvider` in a list
configuration when its cards expose bulk menus. Consumers cannot choose a new
item type independently of their provider.

Wall cards render their selected and accessible state from React data. Each
card subtree is memoized on its own selected flag, so selecting one card does
not rebuild every card. Render callbacks must not read selection through refs.

Scene/image duplicate checkers derive committed criteria from route search
through `components/duplicates/controller.ts`. Their editing drafts remain
local to the filter editor; Back/Forward updates query variables and resets
selection without remounting the page. Shared grouping/selection helpers live
beside that controller.

### Returning to a list

TanStack Router owns scroll tracking and its session cache. The router and
`useElementScrollRestoration` share `core/scroll-restoration.ts`, which keys
positions by the full public URL, including filters, pagination, and deployment
prefix. The app's `useSmartBack` navigates to a saved return URL, so it restores
the same position as browser Back even though it creates a new history entry.
Revisiting the same URL within the session also restores its last position.

`EntityList` exposes a stable `data-scroll-restoration-id` scoped to the list's
`view` (or filter mode). Tables identify their nested scroll container separately.
The shared restoration hook retains the full URL belonging to its rendered route
match. The destination URL can change before the outgoing list unmounts; it must
not change that list's scroll-cache lookup or invalidate its row measurements.
Match pathnames ignore trailing slashes for ownership checks, while cache keys
retain the full URL. Embedded lists follow their owning match when entity params
change. The hook waits for active content to finish loading, including
the first-paint skeleton gate, then applies the cached offset once. Grid/details
virtualizers also receive that offset as `initialOffset`; restoring only the DOM
scroll position would let the virtualizer start at the top. Their
`initialMeasurementsCache` preserves measured row heights so the visible cards
return to the same place too. Geometry is kept for at most 20 recently updated
layouts in memory, keyed by list URL and layout preferences; changed widths or
ordered item IDs invalidate it. Loading virtual rows retain the full page height
without measuring skeletons as real cards. Do not duplicate scroll tracking or
restoration in individual routes.

The retained URL and pending restoration are React state snapshots. Adjust them
conditionally when their identity changes, following React's
[previous-render state pattern](https://react.dev/reference/react/useState#storing-information-from-previous-renders).
Keep DOM scroll writes in the layout effect. Do not replace these snapshots with
refs mutated during rendering: abandoned renders could otherwise revive a
consumed restoration and make a later refresh snap to an obsolete offset.

Keep each embedded list's `view` distinct. Filter data can catch up with the URL
after browser Back, so restoration tracks both identities. Ordinary pagination
still starts at the top, and deletion-refill preservation and removed-last-page
clamping continue to use `list-scroll-state.ts`.

When changing navigation or list layout, check browser Back/Forward and the app's
Back button with filtered, paginated URLs. Exercise grid, details, wall, and table
views with delayed data, including table horizontal scrolling. After restoration,
scrolling and ordinary rerenders must not reapply the saved offset.
Also check the outgoing list through a pending detail navigation and its view
transition: the visible cards must stay in place until the list unmounts.

## State, configuration, and type boundaries

React Strict Mode is enabled at the application root. Render snapshots belong
to state; conditional same-component state adjustment handles changed inputs.
`useCommittedRef` is for imperative listeners and cleanup that need the latest
committed value. Its identity is stable and its value changes in a layout effect.
Do not read it in render callbacks. Interrupted work must not publish values to
the committed UI. Form label caches and editable-row keys use React state.

`hooks/debounce.ts` wraps Lodash's typed debounce, including leading/trailing,
maximum wait, cancellation, and flush. Its result can be `undefined`; it does
not promise immediate execution. Timers cancel on replacement/unmount and
instant state updates cancel pending delayed writes. Persisted task defaults
explicitly opt into flushing on close and consume save failures already
reported by the shared save indicator.

`hooks/stored-state.ts` owns one subscribed localStorage snapshot per key.
`interface-preferences.ts` keeps the legacy `interface` key and synchronizes
mounted consumers and storage events. Updates use the current snapshot;
cross-tab conflicts follow localStorage's last-write-wins behavior. Storage
failure preserves in-page edits. Desktop sidebar visibility is persisted;
mobile sheets open transiently and start closed.

Zod codecs validate known UI configuration, task defaults, interface preferences,
and lightbox settings. The Apollo `ConfigResult.ui` boundary applies the UI
codec. Unknown v2.5/plugin fields survive round trips. Configuration writes
correlate each key with its generated value type; saving one plugin merges only
that plugin into the existing map.

Home Screen saved-filter IDs accept positive integer numbers and decimal
strings, both of which occur in existing v2.5/v3 configurations. Preserve the
stored representation and row order; convert IDs to GraphQL strings only when
querying. Regression tests exercise both formats and mixed layouts through the
actual Apollo cache, including configuration updates after unrelated edits.

The backend's `convertJSONNumbers` also walks arrays when converting GraphQL
JSON values before persistence. Newly submitted numeric IDs stay numeric through
YAML save/reload; existing string IDs remain strings. Both `configureUISetting`
and the map-based `configureUI`/`configurePlugin` paths share this conversion.
Scientific notation uses floating-point conversion rather than integer parsing.

Offline metadata, files, broadcasts, and locks share a deployment identity from
`offline-scope.ts`: the normalized backend mount URL, independent of v3 routes.
Use the storage adapters instead of constructing database names or file paths.
Legacy migration validates source rows and copies only entries with identifiable
ownership automatically. Explicit recovery handles ambiguous entries and prefix
changes while retaining originals. See the [offline storage contract](offline.md#storage-contract)
and [migration rules](offline.md#migration-and-recovery) before changing these
boundaries; the namespace identifies an address, not a server library UUID.

New filter AST URLs use versioned `u.` UTF-8/base64url JSON. The decoder also
accepts existing unversioned ASCII/Latin-1 AST URLs. Saved-filter and legacy
criteria formats remain supported. Invalid AST URLs fail visibly before a
query can accidentally become unfiltered.

Downloads consume the narrow `DownloadableScene` capability and use one snapshot
projection. Lightbox slides extend the library's public image/scene types.
Merge fields encapsulate their value type inside a resolver closure, sharing the
same calculation for the preview and mutation input.

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
- Temporary press-and-hold speed belongs to the current player. Release restores
  its previous rate while the media is attached. Scene or marker auto-advance can
  unmount the player mid-hold; cleanup must clear the gesture without sending
  playback commands to a detached store.

Root [CLAUDE.md](../../../CLAUDE.md) describes the backend HLS constraints.

## Entity editing

All seven single-entity edit sheets use `components/detail/entity-edit-sheet.tsx`.
Its fixed header provides the title. Close stays in the bottom action area on
mobile and in the header on desktop, including while data is loading or
unavailable. Inline detail editors use `detail-editor-layout.tsx` with the same
placement. Forms own their scrolling fields and
pinned action bars inside the remaining height. Close, Escape, and backdrop
dismissal leave without saving; Discard resets the form and keeps the pane open.
Successful saves close the sheet through the existing form callback.

## Settings navigation

`SettingsLayout` keeps the settings page mounted in its own scroller. Desktop
uses a sidebar with section links and inline search results. Mobile reserves one
56px bottom row for Navigation, the current section, and Search, replacing the
global bottom navigation bar. Its top header contains only the Settings title.
Section links open in an upward menu with 44px targets and bounded scrolling.
They remain TanStack Router links, so deep links and browser Back select the
correct section.

Search replaces the mobile row and focuses inside the opening touch handler.
It reuses the generated, locale-resolved settings index and navigates with the
existing `hl` parameter to reveal the chosen setting. Both layouts render results
as ordinary route links with native touch and keyboard activation. Mobile results
appear above the input, bounded to half the visible viewport, while the shared
visual-viewport hook lifts the search area above the keyboard. Closing search
restores its trigger's focus. Search state belongs to the navigation, so a
breakpoint change preserves the query without remounting the settings form.

## Mobile detail navigation

Collection and media detail layouts keep the entity title above the scroller
and navigation below it on mobile. `mobile-detail-chrome.tsx` provides a shared
56px toolbar with direct Navigation, section picker, Search, Filters, View options,
Entity actions, and Back controls. All targets stay at least 44px at 320px width;
the section label truncates and becomes an icon on the narrowest screens. Search
and selection replace that row and put Close at its right edge. Navigation,
Filters, View options, and Entity actions open bottom drawers with scrollable
content and dismiss through swipe-down, outside taps, or Escape. They omit Close
rows; the shared drawer primitive supplies bottom safe-area padding. The section
picker includes page navigation and a page-jump form. Previous/next controls also
appear at the end of list results, and single-page lists omit pagination. Standalone mobile lists use the same
row modes with navigation and a page picker. Desktop retains
its sidebar controls and tab strip. Collection pages use the `md` breakpoint;
media pages use `lg`, matching their existing split layouts.

The footer participates in flex layout, reserving its actual height without
fixed offsets or content overlays. It owns safe-area clearance and keyboard
lifting. React portals move controls into its typed slots while preserving
their tab, list, and action contexts. Only the active list publishes controls;
previously visited panels stay mounted with their filters and state intact.
The mobile picker uses the same Base UI tab state as desktop, with vertical
triggers in an upward popover, and brings a chosen section into view. The
section popover and action drawer keep their portal targets mounted so tabs
and action dialogs survive closing them. `entity-actions-menu.tsx` renders shared
typed action definitions as desktop dropdowns or direct mobile rows. Desktop
submenus become labelled, flat groups on mobile; invoking an action closes the
drawer before showing its form or confirmation. Search mounts and focuses
within the opening touch handler, and flushes any pending debounce on blur
before the row closes.
List controls own their prop contract; the parent bar extends it with view
settings. Page jumping uses TanStack Form with Zod validation and starts a new
draft when the page or page count changes. Collection and media tab panels both
publish their active state through `ListActivityContext`, so kept-mounted lists
cannot leave duplicate controls in the footer.
Tapping the current section also reveals it when the page is showing the
entity information or media above it. The focused scene viewer keeps its
existing player mounted and places Close below it on mobile.
Collection panels use their scroller's container height as a minimum so a
shorter list cannot clamp the viewport back into the entity information.

## Bulk custom fields

All seven entity bulk-edit sheets share `components/forms/bulk-custom-fields-field.tsx`.
Existing editable fields are the intersection of names across every affected
item, regardless of whether their values differ. Each shared field independently
supports Keep (preserve each item's value), Set (write one value to all), Clear
value (retain the field with an empty string), or Remove field (delete the key).
Fields defined on only some items stay untouched. New fields can be added to
every item, provided the name does not already exist on any affected item.

`use-bulk-custom-fields.ts` requests `bulkCustomFieldSummary` for selected IDs or
the complete matching filter. The fork-owned resolver reads one transaction,
ignores pagination, and batches custom-field reads. It returns shared and
partially shared names, without sending every item's values to the browser.
The editor waits for a fresh summary whenever opened or its scope changes;
changing the affected scope resets custom-field edits. Query failures offer
Retry while leaving unrelated metadata editable. As with other bulk edits,
the summary is a read snapshot; it does not lock records while the sheet is open.

`bulk-custom-fields.ts` owns the typed form value, Zod validation, and mutation
serialization. Draft rows belong to TanStack Form and update on every keystroke;
there is no separate pending row or blur-time commit. Validation blocks edits
to nonshared names and additions that collide with any existing target name.
It also blocks empty, untrimmed, oversized, and duplicate names, including
set/remove conflicts. Names use the server's 64-byte UTF-8 limit. Values use
the same decimal coercion as the single-entity editor; non-decimal text remains
text. Suggestions for additions use `customFieldNames`, excluding names
already present in the target.

`custom-field-name-input.tsx` keeps free text separate from selected suggestions.
It filters from the current form value using Base UI's locale-aware filter and
ignores the popup's internal query resets. When changing the editor, check
keyboard selection, freely entered names, and immediate reopening after Tab;
a popup's closing animation must not freeze suggestions or erase a draft name.

Bulk edits send only `CustomFieldsInput.partial` plus `remove`; Keep sends
nothing and Clear value sends an empty string. The bulk editor never sends
`full`, including when removing every shared field. Do not reuse the
single-entity editor's full-map replacement helper: it would erase unshared
fields. The optional
`custom_fields` inputs on bulk studio/tag updates extend the same existing API
contract as the other five entity types. No database migration or v2.5 client
change is needed.

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

On a clean checkout, install both UI dependency trees, run `make generate`, then
`make ui` and `make ui-v3-only` **before** `make validate-fork`. Go validation
inspects the embedded v3 route chunks, so generated placeholder directories are
not enough. The [development guide](development.md#validation) contains the full
command sequence; the [deployment runbook](../../../docs/v3-deployment.md) covers
the Stash publisher, stash-s6 bake, and local Quadlet verification.

The compatibility checker validates mainline operations, additive schema changes,
argument defaults, and the migration track. SQLite fixtures cover v3 close,
mainline writes, and v3 reopen. These checks do not replace testing real v2.5
clients or media gestures on physical iOS devices when changing those contracts.
