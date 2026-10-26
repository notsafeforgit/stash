# v3 read performance

This guide describes the native read path, checked against `develop` on
2026-10-26. Library data uses the native schema and API; upstream formats are
supported historical upgrade inputs, not runtime compatibility requirements.
See [preview images](preview-images.md) for card thumbnails and viewing renditions.
The measurement sections below retain September 2026 results from the earlier
implementation; they are not fresh benchmarks of the current release.

## Query and loading boundaries

v3 scene, image, performer and studio lists fetch rows and exact counts independently,
without requesting unused duration/filesize totals. Cards render as soon as the
page arrives; a loading placeholder represents an unknown total. Page clamping,
deletion refills and bulk actions that require a total wait for that total.
Apollo merges the two selections into the same result object and refreshes both
after mutations or an explicit refresh. The backend skips count work for card
requests and skips page selection/hydration for count requests, including AST
filters. Performer and studio lists use summary fragments rather than full detail
projections. Requested relationship counters use the request-scoped entity-count
loader and [batched repository queries](../pkg/sqlite/entity_counts.go), grouped
by aggregate and studio depth, instead of a query per entity. Other list sources
can still combine page and count selections; aggregate fields remain available
to native callers that request them.

Independent count queries omit page, page size and sort, allowing pagination to
reuse a filtered total. The [client cache policy](../ui/v3/src/core/list-query-cache.ts)
retains active list queries and twelve most recently used inactive results.
It evicts root references before Apollo garbage collection; selected item
snapshots have their own lifetime. Recent Back navigation remains cached and
older results reload. This bounds list-result retention, not every object in
Apollo's normalized cache.

SearchInput retains its 300 ms typing debounce. List fetching adds no second
delay, so Enter, clearing search, and page navigation start immediately.

Home retains visited carousel components, cached results, random seeds and
scroll positions. Rows and cards mount near the viewport, preserving native
snap targets and strip geometry for cards not yet mounted. Once mounted, vertical
row visibility and horizontal card visibility gate card images, preview videos
and sprite media. Leaving the nearby area
releases those media elements while keeping card geometry, controls and open
overlay state. Returning restores media without another list request.

The system-status check still precedes database-dependent work. Configuration
and plugin discovery then overlap. Plugin registration waits for the configured
locale and completes before constructing the router. The optional plugin UI
catalog, migration/setup screens, image lightbox, editors, and table engine load
when needed. The main entry asynchronously preloads the scene lightbox and shared
player on every launch; this loads code without mounting media. Deferred overlays
retain their original lifetime after first use; they do not replace the player
during scene navigation. Offline precaching
includes dynamic dependencies so opening a saved scene remains possible offline.

## Native SQLite browsing and search

The native schema retains these ordinary covering indexes:

```sql
CREATE INDEX fork_scenes_created_at ON scenes (created_at, title);
CREATE INDEX fork_images_created_at ON images (created_at, title);
```

Including the title lets SQLite read and sort timestamp ties from the index.
Queries retain natural title ordering and the explicit ID tie-breaker. The
natural comparator now returns equality for equal titles, allowing that ID
tie-breaker to work consistently with either a table scan or an index scan.

After a full `ANALYZE`, production SQLite's STAT4 estimates can choose a full
table scan and sort for an unfiltered timestamp page despite these indexes.
`pkg/sqlite/media_browse.go` explicitly selects the covering index for bounded,
unfiltered `created_at` pages, including the AST path. Queries with criteria,
joins, CTEs, other sorts, or unbounded exports retain the planner's choice.
The check happens when rendering SQL, after aggregate fields can add joins.
Regression checks run after `ANALYZE` with the production SQLite build tags and
compare both sort directions, timestamp/title ties, counts, and page boundaries.

Their `fork_` names reflect their historical introduction, not a live fork
reconciler. They are native schema objects maintained by normal SQLite writes;
historical reconciliation runs only while upgrading supported old databases.
Native migrations and fresh-baseline equivalence checks preserve the current
schema. The indexes occupied 49 MiB on the historical 915 MiB measurement snapshot.

