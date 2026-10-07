# v3 architecture

This guide describes the implemented frontend on `v3-rewrite`. The
[system overview](../../../docs/ARCHITECTURE.md) covers backend services, storage,
and request flows. Use the [development guide](development.md) for setup and
validation and the [documentation index](../../../docs/README.md) for feature
guides. The [original plan](archive/rewrite-plan.md) and
[early evaluation](archive/rewrite-plan-evaluation.md) are historical snapshots.

## Compatibility boundary

v3 uses React, TypeScript, TanStack Router, Apollo Client, shadcn components
built on Base UI, Tailwind, and Video.js. [package.json](../package.json) and
[pnpm-workspace.yaml](../pnpm-workspace.yaml) own versions and overrides.
The independent fork owns its native schema and API contracts. Compatibility
with upstream v2.5 is frozen at `v2.5-compatible-final`, which retains the
retired application source for migration reference. Convert required callers and validate retained
native invariants when changing contracts. The independent v3 plugin API and
stash-box integration remain supported. See [FORK.md](../../../FORK.md) and the
[transition plan](../../../docs/native-archive-transition-plan.md).

V3 is the sole embedded application. The old UI selector, dependency tree and
build are removed; media, share and ingestion routes use native behavior by
default. Production remains on the frozen compatible release until the full
migration and reviewed cutover are complete.

## Module map

Paths in this guide are relative to `ui/v3/src/` unless indicated otherwise.

| Area | Responsibility |
| --- | --- |
| [main.tsx](../src/main.tsx), [app.tsx](../src/app.tsx) | Main entry, shared providers, and startup gates |
| [router.tsx](../src/router.tsx), [routes/](../src/routes/) | File-based routes, search validation, route loaders, base path, and plugin route composition |
| [core/](../src/core/) | Shared Apollo transport/cache, typed navigation, configuration codecs, job monitoring, and mutation invalidation |
| [core/native-archive/](../src/core/native-archive/), [components/archive/](../src/components/archive/) | Native review contracts, account ownership workflow, and durable browser request recovery |
| [graphql/](../graphql/), [codegen.ts](../codegen.ts) | Authored operations/fragments and generated schema types / `TypedDocumentNode` operations |
| [components/list/](../src/components/list/), [models/list-filter/](../src/models/list-filter/) | Entity list composition, source contracts, filter AST/URL conversion, selection, and layouts |
| [components/detail/](../src/components/detail/), [components/forms/](../src/components/forms/) | Shared detail/editing shells and TanStack Form/Zod form contracts |
| [components/player/](../src/components/player/), [components/lightbox/](../src/components/lightbox/) | Stable media ownership, playback policy, and viewer sessions; see [player](player.md) |
| [components/layout/](../src/components/layout/) | Shell, navigation, footer slots, and visual transitions; see [interactions](interactions.md) |
| [components/ui/](../src/components/ui/), [styles/](../src/styles/) | Repository UI wrappers, semantic theme tokens, and global interaction policy |
| [hooks/](../src/hooks/), [locales/](../src/locales/) | Shared lifecycles/preferences and react-intl messages |
| [plugins/](../src/plugins/) | Timed plugin registration, registry, and explicit browser host capabilities |
| [components/offline/](../src/components/offline/), [pwa/](../src/pwa/) | Download queue, storage adapters, service worker, and standalone offline entry |
| [components/sharing/](../src/components/sharing/), [share-main.tsx](../src/share-main.tsx) | Share management and standalone guest presentation with a separate JSON contract |
| [components/tv/](../src/components/tv/) | TV feed/window/input ownership around the shared player; see [TV mode](tv-mode.md) |

