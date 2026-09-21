# v3 read performance

The September 2026 improvements address list aggregates, startup dependencies,
and SQLite browsing/search. They retain the v2.5 API and upstream database format.
See [preview images](preview-images.md) for the separate card-thumbnail work.

## Query and loading boundaries

Scene list operations select count and card data without requesting unused
duration/filesize totals. The backend still resolves those fields for clients
that request them.

The system-status check still precedes database-dependent work. Configuration
and plugin discovery then overlap. Plugin registration waits for the configured
locale and completes before constructing the router. The optional plugin UI
catalog, migration/setup screens, lightboxes, editors, and table engine load
when needed. Deferred overlays retain their original lifetime after first use;
they do not replace the player during scene navigation. Offline precaching
includes dynamic dependencies so opening a saved scene remains possible offline.

## SQLite compatibility and search semantics

Fork migration 6 adds two ordinary covering indexes:

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

These indexes require no custom collations, columns, triggers, or virtual tables.
Upstream writes maintain them normally; an idempotent fork reconciler recreates
them if an upstream table migration drops them. Upstream's `schema_migrations`
version and table definitions remain unchanged. The rollback/roll-forward tests
exercise plain SQLite writes and reopening in the fork. The indexes occupied
49 MiB on the measured 915 MiB library snapshot.

For global scene/image searches, `pkg/sqlite/media_search.go` probes a necessary
positive term or OR group across metadata, file paths, fingerprints, and scene
markers. It retains at most 4,096 distinct candidates. Selective searches reuse
that set for both count and page queries; broad searches stop the probe early
and retain the original plan. One JSON parameter avoids exhausting SQLite's
parameter limit.

All original joins and search predicates remain in place. This preserves
substring/wildcard matching, phrases, AND/OR/exclusions, matching within joined
rows, and totals over the matching files. The probe only runs inside an existing
transaction so candidates and results observe one snapshot. Negative-only
searches, nontransactional callers, and lists with existing entity criteria use
their original plan. Nothing persists a search index or requires synchronization
after upstream-only edits.

## Measurements

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