For scene/image searches, `pkg/sqlite/search_index.go` stores searchable text in
`<library database>.search.sqlite`, a separate disposable database. FTS5's
[trigram tokenizer](https://sqlite.org/fts5.html#the_trigram_tokenizer) accelerates
substring LIKE searches rather than changing them to whole-word search. It
indexes titles, details, complete file paths, fingerprints and scene marker
titles. A necessary positive term or fully indexable OR group produces at most
4,096 candidates. Broad candidates retain the original plan. One JSON parameter
avoids exhausting SQLite's parameter limit. Short, negative-only and unsupported
patterns retain SQL search; a missing or stale index also falls back safely.

All original joins and search predicates remain in place. This preserves
substring/wildcard matching, phrases, AND/OR/exclusions, matching within joined
rows, and totals over the matching files. Indexing concatenates fields only to
find a candidate superset; it never substitutes for the original predicates.
The SQL fallback probe remains limited to global lists so it does not add a
library-wide scan to an already selective entity filter.

FTS rows stay outside the main database. Native migration 1000131 adds a durable
`library_search_state` token and ordinary triggers for indexed text dependencies.
Each completed index transaction records that token and the SQLite schema version.
Startup reuses an index only when both match the library snapshot. Interrupted
updates, offline text edits, schema changes or invalid cache contents require a
rebuild. Random tokens distinguish divergent writes after a restore even when
the number of writes is equal. The search revision is authoritative native state;
the derived documents remain disposable.

The application writer also installs connection-local TEMP triggers to collect
changed document IDs transactionally, including deletions and replacements.
A background worker batches incremental updates; non-text edits can retain the
index. A pinned observer connection uses SQLite `data_version` to detect commits
outside that tracked writer, conservatively invalidating caches and requesting
a rebuild. Readers pin their main snapshot between observations; only a matching
index snapshot can narrow a query. Ordinary SQL remains available while the index
is unavailable or catching up. See [read_acceleration.go](../pkg/sqlite/read_acceleration.go),
[search_index.go](../pkg/sqlite/search_index.go) and
[search_changes.go](../pkg/sqlite/search_changes.go).

Exact counts use a bounded 256-entry in-memory cache keyed by SQL and typed
bindings. SQLite's compiler supplies the query's actual table dependencies,
including subqueries; [count_changes.go](../pkg/sqlite/count_changes.go) does not
parse SQL or maintain a filter allowlist. Transactional TEMP triggers collect
changed tables, and a tracked commit expires dependent counts only. Rollbacks
retain them; unrelated worker bookkeeping need not invalidate library totals.
Schema changes, external writes and failed tracking invalidate conservatively.
Relative-time and nondeterministic queries bypass the cache.

The search cache is created with mode 0600 and contains library text. Exclude it
from backups if desired. To force a clean rebuild, stop Stash and delete only
`<library database>.search.sqlite` and its `-wal`/`-shm` companions. The next start
rebuilds automatically; no backfill command is needed. Builds without
`sqlite_fts5`, unavailable cache directories and failed index builds retain
ordinary SQL search. Never delete `library_search_state` or its native triggers
to reset a cache; startup validates those required schema objects. Native
libraries cannot be opened with an old upstream/frozen binary.

## Historical indexed-search measurements (September 2026)

A second isolated snapshot contained 267,302 scenes and 492,854 images. With
production tags `sqlite_stat4 sqlite_math_functions sqlite_fts5`, the initial
index build took 20.8 seconds and occupied 329 MiB. The live database was not
modified. These are medians of three warm runs after one warm-up, measuring
count plus 40 IDs sorted by `created_at`. The accelerated runs include cached
counts. Every result's IDs and count matched the current SQL baseline. Times
exclude GraphQL hydration/transport, artwork loading, and browser rendering.

| Query | Current SQL baseline | Index + cached count |
| --- | ---: | ---: |
| Scenes, `2024` | 171 ms | 6.1 ms |
| Images, `2024` | 299 ms | 11.4 ms |
| Scenes, `café` | 162 ms | 0.18 ms |
| Images, `café` | 264 ms | 0.63 ms |
| Scenes, absent term | 161 ms | 0.95 ms |
| Images, absent term | 273 ms | 1.34 ms |
| Scenes, `a` (SQL fallback) | 571 ms | 374 ms |
| Images, `a` (SQL fallback) | 909 ms | 597 ms |

The short-term results demonstrate count reuse, not trigram acceleration.
Regression tests compare substring, wildcard, Unicode, boolean and joined-row
semantics; edits, rollbacks, external writes, upstream table rebuilds, and
concurrent old/new snapshots run under Go's race detector. Browser tests cover
rows before counts, pagination while a total is pending, and Home media release
and restoration in Chromium and WebKit. These checks do not measure physical
iPhone memory behavior.

## Historical browsing and loading measurements (September 2026)

The library contained 266,950 scenes and 492,381 images. SQLite measurements used
a consistent backup obtained through a read-only connection. Only that temporary
copy received the indexes/migration. Medians below cover five warm runs of the
count plus 40 IDs, with identical IDs/counts checked against the original SQL.
They exclude GraphQL serialization, record hydration, and image transfer.

Deployment exposed a STAT4 plan regression after migration's full `ANALYZE`.
A fresh snapshot of the deployed database (267,302 scenes and 492,854 images),
tested with `sqlite_stat4 sqlite_math_functions` and another full `ANALYZE`,
confirmed the bounded-page index selection above: scene page 1 took 174 → 26 ms
and page 1,000 took 327 → 39 ms; image page 1 took 302 → 53 ms and page 1,000
took 540 → 72 ms. All returned IDs and counts matched the reference queries.

| Query | Before | After |
| --- | ---: | ---: |
| Scenes, newest first, page 1 | 181 ms | 25 ms |
| Scenes, page 1,000 | 320 ms | 40 ms |
| Images, newest first, page 1 | 317 ms | 52 ms |
| Images, page 1,000 | 587 ms | 82 ms |
| Scenes searching `2024` | 587 ms | 178 ms |
| Images searching `2024` | 896 ms | 307 ms |
| Scenes, no-match search | 540 ms | 158 ms |
| Images, no-match search | 819 ms | 260 ms |

Broad searches are not uniformly faster: scene search `a` was 615 → 621 ms and
`mp4` was 631 → 667 ms. The bounded probe prevents an unbounded candidate set,
but can add a metadata scan when a common term occurs mainly in file paths.
Avoid treating selective-search improvements as a guarantee for every query.

Two cold browser contexts per route compared production builds against the
running library through a proxy that permits queries and media reads only.
Chromium used a 430×739 viewport, DPR 3, touch input, 2× CPU slowdown, 20 Mbps,
and 40 ms latency. This approximates a mobile workload; it is not physical
iPhone/Safari calibration.

| Route | JavaScript transferred, before → after | First cards, before → after |
| --- | ---: | ---: |
| Home | 913 → 520 KB | 1.75 → 1.59 s |
| Scenes | 985 → 652 KB | 2.21 → 1.96 s |
| Images | 972 → 652 KB | 1.92 → 1.87 s |

The new browsing runs fetched no player, lightbox, editor, or table chunks and
reported no page/GraphQL errors. Configuration and plugin discovery started
together. The baseline build already included the card-thumbnail client work
and removal of unused totals, so these browser comparisons isolate loading
changes rather than measuring the combined improvement from every change.
The proxy omitted the new nested `PreviewImage.thumbnail` selection when talking
to the older running backend and supplied null for it in responses. Existing
artwork therefore used the same full-size fallback in both builds. The live
backend did not receive the SQLite changes, and the live library was not mutated.

Validation passed: 432 v3 unit tests plus type/lint/format/locale checks, the
61-operation v2.5 compatibility check, full Go integration tests and lint,
and production builds. All 63 Chromium regressions passed through the deferred
media and merge-dialog entry points, navigation, and embedded lists. Four
production PWA tests passed, including cold offline playback. Deployment checks
also passed 18 repeated embedded-list tests under a two-core limit and 56
navigation/deletion/embedded-list checks across Chromium and WebKit. These cover
URL-based pagination, browser Back between pages, and stale virtualizer offsets.
Physical iPhone playback and memory behavior still require device verification.