Routes compose feature components; reusable viewers and editors belong under
`components/` so importing one does not import a route into the eager tree.
Shared UI wrappers own primitive behavior. Domain/query helpers belong in
`core/` and filter models, with public browser/plugin inputs narrowed at their
boundaries. Keep generated files out of manual edits: GraphQL output,
`routeTree.gen.ts`, and `settings-search-index.gen.ts` have separate generators
described in [development](development.md#generation-and-builds).

## Startup, routing, and data ownership

### Main application startup

1. `main.tsx` starts offline worker registration and installs page-zoom/chunk-error
   handling, then renders `App` in React Strict Mode.
2. [core/client.ts](../src/core/client.ts) lazily creates the shared Apollo and
   WebSocket clients. [create-client.ts](../src/core/create-client.ts) owns the
   normalized cache and splits HTTP queries/mutations/uploads from subscriptions.
   Main-app features reuse this client.
3. [SystemStatusGate](../src/components/system-status-gate.tsx) checks the server
   before configuration and plugin startup. Setup/migration screens load only when
   needed; failures expose retry actions.
4. [ConfigLoader](../src/components/config-loader.tsx) starts configuration and
   bounded plugin discovery together. Bundled locale messages load before
   optional server overrides.
5. [PluginLoader](../src/components/plugin-loader.tsx) waits for configuration
   and locale setup, then stages time-limited registrations. The UI export
   catalogue loads only for an enabled v3 plugin.
6. [createAppRouter](../src/router.tsx) combines file routes with completed plugin
   registrations. Route collisions are rejected; core routing remains available
   if plugin startup fails. See the [plugin host](plugin-host.md) for the contract.

The backend loads only `apiVersion: 3` manifests. Settings → Plugins separately
queries `pluginLoadErrorsV3` to show rejected manifests and refreshes these errors
after package changes or reloads. Rejected packages never enter browser discovery.

The offline and guest entries are independent of this sequence. The worker can
boot the standalone offline library without configuration, plugins, or GraphQL;
the guest viewer uses share-scoped JSON/media requests without an owner session.
Their data is adapted to reusable lists/viewers at narrow boundaries. See
[offline](offline.md) and [sharing](../../../docs/sharing.md).

### Routing and loading

[core/platform-url.ts](../src/core/platform-url.ts) derives the deployment prefix
from the server's base element. Use `getPlatformURL` for backend requests,
`applicationHref` for raw history writes, and `applicationPath` to convert a
public URL to a TanStack destination. Router destinations are relative to its
configured base path. Cards/table links use checked descriptors in
[core/navigation.ts](../src/core/navigation.ts) (`to`, `params`, `search`).
Stored return URLs pass through `localNavigationHref` before public `href`
navigation.

TanStack's Vite plugin splits route components. Editors use
[detail/deferred-overlays.ts](../src/components/detail/deferred-overlays.ts);
lightboxes use [deferred-lightboxes.tsx](../src/components/lightbox/deferred-lightboxes.tsx).
Most closed overlays load code on first use, but `main.tsx` deliberately warms
the scene lightbox and its shared player asynchronously on every launch.
Preloading does not mount the player or load scene media. After first opening,
deferred wrappers preserve component lifetime, drafts, and closing animations;
pending or failed downloads of feature code remain dismissible. Generic tables
use `lazyModule` to preserve item/column types across the lazy boundary.

Home mounts its first carousel immediately and uses `DeferredMount` to start
other rows when they approach its scroll viewport. Mounted rows retain their
cards, filters and lightbox state when scrolled away. The drawer preloads Home's
route code when its link becomes visible; the customisation sheet loads on first
use and queries saved filters only while open.
[front-page-state.ts](../src/components/frontpage/front-page-state.ts) retains
random seeds, mounted-row flags, and vertical/horizontal scroll positions for the current Home configuration and
Apollo client. Each carousel keeps all native snap targets but mounts cards
only near the visible horizontal range. Returning Home eagerly restores that
range and its saved row height, without rebuilding every offscreen card. Touch
cards omit hover-only tooltip roots and text measurement. Mobile detail action
drawers mount their toolbars on first use and retain their state after closing.
Returning Home renders its previously loaded rows immediately
from Apollo; random rows reshuffle on browser reload or configuration change.
No departed page DOM or duplicate entity data is retained. Loading rows reserve
their card type's cover geometry and typical metadata height.
Home's route loader warms only its configured saved-filter definitions so the
initial placeholders already know their card type; it does not fetch entity
rows or the complete saved-filter catalogue.

Scene and performer routes warm their typed Apollo detail query in the router
loader. Short loads retain the outgoing page and its navigation chrome, then
commit the destination with its essential data. After 600ms, a navigable pending
layout appears without an artificial minimum duration. Failures offer Retry and
Back. The loader returns no entity data, leaving Apollo as its sole cache.
Performer portraits contain the uncropped image in a stable frame during decode, and scene
refreshes retain the existing player, including when a refresh fails.

### Mutation and job refresh

- Apollo normalizes mutation results through the shared client. Scalar updates
  generally need no list refetch; `useEntityMutation` uses `core/mutation-invalidation.ts` for
  membership, relationship, and count changes. `removeEntitiesFromCache` removes
  deleted references immediately. It decrements a cached total only when that
  page proves all deleted IDs belonged to it; other totals are invalidated while
  retained rows remain usable. This includes independently fetched totals.
- `core/mutation-invalidation.ts` defines affected library query roots.
  `core/entity-job-invalidation.ts` refreshes after bulk jobs finish, including
  partial failures, and survives the edit sheet closing. The legacy `"sync"`
  acknowledgment causes an immediate refresh; only numeric job IDs are monitored.
  Query failures retry with bounded backoff; three consecutive failures dispose
  the watcher and refresh once. Unrelated configuration,
  plugin, status, and job queries are excluded from library refreshes.
- `core/monitor-job.ts` owns completion polling independently of mounted views.
  `core/scene-cover-job.ts` uses it to refresh only normalized scene artwork
  after screenshot or cover generation, including JPEG and HDR descriptors.
  Do not refetch scene details for artwork changes: freshly signed stream URLs
  can reload active playback. List queries observe the same normalized scenes
  without a list refetch or loss of scroll/edit state.
- `core/job-queue.ts` combines task lifecycle/progress subscriptions with
  authoritative HTTP snapshots. The mounted task list reconciles on foreground,
  restored pages, connectivity and WebSocket connections, plus every 30 seconds
  while visible. Resume restarts a potentially suspended socket. Missing active
  jobs are removed; known terminal outcomes retain only their original 10-second
  display window. Events received during a snapshot take precedence over it.
  Failed snapshots preserve the list, and disposal cancels requests and timers.

Motion belongs to empty reveal surfaces that preserve mounted media and
scrollers. See [interaction contracts](interactions.md#motion-and-visual-lifetime)
for route, local-view, card, lightbox, and mobile drawer behavior.

### Native archive review

`core/native-archive/client.ts` sends application-session JSON requests through
the deployment prefix, with Zod validation and cancellation. Scene/image metadata
review lives in detail tabs and mobile sections; the Account review route uses
the shared desktop utility menu and mobile drawer. The existing Apollo performer
picker resolves an explicit local selection to a native UUID/revision before a
read-only ownership preview. Applying requires the preview digest.

Account discovery uses bounded native keyset pages. Its selected-account editor
retains the loaded queue, updates only the affected card and uses the shared
`useListScrollRestoration` hook when returning. Identifier evidence and ownership
history load independently on expansion. Account ownership and depicted media
performers are separate relationships.

The Archive activity route shares desktop/mobile navigation and reads bounded
job/run summaries through `activity-api.ts`. Detail reads resolve safe domain
subjects and original collection revisions; histories load only on expansion.
Only the selected active item polls. Failed refreshes retain the last successful
same-scope result with a visible retry action; filter or item changes cannot
inherit stale data or errors. Successful attempts and completed source runs are
distinct. Existing domain review screens own mutation and recovery controls.

The Import history route uses `import-history-api.ts` to inspect catalog and
automation snapshots with bounded UUID pages and explicit selected details.
Snapshot receipt, per-step import progress and current association review are
separate states. Historical warning counts remain historical after a later
resolution; retained operations do not imply active jobs. Step counters and
technical identifiers use native expandable sections. The page shares the
activity reader's cancellation and failed-refresh behavior and the existing
list scroll restoration; switching import kind clears the previous scope.
Desktop and mobile navigation use the same route registry.

The Saved actions route uses `saved-actions.ts` to enumerate the current
installation's browser outboxes. `reviewKeys` uses bounded IndexedDB key cursors;
the owning protocol validates each selected record before a compact summary is
shown. Listing never sends a request or retries an action. Explicit Resume uses
the existing receipt/recovery method with its original bytes and identity.
Rejected and unreadable records stay distinct from uncertain submissions,
admitted jobs and retained file batches. Original-review links preserve creation
and media scopes; attachment and notification targets resolve through checked
read-only APIs on navigation. The route shares native controls, localization,
mobile/desktop navigation and same-scope failed-refresh behavior. These local
requests are not a cross-device unresolved-conflict queue.

The Review queue route uses `review-queue-api.ts` for current account ownership,
post/media association and retained metadata decisions. UUID pages carry a
checked count and may have an empty items array with a continuation cursor.
The UI preserves that distinction, links to existing targeted review screens
and retains its filter/cursor in the URL. It uses the shared activity reader,
scroll restoration, desktop/mobile navigation and expandable technical details.
Refresh recomputes current decisions; historical import warnings remain in
Import history and local pending requests remain in Saved actions.

Historical metadata review offers Apply and Keep current value. Both save an
exact request before transport and recover its original receipt after a lost
reply. Keep accepts unresolved names and unsupported historical fields without
choosing a replacement. Its receipt refreshes review state without refetching
unchanged library metadata, and its success message remains distinct from Apply.
The request key includes the Keep outcome while existing Apply keys and bodies
stay unchanged. A pending choice cannot be silently replaced by the other action.

The Source posts route provides bounded browsing and exact URL, qualified source
ID or archive UUID lookup. It is available in the desktop utility menu, mobile
drawer and scene/image source cards. Deep links load only the selected post.
Captures, publishers, media associations, album choices and identifiers expand
independently. Shared post text is grouped by revision; rejected links and
deleted library identities remain inspectable. Inspection is read-only;
account/media links lead to their existing review workflows, while explicit
album matching controls preview and queue reviewed changes.

Source order on post pages and Source albums on gallery pages share
`source-album-api.ts` and the components in `components/archive/albums`.
Mixed images/videos retain source positions and repeated attachments; unknown
ranges stay compact and visible. Attachment decisions, post-link suppression,
gallery membership and registered file counts are separate facts. Gallery
lookups follow retained redirects without combining source posts. Reads are
lazy and paginated; a changed signature requires a reload. Deleted identities
never link to reused local IDs. The shared `posts/library-link.tsx` renders
ordinary route links with button styling.

`album-review-api.ts` and `album-review-outbox.ts` expose the existing durable
album matching service through `albums/review.tsx`. The policy form previews
bounded candidates and member changes before Apply. Exact requests persist in
IndexedDB before any mutation; reopening reads saved state, and recovery checks
the original receipt explicitly. Cancellation uses current job state to recover
an uncertain response. Unverified receipts and ambiguous conflicts cannot be
discarded as rejected. Job status distinguishes committed gallery changes from
pending plugin notifications, with guarded retry/cancel and independently loaded
history/attempts. Only visible active jobs poll. A publication refreshes its
source-order view and the enclosing post/gallery association without remounting
the review controls or issuing a whole-library query.

`attachment-selection-api.ts` and `attachment-selection-outbox.ts` provide
source-list selection through `albums/selection-review.tsx`. Distinct immutable
lists have bounded pages regardless of repeated captures. A typed form compares
current and proposed source positions before selecting a pinned list, allowing
compatible future updates, or disabling selection. Repeated positions and gaps
remain visible; source evidence is separate from download state and gallery
membership. Lists, history and large reference sets expand independently through
the shared Base UI wrappers.

The deployment-scoped IndexedDB journal saves the exact request before Apply and
serializes competing tabs. Opening reads only; explicit recovery verifies the
original receipt before replaying an absent request. Only a proven stale preview
can be discarded for another review. A saved choice refreshes that post's source
order without changing or reloading gallery associations. Refresh failure is
reported separately from successful publication. Closing the panel cannot cancel
a request already saved for delivery.

Performer detail pages add a Source accounts tab/mobile section backed by
`performer-source-api.ts`. It resolves the local performer once per account page
to a native identity and follows recorded UUID redirects on the server. Existing
links open their selected account review; no name-based ownership is inferred.
Identity history loads separately on expansion and treats old local IDs as
historical values. If the canonical performer changes between pages, the view
requires a fresh read instead of combining incompatible results.

The selected account also offers explicit consolidation of duplicate records on
one service. `account-consolidation-api.ts` validates the reviewed components,
resulting owner, stable-ID conflicts and exact event receipt. The picker scopes
search to the selected namespace and supports a direct UUID lookup. Conflicting
owners require a choice; stable-ID disagreements require acknowledgement. The
existing component digest and explicit performer revision guard application.
Consolidation refreshes the source/destination cards and retains the queue cursor;
redirected source cards disappear from canonical discovery. History and retained
identifier evidence load separately.

Review outboxes use `review-storage.ts` for strict IndexedDB transactions shared
across tabs. Each protocol retains its own endpoint/target scope and request
validation. Save before sending; recover a matching server receipt or retry the
exact original body after interruption. Only a definitive rejection permits
replacing a saved request. Committed choices and their history live in the native
database. See [native review APIs](../../../docs/native-ingestion.md#account-ownership-review-api).
Consolidation recovery POSTs the saved body to a read-only receipt-check route,
which matches the existing event digest without adding a receipt table. Pending
consolidation blocks replacement ownership edits in the selected editor, and
stale-review recovery disables inputs before refreshing/remounting the form.

## Lists

`components/list/entity-list-page.tsx` composes list chrome, selection, sidebar,
error state, and the selected layout. Add a configuration for a new entity rather
than copying a page implementation.

| Module | Responsibility |
| --- | --- |
| [entity-list-types.ts](../src/components/list/entity-list-types.ts) | Configuration and discriminated `source` contract |
| [use-list-data.ts](../src/components/list/use-list-data.ts) | GraphQL/local dispatch, independent totals, normalized query state |
| [use-cached-query-result.ts](../src/components/list/use-cached-query-result.ts) | Preserve usable data on failed refreshes without treating another filter's data as a successful result |
| [use-list-page-filter.ts](../src/components/list/use-list-page-filter.ts) | Layout preferences and filter/URL synchronization |
| [use-filter-state.ts](../src/components/list/use-filter-state.ts) | Parse and persist filters, including per-view defaults |
| [use-list-page-refill.ts](../src/components/list/use-list-page-refill.ts) | Refill shortened remote pages while preserving scroll |
| [use-list-scroll-restoration.ts](../src/components/list/use-list-scroll-restoration.ts) | Apply TanStack's cached position once the active list's content is ready |
| [list-virtualizer-measurements.ts](../src/components/list/list-virtualizer-measurements.ts) | Bound and validate reusable row geometry for returning virtualized lists |
| [use-list-select.ts](../src/components/list/use-list-select.ts) | Selection and stable selection accessors |
| [virtualized-item-list.tsx](../src/components/list/virtualized-item-list.tsx) | Grid/details row virtualization and skeletons |
| [photo-album-wall.tsx](../src/components/list/photo-album-wall.tsx) | Justified wall layout and selection synchronization |
| [entity-data-table.tsx](../src/components/list/entity-data-table.tsx) | Table layout and column preferences |
| [entity-list-configs.tsx](../src/components/list/entity-list-configs.tsx) | Reusable per-entity configurations |

Choose exactly one source, defined in
[entity-list-types.ts](../src/components/list/entity-list-types.ts):

| Source | Required contract |
| --- | --- |
| GraphQL, combined total | `kind: "graphql"`, typed `query`, `makeVariables(filter)`, and `extractResult(data)` returning `{ count, items }` |
| GraphQL, independent total | Same typed query/variables contract plus `countQuery` returning `{ result: { count } }`; `extractResult(data)` returns only `{ items }` |
| Local | `kind: "local"`, raw `items`, and `filter(items, filterModel)` returning the page slice and total; optional `loading`, `error`, and `refresh` |

Carry the generated variables type as the third `EntityListPageConfig` type
parameter; the page and count documents use that same type. Scene/image lists
use independent totals, so cards render before counts arrive. An unknown count
is `undefined`, not zero; count-dependent pagination, clamping, and bulk actions
wait for it. Both queries participate in refresh/error handling. Ordinary list
and Home queries omit unused duration/filesize aggregates; the backend retains
those fields for other callers. See [read performance](../../../docs/read-performance.md).

`extractResult` receives complete generated operation data or `undefined`;
it must handle the initial empty state. Partial Apollo results are not asserted
to be complete. Local sources skip both GraphQL queries. SearchInput debounces
typing before committing filter state; `useListData` adds no further delay, so
Enter, clearing, and page changes dispatch immediately.

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

Embedded list defaults are scoped by `View` (for example `performer_scenes`
and `performer_images`). Only list-owned URL parameters override them;
`tab`, `returnTo`, and other page parameters do not constitute a filter.
Returning to a tab without explicit filter parameters restores that view's
saved default without serializing it into the shared page URL.

### Returning to a list

TanStack Router owns scroll tracking and its session cache. The router and
`useElementScrollRestoration` share `core/scroll-restoration.ts`, which keys
positions by the full public URL, including filters, pagination, and deployment
prefix. The app's `useSmartBack` navigates to a saved return URL, so it restores
the same position as browser Back even though it creates a new history entry.
Revisiting the same URL within the session also restores its last position.
Explicit `returnTo` state takes priority; links without it return to the last
Home or list page visited by the current router. Home must replace an older
list origin when opening performer-name links from a carousel. The fallback is
scoped to the router so a recreated app cannot reuse an earlier session's origin.

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

On mobile collection detail pages, `EmbeddedListScrollContext` supplies the
outer page scroller to the active list. Its grid/details virtualizer measures
the list's offset below the profile header as `scrollMargin`, including header
resizes, and renders only viewport rows plus overscan. The list's content-sized
inner wrapper must not become the virtualizer's viewport: doing so mounts and
fetches the entire page of cards. Desktop lists keep their bounded inner
scroller. Embedded pagination returns to the list start below the header.
Filter URL updates use `router.navigate` with `resetScroll: false` so the
router cannot subsequently restore the previous page's offset over that reset.
Virtual row measurements also require the observed offset to match the current
DOM offset before adjusting scroll: a programmatic reset can precede its scroll
event, especially in WebKit.

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

| State | Owner |
| --- | --- |
| Server entities and query results | Shared Apollo cache; route loaders warm it rather than returning duplicate entity data |
| Shareable filters, pagination, selected detail tab | Validated TanStack search state plus the shared list/filter models |
| Selection, open overlays, gestures | The owning list/viewer/component session |
| Form drafts and validation | TanStack Form with Zod; editors serialize mutations at submission |
| Server UI defaults and configuration | Typed configuration hooks/codecs and server mutations |
| Device preferences | Subscribed localStorage snapshots in `hooks/stored-state.ts` |
| Downloaded files, queue, and local resume | Deployment-scoped IndexedDB/OPFS adapters; see [offline](offline.md#storage-contract) |
| Active media/playhead | Video.js store/native media; transition hooks carry pending resumes |

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

[ScenePlayer](../src/components/player/scene-player.tsx) owns the stable player
root, store, and native video. Source selection, pure transition decisions,
browser recovery, and transcode leases remain separate modules. Scene detail,
lightboxes, and TV reuse this shell; offline and guest viewers adapt their
sources and disable owner activity where appropriate.

The [player guide](player.md) owns the module map, media lifetime, clip/seek
invariants, recovery, platform integrations, and backend HLS boundary.
[TV mode](tv-mode.md) and [offline downloads](offline.md) describe their distinct
feed and storage lifecycles.

## Dialog dismissal

Form/confirmation dialogs provide Cancel alongside their primary action and
disable the duplicate corner close button. Informational dialogs retain one
Close action. Focus capture/restoration belongs to the dialog primitive; use
`DialogContent.initialFocus` for a specific initial field.
See [dialog contracts](interactions.md#dialog-dismissal).

## Entity editing

Single-entity sheets share `detail/entity-edit-sheet.tsx`; forms own their
scrolling fields and pinned actions. Close leaves without saving, while Discard
resets the draft and leaves the editor open.
See [entity editing](interactions.md#entity-editing).

## Settings navigation

Settings share one navigation/search state across desktop and mobile layouts.
The generated settings index resolves localized results and the `hl` route
parameter reveals the target setting.
See [settings navigation](interactions.md#settings-navigation).

## Mobile detail navigation

Shared footer slots host controls from the active list or tab while retaining
visited panels and their state. The shell, footer, and keyboard-layout hook
coordinate safe areas and scrolling.
See [mobile navigation](interactions.md#mobile-detail-navigation) for toolbar,
search, drawer, portal, and focus contracts.

## Bulk custom fields

Each bulk sheet uses one `bulk*Update` document with `BulkUpdateResultData`.
`COMPLETED` includes the committed IDs and refreshes affected library queries.
`QUEUED` includes the selected count and job ID, closes the sheet with an
admission message and leaves completion monitoring owned by the Apollo client.
Failed/cancelled jobs still refresh, since earlier items may have committed.
Mutation aliases are resolved to their response keys. Submission failures or
contradictory acknowledgments show an error and keep the form open. The removed
`*UpdateJob` documents and scalar `sync` convention are no longer part of these
edits. See the [native bulk API](../../../docs/native-bulk-updates.md).

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

Paths in this section are relative to the repository root. The
[system overview](../../../docs/ARCHITECTURE.md#backend-responsibilities) maps
the shared backend layers.

`internal/api/resolver_mutation_bulk_*.go` owns fork bulk-job resolvers and their
per-entity operations. Shared resolvers retain v2.5 synchronous adapters with
explicit IDs. `internal/api/bulk_update.go` holds common selection/enqueue
helpers. Background jobs commit each successful item atomically, report failures,
and execute enabled post-hooks after successful writes.

Saved/default filters use canonical ASTs in the database and API. The old flat
saved-filter projection, config-backed default API and manual migration task are
removed. Historical database/configuration and JSON imports convert once before
native use. `configureDefaultFilter` updates one view atomically and resolves
imported conflicts against the reviewed revision. Do not replace the entire UI
configuration to change one default. Complex conflicts retain both versions for
user review; native saves preserve nested groups and repeated criteria.

`internal/api/job_subscription.go` owns cancel-aware forwarding. Received
lifecycle events are forwarded without dropping; progress can be dropped under
backpressure, and cancellation releases blocked sends. Job-manager buffers and
disconnections can still lose events, so the task UI reconciles with HTTP
snapshots. See the [backend job flow](../../../docs/ARCHITECTURE.md#editing-and-background-jobs).

## Interaction and accessibility

Detailed motion, mobile layout, and dismissal contracts live in
[interactions.md](interactions.md).

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
Vitest, and current native/plugin contract checks. `make validate-fork` adds backend
generation, Go lint, and integration tests. `make ui-v3-only` verifies the bundle.

On a clean checkout, run `make pre-ui`, `make generate`, then `make ui`
**before** `make validate-fork`. Go validation
inspects the embedded v3 route chunks, so generated placeholder directories are
not enough. The [development guide](development.md#validation) contains the full
command sequence; the [deployment runbook](../../../docs/v3-deployment.md) covers
the Stash publisher, stash-s6 bake, and local Quadlet verification.

The contract checker validates current application operations and the retained
versioned plugin API. SQLite fixtures cover one-time historical imports, native
writes, restart recovery and reconciliation. Physical iOS checks remain necessary
for changes to media gestures or playback behavior.
