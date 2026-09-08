# v3 quality audit — 2026-09-08

**Status:** items **1–12 implemented** after approval, including deployment
isolation and recovery for offline downloads. The scroll-restoration cleanup is
also complete. The separate compiler and download-resume limits are recorded
below.

The original review used `v3-rewrite` at `911acc9fa670`, including the local
restoration fix. It combined a static inventory of 529 production TypeScript
files (102,754 lines, excluding tests and generated TypeScript) with focused
manual review of routing, lists, filters, settings, cards, players, offline
storage, plugins, and supporting bulk-job/API code. It was not a line-by-line
review of every file or a security audit of the entire Go backend. The original
12 diagnostic checks established the failures summarized below; repository
regression tests now assert the corrected behavior.

Current design belongs in [architecture.md](architecture.md),
[development.md](development.md), and [offline.md](offline.md). This document
records the findings, approved scope, implementation decisions, and validation.

## Completed: scroll-restoration lifecycle

[The restoration hook](../src/components/list/use-list-scroll-restoration.ts)
keeps its owning URL and pending restoration in React state. Scroll writes
remain in a layout effect. Conditional state adjustment follows React's
[documented previous-render state pattern](https://react.dev/reference/react/useState#storing-information-from-previous-renders).
There are no render-time ref mutations, type assertions, or non-null assertions
in this hook.

The outgoing list retains its own geometry while the incoming detail route
loads. Abandoned renders cannot re-arm a consumed restoration. The latter was
reproduced against the previous ref implementation: after the user scrolled to
420, an abandoned filter render followed by a refresh restored the obsolete
offset 950.

[Five integration tests](../src/components/list/use-list-scroll-restoration.test.tsx)
exercise real TanStack routing/restoration under Strict Mode, delayed data,
query/parameter changes, suspended renders, and superseded navigation. The
[architecture guide](architecture.md#returning-to-a-list) explains ownership.

## Approval scopes and results

| # | Original priority | Scope | Result |
| --- | --- | --- | --- |
| 1 | High | Unicode-safe filter URLs | Implemented, legacy decoding retained |
| 2 | High | Honest debounce types and lifecycle | Implemented |
| 3 | High | Shared reactive preferences | Implemented, existing keys retained |
| 4 | High | Validated configuration boundaries | Implemented, extension fields retained |
| 5 | High | Selection IDs and typed list actions | Implemented |
| 6 | High | Offline ownership and cancellation | Implemented with Web Locks and broadcasts |
| 7 | Medium | Stable offline metadata refresh | Implemented |
| 8 | Medium | Offline deployment identity | Implemented with copy migration and explicit recovery |
| 9 | Medium | URL-owned duplicate-checker filters | Implemented |
| 10 | Medium | React render purity | Shared lifecycle code updated |
| 11 | Medium | Typed navigation, media, merges, and job adapters | Implemented |
| 12 | Foundation | Compiler and lifecycle enforcement | Implemented; exact optional properties deferred |

### 1. Unicode-safe filter URLs

**Finding:** `btoa(JSON.stringify(...))` threw for criteria containing text such
as `日本語 🎬`. JSON serialization does not guarantee ASCII; see
[the byte-string contract for btoa](https://developer.mozilla.org/en-US/docs/Web/API/Window/btoa#unicode_strings).

**Resolution:** [url-json.ts](../src/utils/url-json.ts) encodes UTF-8 bytes as
base64url with an explicit `u.` prefix. Unversioned URLs retain the original
ASCII/Latin-1 interpretation, including ambiguous text that must not be silently
reinterpreted as UTF-8. Malformed AST input fails closed through the existing
filter-error path. Legacy criteria and saved-filter representations remain
supported. Codec and real-model tests cover Unicode, emoji, Latin-1, malformed
input, and saved-filter round trips; duplicate-controller tests cover history.

### 2. A debounce contract that matches execution

**Finding:** the hook promised an immediate return value, ignored its declared
options, left replaced/unmounted timers alive, and allowed delayed state to
overwrite a later instant update.

**Resolution:** [debounce.ts](../src/hooks/debounce.ts) uses the typed Lodash ES
implementation for leading, trailing, and maximum-wait behavior. The callable
returns the latest result or `undefined`. Replaced and unmounted timers are
cancelled, with an explicit `flushOnUnmount` option for task-default persistence.
Instant updates cancel pending writes. Callbacks become visible at commit time;
async task-save failures have a rejection handler and retain save-indicator
reporting. Tests cover these timing, return-type, replacement, and flush rules.

### 3. One reactive preference store

**Finding:** separate `useState` wrappers around the same localStorage key lost
updates made by another mounted consumer.

**Resolution:** [stored-state.ts](../src/hooks/stored-state.ts) supplies one
validated store per key using `useSyncExternalStore`. Functional updates use the
current shared snapshot; storage events synchronize other tabs. Cross-tab
conflicts remain last-write-wins, rather than implying transactional writes.
Storage failures preserve usable in-memory state. Interface preferences retain
the `interface` key, including unknown fields. Desktop sidebars follow shared
preferences; mobile sheet opening remains transient. The misleading
`local-forage.ts` wrapper was removed. Tests cover concurrent consumers,
storage events, and failed persistence.

### 4. Validate data before calling it configuration

**Finding:** arbitrary parsed JSON was asserted to be valid settings; invalid
front-page content could reach array operations and crash. Configuration cache
keys and values were not correlated.

**Resolution:** Zod validates known UI configuration at the Apollo JSON-scalar
boundary in [create-client.ts](../src/core/create-client.ts), using
[config-schema.ts](../src/core/config-schema.ts). Interface preferences,
lightbox settings, and task defaults have their own boundary schemas. Invalid
fields receive valid defaults or honest optional values. Loose object schemas
retain unknown v2.5/plugin fields; task-default keys are checked against the
generated mutation inputs. Invalid clean-task dry-run settings default to the
safe dry-run value.

`writeConfigKey` correlates the selected key with its generated value type.
Saving one plugin now merges that plugin's returned settings into the cached
plugin map instead of replacing the entire map. Tests cover malformed and
partial settings, ranges/enums, extension preservation, and negative type checks.

### 5. Selection identity and typed list actions

**Finding:** selection retained stale query objects with matching IDs; a generic
context allowed consumers to assert an unrelated entity type.

**Resolution:** [use-list-select.ts](../src/components/list/use-list-select.ts)
stores IDs and derives objects from current typed list data. Pruning no longer
nests a state update inside another updater. Common list context exposes only
shared capabilities; [entity-list-items.ts](../src/components/list/entity-list-items.ts)
binds providers and consumers to concrete generated entity types. Bulk menus
capture selected IDs and read fresh objects. Wall cards receive selection
through React, with memoized unchanged card subtrees. Tests cover refreshed
objects, pruning, pagination, range/inverse selection, interrupted rendering,
and rejection of image data at a scene provider.

### 6. Offline worker ownership and cancellation

**Finding:** a second tab could mark another tab's live download as interrupted.
Cancellation during initialization could miss an as-yet-uncreated controller,
and deletion did not wait for the writer to settle.

**Resolution:** [use-download-queue.ts](../src/components/offline/use-download-queue.ts)
uses a named Web Lock for the serial worker and per-scene locks for commands
and writers. IndexedDB remains the durable queue; BroadcastChannel publishes
changes across tabs. Recovery happens only after acquiring worker ownership.
Each attempt has a request ID; durable cancellation targets that attempt and
deletion waits for its writer and pending progress checkpoints. Abort ownership
exists before initialization awaits. Clear-all requests cancellation and waits
for the worker lock before removing files and metadata.

Browsers without Web Locks or BroadcastChannel cannot mutate the offline
library; existing downloads remain playable. This fallback prevents an
uncoordinated second writer. Retry preserves existing partial-file behavior,
checks the returned range start before appending, and restarts from zero when
the server sends a full response. It does not add ETag/source-version validation.
Read subscriptions reject stale asynchronous results and surface recoverable
storage errors.

Tests cover competing stores, duplicate enqueue, initialization cancellation,
writer settlement, queued cancellation, quota/API failures, range responses,
clear-all, stale reads, and retry after a read failure. Item 8 gives these locks
and stores one shared deployment identity.

### 7. Offline metadata refresh must survive progress events

**Finding:** a progress update cancelled the current metadata request, then
unchanged ID membership prevented replacement. Strict Mode exposed the same gap.

**Resolution:** [offline-metadata-refresh.ts](../src/components/offline/offline-metadata-refresh.ts)
keys request ownership to sorted ID membership, reads current entry data at
commit time, and ignores obsolete results. Progress events do not restart the
query. Online events retry it. Only requested valid IDs are queried, and an
empty ID set never becomes an unfiltered request. A successful response is
required before marking a scene missing. Tests cover Strict Mode, progress,
superseded responses, online retry, and invalid IDs.

### 8. Offline deployment identity and recovery

**Finding:** the fixed database `stash-offline` and OPFS path
`scenes/<id>.mp4` were shared by all installations on one browser origin.
`https://example.test/stash-a/` and `https://example.test/stash-b/` could use the
same scene ID for different media, colliding in local storage or sending queued
work to the wrong server prefix.

**Resolution:** [offline-scope.ts](../src/components/offline/offline-scope.ts)
derives database, directory, channel, and lock names from the normalized backend
mount URL. This is independent of v3 route paths. Database names retain the URL
for recovery; OPFS directory names use its SHA-256 digest to bound their length.
Each deployment has its own serial worker and scene files.

[offline-migration.ts](../src/components/offline/offline-migration.ts) copies
validated legacy entries automatically only when their saved artwork URLs
consistently identify the current deployment. Ambiguous data and previous
prefixes are offered through **Restore saved downloads** on the Offline page
and in settings. The user selects scenes and confirms ownership. Original
metadata and files are retained; existing destination rows win collisions.
Recovery preserves the playhead and rebases recognized asset URLs after a
prefix change. Browsers without database enumeration support local inspection
by previous address.

Imports hold source/destination locks, stream bytes, recheck the source, and
atomically commit metadata with an import receipt. Receipts survive deletion;
clear-all disables automatic reimport without clearing another deployment or
the original source. Quota failures and cancellation clean partial destination
files and leave recovery retryable. Incomplete entries require explicit retry;
live legacy downloads are excluded from automatic migration. Browsers without
coordination retain read-only playback of identifiable legacy entries.

[Migration regression tests](../src/components/offline/offline-migration.test.ts)
cover normalization, ownership, corrupt metadata, collisions, durable receipts,
concurrent imports, quota failure, cancellation, changing/busy sources, prefix
recovery, missing databases, and the read-only fallback. The
[offline guide](offline.md#migration-and-recovery) documents recovery and its
limits: copies require extra space, other origins/profiles cannot be inspected,
and replacing a library at the same address requires a separate identity policy.

### 9. URL-owned duplicate-checker filters and shared logic

**Finding:** scene and image duplicate checkers copied route filters only on
mount, so same-route navigation could display data for a different filter than
the URL. Both duplicated the flawed controller.

**Resolution:** [duplicates/controller.ts](../src/components/duplicates/controller.ts)
derives the committed filter from validated route search. A shared typed
controller updates search and resets pagination; shared pure helpers handle
safe group selection, ordering, and tints. Media-specific query and rendering
logic stays local. Real memory-router tests cover both filter modes, Unicode
criteria, same-route updates, Back/Forward, clearing, and pagination reset.

### 10. React render purity in shared lifecycle code

**Finding:** shared selection, shortcut, filter, player, and settings code
published values into refs during render, exposing work React might abandon.
See [React's ref caveats](https://react.dev/reference/react/useRef#caveats).

**Resolution:** render snapshots and editable row identities use React state.
Imperative listeners and cleanup that need stable identity use
[useCommittedRef](../src/hooks/use-committed-ref.ts), whose layout effect
publishes only committed values. Picker labels use a React-owned cache; row keys
no longer depend on global counters. Wall selection no longer mutates card
attributes in a layout effect. Player source decisions, shortcut scope identity,
settings drafts, and bulk-sheet cleanup follow the same ownership rules.
Lifecycle tests include abandoned rendering, stable form identity, and delayed
callbacks. Touch/zoom/text-selection policy is unchanged.

### 11. Preserve types through shared adapters

**Finding:** core navigation, scene downloads, lightbox slides, and merge fields
erased useful type information. Bulk-job responses conflated numeric job IDs
with the legacy synchronous acknowledgment `"sync"`; watchers lacked bounded
failure cleanup.

**Resolution:** [navigation.ts](../src/core/navigation.ts) carries checked
`to`/`params`/`search` descriptors through cards and tables. Runtime return URLs
are validated against the current origin and deployment prefix. Plugin routes
remain a separate runtime registration boundary.

Scene downloads accept a narrow structurally checked capability and share one
snapshot projection across detail/card/bulk actions. Lightbox slides use the
library's public types, augmentation, and guards. Merge fields retain their
value type inside a closure exposing typed preview/projection operations;
heterogeneous field lists no longer require a double assertion.

[Entity job acknowledgment decoding](../src/core/entity-job-invalidation.ts)
distinguishes synchronous completion, numeric scheduled IDs, and invalid input.
Only scheduled jobs are monitored. Watchers outlive edit sheets, retry transient
query failures with bounded backoff, and dispose/refetch after terminal results
or repeated failures. The GraphQL response shape remains compatible with v2.5.
Tests cover invalid contracts, scene projections, merge behavior, job outcomes,
transient/permanent failures, and explicit disposal.

### 12. Enforce the intended foundation

**Finding:** the initial indexed-access compiler experiment reported 180
diagnostics in 54 files. These were review sites, not 180 proven runtime bugs.
The initial inventory had zero explicit `any` annotations, but that did not
prevent JSON assertions and lifecycle errors.

**Resolution:** `noUncheckedIndexedAccess` is enabled after migrating accesses
to iteration, checked lookups, or explicit invariants. No blanket non-null
assertions were added to silence those diagnostics. Marker interval selection
uses checked dynamic-programming rows and iterative reconstruction. Negative
type tests enforce route, configuration, list, and media contracts.

[eslint.config.mjs](../eslint.config.mjs) adds React's `refs`/`purity` checks and
restrictions on `as never` and double assertions. Biome still owns formatting,
general lint, and hook dependency checking; it recognizes `useCommittedRef` as
stable. The application entry point enables Strict Mode. Existing plugin startup
already shares an initialization promise and guards cancelled effect consumers.

`exactOptionalPropertyTypes` remains a separate deliberate migration: omitted
values and explicitly cleared fields have real API/storage meaning. These
checks improve the foundation; they do not prove every runtime boundary or
browser integration infallible.

## Validation

- `make validate-ui-v3`: passed, including **173 tests in 41 files**, lint,
  TypeScript, formatting, locale checks, and the pinned v2.5 compatibility gate.
  Compatibility covers 61 legacy operation files and 87 unchanged primary
  migrations at `48b1409c40db`.
- `make ui-v3-only`: passed. The existing large-chunk warning remains.
- Before publication, `make generate`, `make ui`, `make ui-v3-only`, and
  `make validate-fork` passed in the documented order, including Go lint,
  integration tests, and the embedded-asset checks.
- A temporary Chromium fixture using real shared list components passed **40
  scenarios**: grid/details/wall/table, desktop/mobile viewports, app/browser
  Back, delayed data, pagination history, and outgoing transition geometry.
- A two-tab Chromium fixture using the real queue, IndexedDB, and OPFS passed
  duplicate enqueue, remote removal while writing, owner closure/recovery,
  retry to a complete file, and clear-all during writing.
- A deployment-isolation Chromium fixture passed automatic legacy ownership,
  retained originals, deletion receipts across reloads, overlapping scene IDs,
  independent workers, and same-prefix tab coordination. The real recovery
  dialog, using the production stylesheet, passed explicit legacy ownership
  confirmation, retry after a failed storage read, and prefix-change recovery
  without database enumeration. Clear-all remained scoped; files and
  resume state survived recovery; browsers without Web Locks retained read-only
  legacy playback. Recovery actions remained visible in desktop and mobile
  layouts. No uncaught browser errors occurred.
- A Strict Mode Chromium fixture using real wall cards passed pointer/keyboard
  selection, accessible pressed state, memoization of unchanged cards,
  select-all/none, and refreshed selected entity data.

## Compatibility and limits

No Go implementation, GraphQL schema, primary migration, or v2.5 UI file changed.
Existing preference keys, saved-filter formats, and extension configuration
fields are preserved. New offline stores use deployment namespaces; original
legacy stores remain intact for migration and recovery. General page pinch/double-tap zoom
remains disabled, custom media zoom remains available, and UI text selection
retains the approved editable/technical exceptions.

The browser fixtures use synthetic data and temporary storage rather than the
deployed library. Physical iOS/touch gestures were not revalidated. Exact
optional properties and source-version validation for resumed downloads remain
outside this completed scope.
