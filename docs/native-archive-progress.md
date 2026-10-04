# Native archive implementation progress

This is the implementation record for the
[full transition plan](native-archive-transition-plan.md). It does not narrow
that plan's scope or replace its completion criteria. Development remains on
`v3-rewrite`; merge into `develop` requires verification and the owner's success
review. Production has not been migrated.

## Current release position — 2026-10-03

Implementation remains in phases 1–4. The native schema, archive services and
producer transport are substantial, and all 1,697 frozen catalog bodies have
been imported and reconciled on isolated copies. This does not close catalog
migration: historical policies, remaining operational families, review resolution
and a complete cutover reconciliation are still required.

Durable enrichment execution, queued-job dispatch, stale-job maintenance and
shared download/enrichment service reservations are implemented, including linked
download services, typed service failure reporting and bounded download preference
across collections. Dispatch across multiple profiles and automatic discovery of
permitted metadata collections are implemented. Legacy enrichment preparation
validates queue/progress semantics against the frozen inputs, and historical
catalog receipts now have native import and inspection. Legacy enrichment queue,
cooldown and progress mapping, reviewed activation and collection-revision
handoffs are implemented. Retained legacy collector staging now has native
conversion, inspection and reviewed acceptance into shared native captures.
Native captures distinguish a missing historical observation time from the time
the archive received retained evidence; acceptance retains the review hold.
Exact handoff review, seed inspection and reviewed worker execution are now
implemented, including child retry, publication and recovery without assigning
new observation times to retained evidence. File edits, state changes and
deduplication assertions now have native history, bounded import and inspection,
while preserving current Stash selections. Historical metadata alternatives now
have typed preview/application APIs, guarded relationship resolution and durable
retry receipts. Scene/image review controls now expose those choices on desktop
and mobile with saved-request recovery. Account ownership review now provides
bounded discovery, explicit performer choices, selected-account updates and
interrupted-request recovery through desktop and mobile navigation. Explicit
account consolidation review now exposes same-service selection, conflicting-ID
acknowledgement, ownership resolution and exact request recovery. Remaining
import review resolution and the broader native management interfaces still
require work.
Native UI, live host/n8n conversion, compatibility removal, coordinated
backup/export/restore, production cutover and retirement remain major release
gates. The phase table below records that distinction; commit count is not a
completion percentage.

## Local rehearsal storage budget

Keep at least **50 GiB free** on the host filesystem for downloads and normal
activity. Before write-heavy checks or a full-copy rehearsal, run
`python3 scripts/check_rehearsal_space.py --additional-bytes <estimated-peak-bytes>`.
Include the new copy, migration growth, indexes and WAL in that estimate; check
again during long copies/imports. The check reserves space for future writes;
it does not allocate or lock that space against other host processes.

Retain the original compatible snapshot, frozen migration inputs, the latest
verified native database and, while needed, one candidate under verification.
After verifying its replacement, remove the superseded database and its search
indexes, WAL files and compressed copies once no process has them open. Do not
accumulate a full archive for every schema increment. Small reconciliation
reports and scripts can remain. Production databases, media, active downloads
and ordinary service backups are outside this cleanup policy.

On 2026-10-03, the owner authorized pruning superseded goal artifacts. Removing
197 obsolete database/archive/index files reclaimed about 140 GB and left about
162 GB free. Historical sections below describe evidence recorded at the time;
their intermediate database paths are no longer retained. The original
`native-archive-rehearsal-20260930/compatible-snapshot.sqlite`, frozen import
inputs remain under `.local/`. `.local/native-rehearsal-current.json` identifies
the latest verified native database, schema and reconciliation receipt. The
superseded copies are removed after their replacements pass comparison. After
the schema-56 checks finished, pruning Go cache entries unused for more than
49 hours reclaimed another 33.7 GB. During schema-60 verification, pruning
regenerable Go cache entries older than 24 hours reclaimed 31.5 GiB, leaving
139.3 GiB free with both rehearsal copies still present. Recently used and open
cache files were retained. Cache pruning applies to generated build artifacts;
it does not remove source files or recovery inputs.

## Frozen baseline

- Stash source: `0a603d07e9ec3dd9863a7340858a635525bb2c11`, pushed annotated tag
  `v2.5-compatible-final`; primary schema 86, fork schema 9.
- The running binary reports `v0.26.2-1182-g0a603d07e`. Its wrapper index is
  `sha256:b9c30164d591cb9764f809505208fbf7d42d0bfdf209230b94f0dba69b46014b`.
- The local Quadlet now uses that digest and has registry auto-update disabled.
  A daemon reload succeeded; the existing container was not restarted. The
  original Quadlet is retained in the owner's config backup directory under
  `backups/native-archive-baseline-20260930/`.
- The [release manifest](releases/v2.5-compatible-final.json) records all frozen
  image digests. Base-image preservation passed in
  [run 36757532460](https://github.com/notsafeforgit/stash/actions/runs/36757532460),
  including embedded revision and exact manifest-digest checks. Wrapper image
  preservation passed in
  [run 36758167764](https://github.com/notsafeforgit/stash-s6/actions/runs/36758167764)
  for all three variants and their child manifests.

## Incremental implementation

| Repository / commit | Work and evidence |
| --- | --- |
| Stash `781ee4b93` | Full transition plan and documentation links |
| Stash `b76c600a8` | Frozen release manifest, non-overwriting preservation script, child-manifest retention, isolated native preview tags; seven safety tests and actionlint passed |
| Stash `5c4a4c057` | Independent-fork policy and baseline progress record |
| stash-s6 `5dab164` | Explicit digest selection for native builds, isolated preview variants, preservation workflow using pinned Stash release tooling; actionlint and [resolved bake validation](https://github.com/notsafeforgit/stash-s6/actions/runs/36757903303) passed |
| Stash `bc77846a6` | Native lineage 1000000, sidecar promotion, pre-write refusal, historical migration audit, full-copy rehearsal, native plugin/current-operation validation; complete validation gate passed |
| Stash `43d8bba99` | Canonical saved-filter ASTs, legacy conflict evidence, strict persistence validation, full-copy semantic reconciliation; complete validation gate passed |
| Stash `e8fb366d2` | Unified performer names, per-name auto-tag policy, nonunique display names, full-copy semantic reconciliation; complete validation gate passed |
| Stash `e4fa14045` | Native default filters, durable config publication, revision-checked review, full-copy reconciliation; complete validation gate passed |
| Stash `bd7f2af84` | Portable archive UUIDs, transactional merge redirects and catalog UUID adoption; full-copy reconciliation and complete validation gate passed |
| Stash `4d1c7571d` | Qualified source accounts, evidence replay, and audited ownership choices; full-copy reconciliation and complete validation gate passed |
| Stash `02d3c67c2` | Shared post/profile evidence, lossless captures, versioned retention, replay and integrity checks; full-copy reconciliation, catalog size inventory, and complete validation gate passed |
| Stash `c1d2c9387` | Portable gallery identities, membership revisions, and explicit source-album requirements; full-copy reconciliation and complete validation gate passed |
| Stash `e31ced0d9` | Ordered shared attachment manifests and audited media associations; full-copy reconciliation and complete validation gate passed |
| Stash `a7ab4a1cc` | Reviewed attachment selections and compatible partial source lists; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `377f7a6ad` | Source albums with persistent manual membership intent; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `5bc0149ea` | Portable tag/studio/group identities and tag-merge redirects; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `5fd4d0f93` | Scalar field decisions with explicit clear/inherit, preserved legacy values and new-album capture provenance; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `b2ecfe605` | Typed metadata relationships and coalesced collection choices; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `f96577925` | Reviewed source-account consolidation with retained identity evidence and ownership history; full-copy reconciliation and complete validation gate passed |
| Stash `05105cb15` | Versioned captured-account claims across native/mirror services and unfamiliar extractors; complete validation gate and CI lint/build passed |
| Stash `e3db87bec` / `7f2a87c09` | Explicit source album extraction and current documentation; complete validation gate, CI lint/build, and preview image publication passed |
| Stash `d2d6fcf9f` | Revisioned logical roots and source collections, confined file opening, and intake provenance; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `bd7848d58` | Captured publisher decisions connect source evidence to accounts; full-copy reconciliation and complete validation gate passed |
| Stash `dd9ee3768` | Isolated native test fixtures retain real migration coverage while avoiding repeated empty-schema construction; complete validation gate passed |
| Stash `367ea96ed` | Scoped capture ingestion and durable receipts; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `2215503f7` | Verified media preparation through the shared scanner; complete validation gate, Windows package cross-compilation, CI lint/build, and preview image publication passed |
| Stash `5777a6b60` | Verified content identities, immutable root/file verification history, and file-generation guards; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `b5e996347` | Durable archive jobs, fenced leases, coalesced submissions and atomic publication; full-copy reconciliation, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `a2fdd056e` | Verified file/media publication and persistent path removal fences; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `372f3267b` | Collection/source intake and selected media feed native album galleries; full validation, targeted index checks, CI lint/build, and preview image publication passed |
| Stash `d24eb5074` | Durable file admission, worker checkpoints, previews, scoped status and retryable plugin delivery; full-copy reconciliation, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `f244fd4f3` | Revisioned metadata policies, typed jq mapping, guarded previews and ordinary-scan defaults; full-copy reconciliation, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `c84540426` | Gallery-dl lifecycle, source lease checks and final-file outbox publication; complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `bad3f2ca5` | Offline source request coalescing, caller tickets and immutable admission replay; producer migration fixtures, shared window corpus, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `5af18a486` | Scoped source dispatch and pinned n8n worker image; complete validation gate, producer tests on both host runtimes and the installed container package, CI lint/build, and preview image publication passed |
| Stash `a2b8a0de1` | Caller completion tied to original submissions and exact source windows; producer migration/restart fixtures, complete validation gate, CI lint/build, and preview image publication passed |

## Phase status

| Phase | Status |
| --- | --- |
| 0 Baseline and contract | In progress: source tagged, runtime pinned, all compatible images preserved, independent-fork policy updated. Full backup boundary, fixtures, scoped API contract and performance budgets remain. |
| 1 Native schema and services | Schema promotion, canonical saved/default filters, durable config import, unified performer names, portable archive identities including galleries and metadata relationship targets, native account/ownership storage and reviewed consolidation, shared post/profile/capture storage, ordered attachment manifests, audited media associations, reviewed source-list selection, and source-gallery synchronization with manual membership intent are implemented. Scalar and relationship field choices protect explicit and preserved metadata. Revisioned logical roots and source collections retain capture/manual-intake provenance; captured publisher choices connect source evidence to accounts independently of depicted performers. Verified byte identities and immutable per-file verification history now use persistent file-generation guards. Producer identity matching, policy resolution, review APIs/UI and remaining domain services are in progress. |
| 2 Ingestion and producer adapter | In progress: scoped producer tokens, Reddit/Twitter capture batches and durable receipts are implemented. Verified preparation uses the shared scanner; file/media publication checks descriptors, generations and persistent path removals, reuses concurrent scans, and rejects ambiguous verified-byte owners. Intake publication connects collection provenance, selected source media, attachment evidence and album galleries while preserving explicit choices. Persistent jobs have coalesced submissions, fenced leases, retry/cancellation/recovery, and atomic domain/result publication. File-completion admission now queues a durable worker that checkpoints registration, generates previews and delivers retryable media/gallery hooks; scoped status reports actual completion. Native collection policies now apply typed metadata and explicit performer defaults in intake and ordinary scans, with dry preview and guarded apply. Source-run coordination now coalesces missing date ranges, fences worker ownership, retains checkpoints and deferrals, and excludes overlapping destinations. A supported Python producer package now provides retained-payload outboxes, fenced delivery, durable receipts, backoff and review. The gallery-dl SDK now queues source evidence before download, verifies root/prefix and lease ownership, holds shared destination locks, and queues flushed final files before archive acknowledgement. Reddit single-media evidence and original Twitter attachment membership are covered. The producer now coalesces offline source requests, freezes submissions for replay, retains caller tickets and validates native admission receipts. Claimed Reddit/Twitter windows use precise source timestamps and parent context, with directory checks before postprocessor callbacks. Reviewed portable profiles now fingerprint settings/assets and execute one claimed attempt with concurrent outbox delivery and lost-finish recovery. A local converter now stages ordered, source-scoped profiles from the host and n8n JSON layers without copying website credentials. Scoped dispatch discovers eligible runs with durable pagination/backoff, and a separate n8n image packages the pinned worker runtime. A local profile list now rotates download and metadata work, automatically discovers permitted metadata collections, and recovers saved metadata delivery before loading website profiles. Staged host Twitter/Reddit launchers preserve saved lists, modes, date filters and full-history profiles through durable caller snapshots. Staged n8n backfill calls now check permanent history before source admission, preserve original completion proof, and expose pending results to converted workflow waits. Live image/profile activation, operational-history and policy migration, general durable edit notifications, additional source adapters and recovery caller conversion remain. |
| 3 Catalog importer and full-copy reconciliation | Permanent backfill decisions, retained scan journals/reviewed recovery activation, performer UUIDs and saved ownership, and account/catalog registry imports are implemented and rehearsed against full copies. All 1,697 frozen catalog bodies have bounded snapshot receipt and native mappings for posts/profiles/captures, account/post relationships, captured publishers, attachment lists, assets/files/appearances, collection memberships, retained documents and translation results. Historical album backfill is resumable and rehearsed. Original evidence, replay receipts and unresolved conflicts are retained. Shared native translation request/cache/target storage, bounded execution, provider workers and application inspection/control are implemented. Automation snapshot preparation preserves all ten operational families and has passed full-source reconciliation. Resumable native receipt, translation request/cache/target mapping and reviewed activation are implemented. The saved-plan bulk client has activated 324,169 imported holds in a full-copy rehearsal with lost-response recovery and independent reconciliation. Automatic capture scheduling now records revisioned collection-policy decisions with its requests and targets in the capture transaction. Native enrichment targets preserve revisioned scheduling choices and exact capture-completion evidence, with application inspection/control. Internal execution binds jobs to exact source/target revisions, authenticates each attempt producer and retains compact checkpoints with original observation provenance. Verified publication now uses native capture, publisher, album and translation services and commits record associations with target/job completion. Completed staging is verified and released atomically, retaining native captures, acknowledgement/provenance rows and unresolved references. Scoped enrichment routes and the Python transport/lease client now expose those services, with controlled failure replay, bounded checkpoint transfer and exact source-number preservation. Selected-job execution now journals stable claims, returned checkpoints and pending delivery intents in producer outbox schema 8; reviewed metadata-only profiles and delivery-only recovery are implemented. Collection-scoped dispatch now discovers admitted retries before new targets, with durable producer cursors/backoff and pagination past incompatible URLs. Native server maintenance recovers expired attempts and cancels stale work without discarding checkpoints. Native shared service reservations and cooldowns now coordinate downloads with enrichment, including fenced reservations before contacting newly discovered child services. Download-side linked-service reservations, typed failures, bounded download preference and cooperative source turns are implemented. Persistent worker/profile rotation and automatic collection discovery are also implemented. Historical catalog enrichment receipts now have resumable native import and application inspection, preserving the old assertion without creating native attempts. Frozen legacy enrichment jobs, cooldowns and seed/source progress now have bounded native mapping with held work, scoped historical completion, explicit review and retained exclusions. Reviewed legacy enrichment activation is implemented, including explicit collection-revision handoffs. Retained staged results now have conversion, evidence acceptance and exact handoff review; scoped native admission, worker resume, child service scheduling, publication and released proofs preserve original observations separately from fresh child captures. Additional download adapters, existing policy migration, other histories and operational families, validated source routing, review resolution, global reconciliation and cutover import remain unfinished. |
| 4 Native UI and client conversion | In progress: scene/image metadata review is available through desktop tabs and mobile sections, with explicit relationship choices and browser request recovery. Account ownership review has native controls, desktop/mobile navigation, bounded discovery, explicit performer choice, selected-account refresh and saved-request recovery. Account consolidation review supports same-service selection, ownership conflict resolution, stable-ID acknowledgement, retained history and exact recovery. Broader source/account/collection management and remaining caller conversion are unfinished. |
| 5 Compatibility removal and packaging | Preview packaging isolated and old compatibility gate replaced by current v3 operation/plugin-contract checks. Old UI/API/plugin adapters and config bridge still require conversion/removal. |
| 6 Backup and cutover rehearsal | Not yet implemented |
| 7 Production cutover | Not started; compatible production continues |
| 8 Retirement and acceptance | Not started |

No live catalog data or Stash schema has been changed. Do not retire existing
writers, mounts, plugin data, or backups until rehearsal and cutover requirements
have passed. Original catalogs and operating state are migration inputs, including
performer merges, UUIDs, explicit unlink decisions, and pending producer work.

## First native schema rehearsal

On 2026-09-30, an online SQLite backup of the running compatible library produced
a 1,071,263,744-byte isolated snapshot in 3.289 seconds. A separate copy promoted
from primary 86 / fork 9 to native 1000000 in 3.207 seconds. This measurement
covers the initial table promotion, not the later catalog import or production
cutover downtime.

The snapshot contains 270,760 scenes, 498,646 images, 769,643 files, 1,426
performers, and 83 saved filters. Streaming row digests matched across all 65
retained data tables after accounting for table names and equivalent empty
nullable filter fields. Foreign-key checking found zero violations. The private
snapshot, migrated copy, and full comparison receipt are retained locally in
`.local/native-archive-rehearsal-20260930/`; no private library data is committed.

The actual frozen compatible binary was run against an isolated empty native
database and refused schema 1000000 as an unknown legacy fork version. The
native version stayed unchanged. Unit/integration tests also reject foreign
lineage, unsupported versions, missing native tables, unknown legacy objects,
destination collisions, dirty states, and partial SQL promotion.

The full Go unit/integration suite (`GOTOOLCHAIN=auto make it`), SQLite/sharing
integration tests, targeted Go lint, and the retained v3 plugin/current-operation
check passed. `GOTOOLCHAIN=auto make validate-fork` also passed: generation,
frontend lint/types/locales, 527 UI tests in 90 files, all-repository Go lint and
unit/integration tests, and retained native contract checks.

## Canonical saved filters

Migration 1000001 stores ASTs directly on `saved_filters`, removes the live legacy
projection/shadow state, and retains pending alternatives as migration review
evidence. Invalid ASTs fail before conversion; create/update reject invalid or
unserializable criteria without replacing the selected filter. A cleared AST
stays cleared across restart.

The full-copy migration from native 1000000 took 45.6 milliseconds. All 83 filter
ASTs and their name/mode/UI/find options reconciled, 65 other retained tables had
identical streaming row digests, and foreign-key checking found zero violations.
The copied library has no pending legacy filter conflicts. The reconciliation
receipt is `.local/native-archive-rehearsal-20260930/saved-filter-reconciliation.json`.
Focused SQLite/import tests and the complete `make validate-fork` gate passed,
including all 527 frontend tests and retained native contract checks.

## Unified performer names

Migration 1000002 consolidates canonical names, ordered aliases, and per-name
auto-tag policy into `performer_names`. Display names and disambiguation no longer
have to be globally unique. Existing alias selection transfers its policy and
keeps the previous canonical name; database writes replace a name set atomically.
The old name column and both name-related tables are removed. Existing search,
filter, sorting, auto-tag, merge, and anonymised-export callers use the new model.

The full-copy migration took 77.3 milliseconds. All 1,426 performer records,
1,927 names (including 501 aliases), and their policies reconciled. Streaming
digests matched for the other 64 retained tables; foreign-key checking found
zero violations. The receipt is
`.local/native-archive-rehearsal-20260930/performer-name-reconciliation.json`.
Focused migration/domain/API tests and the full validation gate passed.

## Native default filters

Migration 1000003 makes default filters database records with persistent
revisions. A durable configuration checkpoint commits the original filter-only
input, native records, and conflict evidence before atomically publishing the
cleaned config. Generic UI configuration writes cannot bypass the native API;
resolving an alternative requires the revision shown to the reviewer.

Interruption tests cover both publication boundaries, restart, changed inputs,
invalid criteria, preserving edits after import, and stale review actions.
Configuration writes retain permissions and symlinks. Anonymised exports remove
filter evidence and criteria, including freed pages. New database creation no
longer runs old configuration rewrites against modern settings. Pre-migration
backups remain available after success.

The full-copy SQL migration took 31.4 milliseconds; staging and publishing the
nine actual default filters took 20.5 milliseconds. Historical string pagination
was converted without losing sort/display options or the original input. All
66 other retained tables matched by streaming row digest, with zero foreign-key
violations and no pending default conflicts. The receipt is
`.local/native-archive-rehearsal-20260930/default-filter-reconciliation.json`.
The complete `make validate-fork` gate passed, including 528 UI tests, native
contract checks, Go lint, and all Go unit/integration tests.

## Portable archive identities

Migration 1000004 assigns stable UUIDs to performers, scenes, images, and file
records while retaining their integer IDs. Typed foreign keys and partial unique
indexes connect identities to the existing records. Creation, edits, deletion,
and current performer/scene merge callers maintain the identity lifecycle.
Redirects survive merges and subsequent deletion; reused local IDs cannot
resurrect an old identity. Partial merges that leave the source without a UUID
are rejected at commit.

The archive repository supports revision-checked adoption of catalog UUIDs,
preserving generated UUIDs as redirects and refusing conflicting assignments.
It rejects cycles and cross-kind redirects. Anonymised exports replace UUIDs
without breaking the graph. Actual catalog import and API/UI exposure remain
outstanding.

The full-copy migration took 12.6 seconds and created 1,540,475 identities for
1,426 performers, 270,760 scenes, 498,646 images, and 769,643 file records. All
69 retained tables matched by streaming row digest, every existing record had
its identity, and foreign-key checking found zero violations. Database growth
was 249,470,976 bytes (about 238 MiB). The private reconciliation receipt is
`.local/native-archive-rehearsal-20260930/archive-identity-reconciliation.json`.
The full validation gate passed, including migration/lifecycle and merge tests,
anonymised-export checks, 528 UI tests, native contracts, and Go lint/tests.

## Source account foundation

Migration 1000005 adds independent source accounts, qualified identifier claims,
observation evidence, and ownership decisions. Indexed candidate queries retain
reused handles and conflicting IDs for review. Native IDs, handles, and mirror
identifiers remain distinct; matching never assigns media performers. Accounts
and ownership are optional for directly scanned or purchased media.

Evidence replay preserves large numeric identifiers and subsecond timestamps,
extends observation intervals, and rejects changed contents under the same key.
New evidence invalidates stale review revisions. Linked, explicitly unlinked,
and undecided decisions retain immutable history with a checked current head.
Applying a link validates both account and performer revisions. Profile discovery
cannot undo an existing explicit choice. Merge redirects, adopted UUIDs, and
deleted-performer tombstones preserve previous decisions. Anonymised exports
remove source account evidence.

The full-copy migration took 32.6 milliseconds and added 69,632 bytes (68 KiB).
All 70 existing tables, including 1,540,475 archive identities, matched by streaming
row digest, and foreign-key checking found zero violations. New account tables
were empty: schema promotion does not invent provenance for library records.
The private receipt is
`.local/native-archive-rehearsal-20260930/source-account-reconciliation.json`.
The complete `make validate-fork` gate passed: 528 UI tests, current native
contracts, Go lint, and all Go unit/integration tests. Go compilation used
workspace scratch space through `GOTMPDIR` after the shared temporary filesystem
hit its quota; no checks were skipped.
Source equivalence/review services, actual catalog import, and API/UI exposure
are still outstanding. Production remains compatible.

## Physical catalog inventory

The read-only schema inventory on 2026-09-30 found 1,700 databases under the
catalog root: 1,697 schema-3 source catalogs plus the registry, automation, and
run-journal databases. It covers 54 table families and six exact schema variants,
with no unreadable databases after allowing SQLite's transient shared-memory
files. Base database files totalled 2,979,741,696 bytes; this excludes WAL files
and producer state outside the root.

The migration coverage now explicitly includes older plugin profile/binding
tables, collection backfill completions, and one-time backfill policy receipts.
These records must be reconciled with the newer registry and job state rather
than applied twice or used to restart completed work. The private inventory
manifest includes complete schema definitions and per-file schema hashes.
This is an individually consistent schema inventory, not a coordinated backup
boundary or a complete integrity assessment; those remain release requirements.

## Shared source evidence

Migration 1000006 adds portable source posts, shared post revisions and profile
bodies, immutable captures, and checked profile references. Post bodies remain
shared across attachments and meaningful profile edits. Evidence reconstructs
exactly, including large numeric IDs and nanosecond capture times. Versioned
retention removes redundant Reddit renditions and noisy Twitter/Reddit profile
fields for new data; trusted historical import preserves already-retained data.
The 26 synthetic reference fixtures exercise the existing Python policy.

Capture replay is idempotent across restart and rejects changed content under
an existing UUID. Integration tests cover rollback after a late write failure,
cross-post foreign keys, immutable evidence, missing-reference and payload
corruption, bounded indexed queries, and forgotten-post protection. Anonymised
exports remove the new data while preserving the original database. Direct-scan
performer assignments survive migration without invented source posts.
The complete `make validate-fork` gate passed, including 528 UI tests, current
native contracts, Go lint, and all Go unit/integration tests.

The full-copy schema migration took 31.4 milliseconds and added 90,112 bytes
(88 KiB). All 75 retained tables matched by streaming row digest, with zero
foreign-key violations and empty new source tables. The private comparison
receipt is `.local/native-archive-rehearsal-20260930/source-evidence-reconciliation.json`.
An additional read-only inventory covered all 1,697 catalogs: 366,963 observation
records, 430,910 capture-detail records, and 776 separately stored profile bodies.
The largest reconstructed payload size bound was 2,928,259 bytes, below the new
4 MiB limit; no catalog failed inspection. The calculation covers both embedded
and shared profile formats, and deliberately overcounts shared/patch overlap.
It is not a full decode/depth/import validation or a common backup boundary.
Its private receipt is `catalog-evidence-size-inventory.json` in the rehearsal
directory; the all-catalog import rehearsal remains required.
Source-account/capture associations, media appearances, gallery-dl API ingestion,
actual catalog import, and UI exposure remain subsequent work. Production has
not been migrated.

## Source album requirement and gallery identities

The owner's album request is included in the transition plan and acceptance
matrix: one logical gallery per evidenced album post, source attachment order,
mixed images/videos, partial and late downloads, and preservation of manual
choices and existing galleries. Source grouping does not infer performers from
an aggregator. Catalog backfill and standalone export/restore cover albums.

Migration 1000007 gives existing and future galleries portable identities in the
shared registry. Creation, renaming, deletion, UUID adoption, and redirects retain
their semantics; gallery memberships and related metadata advance revisions so
intervening edits invalidate stale review actions. Tests cover existing covers,
image/scene/performer memberships, account links, redirect preservation, deletion,
ID reuse, revision conflicts, and anonymised exports.
The complete `make validate-fork` gate passed, including 528 UI tests, native
contracts, Go lint, and all Go unit/integration tests.

The full-copy migration took 9.56 seconds. All 1,540,475 prior identities and
their UUIDs matched exactly, all 81 other retained tables matched, and all 1,328
existing galleries received identities. Foreign-key checking found no violations.
The rebuild grew the database file by 150,364,160 bytes (143.4 MiB), with
150,929,408 bytes (143.9 MiB) on the reusable free-page list afterward; that space
can serve subsequent native writes. The private receipt is
`.local/native-archive-rehearsal-20260930/gallery-identity-reconciliation.json`.

Source-gallery construction is added in the later checkpoint below; the native
album UI remains required. Ordered attachment associations are described below.
No production galleries or files have been modified.

## Ordered source attachments and media associations

Migration 1000008 adds shared ordered attachment manifests and capture-to-manifest
references. Source order, repeated attachment slots, declared albums, expected
counts, and incomplete source lists survive independently of download state.
Repeated captures share one list; partial captures retain prior snapshots.
Media evidence must cite a capture containing that attachment and typed archive
identities. Observed/verified candidates require an actual current file link.

Selecting media is an audited revision-checked operation. Ingestion can select a
unique supported match; ambiguous candidates require review. Explicit unlinks
survive later evidence. Merge redirects, UUID adoption, deletion tombstones,
restart, keyset pagination, late-write rollback, and anonymisation are covered
by the focused tests. Core/API ingestion must still validate producer evidence;
these repositories are not exposed directly to untrusted producers.

The full-copy schema migration took 54.9 milliseconds. Streaming digests matched
all 82 retained tables, foreign-key checks found zero violations, and all seven
new tables were empty. The database file did not grow because existing free
pages could hold the new schema. The private receipt is
`.local/native-archive-rehearsal-20260930/attachment-reconciliation.json`.
The complete `make validate-fork` gate passed: generation, frontend checks and
528 UI tests, retained native contracts, Go lint, and all Go unit/integration
tests. The previous gallery checkpoint also passed CI lint, build, and preview
image publication.

Source manifest selection and gallery synchronization are described in later
checkpoints below. General field decisions, importer integration, and native UI
remain outstanding.
Production remains on the compatible release.

## Reviewed source-list selection

Migration 1000009 records the selected attachment evidence for each source post.
Compatible partial lists accumulate known positions and counts without erasing
earlier knowledge. A complete capture can replace redundant partial references;
all original captures and earlier decisions remain available. Different IDs at
one position, conflicting known media kinds/counts, and out-of-count positions
produce explicit conflicts rather than a guessed list.

Read-only previews carry the post revision. Applying a choice requires that
revision; pinned and disabled selections survive later captures. Review can
choose a specific capture or explicitly resume automatic selection from it.
Equivalent automatic replay retains the original decision. Bounded bulk queries
load supporting manifests and entries; source payloads are not duplicated.

Focused tests cover accumulation, complete/partial semantics, redundant evidence,
conflicts, stale actions, pin/disable/re-enable, restart/replay, pagination,
late-head failure rollback, cross-post foreign keys, corruption detection,
forgotten posts, migration preservation, and anonymised copies.

The full-copy migration took 45.1 milliseconds. All 89 retained tables matched
by streaming row digest, foreign-key checks found zero violations, and the three
new tables were empty. Existing free pages held the new schema without growing
the file. The private receipt is
`.local/native-archive-rehearsal-20260930/selection-reconciliation.json`.
The complete `make validate-fork` gate passed: generation, frontend validation
and 528 UI tests, retained native
contracts, Go lint, and all Go unit/integration tests. The previous attachment
checkpoint also passed CI lint, build, and preview image publication.

The source-list repository remains separate from gallery synchronization, added
below. Native review API/UI, general field decisions, catalog importer integration,
and the remaining transition phases are still required. Production has not been
migrated.

## Source gallery synchronization and manual membership intent

Migration 1000010 adds explicit gallery origin, audited post-to-gallery choices,
and persistent membership intent. Source synchronization creates one logical
gallery per evidenced album, including partial albums and mixed images/videos.
Repeated slots and reposts reuse existing media. Ordinary single-media posts do
not create galleries. Read-only previews preserve source order and distinguish
unselected, unlinked, deleted, and manually excluded entries; they do not pretend
a linked library entity proves a completed download.

Fresh synchronization replay makes no duplicate galleries, memberships, or audit
events. Manual additions/removals, covers, and deliberately empty metadata survive
later synchronization. Existing manual galleries need an explicit reviewed link;
folder/ZIP galleries cannot be adopted. Disabled associations and deleted galleries
suppress recreation, and gallery redirects require review. No names or titles
establish identity, and source publishers do not become depicted performers.

Repository integration tests cover partial/late media, mixed/repeated/shared
attachments, replay/restart, stale previews, both sides of membership editing,
disable/re-enable, UUID adoption and media redirects, integer-ID reuse, preserved
existing galleries, and atomic rollback after late failures. Anonymised exports
remove association and membership history. Pre-commit and startup guards reject
unfinished source-write context.

The full-copy schema migration took 72.7 milliseconds. All prior columns across
92 retained tables matched by streaming row digest, including every existing
gallery and archive identity. The new gallery-origin values matched the prior
folder/file associations. Foreign-key checking found zero violations, the five
new tables were empty, and the database file did not grow. The private receipt is
`.local/native-archive-rehearsal-20260930/source-gallery-reconciliation.json`.
The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and 528 UI tests, native contracts, Go lint,
and all Go unit/integration tests. Existing gallery fixtures now assert their
explicit creation origin. Regression tests also cover reaffirming membership
through scene/image add operations and file associations without a primary file.

The native API/UI, per-field source/review policies, manual mixed-media ordering,
durable after-success delivery, and producer/catalog import integration remain
required. This checkpoint initializes new album title/description/date and
preserves all existing gallery metadata. Production has not been migrated.

## Portable metadata relationship targets

Migration 1000011 adds tag, studio, and group UUIDs to the archive registry. They
retain identity across renames and preserve tombstones after deletion or reuse
of an integer ID. Tag merges retain redirects to the surviving tag while moving
the existing aliases, remote IDs, and media relationships. Definition and
hierarchy edits advance revisions; moving a relation invalidates both owners.
These identities are a prerequisite for typed references in metadata history,
not an implementation of field decisions themselves.

Focused tests cover all three kinds, stale revisions, UUID adoption, redirects,
transaction rollback, indexed lookup, relationship edits, existing source-gallery
references, and anonymised exports. The full-copy migration took 10.28 seconds.
All 1,541,803 prior identities matched exactly, all 96 other retained tables
matched by streaming row digest, and all 118 tags, 124 studios, and five groups
received identities. Foreign-key checking found zero violations. File growth
was 1,642,496 bytes, with 146,558,976 bytes available on the reusable free-page
list. The private receipt is
`.local/native-archive-rehearsal-20260930/metadata-identity-reconciliation.json`.
The complete `make validate-fork` gate passed, including generation, frontend
validation and 528 UI tests, native contracts, Go lint, and all Go unit/integration
tests. Production has not been migrated.

## Scalar metadata choices

Migration 1000012 introduces set, clear, and inherit for curated scene/image/gallery
scalar fields. Existing values, including empty ones, remain protected until
reviewed. Their prior values enter history on the first edit, avoiding a copy of
every entity or a separate initial row for every field. Ordinary edits record
library intent in the database. An explicit empty title survives later source
and filename suggestions, and filename fallback cannot replace a nonempty
source title. New albums retain the capture provenance of their initial fields.

Focused tests cover typed targets, date precision, invalid inputs, canonical
integer round trips, clear/inherit/replay, stale revisions, ordinary same-value
edits, lazy legacy history, adoption and deletion/ID reuse, indexed pagination,
scope and immutability constraints, late rollback, startup refusal, and removal
of private history from anonymised exports. The initial full gate caught two
unused test-result assignments; the tests now assert those results. Broader album
tests also caught and corrected a query that treated revision metadata as a
standalone column rather than reading its JSON member.

The full-copy migration took 1.62 seconds. Streaming comparison matched all 97
retained tables, and all 770,734 existing scene/image/gallery identities received
a preservation baseline. No decisions were fabricated during migration.
Foreign-key checking found zero violations; the database file did not grow, with
110,927,872 bytes remaining on its reusable free-page list. The private receipt
is `.local/native-archive-rehearsal-20260930/metadata-fields-reconciliation.json`.
Existing selected text fields were also checked against the scalar value bound.
The final complete `make validate-fork` gate passed: generation, frontend
validation and 528 UI tests, retained native contracts, Go lint with zero issues,
and all Go unit/integration tests.

Relationship choices, policy/source resolution, native API/UI and creation-intent
conversion, importer/producer integration, and durable after-success delivery
remain required. This checkpoint does not enable automatic rescanning or overwrite
existing album headers. Production has not been migrated.

## Collection and relationship metadata choices

Migration 1000013 adds the same set/clear/inherit behavior to performers, tags,
studio, URLs, custom fields, and scene groups. Relationship decisions retain
normalized UUID references, with reviewed target revisions and immutable sealed
history. Performer/tag merges preserve the meaning of historical associations;
deleting a target cannot silently rebind old history when its integer ID is
reused. Native browsing fields remain the selected state.

Ordinary bulk edits retain the previous value and record one final choice per
field at commit. Explicit empty sets and reaffirming existing relationships also
protect user intent. SQL guards, transaction checks, and startup validation reject
unfinished decisions. The existing scalar history survives migration intact.
Anonymised exports remove the added references and transaction state.

Focused tests cover every field type across scenes/images/galleries, target
revision conflicts, wrong-kind references, coalescing, clear/inherit/replay,
empty updates through existing APIs, merge/adoption/delete/ID reuse, previous
values without recorded history, preserved history sequence counters, late
rollback, startup refusal, and indexed lookup. Numeric tests include large
doubles and exact signed 64-bit integers, including writes after replay. The
query-plan test identified a missing scene-to-group index, which is
included in this migration. Full-copy probing found no full scans for selected
metadata lookups; a representative single studio edit took about one millisecond.
Existing relationship sizes were within the 4096-target bound.

The final full-copy schema migration took 2.30 seconds. Streaming row digests
matched all 101 retained tables and columns, with zero foreign-key violations.
The new reference and pending tables were empty, no provenance was invented, and
the file did not grow; 110,845,952 bytes remained available on its free-page list.
The private receipt is
`.local/native-archive-rehearsal-20260930/metadata-collections-reconciliation.json`.

The complete `make validate-fork` gate passed on the final code: generation,
frontend validation and 528 UI tests, the retained v3 extension contract and
71 application operation files, Go lint with zero issues, and all Go unit and
integration tests. The SQLite suite completed in 402.1 seconds.

Source/policy resolution, native review API/UI, creation-intent conversion,
producer/catalog import integration, and durable after-success delivery remain
required. Production has not been migrated.

## Reviewed source-account consolidation

Migration 1000014 adds explicit consolidation of duplicate account records in
one service namespace. Original account UUIDs, identifier evidence, and ownership
history remain available. Canonical account and identifier indexes follow the
surviving record through checked foreign keys, including after nested merges.
Lookup remains indexed and paginated instead of traversing redirect chains.

A preview covers the selected account components and their current performer
revisions. Compatible ownership choices can be retained; contradictory ownership
requires an explicit resulting choice. Conflicting stable IDs require a separate
acknowledgement and stay in the evidence. Cross-service and native/mirror accounts
remain distinct, and consolidation does not assign depicted performers to media.
An optional operation UUID provides exact replay without duplicate history.
Transaction/startup guards reject unfinished work, and anonymised exports remove
the added private history.

Focused tests cover retained identifiers and local ownership history, canonical
lookups and query plans, nested consolidation, stale identity and performer
reviews, explicit unlink preservation, conflict resolution, replay, rollback,
startup refusal, anonymisation, and migration with existing decisions. Fixtures
exercise Reddit, Twitter, Instagram, Bluesky, TikTok, Patreon, OnlyFans, Fansly,
Coomer/Kemono namespaces, and an unfamiliar extractor. A regression fixture also
requires acknowledgement before consolidating conflicting TikTok `secUid`
claims. Review bounds are 4096 account records and 8192 identifiers.

The full-copy migration took 1.054 seconds. Streaming comparisons matched all
103 retained tables and columns, with zero foreign-key violations. The new
consolidation/context tables were empty, and every existing account began as its
own canonical identity. No links were inferred and the database file did not
grow, with 110,796,800 bytes remaining on its reusable free-page list. The private
receipt is
`.local/native-archive-rehearsal-20260930/account-consolidation-reconciliation.json`.
The Stash copy has no imported source accounts yet; populated migration fixtures
separately verify existing identifiers and ownership choices.

The final complete `make validate-fork` gate passed: generation, frontend
validation and all 528 UI tests, retained v3 extension contracts and 71 application
operation files, Go lint with zero issues, and all Go unit/integration tests.
The SQLite suite completed in 450.9 seconds. An earlier run exhausted the shared
temporary-files quota during linking; both Go build and linker temporary files
were redirected to the workspace disk for successful validation.

Producer evidence matching, native account-review API/UI, and catalog import
remain required integration work. Production has not been migrated.

## Captured account identity extraction

Core now implements `captured-account-v1` for deriving qualified identifier claims
from reconstructed gallery-dl/yt-dlp captures. Each claim retains its source JSON
pointer and evidence basis. The parser preserves exact numeric IDs and handles
Reddit parent context, Twitter, Instagram, Bluesky, TikTok (including `secUid`),
Tumblr, native subscription services, Coomer/Kemono, and unfamiliar extractors.
It leaves missing IDs unresolved and reports malformed claims for review.

Mirror user IDs and matching public identifiers retain the mirror/service
namespace; display labels do not become native handles. Directory names, scraper
target URLs, and unrelated feed-owner profiles cannot establish the publisher's
ID. A generic extractor's display name remains a label. Existing legacy links
and handle-only inventory records still require lossless import independently
of whether the parser can derive new claims.

Focused fixtures cover these service shapes, exact integers above 2^53, duplicate
keys and malformed identifiers, replay, immutable input bytes, irrelevant profile
noise, retained-payload equivalence, and missing/mismatched mirror profiles.
The complete `make validate-fork` gate passed on this code with the consolidation
checkpoint: 528 UI tests, native contracts, zero Go lint issues, and every Go
unit/integration package. No database migration is required for this parser.

See [native source identity](native-source-identity.md) for the contract and
evidence limits. Account resolution, publisher associations, ingestion/import
integration, and review API/UI remain required. The parser does not create
accounts, merge candidates, change ownership, or assign depicted performers.

## Source-list extraction for albums

Core now derives `captured-album-v1` manifests from Reddit gallery lists and
complete Twitter media lists. It retains qualified post/media IDs, source order,
crosspost and parent evidence pointers, missing slots, unavailable source items,
and repeated attachments. Manifest completeness stays independent of whether
the associated media has downloaded. Unknown lists remain unresolved.

Inspection of the installed gallery-dl 1.32.15-dev extractors confirmed that
Reddit download numbers skip items with unavailable URLs and Twitter's `count`
counts extracted output files, including optional renditions/card images. The
parser therefore does not infer a complete source album from `num`, `count`,
filenames, or a per-file media ID. The native Twitter adapter must supply the
post's media list before extractor filtering; old count-only captures require
additional evidence or review during backfill.

Focused parser fixtures and SQLite integration passed. They cover source order,
partial identity evidence, unavailable items, repeated IDs, crosspost identity,
conservative type hints, contradictory input, exact large IDs, retained-payload
equivalence, bounds, and replay. Parsed lists pass through capture storage and
selection into a gallery with an image, then accept a later video association
without making a second gallery or duplicating memberships.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and 528 UI tests, retained native contracts,
Go lint with zero issues, and all Go unit/integration packages. The SQLite suite
completed in 394.2 seconds.

See [native source albums](native-source-albums.md). No schema migration is
required for the parser. Producer integration, catalog backfill, native API/UI,
and source-to-file verification remain required; production has not changed.

## Logical media roots and source collections

Migration 1000015 gives roots and scrape/manual collections stable UUIDs,
revision-checked definitions, immutable history, and bounded indexed queries.
Collection targets remain separate from performer attribution. Purchased MP4
batches can retain intake provenance without creating an account or source post;
aggregator/feed collections can share captures without assigning their publisher
as a depicted performer. Historical target URLs return current collection
candidates rather than establishing unique identity.

Media roots retain identity when a deployment binding changes. File opening
checks the actual open directory identity and confines paths to it; inactive or
unbound roots, replaced mounts, escaping symlinks, nonregular files, and `.part`
paths are rejected. A file descriptor opened before a rename still references
the original file. Authorization, complete-file verification, and producer
receipts remain separate required checks for the ingestion service.

Capture provenance pins collection revisions. Manual intake uses replayable
event UUIDs, typed scene/image references, and original submitted UUIDs retained
through archive UUID adoption. Deleted media retain their evidence via
tombstones. These operations do not edit metadata or gallery membership. SQL
revision guards and deferred foreign keys prevent stale publication and orphan
identities; anonymised exports remove private definitions and provenance.

Focused tests passed for mount replacement, symlink confinement, regular-file
checks, descriptor identity, stale edits, retirement, URL ambiguity, indexed
lookup, composite pagination, ignored-error rollback, exact/conflicting replay,
UUID adoption, deletion, populated migration, and anonymisation. The filesystem
package also compiled for Windows.

The isolated full-library copy migrated from 1000014 to 1000015 in 0.885 seconds.
Streaming semantic digests matched all 105 retained tables, with zero foreign-key
violations and no invented rows in the six new tables. The 1,473,081,344-byte file
did not grow; 110,706,688 bytes remain reusable. The private receipt is
`.local/native-archive-rehearsal-20260930/source-collections-reconciliation.json`.
The copy still has no imported catalogs; populated fixtures separately cover
actual collection histories and intake records.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and all 528 UI tests, retained v3 extension
contracts and 71 current application operation files, Go lint with zero issues,
and all Go unit/integration packages. The SQLite suite completed in 499.6 seconds.
An initial validation run found a test cursor-cleanup lint issue; the final gate
includes that correction.

Native API/UI, folder/batch defaults, ingestion, and catalog import remain
required; production has not changed.

## Captured publishers and account resolution

Migration 1000016 adds publisher choices, replay identity, current selections,
and references to the captured identifier evidence used by each choice. The
service verifies one stored capture and uses qualified IDs to reuse or create
accounts. Handle-only candidates and conflicting IDs remain for review; explicit
unlinks survive automatic processing. Review can select a publisher without
merging account records. Current reads follow later account consolidation while
preserving the original association.

Publisher identity does not assign performers or change account ownership.
Post/profile bodies remain shared. Preview signatures cover relevant identity
changes and current choices while ignoring unrelated observation counters, so
ongoing scrapes of the same account do not constantly invalidate review. An
account-leading identifier index and a post-first capture query keep checks
scoped to the affected records. Candidate previews are bounded and disclose
truncation; direct target lookup remains available for explicit review.

Focused fixtures passed for allocation and ID reuse, handle changes/reuse,
ambiguous IDs, TikTok secondary-ID contradictions, native/mirror namespace
separation, missing/malformed claims, explicit unlink/inherit, stale review,
unrelated captures, exact/conflicting replay, account consolidation, source
retirement, late-error rollback, startup refusal, anonymisation, pagination,
and query plans. A populated actual schema-15 fixture retains signed captures,
shared profiles, identifiers, logical roots, and collection associations through
migration, then successfully resolves its publisher.

The isolated 1,473,081,344-byte library copy migrated from 1000015 to 1000016 in
0.606 seconds. Streaming semantic digests matched all 111 retained tables, with
zero foreign-key violations and no file-size growth. Migration left all four
publisher tables empty rather than inventing associations. The private receipt
is `.local/native-archive-rehearsal-20260930/capture-publishers-reconciliation.json`.
The copy still has no imported catalogs; populated fixtures separately verify
actual source captures and existing associations.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and all 528 UI tests, retained v3 extension
contracts and 71 current application operation files, Go lint with zero issues,
and all Go unit/integration packages. The SQLite suite completed in 567.3 seconds.
The initial focused query-plan fixture caught a broad decision scan; the final
query fixes its join order to start with the selected post's indexed captures.

Native producer/API integration, review UI, account-profile presentation,
metadata policies, and catalog import remain required. Production has not changed.

## Isolated native test fixtures

Domain tests now copy a closed, empty database built once through the real
initializer, then insert their own fixture entities. Every test still opens its
own database and receives fresh archive UUIDs. Historical migration and new-install
tests continue constructing their actual input schemas. An isolation test verifies
that identities and edits do not leak between copies.

The complete `make validate-fork` gate passed with 528 UI tests, native contracts,
zero Go lint issues, and all Go unit/integration packages. The SQLite suite took
154.3 seconds, down from 567.3 seconds before avoiding repeated empty-schema
migrations. This changes test setup only.

## Native capture ingestion and durable receipts

Migration 1000017 adds producer identities, scoped access tokens, collection/root
grants, and immutable event receipts. These are tokens for calling Stash's API;
third-party service credentials stay with gallery-dl. Token administration uses
the existing authenticated application router. The isolated producer router
accepts bearer tokens and has no application, GraphQL, or plugin fallback.

`/api/v3/ingest` now exposes capability discovery, bounded capture batches, and
producer-scoped receipt lookup. A capture transaction verifies the source post
identity and retention policy, records shared evidence and collection provenance,
resolves its publisher, selects compatible album manifests, and publishes the
receipt. Metadata receipts explicitly report that media has not been ingested.
The initial identity adapters accept Reddit and Twitter; other extractors are
reported as unsupported until their post identity adapters are implemented.

Exact retries preserve the original acknowledgement; changed event bytes
conflict. Batch items commit independently. Receipt-storage failure rolls back
all domain changes. Delayed events can retain historical collection definitions
within their granted root scope. Pinned/disabled albums and ambiguous publisher
or album evidence remain reviewable. Publisher identity never assigns depicted
performers. Token rotation preserves producer/event identity; expiry and permanent
revocation are checked in the transaction that writes each event.

Focused service, HTTP, and SQLite fixtures passed for concurrent duplicates,
restart/lost acknowledgement, conflicting replay, strict JSON and retention,
large source IDs, scope isolation, delayed definitions, partial batches,
revocation/expiry, album conflicts and protected choices, rollback, immutable
receipt constraints, indexed lookup, anonymisation, and actual schema-16 promotion
with populated source evidence and publisher decisions.

The isolated 1,473,081,344-byte library copy migrated from 1000016 to 1000017 in
0.068 seconds. Streaming semantic digests matched all 115 retained tables, with
zero foreign-key violations and no size growth; 110,592,000 bytes remain reusable.
All four new tables remain empty after schema migration. The private receipt is
`.local/native-archive-rehearsal-20260930/ingest-reconciliation.json`. This copy
still has no imported catalogs; populated fixtures separately verify source-data
retention through the actual historical schema.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and all 528 UI tests, retained v3 extension
contracts and 71 current application operation files, Go lint with zero issues,
and all Go unit/integration packages. The SQLite suite completed in 180.9 seconds.
An initial gate found response-encoding and test cursor-cleanup lint issues;
the final gate includes both corrections.

The [ingestion contract](native-ingestion.md) documents request bytes, bounds,
access tokens, and receipt semantics. File completion/verification, producer
outboxes, run leases, gallery synchronization during media ingestion, additional
post identity adapters, native UI, catalog import, and actual host/n8n conversion
remain required. Production has not changed.

## Verified media preparation

`ingest.PrepareMedia` verifies a confined root-relative regular file, computes
SHA-256, checks optional producer size/digest claims, and holds its descriptor
through shared scanner preparation. New-file scans and native preparation now
reuse `Scanner.PrepareFile` for matching fingerprints and media decorators.
Independent descriptor readers preserve seeking, cancellation, and ownership;
closing a reader does not close the held file.

FFprobe reads that same descriptor on Linux, with a private descriptor-copy
fallback on other platforms. Its native intake path has a self-contained format
allowlist, no network/playlist inputs, bounded JSON output, and a timeout. A
promoted `bytes.Buffer.ReadFrom` method would bypass the output writer's limit;
the bounded writer deliberately does not embed that type. Audio-only output
cannot become a video file. Animated images retain the existing clip behavior.

Revalidation detects changed bytes, file/root replacement, changed bindings,
symlink retargeting, and restored modification times using Linux ctime or a
portable digest recheck. Preparation performs no database writes or handlers.
The eventual worker must revalidate inside its publication transaction and
enforce authorization, collection policy, and file-generation fences. The API
continues to advertise `file_ingestion: false`; file completion, durable workers,
and transactional file/source/gallery/receipt publication remain unfinished.

Focused tests passed for the filesystem checks, independent readers, cancellation,
real image/animated-image/audio-only probing, trailing-MP4 metadata, and a file
replaced between hashing and probing. Windows cross-compilation of the archive,
file, and FFprobe packages passed. No database migration or production changes
are part of this checkpoint.

The complete `make validate-fork` gate passed: generation, frontend checks and
528 UI tests, retained extension contracts and 71 application operation files,
zero Go lint issues, and every Go unit/integration package. The SQLite suite took
186.1 seconds. The [ingestion documentation](native-ingestion.md) also clarifies
that token revocation controls access to Stash's API; scraper website credentials
remain with gallery-dl.

## Verified content identities and file generations

Migration 1000018 adds shared SHA-256 content identities and immutable verification
history for each file generation. File UUIDs remain separate from content and
scene/image identities: equal bytes do not merge library items or their metadata.
Location, size, modification time, and matching-fingerprint changes advance the
file's generation. Ordinary file updates require the generation read by the
scanner or task. Fingerprint updates preserve identical rows, avoiding spurious
generation changes on an unchanged rescan.

`PreparedMedia.RecordContent` retains the descriptor through publication, records
the reviewed root revision and exact relative path, and checks the root, file,
and generation before commit. Changes or closure before commit roll back the
proof and associated writes. Historical proofs retain their original root
definition through later edits and survive file deletion and UUID adoption.
Indexed content lookup returns only active, current file generations. Anonymised
exports remove content hashes and private verification evidence.

Focused SQLite and media-publication tests passed for shared bytes, stale scans,
replaced/deleted paths, root revisions, immutable history, generation changes
before commit, disabled roots, closed descriptors, UUID adoption, managed
transactions, migration, and anonymisation. The existing file/media fixtures now
retain storage-assigned generations while continuing to compare complete records.

The final isolated copy migrated from 1000017 to 1000018 in 0.304 seconds. All
119 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. All 769,643 existing files start at generation 1;
both new content tables remain empty, so old checksums are not claimed as newly
verified bytes. The private receipt is
`.local/native-archive-rehearsal-20260930/file-content-final-reconciliation.json`.
This copy remains a library-schema rehearsal without catalog imports. Production
has not changed, and the ingestion API still advertises `file_ingestion: false`.

The final complete `make validate-fork` gate passed: generation, frontend checks,
all 528 UI tests, retained v3 extension contracts and 71 application operation
files, Go lint with zero issues, and every Go unit/integration package. Durable
file jobs and atomic file/source/gallery/completion-receipt publication remain
the next ingestion work.

## Durable archive jobs and publication

Migration 1000019 stores archive jobs, immutable submission acknowledgements,
and per-attempt history. Replayed submissions return their original job even
after completion. Equivalent active submissions share one job; different work
sharing a resource cannot run concurrently. Pending capacity, attempt limits,
bounded recovery, and indexed pagination keep queue work bounded. Repeat
submissions may promote priority but cannot bypass retry delays.

Claims record an owner, deadline, and increasing fence. Renewal, progress, and
publication require that exact unexpired lease. Cancellation uses the reviewed
job revision and invalidates ownership. Expired attempts remain in history and
are requeued or failed at their attempt limit. `job.Durable.Publish` commits
domain writes and the result together, checking expiry and final job state before
commit. Failures roll back both, including expiry inside an earlier commit hook.
Result/progress JSON is bounded and error codes exclude raw worker output.

Focused tests passed for coalesced and conflicting replay, queue capacity,
concurrent submissions/claims, restart recovery, stale owners, renewal, deferred
retry, retry exhaustion, shared destinations, cancellation, SQL immutability,
atomic domain/result rollback, query plans, schema-18 promotion, startup guards,
and anonymisation. The concurrent claim and bounded recovery tests also passed
under Go's race detector. The pinned linter reported zero issues after correcting
the error-code predicate and test cursor cleanup.

The isolated full copy migrated from 1000018 to 1000019 in 0.060 seconds. All
121 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. Existing file generations and content history
remained intact; all three job tables begin empty. The private reconciliation
receipt is `.local/native-archive-rehearsal-20260930/archive-job-reconciliation.json`.

The only accepted job kind is currently `media.verify`. File-completion admission,
path/file reservations, the actual worker, file/source/gallery/receipt integration,
source-run coordination, and producer/host/n8n delivery remain subsequent work.
No worker starts automatically, no legacy journals were imported or retired,
and production remains on the pinned compatible release.

The final complete `make validate-fork` gate passed with all corrections included:
generation, frontend checks and 528 UI tests, retained v3 extension contracts and
71 application operation files, zero Go lint issues, and all Go unit/integration
packages. The SQLite suite completed in 204.5 seconds and the API suite in
118.9 seconds.

## Verified file and media publication

Migration 1000020 retains regular-file path removal counters independently of
file lifetimes. Deletion, rename, and folder moves invalidate queued intake,
including when a concurrent scan created and removed a file after admission.
Recreation retains the removal counter; delayed completions cannot automatically
restore an absent removed path. Ordinary scans can establish a new file lifetime.
Anonymised exports remove path history after rewriting paths, and startup checks
require the new table, lookup index, and triggers.

File publication validates the original descriptor and any case-insensitive
database path spelling, reuses a concurrent scan's file record, and preserves
generated fingerprints for unchanged verified bytes. Media publication reuses
the exact file owner or one unique owner of current verified bytes. Multiple
owners and incompatible kinds require review; legacy matching fingerprints and
stale proofs cannot establish these associations. Existing media metadata stays
intact. File, proof, scene/image association, and durable job result commit
together; final checks also reject deleted or detached media.

Focused SQLite and ingestion tests passed for removals/recreation/restart,
case-folded path history and descriptor aliases, migration/startup/anonymisation,
concurrent scans, replay, UUID adoption, unchanged generated fingerprints,
verified/unverified/stale/ambiguous matches, exact-owner precedence, image/video
classification, metadata preservation, indexed candidate lookups, and atomic
rollback after file/media removal, detachment, conflicting ownership, physical
replacement, or lease expiry.

The isolated full copy migrated from 1000019 to 1000020 in 0.062 seconds. All
124 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. The new removal history starts empty and all
769,643 existing file generations are preserved. The private receipt is
`.local/native-archive-rehearsal-20260930/file-publication-reconciliation.json`.

File-completion HTTP admission, the actual worker, source/gallery/receipt
integration, durable generated assets/notifications, and producer conversion
remain unfinished. No new worker starts automatically, `file_ingestion` remains
false, and production remains pinned to the compatible release.

The complete `make validate-fork` gate passed: generation, frontend checks and
all 528 UI tests, retained v3 extension contracts and 71 application operation
files, zero Go lint issues, and every Go unit/integration package. The SQLite
suite completed in 236.0 seconds, API tests in 136.3 seconds, and ingestion tests
in 125.3 seconds. The final checks include the query cursor cleanup correction.

## Collection, source attachment and album publication

`PreparedMedia.PublishIntake` now combines verified file/media publication with
collection provenance, attachment evidence and source album synchronization.
The captured collection revision must permit the root/path, and the current
definition must still be active and permit that location. Historical label edits
do not invalidate accepted provenance. A referenced capture must belong to that
collection revision and contain the attachment. Manual intake records provenance
without inventing a scraped account, capture or gallery.

An existing attachment media choice directs an unowned file to the selected
item, resolving canonical UUID adoption. The same choice can select one owner
of an intentionally shared file without moving or merging other owners. Existing
file ownership is preserved when it conflicts with the selected source item;
evidence is retained for review. Deleted selections require review instead of
recreation. Explicit unlinks, disabled albums, and manually excluded gallery
members remain intact. No publisher is implicitly assigned as a depicted performer.

Unambiguous observed attachments can establish their media association and add
arriving items to a shared source album. Replays retain one intake/evidence fact
and reuse the gallery. File, content proof, library media, collection provenance,
source choices, album changes, and durable job result share the transaction;
failure rolls them back together. Before-commit checks reject disabled collection
scope, removed media/file associations and changed source selections.

Focused tests passed for partial album arrivals, replay, manual membership
exclusion, direct unsourced intake, preserved explicit choices, conflicting
evidence, deleted/adopted selections, shared files with more than two owners,
selected-target priority over a separate byte match, historical/current scope,
and gallery-failure rollback including the job outcome. Indexed ownership
lookup was checked read-only against the full schema-20 library copy: both UUID
lookups and media/file joins used indexes; 1,000 repeated cached lookups took
0.0033 seconds locally. This is not an end-to-end ingestion throughput claim.
The private query-plan receipt is
`.local/native-archive-rehearsal-20260930/intake-owner-lookups.json`.

No schema migration is added in this checkpoint. Field policy and performer
defaults, durable generated assets/notifications, file-completion admission and
receipts, the actual worker and producer conversion remain unfinished.
The ingestion API still advertises `file_ingestion: false`; production remains
on the frozen compatible release.

The complete `make validate-fork` gate passed after the selected-media and lint
corrections: generation, frontend checks and 528 UI tests, retained v3 extension
contracts and 71 application operation files, zero Go lint issues, and every Go
unit/integration package. The SQLite suite completed in 268.4 seconds, ingestion
in 241.5 seconds, and API tests in 135.3 seconds.

## File event admission, worker and runtime

Migration 1000021 connects immutable file-event receipts to durable verification
jobs while preserving all source-capture receipts. File intake can reference an
acknowledged source attachment, or omit source data for manual media. Admission
checks the producer's Stash API scope, final root-relative file, collection
revision and attachment membership. Receipt failure rolls scheduling back.
Replaying the same event returns its original receipt, including after restart,
file disappearance or API token rotation; changed bytes under that event conflict.
Website credentials remain entirely with the scraper.

The v3 HTTP server exposes `file.completed` and a receipt status route when its
media tools are configured. File admission returns 202 and never calls a claimed
hash verified. The worker hashes/probes outside SQLite, renews its lease, and
commits verified registration with a resumable progress checkpoint. It then
creates missing previews and delivers scene/image and gallery hooks with stable
event identities. Errors remain pending/failed; retries retain the original
creation/link facts and do not create another item. Plugin delivery is at least
once. Cancellation waits for the worker before SQLite closes; expired attempts
are recoverable after restart. General edit-hook persistence remains unfinished.

Status separates `registration_committed` from `media_ingested`. An effect failure
or cancellation does not hide an earlier committed registration. Final validation
rejects changed roots/scopes, replaced files, obsolete generations and removed
ownership. Existing cover choices remain protected. File case detection now uses
filesystem identity instead of equal timestamps and skips letters without case.

Focused tests passed for duplicate and concurrent delivery, token rotation,
source/manual scope, rollback, restart, retry delays, cancellation, heartbeat
renewal, checkpoint expiry, descriptor changes before final commit, HTTP lifecycle,
actual image/video preview generation, and plugin retry identities/cancellation.
Schema-20 receipt promotion and anonymisation with file jobs also passed.

The isolated full copy migrated from 1000020 to 1000021 in 0.680 seconds. All
125 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. Reconciliation took 49.0 seconds. The private
receipt is `.local/native-archive-rehearsal-20260930/file-ingest-reconciliation.json`.

This development capability does not switch existing gallery-dl/n8n producers.
Native field policy and performer defaults, source-run leases/coalescing,
additional source adapters, producer outboxes, catalog import, review UI, and the
remaining transition phases are still required. Production remains pinned to
the compatible release; no production database or catalog was migrated.

Targeted race checks passed for worker publication/resumption/renewal, the HTTP
worker lifetime, and plugin cancellation (ingestion 25.4 seconds, API 3.8 seconds,
plugins 1.1 seconds). Receipt replay also remains available when media tools are
unavailable, while new admissions are rejected.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend checks and all 528 UI tests, retained v3 extension contracts
and 71 application operation files, zero Go lint issues, and all Go packages.
The API suite completed in 135.1 seconds, ingestion in 304.1 seconds, manager in
43.2 seconds, and SQLite in 285.6 seconds.


## Native metadata policies and ordinary scans

Migration 1000022 makes collection policy definitions and their immutable history
native database records. Automatic field decisions retain a foreign-key reference
to the applied policy revision. Scoped directory matching uses root/path indexes;
performer canonical/alias matching uses the native name index, reports collisions,
and resolves reviewed UUID defaults through merge/adoption redirects.

The shared evaluator constrains targets to curated native field schemas. It
preserves manual and legacy choices, explicit empties, source alternatives, and
existing organized state. Filename initialization requires an empty inherited
title and does not oscillate when another file joins an item. A newer capture of
the same post can update an inherited value; competing posts require review.
No publisher or source account becomes a depicted performer implicitly.

Verified intake pins the current policy at admission and applies it inside the
registration checkpoint. A later policy/scope edit produces a review outcome
without losing the verified media. Ordinary scene/image scan handlers use the
same evaluator and record direct/manual intake provenance without inventing a
scrape. Eligible unchanged files are evaluated during rescans. Ambiguous directory
rules do not select a winner by enumeration order.

Application-authenticated root/collection/policy endpoints, typed field discovery,
and preview/apply now exist under `/api/v3/archive`. Preview can show disabled
rules and protected alternatives; applying requires a current digest. Jq data
contains the selected source, entity, and relevant file context, with no internal
plugin settings. The plugin and native policy services share one bounded jq
implementation while retaining their different documented input limits.

The full library copy migrated from 1000021 to 1000022 in 0.059 seconds. All
125 existing tables matched by streaming semantic digest, with zero foreign-key
violations and no file growth. Reconciliation took 50.0 seconds. No policies,
decisions or intake facts were fabricated. The private reconciliation receipt is
`.local/native-archive-rehearsal-20260930/metadata-policy-reconciliation.json`.

The backend integration remains separate from the unfinished native settings and
review UI. Installed mapping conversion/comparison, ZIP-member folder policies,
review-queue persistence for scan conflicts, source-run coordination, producer
conversion, catalog import and the remaining transition phases are still needed.
Production and existing gallery-dl/n8n writers remain unchanged.

Focused tests passed for immutable/dry previews, protected alternatives, disabled
policies, explicit empty values, typed target validation, canonical/alias
collisions, redirected performer UUIDs, partial name matches, source precedence,
queued policy changes, actual scene/image scan handlers and idempotent rescans,
HTTP guarded apply, and anonymisation of populated policy history. The actual
directory-matching SQL uses `source_collection_directory` for its root/prefix
lookup; this was checked read-only on the full copy. A separate jq regression
confirms larger native input does not relax plugin input or shared output limits.

The final `make validate-fork` gate passed: backend generation, all 528 UI tests,
71 native application operation contracts, zero Go lint issues, and every Go
unit/integration package. API tests took 155.1 seconds, ingestion 332.4 seconds,
manager 43.6 seconds and SQLite 306.0 seconds. Targeted race checks also passed
for SQLite policy application, queued file-policy processing and HTTP preview/
apply (14.9, 8.2 and 7.3 seconds respectively). The final logs are
`/tmp/stash-native-transition/metadata-policy-final-validation.log` and
`/tmp/stash-native-transition/metadata-policy-race.log`.


## Native source-run coordination

Migration 1000023 persists source traversal requests, coalesced windows, worker
leases, checkpoints, attempt outcomes, target cooldowns and owner review actions.
This specialized traversal state is separate from immutable media-verification
jobs. Host and n8n producer identities can submit equivalent work for the same
configured collection. Expanding a running date range keeps its missing portions
without queuing another complete scan of the already claimed range. Disjoint
windows preserve their gaps, with explicit queue and interval capacity limits.

Claims serialize a collection, shared target URL and overlapping destinations;
root identity and resolved paths account for host/container mount mappings and
existing symlinks. Newest pending ranges and pending download work take precedence
over older ranges and enrichment. Every claim advances a fence. Lease expiry,
retry, cancellation and process restart cannot let an obsolete worker finish a
newer attempt. Same-window retries retain their cursor; changing the traversal
window cannot blindly reuse that cursor. Repeated failures remain durably deferred
until application-authenticated review. Repeated timers preserve retry delays.

Producer routes expose typed submit, scoped listing/status, claim, renewal,
progress, completion and attempt history. Policy fingerprints identify effective
worker configuration without accepting commands or storing website credentials.
API-token rotation preserves producer identity. Late source evidence delivery
does not alter run state. The application-only review endpoint uses the reviewed
revision for retry/cancellation. The protocol is documented in native-ingestion.md.

The isolated full library copy migrated from 1000022 to 1000023 in 0.070 seconds.
All 128 existing tables matched by streaming semantic digest; prior migration
history also matched, with zero foreign-key violations and no file growth.
Reconciliation took 154.4 seconds while other validation ran. All five new run
tables remained empty. The private receipt is
`.local/native-archive-rehearsal-20260930/source-run-reconciliation.json`; the migrated copy is
`native-source-run-final-rehearsal.sqlite`.

Focused tests cover real SQLite concurrency, duplicate admission/claim responses,
wider and disjoint windows, capacity, mount/directory exclusion, token rotation,
root-scoped history, retry/deferral, cancellation, restart recovery, expiry during
publication, HTTP operations, late capture delivery, and anonymisation. Targeted
race checks passed (SQLite 7.0 seconds, API 2.5 seconds, interval/path helpers
1.1 seconds). Existing gallery-dl/n8n processes and journals remain unchanged.

The external adapter, local outage coalescing/outbox, actual shared filesystem
locking and lease enforcement, legacy checkpoint/deferral migration, source
adapters, catalog import, UI/client conversion and later transition phases remain
required. A coordinator lease does not physically stop an old process; production
cutover must verify the converted workers pause before further source requests
and preserve download locks. No production schema or writer was changed.

The complete `make validate-fork` gate passed on the final per-item checkpoint
contract: backend generation, all 528 UI tests, 71 native operation contracts,
zero Go lint issues, and every Go unit/integration package. API tests took
160.7 seconds, ingestion 343.1 seconds, manager 44.9 seconds, and SQLite 318.9
seconds. The final validation log is
`/tmp/stash-native-transition/source-runs-final-validation.log`.
Final race results are in `/tmp/stash-native-transition/source-runs-final-race.log`.

## Producer retention and durable delivery

The supported Python package now lives under `integrations/gallery-dl`, with a
standard-library outbox, bounded HTTP delivery and an inspection/retry CLI.
Source payloads must satisfy the shared retention policy before persistence;
runtime extractor objects and known secret fields are removed from the copied
metadata. Added common fixtures cover unusable rendition URLs and nondecimal
dimensions so Go and Python select the same retained preview.

The outbox has its own explicit SQLite lineage, stable origin/producer binding,
FULL WAL transactions, source-before-file dependencies and bounded queued bytes
and events. Concurrent drainers use expiring fenced leases. A server receipt must
match the event identity, digest, collection revision, root, run and kind before
acknowledgement and payload release commit together. Receipt rows remain for
replay and dependent events. Restart, lost HTTP responses and Stash API token
rotation preserve event identity. Transient errors back off durably; rejected
events retain their payloads for explicit review. File admission remains distinct
from completed verification. No website credential is stored or managed here.

The Python tests cover shared retention, strict input, capacity, process death,
concurrent claims, receipt dependency/replay, endpoint binding, header-only token
rotation, redirects, capability outages, partial responses, mismatched receipts
and disabled file processing. The real Go HTTP/SQLite test runs the Python client,
loses a response after the commit and confirms receipt replay plus independent
rejection. Targeted interoperability passed in 1.5 seconds. The build workflow
and `make validate-fork` now include producer validation; Python 3.12 or newer is
required. All 18 Python tests passed on Python 3.14 and in the installed gallery-dl
Python 3.12 environment. Building a wheel and installing its CLI into an isolated
environment also passed. The final real HTTP test and shared retention corpus
passed under Go's race detector (7.0 and 1.1 seconds); final Go lint reported zero
issues. Python source reads are registered with Go's test cache so producer edits
invalidate the interoperability result.

Actual gallery-dl hooks, native source-run leases, filesystem locks, launcher
conversion, local outage run coalescing and catalog import remain unfinished.
Integration inspection also found that ordinary single-media Reddit posts need
explicit attachment evidence in the server adapter before their file events can
be linked; the existing capture parser handles declared Reddit galleries and
Twitter media manifests. Do not omit attribution or invent albums to bypass that
remaining work. Host/n8n configuration, production data and the frozen compatible
release remain unchanged. This increment changes no native database schema.

The complete `make validate-fork` gate passed: backend generation, all 528 UI
tests, 71 native operation contracts, all 18 producer tests, zero Go lint issues
and every Go unit/integration package. API tests took 177.8 seconds, ingestion
345.0 seconds, manager 44.1 seconds and SQLite 323.1 seconds. A subsequent final
test-cache dependency update passed targeted race tests and Go lint. Logs are
`/tmp/stash-native-transition/producer-outbox-final-validation.log`,
`producer-outbox-final-race.log`, `producer-outbox-final-lint.log`, and
`producer-outbox-python312.log` in that directory. The isolated wheel/CLI check
is under `/tmp/stash-native-transition/producer-wheel.Tll9Rf`.

## Gallery-dl lifecycle and source ownership

The development producer SDK now connects actual gallery-dl download/skip hooks
to native source leases and its durable outbox. Captures are queued before
download. Final files are flushed, hashed and queued with their capture dependency
before gallery-dl's archive is acknowledged. Existing unarchived files can retry
failed metadata/exec processing; unresolved archive skips retain source evidence
without inventing a completed file. Atomic metadata writes and the existing
GIF-to-MKV output convention are supported. UTF-8 filename budgeting preserves
source IDs and reserves space for configured temporary files and yt-dlp formats.

Run responses include the target URL and destination prefix from their immutable
collection revision. Historical responses keep that definition after collection
edits. The worker verifies its reviewed local root identity and prefix, shares
destination locks across writers, disables asynchronous source prefetch and
checks ownership before traversal and each source HTTP attempt, including retries.
Lease deadlines use server time and a conservative monotonic budget. A current
file can finish queuing after ownership loss; subsequent source work pauses.
Bounded cursor replay preserves the prior checkpoint until it is encountered.

The server attachment parser now accepts direct single-media Reddit evidence,
including explicit video stream URLs and validated crosspost context. Single
items can link to library media without creating a gallery. The Twitter adapter
preserves original attachment membership before gallery-dl expands video previews
or transforms tweet metadata, retaining each output's attachment ID. Source order
and identity do not come from local output numbers or filenames. The parser policy
is `captured-attachments-v2`; the retained payload policy is unchanged.

Actual runtime fixtures cover download/skip paths, archive recovery, failed or
missing postprocessors, capacity exhaustion, transformed paths, source retry
fencing, missing resume cursors, destination checks and both raw/transformed
Twitter payloads. A real Python-to-Go HTTP test covers source-run submission,
claim exclusion, renewal, checkpoints and completion in addition to durable
delivery/replay. Backend publication tests confirm that a verified single Reddit
attachment links without an album. The isolated pinned runtime installs with
`make pre-producer`; CI and the full fork gate require the runtime tests.

All 59 Python tests passed on Python 3.12 and 3.14. Targeted Go race tests passed for the
parser, pinned definitions, single-attachment publication and real HTTP client
(1.0, 15.2, 7.5 and 7.2 seconds respectively). No native migration is added.
The production host/n8n wrappers, website credentials, catalog writers and frozen
compatible deployment have not changed.

The SDK is not yet a production launcher. Effective window/configuration
handling and policy fingerprints, durable offline run requests, metadata-only
enrichment, other platform/external-host adapters, multi-entry yt-dlp output
association, legacy checkpoint migration and host/n8n/recovery conversion remain
required. Catalog import and later transition phases are also unfinished.

The complete fork gate passed: backend generation, 528 UI tests, 71 native
operation contracts, zero Go lint issues and every Go unit/integration package.
API tests took 173.4 seconds, ingestion 345.1 seconds, manager 44.8 seconds and
SQLite 318.8 seconds. The producer suite had 58 tests during that gate; the added
source HTTP-retry regression and final adapter passed all 59 afterward on both
Python versions. Logs are `producer-lifecycle-final-validation.log`,
`producer-lifecycle-final-python.log`, `producer-lifecycle-python312.log` and
`producer-lifecycle-race.log` under `/tmp/stash-native-transition`.

## Offline source requests and admission receipts

The producer now records scheduled source requests locally without requiring
Stash to be online or starting a downloader. Requests with the same collection
revision, operation, policy and cooldown coalesce overlapping date windows while
preserving gaps. New downloads take priority over enrichment, with newer ranges
first. Once a request is claimed for submission, its UUID and exact bytes remain
immutable across backoff, worker interruption and lost responses. Wider requests
retain only the ranges outside that frozen submission. A review state holds that
configuration until an explicit retry; repeating a timer cannot bypass it.

Optional caller tickets identify one scheduling execution. Replaying a ticket
and its original absolute window after a lost command response returns the
existing intent, even after server admission. Reusing the ticket for different
work conflicts. Local history is paginated and records admission separately
from actual run completion. The native POST response now echoes its committed
request UUID and advertises submission receipts as a capability, allowing the
producer to release exactly the acknowledged request.

Producer database schema 2 adds request groups, frozen submissions and caller
tickets transactionally. Migration fixtures retain schema-1 event bytes,
dependencies, receipts and in-flight delivery ownership. Concurrent opening,
wrong-destination rollback and interrupted migration are covered. No native
Stash schema migration is added. Shared Python/Go window fixtures now cover
overlap, adjacency, gaps, UTC normalization and millisecond precision, including
offsets that carry dates outside the supported year range.

All 75 Python tests passed on Python 3.12 and 3.14. Tests cover offline timers,
concurrent submission claims, leases, process death, capacity, caller replay and
review. The real Python-to-Go HTTP fixture loses a committed run-submission
response, reopens the producer database and recovers the same request and run.
Focused Go race tests passed for source-window algebra and HTTP interoperability.
The full fork gate passed backend generation, 528 UI tests, 71 native operation
contracts, producer tests, zero Go lint issues and every Go unit/integration
package. API tests took 161.3 seconds, ingestion 336.8 seconds, manager 43.9
seconds and SQLite 309.6 seconds. Logs are `source-request-final-validation.log`,
`source-request-python312.log`, `source-request-focused-go.log` and
`source-request-race.log` under `/tmp/stash-native-transition`.

Effective worker configuration and window enforcement, other source adapters,
metadata enrichment and actual host/n8n/recovery conversion remain. The live
launchers and catalog writers have not switched, and production remains pinned
to the compatible release. Catalog import and later transition phases remain
unfinished.

## Claimed source windows and postprocessor boundaries

The download adapter now owns enforcement of the claimed source-post window.
The lower bound is inclusive and the upper bound exclusive. Reddit's raw
publication timestamp and Twitter's Snowflake retain the milliseconds discarded
by gallery-dl's formatted dates; raw and transformed Twitter payloads agree.
Unknown dates stop before file processing. Older pinned posts are skipped
without ending traversal, and linked child downloads inherit the accepted
parent post's date instead of using an unrelated child upload date.

Per-extractor date configuration replaces inherited limits without editing the
shared configuration file. Other filters, skip rules, pacing and authentication
remain with the configured worker. Keywords cannot overwrite the source identity
or date used for the window. Postprocessor initialization waits for an accepted
post. Initial and subsequent proposed directories are formatted with gallery-dl's
own path implementation and checked against the claimed destination before init
or post callbacks run.

Real gallery-dl fixtures cover Reddit pagination, raw/transformed Twitter albums,
subsecond boundaries, all-outside results, pinned posts, parent/child date
semantics, retained filters and invalid destinations before callbacks. All 85
producer tests passed on Python 3.12 and 3.14. The full fork gate passed backend
generation, 528 UI tests, 71 native operation contracts, zero Go lint issues and
all Go unit/integration packages. It ran 84 producer tests before the final
initialization/directory regression; the final 85 passed on both runtimes after
that addition. API tests took 158.6 seconds, ingestion 332.7 seconds, manager
43.8 seconds and SQLite 306.1 seconds. Logs are
`source-window-final-validation.log`, `source-window-final-python.log` and
`source-window-python312.log` under `/tmp/stash-native-transition`.

Read-only inspection of the effective host and n8n Reddit/Twitter configuration
confirmed that their source keywords pass the new check, and both still resolve
the existing prepare/after/skip catalog writers. Those launch paths have not
switched. Full configuration fingerprints, launcher conversion, additional
source adapters and the later migration phases remain unfinished. No native
schema change or production deployment is included here.

## Portable worker profiles and attempt execution

The producer now accepts a reviewed `stash-gallery-worker-v1` profile. Portable
gallery-dl settings, local path bindings, reviewed helper assets and private
website-access references have separate roles. Policy identity includes the
settings, asset digests, pinned gallery-dl/yt-dlp runtime and adapter source;
host/container mount paths and rotated website logins do not change it. Roots
and shared lock directories require explicit device/inode identities. Python
postprocessor functions must name the reviewed asset exactly; changed helpers
and legacy catalog writer definitions stop source work. Trusted local processors
remain executable worker code, not a sandbox supplied by Stash.

Website credentials are read locally from existing JSON settings through a
JSON Pointer or a named environment reference. They stay with gallery-dl and
are excluded from policy serialization and the outbox. Stash ingestion API
tokens still have their separate environment reference. Configuration activation
rejects concurrent use of gallery-dl's process-global settings and restores the
previous settings when the attempt ends.

The CLI can validate a profile, use its digest when queuing a request, and execute
one admitted download attempt. Execution checks capabilities, root, operation,
policy and live ownership before extraction. A separate drainer owns its SQLite
connection and delivers captures while downloading continues. Source failure,
storage/capacity failure, changed assets, interruption and lost ownership retain
retry/deferred/paused states. Current-file evidence remains durable. Python and
subprocess logs go to stderr, preserving the JSON command result on stdout.

After a lost finish response, the worker checks the server's attempt history for
the exact producer, owner, fence and outcome; it never infers completion merely
because ownership disappeared. Successful source attempts remain distinct from
remaining source windows, event admission and verified media intake. Final lease
responses now validate the immutable definition and allowed resulting states.

The real Python-to-Go fixture runs the download adapter against the HTTP router
and native SQLite, delivers a capture during download, loses a committed finish
response and recovers it, then reopens the producer database and admits the
dependent file. The native file job remains queued and no library file is
declared verified. Unit coverage also exercises portable paths, secret rotation,
asset changes, failure outcomes, exact attempt matching, drainer restart and
stdout isolation.

All 102 producer tests passed on Python 3.12 and 3.14. The full fork gate passed
backend generation, 528 UI tests, 71 native operation contracts, producer tests,
zero Go lint issues and all Go unit/integration packages. Focused API race checks
passed. After tightening the exact helper-asset reference, both Python suites
and the real Python-to-Go fixtures passed again. Logs are
`worker-final-validation.log`, `worker-final-python.log`,
`worker-final-python312.log`, `worker-http-final.log` and `worker-http-race.log`
under `/tmp/stash-native-transition`.

Actual host/n8n configuration conversion and launchers, additional source
adapters, enrichment, catalog import and later transition phases remain. The
production deployment, website login configuration and existing catalog writers
have not switched. This checkpoint adds no native or producer schema migration.

## Local configuration conversion and source-scoped policies

`stash-ingest-config` now converts ordered gallery-dl JSON layers into a new,
inactive worker profile. Its merge retains object insertion order and replaces
arrays/scalars as the pinned gallery-dl runtime does. Recognized catalog
prepare/complete hooks are removed from named, typed and inline definitions;
unknown legacy callbacks stop conversion. The converter preserves archive
segment-list formatting and existing download IDs, skip/early-stop policy,
original-quality flags, pacing and remaining processors. Local helper scripts
receive checked asset digests. Publication flushes a temporary file and links it
exclusively into place with private permissions; it never overwrites an input
configuration or an existing output.

Website-access fields retain references to the original private files. Layered
headers/cookies merge from their respective JSON Pointers, including ancestor
replacement semantics, without copying secret values into the profile. Known
yt-dlp credential argument values use references while other arguments, including
format selection, remain part of policy identity. Conditional filename/directory
map order is now retained both when saving a profile and computing its digest;
sorting those maps would change first-match behavior without changing the old
digest.

Optional source categories constrain the root extractor before source work.
Twitter profiles omit unrelated service settings. Reddit profiles can do the
same with its finite supported child-host whitelist, retaining child base and
parent-specific settings. Unknown dependency graphs retain their configuration.
Unused named processors are omitted from scoped profiles. This matters for the
actual deployment: host ThisVid recovery refreshes cookies, while n8n asks for a
host refresh and exits. That intentional difference remains in full profiles
but no longer splits otherwise equivalent Reddit/Twitter policies.

Read-only conversion of the actual host configuration and merged n8n configuration
produced matching ordered portable settings and helper digests for Reddit and
Twitter. The n8n check ran in an isolated copy of its image, with networking
disabled and read-only mounts. Both mounts report the same media/lock identities.
The installed n8n gallery-dl reports version 1.32.15.dev0 but source commit
`0d2966061c5c5138a6961da26eab5583861962a1`, while the supported worker requires
`c40eb2a42fbaa1d26a2bb7c96804b7f47d1f73f8`. Its native profile validation rejects
that mismatch as intended. The runtime must be aligned before activation.

Private staging artifacts are in `.local/native-worker-conversion-20261001`.
Their shared root UUID is explicitly unregistered and for rehearsal only;
regenerate deployment profiles against the actual registered root after the
cutover gates. The packaged converter was installed and exercised in the isolated
producer environment. No live config, wrapper, workflow, credential, catalog or
production deployment was changed. Host/n8n/recovery launcher conversion,
additional source adapters, enrichment, catalog import and the later transition
phases remain unfinished.

Validation passed the full `make validate-fork` gate: 528 v3 tests, native
contracts for 71 operation files, Go lint and the complete Go test suite. All 113 producer tests passed
on Python 3.12 and 3.14. After the final argument-type guard, both Python suites
and the real HTTP producer/download-worker fixtures passed again. Logs are
`config-conversion-final-validation.log`, `config-conversion-final-python.log`,
`config-conversion-final-python312.log` and `config-conversion-http-final.log`
under `/tmp/stash-native-transition`. This checkpoint adds no schema migration.

## Scoped dispatch and a packaged n8n worker

Workers can now discover download work using `POST /api/v3/ingest/runs/ready`.
The bounded active-run index filters by the token's collections, exact media
root, reviewed policy and server-side eligibility time. Results contain only
run UUIDs and sequence cursors. Discovery never claims work: source execution
still requires the existing fenced claim, including definition, destination and
cooldown checks. Expired leases go through normal recovery and retry delay;
explicit deferrals remain outside automatic dispatch.

`stash-ingest dispatch --profile FILE` drains one event batch, submits one
offline request and scans one candidate page, executing at most one attempt.
Producer schema 3 stores pagination and discovery backoff in the same outbox.
Cursor revisions handle competing dispatchers, and advancing before execution
preserves fairness across crashes and pages of busy runs. Reaching a page end
wraps the cursor for a subsequent pass; it does not certify queue completion.
Existing schema-1/schema-2 payloads, receipts, leases, requests and tickets are
preserved transactionally. The Stash database schema remains unchanged.

`integrations/gallery-dl/Containerfile.n8n` now installs the pinned worker into
`/opt/stash-ingest` on an explicitly selected custom n8n base. It retains n8n's
entry point, PATH and system interpreter. The isolated rehearsal used base image
`2fcb84852f4a6dfc898796126764638ee43c2183a961061e42bcfe4103404a8c`; the resulting
local image is `localhost/stash-n8n-native-rehearsal:20261001`. Its worker validates
the supported gallery-dl commit rather than accepting the older system build's
identical version label. With the real configuration/helper/media mounts read
only and networking disabled, its Reddit/Twitter profile hashes and complete
worker runtime identity match the host profiles. No source requests were made.

The full fork gate passed: 528 v3 tests, native contracts for 71 operation files,
Go lint and all Go tests. All 122 producer tests passed on host Python 3.12 and
3.14, and against the installed package in the n8n rehearsal image. The real
HTTP fixture now invokes the dispatch CLI after outbox restart, verifies queued
submission/discovery, recovers a lost finish response and retains file intake
as a separate durable result. Migration fixtures cover populated schema-2
queues as well as concurrent schema-1 upgrades and transaction rollback.
Logs are `dispatch-final-validation.log`, `dispatch-python312.log`,
`dispatch-n8n-python-final.log` and `dispatch-n8n-profile-validation.json`
under `/tmp/stash-native-transition`.

A read-only inventory of n8n's current and published workflow graphs is staged
at `.local/native-launcher-inventory-20261001.json`, with command hashes and
connections rather than raw commands or credentials. Workflow conversion still
needs to retain caller receipts and account backfill decisions. Host/n8n
launchers, activation, additional adapters, enrichment, catalog import and later
phases remain unfinished. The production image tag, services and workflow
database were not changed.

## Completion tied to original caller requests

Caller tickets now retain the source submissions assigned to each part of their
original time window. Assignments are written in the same transaction as frozen
requests, including when a new caller shares an existing in-flight request. A
later successful rescan cannot make an earlier cancelled ticket appear complete.

`stash-ingest ticket-status UUID` validates the original admission identities and
reads each assigned native run's actual completed windows. It reports remaining
coverage, deferral, cancellation, review and API outages, and exits successfully
only when the entire requested source range is covered. A wider shared run may
still have other work. File intake remains a separate status; source completion
does not assert that its media has finished importing.

Producer schema 4 adds ticket assignments and unassigned ranges. Its transactional
migration reconstructs the first covering submissions from the old tickets'
original request sequences, preserves later rescan boundaries and retains
existing event, lease, receipt and dispatch state. Both assignment and migration
use bounded pages. The Stash database schema remains unchanged.

All 134 producer tests passed on Python 3.12 and 3.14. Migration coverage includes
populated schema-3 queues, multiple pages of waiting tickets and rollback on
invalid input. The real HTTP fixtures verify queued/running/completed ticket
states after lost admission responses and check the CLI after restart while
media intake remains queued. The full `make validate-fork` gate passed: 528 v3
tests, native contract validation, Go lint and all Go tests. Logs are
`ticket-completion-python312.log`, `ticket-completion-http-final.log` and
`ticket-completion-final-validation.log` under `/tmp/stash-native-transition`.

Inspection of the current n8n backfill wrapper found that it records completion
from process exit status. Launcher conversion must instead use these original
ticket ranges and preserve existing permanent backfill decisions during import.
The host/n8n launchers and live services have not changed; the prior n8n rehearsal
image remains at `5af18a486` and must be rebuilt for this producer revision before
activation. Additional adapters, catalog import and later phases remain open.

Documentation now calls credential revocation **Stash API-token revocation**.
It controls a producer's access to Stash; website passwords, cookies and login
configuration remain with gallery-dl in the host/n8n worker environments.

## Scoped lookup for actual source URLs

`POST /api/v3/ingest/collections/lookup` now resolves up to 50 exact source URLs
against the producer's permitted collection IDs and current media-root binding.
Results group candidate UUIDs, revisions and states by the requested URL. They
exclude labels, account associations, directory names and local mount paths.
Every authorized duplicate remains visible; unrelated matches cannot consume
a page and hide a permitted candidate. Historical URLs do not redirect work to
a changed target; a moved collection does not match requests under its old root grant.

The Python adapter and `stash-ingest lookup-collections` validate the complete
response before returning bindings. Missing visible matches, duplicates and
inactive definitions remain explicit. This operation neither creates collections
nor queues source work. The real HTTP metadata and download fixtures now resolve
their source URLs through the API before submitting native requests, including
the CLI entry point for download callers.

The full `make validate-fork` gate passed, including 528 v3 tests, native contract
validation, Go lint and all Go tests. All 139 producer tests passed on Python
3.12 and 3.14. HTTP coverage includes 55 permitted duplicates among unrelated
matches, unbound collections, inactive states, changed targets/roots, rejected
credentials and API-token revocation. Logs are `collection-lookup-http-final.log`,
`collection-lookup-python312.log` and `collection-lookup-final-validation.log`
under `/tmp/stash-native-transition`. No database schema version changed.

A read-only check of the installed timers confirms that regular Reddit scans
include saved posts, and the weekly scan adds `--date-min-relative '1 week ago'`.
The saved lists currently contain 497 distinct first-token entries for Twitter
and 421 for Reddit. Those lists exceed the current 128-collection token limit;
bulk worker access and durable unresolved caller/list requests must be addressed
before the launchers switch. Collection registration/import, preservation of
full-history policy, n8n receipt conversion and the remaining transition phases
are still required. Production services and workflow graphs remain unchanged.

## Explicit media-root grants for bulk producers

Native schema 1000024 adds root grants for a producer's Stash API token. A grant
covers all registered collections at that logical root, including subsequent
additions, so the existing 497-entry Twitter and 421-entry Reddit lists no longer
require a grant per source. Named collection/root grants remain available and
retain their existing authority. Tokens hold at most 128 combined grant records;
neither form grants source administration. Root grants exclude unbound metadata.
Website logins and cookies remain with gallery-dl/n8n.

Capture/file admission, receipt reads, collection lookup, source-run admission,
dispatch and run history use the same permissions. Historical admission replay
checks the recorded root after a collection moves; a token for only the new root
cannot acquire access to the old run. Rejected admissions roll back. Issuance
requires active registered roots, and capabilities expose the explicit grants.
Root grants are created only through administration, never schema migration.

Collection lookup applies permission filters before its per-URL limit. It returns
up to 128 candidates with `has_more` when additional matches exist. The Python
client preserves that ambiguity and allows a bounded 4 MiB lookup response;
other responses retain their 1 MiB limit. The new target/root index supports
these lookups without returning unrelated collection information.

The migration preserves receipt values, including accepted file-job references.
SQL guards validate either form of permission and retain grant evidence while
receipts reference it. Populated fixtures verify named-token preservation,
receipt replay, queued file work, rejection of forged unbound receipts, root
movement, later collection registration, revocation and anonymisation. The real
Python HTTP download fixture now uses a root-only token through capture and
dependent file admission.

A concurrent outbox startup test exposed SQLite journal-mode lock contention.
Commit `88e87e162` adds a bounded retry without replacing the queue. A real reader
lock regression verifies preservation of pending events, leases and receipts.
All 141 producer tests pass on Python 3.12 and 3.14.

A separate 1,473,081,344-byte copy of the schema-1000023 rehearsal migrated in
0.132 seconds. Streaming semantic comparison checked all 133 existing tables:
no differences, no foreign-key violations, all 1,542,050 archive identities and
770,734 metadata baselines retained, and no invented root grants. Reconciliation
took 140.440 seconds; evidence is
`.local/native-archive-rehearsal-20260930/root-grant-reconciliation.json`.
The populated receipt fixtures cover values absent from this production-derived
copy, whose native producer/job tables remain empty.

The full `make validate-fork` gate passed: 528 v3 tests, native contract checks,
all 141 producer tests, Go lint with zero issues and the complete Go suite.
Logs are `root-grants-final-validation.log`, `root-grants-host-python.log`,
`root-grants-preservation-final.log`, `root-grants-outbox-contention.log`,
`root-grants-full-copy-migration.log` and
`root-grants-full-copy-reconciliation.log` under `/tmp/stash-native-transition`.

Durable caller/list requests, source registration/import, actual host/n8n
launcher conversion, full-history policy preservation and later transition
phases remain unfinished. No production service, configuration, workflow,
website login or live catalog was changed.

## Durable caller snapshots before collection lookup

Producer schema 5 adds `source_calls` and `source_call_targets`. A caller records
its execution UUID, ordered URL list, reviewed policy, root, operation and
absolute time window before network access. The command options have a stable
digest; retries retain the original snapshot without reopening changed or
missing list/profile files or recalculating relative time bounds. Concurrent
first requests keep the snapshot that commits first. Different options require
a different caller UUID.

`queue-sources` accepts a UTF-8 URL list and an absolute lower bound or a lookback
interval. `resolve-sources` binds up to 50 pending URLs through scoped native
lookup. Resolution uses fenced leases and retained backoff, prioritizing downloads
and newer cutoffs before backfill/enrichment. Each chosen collection/revision
and deterministic source ticket commit in the same outbox transaction; a failed
commit leaves neither half behind. Existing queue coalescing shares equivalent
work between different callers. Bound targets never silently change collection.
Missing, ambiguous, disabled or retired matches remain in review; `retry-call`
retries only those unresolved targets after their definitions/access are fixed.
The dispatcher now resolves one bounded page before source admission/discovery.

`calls-status` provides local counts and 50-target pages. `call-status` checks all
original source tickets, verifies their frozen definitions/root and requires
confirmed coverage of every requested window. It shares a bounded run-status
cache only within that inspection and limits detailed issue output to 20 URLs.
It cannot report successful media intake or let a later rescrape complete an
earlier cancelled caller request. Temporary API failure remains unavailable.

The schema-4 promotion fixture preserves every existing table row, including
pending events, active leases, receipt/ticket assignments and dispatch cursors.
Unknown conflicting tables roll back migration without recreating the outbox.
Existing schema-1/2/3 promotion tests still pass. No Stash database migration or
production queue conversion is part of this increment.

The required `make validate-fork` gate passed: 528 v3 tests, native contracts,
Go lint and all Go tests. The final producer suite passes all 154 tests on
Python 3.12 and 3.14. Coverage includes 500-source lists, offline admission,
frozen cutoffs, concurrent retries, expired resolution leases, atomic
binding/ticket rollback, priority, review, capacity, original-root validation
and whole-call completion. Both real HTTP producer fixtures pass without cached
Go results. The download fixture now records a URL-list caller through the CLI,
replays it after deleting the input file, restarts, resolves/submits/executes it,
recovers a lost finish response and checks both ticket/call status while actual
media intake remains queued. Evidence is in `source-calls-final-validation.log`,
`source-calls-python-final.log`, `source-calls-host-python-final.log` and
`source-calls-http-final.log` under `/tmp/stash-native-transition`.

The existing host helpers and n8n workflows still need their handle/list parsing,
Reddit mode/date expansion, full-history archive/skip policies and receipt/result
contracts converted to these caller records. Source registration/import,
additional extractors, actual launcher activation and subsequent transition
phases remain unfinished. Production services and data remain on the frozen
compatible deployment. Parent commit `f4edf75b7` passed all three CI workflows.

## Staged host launchers preserve scrape inputs

`stash-ingest-twitter` and `stash-ingest-reddit`, with matching scripts under
`integrations/gallery-dl/bin`, now record the installed host helpers' input
formats as durable source calls. Twitter keeps handle/ID/profile normalization,
first-token/comment handling and list order. Reddit keeps sorted accounts and
communities, exact profile/search URL expansion, new/top modes, saved targets
and date filters. Absolute dates take precedence; relative dates retain the old
gallery-dl boundary and freeze at first recording. Systemd invocation IDs derive
stable call UUIDs; other retrying callers supply an explicit UUID.

Default exit 0 acknowledges only local recording. Strict inspection returns
pending until every original source ticket succeeds and still directs callers
to separate file-intake receipts. Reviewed profiles replace arbitrary downloader
flag passthrough. Invalid/empty source lists cannot claim successful completion.
`stash-ingest-config --full-history` publishes a separate global `skip=true`
profile; full-history and Reddit top launchers require it. This preserves the
old override of extractor/child `abort:4` rules, including archived-file skipping,
while retaining any explicit date minimum.

A read-only comparison against the installed scripts' parsing functions and
actual saved lists matched every URL and its order: 497 Twitter targets, 416
Reddit users and four communities; Reddit new expands to 836 URLs (837 with
saved), top to 1,672 (1,674 with saved). No list lines were ignored. The comparison
and source-file hashes are recorded privately in
`.local/native-worker-conversion-20261001/host-launcher-comparison.json`.
Separate host full-history profiles were staged there and validated against the
actual Python 3.12 gallery-dl configuration, helpers and mount identities. Input
configurations were unchanged. These use the existing unregistered rehearsal
root and have not been activated.

All 164 producer tests pass on Python 3.12 and 3.14. The real Go/Python HTTP
download fixture now also executes the staged Reddit script, replays its call
after deleting the input list, restarts the outbox, resolves/submits/downloads,
recovers a lost finish response, and verifies source completion independently of
queued file intake. The focused HTTP fixture passed in 7.674 seconds.
The full `make validate-fork` gate also passed: 528 v3 tests, native contracts,
Go lint and all Go tests (API 187.977 seconds, ingest 338.767 seconds, SQLite
317.017 seconds). Logs are `host-launchers-validation.log`,
`host-launchers-python312.log`, `host-launchers-focused.log`,
`host-launchers-converter.log` and `host-launchers-http-initial.log` under
`/tmp/stash-native-transition`. This increment adds no database migration.

n8n conversion remains outstanding. Its old runner's result receipts and
permanent backfill decisions must be retained before switching workflow commands.
A read-only journal inspection found 1,329 completed components (497 Twitter,
416 Reddit new and 416 Reddit top), one per-scan completion and no legacy-skip
rows. Of those component records, 1,328 contain user-confirmation provenance;
40 explicitly say exhaustive history was unverified under the old `abort:4`
policy but accepted as complete. Import must preserve those decisions and their
provenance without inventing verified source-window coverage or rescraping them.
The old skip-table format must remain supported as historical input even when
this snapshot has no rows. Production launchers, workflows and services remain
on the compatible deployment; parent commit `66d7e91e2` passed all three CI jobs.

## Native permanent backfill decisions and journal import

Native schema 1000025 adds `source_backfill_decisions` and foreign-keyed original
request proof. Historical completion and deliberate skips retain the entire
source record, including the exact result JSON and user-confirmation provenance.
Stable UUIDs derive from an input-database UUID and the original table/key;
replaying unchanged rows preserves the original decision, while changed evidence
conflicts. Migration creates no decisions, runs or proof for existing rows.

The API now exposes root-scoped compact status and native completion proof.
Account-wide reads/writes require an explicit producer root grant; a collection
grant alone does not widen. Application-authenticated endpoints import historical
assertions in atomic bounded batches and expose retained evidence. The component
definitions preserve the eight existing Reddit/Twitter n8n modes. Qualified
source-account lookup is independent of performer identity or ownership.

Native completion checks the authenticated producer's original request hashes,
root, exact target URLs, policy and actual completed ranges for the entire
component. Queue admission, partial coverage, another account's URLs, missing
requests and fabricated windows cannot become completion. Imported acceptance
remains distinct from native source-run proof; neither certifies media intake
or exhaustive availability from a source website. Startup checks preserve proof
integrity, and anonymised exports remove this private operational history.

`stash-import-backfills` opens a journal snapshot read-only, validates the two
recognized account-backfill table shapes and all rows before network writes,
then submits at most 50 records/4 MiB per batch through the application API.
Both passes use one SQLite read snapshot. Stable returned IDs and outcomes are
checked before acknowledging a batch. The real Go/Python HTTP fixture commits a
batch and drops its response, then verifies successful replay with 52 decisions,
no duplicates, no fabricated source runs and unchanged source bytes.

The isolated full-copy rehearsal is
`.local/native-backfill-rehearsal-20261001/native-backfill-rehearsal.sqlite`.
Schema 1000024 → 1000025 took 0.071 seconds on the 1,473,081,344-byte copy.
Before importing, all 134 existing tables matched their source semantically;
the two new tables were empty and foreign-key violations were zero. The
reconciliation took 53.521 seconds and found no size growth.

The actual journal snapshot then supplied 1,329 completion records: 497 Twitter
and 416 each for Reddit new/top. Import and replay took 0.299 seconds through the
core repository services. Every original field and result-JSON byte matched;
replay and reopening preserved every decision. Source runs, source requests and
native proof rows stayed empty. Indexed status checks on this full-library copy
had median 0.01869 ms, p95 0.02498 ms and maximum 0.14606 ms in one read transaction;
these are local repository timings, not network or UI latency guarantees.
The snapshot has no legacy-skip rows, so synthetic fixtures cover those semantics.

Private evidence includes `review.json`, `input-records.json`,
`backfill-migration-reconciliation.json` and `backfill-import-reconciliation.json`
in that rehearsal directory. The preserved schema-24 copy and live journal were
not changed. Parent commit `7852533f1` passed all three CI workflows.

The full `make validate-fork` gate passed: 528 v3 tests in 91 files, native
contracts covering 71 application operation files, all 167 producer tests,
Go lint with zero issues and all Go tests (API 205.548 seconds, ingest 350.598
seconds, SQLite 329.962 seconds). The same 167 producer tests also passed on the
host's Python 3.12 runtime. Focused store/API checks and the real HTTP importer
fixture passed separately. Logs are `backfill-final-validation.log`,
`backfill-python312.log`, `backfill-focused-v2.log`,
`backfill-http-import-v2.log` and `backfill-real-import-rehearsal.log` under
`/tmp/stash-native-transition`.

The installed n8n runner still needs to consume this history, freeze/persist its
caller receipts, inspect original source tickets and submit completion proof.
Per-scan completions, deferred/ignored work, the other catalog families, source
registration and subsequent transition phases remain outstanding. This is not a
live migration or deployment; production remains on the frozen compatible image.

## Durable native n8n backfill callers and staged workflow conversion

`stash-ingest-n8n` records the existing eight account-backfill modes using stable
producer/workflow/execution/node/item identities. The first invocation freezes
the account spelling, exact targets, profile policy and full-history cutoff.
Replay returns the same token without reopening changed or missing profile
inputs. Normal record success means local acceptance; strict inspection remains
pending until native completion or an imported acceptance/deliberate skip.

Producer schema 6 adds `backfill_calls` to the existing outbox. Before any source
call exists, a root-authorized native history lookup must succeed. A needed
decision and its child source snapshot commit together. Historical completion
and skip create no source tickets. Fenced leases, bounded capacity and backoff
retain interrupted checks across restart. Migration preserves all populated
schema-5 tables, including pending/bound source calls and their original
submissions; unknown table collisions roll back instead of replacing history.

An active call remains tied to its original source tickets. Another call's later
account history cannot complete or replace unfinished/cancelled work. Completion
requires every target's assigned windows, and the adapter saves its exact proof
before sending it. A lost server response replays the same proof/decision UUID.
The dispatcher advances these calls alongside existing source requests, and its
exit status stays pending when a backfill check or completion is outstanding.
Finished workflow receipts remain local and separate from media-intake receipts.

`stash-ingest-n8n-config` stages the known command/inspection graph contract in
a new private file. It preserves workflow/node IDs, credential references,
unrelated parameters, inputs and existing error/success outputs. Converted
commands include stable execution context. Pending results retain their token,
enter a 90-second persisted Wait and inspect that same call again. Unknown
command/connection/result changes require review rather than speculative edits.

Read-only inspection found three active backfill child workflows. Each current
graph exactly matched its published version. Their three parent references all
explicitly wait for subworkflow completion. Private input exports, staged graphs
and review manifests are in `.local/native-n8n-adapter-20261001/`; the live n8n
database was not changed. The actual installed n8n evaluator resolved 62 command
and result expressions across the three conversions, including rejected account
and token text. Its real Wait implementation checkpointed all three waits and
retained the input token. That isolated check executed no scraper command.
A read-only comparison also matched all eight old runner modes: five direct
URLs and three delegated host expansions. Its source hash and result are in
`legacy-mode-parity.json` beside the staged workflow exports.

The real Go/Python download fixture now includes the packaged n8n command
contract, local record replay, pending inspection, native download completion,
a lost permanent-completion response, outbox reopening and exact-proof replay.
It separately verifies that file admission remains queued after source success.
Imported acceptance/skip, concurrent recording, fenced ownership, atomic guard
publication, unrelated later history and cancelled original jobs have focused
coverage. Parent commit `f5385af46` passed all three CI workflows.

The full `make validate-fork` gate passed: 528 v3 tests, the 71-operation native
contract check, Go lint with zero issues and all Go tests (API 210.155 seconds,
ingest 344.951 seconds, SQLite 323.350 seconds). After the final Python dispatch
pending-state fix, all 183 producer tests passed on Python 3.12 and 3.14 and in
the installed n8n package. The final real HTTP/download fixture passed in 11.722
seconds. Logs are `n8n-final-validation-v2.log`, `n8n-python312-final.log`,
`n8n-python314-final.log`, `n8n-http-final.log`, `n8n-image-python-final.log` and
`n8n-workflow-runtime-final.log` under `/tmp/stash-native-transition`.

The final isolated image is
`localhost/stash-n8n-native-rehearsal:adapter-final-20261001`, ID
`e1248da527bcde3276967768825b9cb339b71cf9a047945549a9910731b4edbc`, built from
the explicit existing n8n base `2fcb84852f4a…`. Its installed adapter fingerprint
matches the final workspace files. Read-only mounts of the actual ordered n8n
configuration, helpers, archives and media validated separate Reddit/Twitter
full-history profiles; their source files were unchanged. The profiles and
runtime review live under `.local/native-n8n-adapter-20261001/current/`, with
image/graph provenance in `rehearsal-review.json`. They retain the unregistered
rehearsal root UUID and have not been activated. This increment adds no Stash
database migration.

The remaining operational-history migration includes old result-file tokens,
per-scan completions, deferred/ignored work and the other catalog families.
Source registration, worker service configuration, profile/image activation,
recovery callers and all later transition phases remain outstanding. Converted
workflow files and profiles are staging artifacts, not a live deployment.

## Retained legacy n8n receipts and concurrent outbox startup

`stash-import-n8n-receipts` now validates a frozen receipt-directory snapshot
before opening the destination outbox. Apply requires the reviewed input digest
and a stable source UUID. One transaction retains every original token and JSON
byte, its classification, and an immutable import manifest. Replay after a lost
response preserves those records. Conflicting input bytes, source identities or
native caller tokens roll back the whole import. Unknown directory entries,
nonregular/changing files and malformed JSON block import; valid but unsupported
result shapes remain review records. Bounded capacity never evicts history.

Producer schema 7 adds `legacy_n8n_receipts` and `n8n_receipt_imports`. Promotion
preserves populated earlier delivery, ticket, source-call and backfill tables,
including pending leases and finished results. A native call cannot reuse an
imported token. Inspection recognizes the original token locally without opening
a network client or requiring the old directory. It distinguishes historical
success, deliberate skip, failure and review. A recorded network block remains
a failure even if the old child exited zero; this matters because the actual
Reddit workflows inspect only `command_failed`. Raw original fields remain in
the immutable receipt body. Historical results cannot be retried as native jobs.

Receipts contain no account/execution identity, so the importer does not infer
one from their log tails. It creates no source requests, permanent account
decisions or native completion proof, and never certifies file intake. Original
successful result flags remain historical claims. General producer status lists
these receipts separately from current work. Saved n8n execution graphs still
need drain/resume handling before their old command paths can be removed.

The installed-image concurrency test exposed an existing startup race: another
first opener could publish a schema between the version/application/table reads,
causing a valid outbox to appear foreign. Those checks now use one SQLite read
snapshot before the existing transactional migration and binding validation.
A deterministic regression publishes the schema between those reads and verifies
the original queued event survives. Concurrent receipt imports publish one
manifest, and an injected mid-import failure leaves no partial rows.

The actual source directory contained one 3,640-byte successful command receipt.
Its private snapshot and rehearsal are in `.local/native-n8n-receipts-20261001/`.
Import, replay, restart and packaged command inspection preserved its exact
bytes and original token. Native events, requests, source calls and backfill
calls stayed empty; integrity and foreign-key checks passed. No API requests or
production writes occurred. Three synthetic failure/review cases joined that
actual result in the real n8n evaluator: 166 expressions, 12 result branches and
three durable Wait checkpoints passed across the three staged workflows.

The full `make validate-fork` gate passed with 528 v3 tests in 91 files, native
contract checks, all then-current 191 producer tests, zero Go lint issues and
all Go tests (API 211.930 seconds, ingest 350.228 seconds, SQLite 329.835 seconds).
After the startup fix, all 192 producer tests passed on Python 3.12 and 3.14 and
inside the final installed n8n image. The real HTTP/download fixture was rerun
for that fix. Logs are `n8n-receipts-validation.log`,
`n8n-receipts-python312-final.log`, `n8n-receipts-python314-final.log`,
`n8n-receipts-image-tests-final.log`, `n8n-receipts-http-final.log`,
`n8n-receipts-actual-rehearsal.log`, `n8n-receipts-packaged-rehearsal.log` and
`n8n-receipts-workflow-runtime.log` under `/tmp/stash-native-transition`.
Parent commit `8454a0b7f` passed all three CI workflows.

The final isolated image is
`localhost/stash-n8n-native-rehearsal:receipts-final-20261001`, ID
`acbc5b7077d6bd11e5da74e9abf4ffcddc0af7d16f7ca965d49ef0bfc2377761`.
Its installed adapter fingerprint matches the workspace. Refreshed full-history
Reddit/Twitter profiles under the rehearsal's `current/` directory match that
runtime and retain the unregistered rehearsal root. Existing configuration and
media mounts were read-only; no worker was activated. The live n8n container and
`localhost/n8n:latest` remained on the original `2fcb84852f4a…` image.
This increment adds no Stash database migration.

The scan journal, extractor checkpoints, deferrals, per-scan/collection/policy
completion records, historical handoffs and other catalog families remain to
be migrated. A read-only audit found 907 queued scans, 28 extractor checkpoints,
166 deferrals, one per-scan completion, four collection completions, one policy
migration and no handoffs at inspection time. These are live changing counts,
not a cutover boundary. Source registration, recovery callers, profile/service
activation and subsequent transition phases remain outstanding.

## Native retention and inspection of frozen scan journals

Migration 1000026 adds `scan_journals` and `scan_journal_records` to the native
database. The application API retains one complete snapshot atomically, with
its UUID, original database/root identity, capture time, document digest, table
inventory and every original row. Replay returns the same receipt; reusing an
identity with different evidence conflicts. Indexed pagination exposes compact
summaries, while a separate record lookup returns the original evidence.
Startup reconciles retained counts and requires the schema guards/indexes.
Anonymised exports remove this private operational history.

The seven supported families are `scan_jobs`, `extractor_jobs`, `scan_deferrals`,
`backfill_scan_completion`, `collection_backfill_completion`,
`backfill_policy_migrations` and `legacy_handoffs`. Empty tables and older
extractor column variants remain identifiable. Embedded command/result/detail
JSON strings retain their values, including timestamp spelling and large IDs.
Exact old scope IDs connect extractor evidence to pending scans; manual or
unbound scopes remain reviewable. Deferrals retain their reasons and retry
requirements. Unknown tables/columns and unsupported command forms block import;
no command, PID or historical service handoff is executed or resumed.

`stash-import-scan-journal` reads one SQLite snapshot, inventories every table/view,
and prepares a bounded document without modifying the source. Apply requires an
explicit application endpoint, a fixed snapshot identity/time and the reviewed
input digest. Its acknowledgement must match the expected inventory, root,
source, snapshot and document hash. The two permanent account-backfill tables
are explicitly inventoried as external to this importer and retain their
separate account-history migration. Producer tokens cannot use these routes.

This completes an evidence-retention layer, not operational activation. Imported
records have `pending_binding`, `historical` or `review` dispositions. They create
no native runs, attempts, requests or completion decisions. The old archive-key
cursor hash differs from the native post/attachment hash; copying it into a
native progress record would not implement correct resume. Activation still
needs source/profile bindings, a reviewed cutoff, retry/ignored-work handling,
cursor conversion and a common quiesced cutover boundary. Historical collection
and hashed per-scan receipts do not become native source-window proof.

The full-copy rehearsal is
`.local/native-scan-journal-rehearsal-20261001/native-scan-journal-rehearsal.sqlite`.
Schema 1000025 → 1000026 took 0.066752 seconds on the 1,473,081,344-byte copy.
All 136 preexisting tables matched their source semantically, the two new tables
were empty before import, and foreign-key violations and size growth were zero.
Reconciliation took 48.715 seconds. The source schema-25 copy was preserved.

A fresh read-only backup of the actual journal supplied 1,063 retained records:
863 scans, 28 extractor checkpoints, 166 deferrals, one per-scan completion,
four collection completions and one policy migration, with no handoff rows.
Its separate account-history inventory reports 1,329 completions and zero skips.
Import/replay took 0.100069 seconds through the core repository service. Every
original evidence row and derived summary matched after replay and reopening.
Dispositions are 1,050 pending bindings, seven reviews and six historical rows.
Native source work remained empty. These are local rehearsal timings, not API
or UI latency guarantees, and this changing live journal is not a final cutover
boundary. `review.json`, `input.json`, the frozen SQLite input and both
reconciliation reports are retained privately in that rehearsal directory.

The real Go/Python HTTP fixture commits an import, drops its response, and then
verifies exact replay, paginated summaries, every evidence record, application
authority and unchanged source bytes. Repository tests cover older extractor
shapes, unknown inputs, conflicting identities, atomic rollback, immutable rows,
restart and detection of missing evidence. The shared fixture preserves all
seven families and never fabricates native work.

The full `make validate-fork` gate passed: 528 v3 tests in 91 files, native
application contract checks, all 195 producer tests, zero Go lint issues and
all Go tests (API 229.979 seconds, ingest 361.327 seconds, SQLite 339.588 seconds).
The producer suite also passed on the host's Python 3.12 runtime. Focused store
and real HTTP checks passed separately. Logs are `scan-journal-validation-final.log`,
`scan-journal-python314.log`, `scan-journal-python312.log`,
`scan-journal-focused.log`, `scan-journal-http.log`, `scan-journal-migration.log`,
`scan-journal-reconciliation.log` and `scan-journal-real-import.log` under
`/tmp/stash-native-transition`. Parent commit `220c20dc1` passed all three CI jobs.

Production, installed workers and workflows remain unchanged. Remaining work
includes activation of retained operational state, source registration, the
other catalog families and all later transition phases. This increment neither
finishes the migration nor changes the frozen compatible release.

## Native activation of retained scan requests

Schema 1000027 adds immutable activation plans and original-job bindings. The
application preview binds a retained scan to the exact native collection URL,
logical root revision, reviewed worker policy and an explicit cutoff. All scans
in that snapshot/context/URL group consolidate into one window when their old
command policies agree. Original date minima, UTC interpretation, retry delays
and failure limits survive. An existing deferral remains deferred; ordinary timer
requests cannot release it. Manual/unbound extractor scopes and historical
service handoffs remain review evidence.

`stash-activate-scan-journal` saves the normalized preview and applies its reviewed
plan digest. Activation creates one native queued/deferred run, without inventing
producer requests, access tokens, process ownership, attempts or completion proof.
Replay returns the same receipt after response loss or restart. Original scan
keys are unique within their source database across snapshots, preventing a later
snapshot from scheduling the same pending job again. A collision with an existing
active native run rolls back instead of replacing its progress or lease.

An explicitly selected checkpoint must match a selected scan's consolidated
scope. The first real claim seeds its old item count and qualified archive-key
cursor, with zero claimed completed files. The worker reproduces the original
gallery-dl key hash and JSON encoding, replays through the position, restores
the archive stop rule and then writes native cursors. Missing checkpoints remain
unfinished. Full-history/no-skip profiles without a stop rule preserve the old
behavior of ignoring that checkpoint. A wider window replays without archive
stopping, including on subsequent retries; omitting a checkpoint deliberately
selects that replay behavior as well.

The server advertises recovery protocol 1 and requires workers to acknowledge it
when claiming recovered work. Older workers cannot silently ignore that policy
and mark a partial traversal successful. Current workers require the capability,
and verify that recovery policy remains fixed during their lease. Lease/status
queries read only the bounded recovery fields through the run's unique index,
not the complete retained maintenance plan. Producer outbox schema stays at 7.

The isolated rehearsal database is
`.local/native-scan-activation-rehearsal-20261001/native-scan-activation-rehearsal.sqlite`.
Schema 1000026 → 1000027 took 0.079122 seconds on the 1,473,081,344-byte copy.
All 138 preexisting tables matched before activation, with zero foreign-key
violations or size growth; reconciliation took 55.085 seconds. Three selected
actual journal requests exercised a bound checkpoint, a deferral and full replay
against an empty isolated media root and an explicitly unusable placeholder
worker policy. Activation/replay took 0.056358 seconds, producing two queued runs
and one deferred run. Reopening preserved all three receipts; no worker, producer
request or attempt was created. The snapshot, its 1,063 rows and all 1,329 permanent
account-backfill decisions still matched their originals exactly afterward.
These are local rehearsal timings, not production latency guarantees.

The rehearsal inputs, migration and retained-evidence reconciliation reports,
activation results and helper source are retained in that private directory.
Final production source/profile bindings have not been created. Earlier staged
worker profiles/images need their adapter fingerprints refreshed before they can
be activated with the new recovery contract. The live database, host launchers,
n8n workflows and frozen compatible deployment remain unchanged. Broader catalog
migration, recovery callers and subsequent transition phases remain open.

Final `make validate-fork` passed: 528 v3 tests in 91 files, native application
contracts, 200 producer tests, zero Go lint issues and the complete Go suite.
The same producer suite passed separately on the host's Python 3.12 runtime.
Focused database and real Go/Python HTTP checks cover response loss, exact replay,
deferral/backoff preservation, old-worker rejection, incompatible scope/policy,
expanded-window replay, cross-snapshot duplicate protection and rollback of a
native-run collision. Gallery-dl lifecycle fixtures cover restoration of the
archive stop rule, missing cursors and full-history replay; hash fixtures preserve
legacy spacing, Unicode escaping and large IDs.

Validation logs are `scan-activation-validation-verified.log`,
`scan-activation-python312-verified.log`, `scan-activation-final-http.log` and
the focused database logs under `/tmp/stash-native-transition`; rehearsal logs
are `scan-activation-migration.log`, `scan-activation-reconciliation.log` and
`scan-activation-real-activation.log`. Parent `298ac862a` passed all three CI jobs.

## Reviewed performer-registry import

Schema 1000028 imports the five performer-registry families and both older
plugin-binding families through an application-only preview/apply API. The
`stash-import-catalog-identities` command reads one frozen SQLite snapshot,
inventories every supported table/column, and requires a reviewed server plan
before applying changes. Original JSON text and all row outcomes remain retained;
the account map is temporary migration input, not a restored plugin setting.

Saved catalog UUIDs are adopted by explicitly bound local performers while
selected metadata and local IDs remain unchanged. Historical catalog redirects
record already-completed merges without deleting or recreating local performers.
Missing/reused IDs, competing bindings, UUID collisions, and unbound identities
remain review records. Foreign-library IDs are never interpreted locally.
Explicitly mapped accounts import saved links/unlinks or reuse an equal native
decision; later native choices and contradictory saved alternatives require
review. Older plugin bindings covered by their migration receipt cannot resurrect
a later unlink. A late write error rolls back all domain and receipt changes,
even if a caller accidentally swallows the error.

Receipts preserve the reviewed plan after response loss, restart and subsequent
native edits. A second snapshot cannot replay the same source-registry/namespace
cutover. Each source row has a previewed outcome; retained evidence is available
through bounded record pages. Anonymised exports remove the import evidence.
Unresolved records need native review operations in a later increment. Bulk
account-identifier/source routing import and the other catalog families remain
separate required work; this is not the complete catalog migration.

The private rehearsal is in
`.local/native-performer-registry-rehearsal-20261001/`. It contains a read-only
backup of the actual registry, its complete table inventory, exact prepared/bound
inputs, preview, immutable receipt, original records, reconciliation reports and
helper source. The library copy migrated from 1000027 to 1000028 in 0.069857
seconds. All 140 existing tables matched before import, with zero foreign-key
violations and zero size growth; that comparison took 53.614 seconds.

The actual registry contains four performer identities, four local bindings,
five saved account choices, fourteen identity events, one migration receipt,
and two older plugin rows: thirty retained records. All four performer UUIDs
were adopted. Elizabeth Tran retained catalog UUID
`1d8d50be-1de6-4320-8725-b617f8ceb062`, local performer 721, and the existing
canonical name/aliases. This registry has no recorded UUID redirects or merge
events for the older imelizabethtran merge; the importer preserved its survivor
and historical names without inventing a deleted performer identity.

One native Twitter account was explicitly prepared from the registry's captured
ID/handle evidence for account 742448640, then linked to that surviving performer.
The other four saved account choices were retained for review pending native
account mapping. This includes the directory-derived Instagram label, which was
not silently converted into a service identity. Import and exact replay took
0.011440 seconds; reopening retained the same receipt. The outcomes were nine
mapped rows, fifteen copied historical rows, four review rows and two superseded
plugin rows.

The post-import comparison checked all 140 preexisting tables. Selected library
metadata and all 1,542,046 other archive entities matched exactly; only the four
reviewed UUID adoptions/redirects and the prepared account/ownership records
changed. All thirty source rows matched their frozen evidence, with no unexpected
differences or foreign-key violations. Reconciliation took 97.796 seconds. These
are local rehearsal measurements, not production latency guarantees.

The compatible deployment, live registry/library, workers and n8n workflows
remain unchanged. Parent `f5f70f5dc` passed all three CI jobs. `make validate-fork`
passed: 528 v3 tests in 91 files, native application contracts, 204 producer tests,
zero Go lint issues and the complete Go suite. The producer suite also passed on
the host's Python 3.12 runtime (6.165 seconds). After the final transaction guard,
the focused SQLite tests, real Go/Python HTTP replay test and Go lint passed again.

Coverage includes response loss, restart, native edits after successful import,
stale previews, changed historical local bindings, explicit unlinks, preserved
native choices, competing catalog identities, missing/cyclic redirects, conflicting
and coalesced account mappings, unchanged selected metadata, late failure rollback,
swallowed-error rollback, strict snapshot shapes, and anonymised evidence removal.
The main log is `catalog-identity-validation.log`; final focused evidence is in
`catalog-identity-atomic-final.log`, `catalog-identity-http-final.log`,
`catalog-identity-lint-final.log`, and `catalog-identity-python312.log`. Rehearsal
logs are `catalog-identity-migration.log`, `catalog-identity-reconciliation.log`,
`catalog-identity-real-import.log` and `catalog-identity-import-reconciliation.log`,
all under `/tmp/stash-native-transition`.

## Reviewed account and catalog registry import

Schema 1000029 imports the six remaining registry families through an
application-authorized preview/apply API and `stash-import-catalog-registry`.
The frozen input must complement the completed performer-registry import, with
matching source UUID, capture time and table inventory. Original rows, plan
outcomes, source-qualified account/catalog mappings and an immutable receipt
remain in the native database. Exact retry and reopening preserve the receipt;
stale plans and later native ownership choices cannot silently reapply history.

Captured qualified IDs create or resolve source accounts. Captured handle/ID
pairs share that account; reused aliases remain ambiguous. Different ID kinds
can resolve together only through an already established native account.
Conflicting native candidates are reported with their actual UUIDs. Mirror and
native service namespaces stay separate; mirror display names never become
native handles. Known legacy locators carry explicitly provisional evidence,
while unknown services and directory-derived account labels remain review items.
No name-only performer match or website lookup is performed.

Every surviving catalog becomes a disabled `legacy_catalog` collection; historical
redirects point to its survivor. Publisher accounts remain separate from depicted
performers. Routes, media keys, links and identifier checkpoints are retained
without activating jobs or inventing scrape URLs/root bindings. The actual registry
contained 1,078 empty identifier checkpoint timestamps; the old index writes
these when no account capture exists. They survive as historical checkpoints.
ID evidence without a handle also retains its empty alias key.

The private full-copy rehearsal is in
`.local/native-registry-rehearsal-20261001/`. It reuses the previous increment's
frozen registry and starts from a new copy of its schema-1000028 library.
Promotion to 1000029 took 0.074117 seconds. All 142 preexisting tables matched
before import, with no foreign-key violations or database-file growth; comparison
took 63.700 seconds. The prepared input, reviewed plan, receipts, retained records,
helper source and reconciliation reports are saved privately there.

The snapshot contains 1,697 catalogs, 4,032 routes, 914 identifier rows and 1,697
checkpoints, with empty link/profile-URL tables: 8,340 retained rows. Catalogs
include 1,237 creator catalogs, 456 other collections and four subreddits. Preview
took 0.121751 seconds. It created 1,181 source accounts and reused the previously
bound Twitter account: 662 captured identity groups and 520 provisional locator
groups. It mapped 1,358 old account keys and retained 138 unqualified keys for
review. All 1,697 catalog collections were created disabled; 1,099 have a resolved
publisher account. None received a scrape target or filesystem root.

Three previously unresolved saved ownership choices were imported: Elizabeth
Tran's Instagram account and the Reddit accounts for petitebbygirl and
Southern-Lobster-808. Elizabeth Tran's existing Twitter choice was preserved.
The directory-derived miaxmall account label remains unresolved rather than
being silently asserted as an Instagram identity. All performer UUIDs, local IDs,
selected names and aliases remained unchanged, including the existing Elizabeth
Tran merge survivor.

Import plus exact replay took 1.156696 seconds, and reopening returned the same
receipt and ownership. All 8,340 source rows matched their frozen evidence:
2,611 mapped records and 5,729 copied historical records. The post-import
comparison checked all 142 preexisting tables; all 1,542,054 archive entities,
selected library metadata, previous import evidence and preexisting collection
definitions remained unchanged. Only the planned account evidence/revisions,
new ownership choices and collection records changed. There were no unexpected
differences, foreign-key violations or database-file growth. Reconciliation took
51.728 seconds. These are isolated rehearsal measurements, not production timing
guarantees.

`make validate-fork` passed: 528 v3 tests in 91 files, native contracts, 207
producer tests, zero Go lint issues and the full Go suite. After the final account
matching/checkpoint changes, the focused SQLite suite passed in 6.598 seconds,
the real Go/Python HTTP checks passed, and Go lint reported zero issues. The final
snapshot-reader regression brought the Python 3.12 suite to 208 passing tests
(5.954 seconds); all eight registry/identity tests passed on Python 3.14, and the
real HTTP checks passed again in 3.388 seconds. Coverage includes exact response-
loss replay, stale previews, native choices after import, mirror separation,
reused handles, multiple identifier kinds, empty historical checkpoints, missing
aliases, strict inventory, rollback after swallowed errors and anonymisation.
Parent `29445f5b5` passed all three CI jobs.

Validation logs are `catalog-registry-validation.log`,
`catalog-registry-verified-focused.log`, `catalog-registry-verified-lint.log`,
`catalog-registry-python312-final.log`, `catalog-registry-python314-final.log`
and `catalog-registry-python-final-http.log`. Rehearsal logs are
`catalog-registry-migration.log`, `catalog-registry-reconciliation.log`,
`catalog-registry-real-preview.log`, `catalog-registry-real-import.log` and
`catalog-registry-import-reconciliation-final.log`, all under
`/tmp/stash-native-transition`.

This imports the registry, not the individual catalog bodies. Native review
resolution, validated source/root registration, media/post/profile history,
worker activation and the later transition phases remain required work. The live
library, catalog registry, host/n8n workers and frozen compatible deployment remain
unchanged.

## Bounded snapshots of individual catalog bodies

The producer package now includes `stash-prepare-catalog`, a versioned read-only
reader and durable snapshot command for recognized catalog schema versions 1–3.
It inventories every physical family and schema object, validates table/key
shapes, checks SQLite and logical references, and rejects unknown tables/columns
or views. Older physical sidecars and normalized document/source layouts remain
distinct supported inputs; the normalized view is not exported as duplicate data.

The snapshot preserves original SQLite values and JSON strings, with an explicit
binary representation for exact sidecar bytes. Ordered chunks contain at most
1,000 records and 16 MiB; each table and chunk has a row count and digest. The
manifest retains the original registry/catalog/snapshot identities, schema,
reference counts and reconstructed capture inventory. Profile bodies and shared
observations remain physical rows rather than being expanded into repeated stored
payloads. The reader verifies profile hashes and paths, per-capture patches and
sidecar content hashes. Shared observations with detail rows create no extra
synthetic capture. Profile caching has both entry and byte bounds.

Files and directories are flushed before publishing a private snapshot directory.
Existing destinations are not overwritten; interrupted or lost acknowledgements
can be resolved by verifying the saved manifest and chunks. Verification checks
names, digests, counts, ordered unique keys, binary checksums and table/reference
inventories. The command reports `imported:false`: native batch mapping, review
outcomes and import completion receipts remain required subsequent work.

The full rehearsal is in
`.local/native-catalog-source-rehearsal-20261001/`. All 1,697 catalogs listed in
the frozen registry were copied and prepared; there were no missing or additional
catalog files. Initial sandboxed backups could not create SQLite's transient WAL
shared-memory files for some sources. The isolated helper was stopped, its 227
completed copies were preserved, and the remaining 1,470 read-only backups were
completed with access for those temporary files. That backup pass took 97.718
seconds. No catalog data or native library was modified.

The final preparation/verification covered 4,764,236 physical rows, including
256,991 posts, 379,449 observations, 444,898 detail captures, 781 profile bodies,
272,556 sidecar documents and their references. Reconstruction yielded 526,348
original captures with 18,245 profile references; the largest payload was
2,928,257 bytes. All 22 present physical families were retained, including empty
edit, file-event and prune queues. Preparation plus verification took 201.312
seconds with peak resident memory of 84,004 KiB. Frozen databases occupy
2,940,776,448 bytes and prepared record chunks 2,816,108,769 bytes, excluding their
manifests. These individually consistent backups are rehearsal inputs, not a
coordinated production cutover boundary.

An independent comparison then read every original SQLite row and every exported
record, preserving SQLite value types, JSON strings and binary bytes. It also
compared all 526,348 reconstructed payloads, capture IDs, timestamps, metadata and
extractor versions with the existing catalog reader's `observations.expand` path.
All 1,697 catalogs, all 4,764,236 rows and all captures matched, with zero errors.
Original database file hashes matched before and after inspection. This pass took
136.975 seconds with peak resident memory of 134,852 KiB. Private manifests,
source hashes, helper code and both reconciliation reports are retained locally.

The producer validation gate passed all 216 tests on Python 3.14 (5.344 seconds)
and the complete suite also passed on Python 3.12 (6.003 seconds). Eight new tests
cover older layouts, shared/flat captures, nested profile references, exact binary
preservation, unknown/corrupt inputs, deterministic bounded output, destination
protection, tampering, publication response loss and the CLI. The rebuilt isolated
package's installed command prepared and verified the Elizabeth Tran catalog with
the exact same manifest digest as the source checkout. Parent `e172751f6` passed
lint, build and preview image publication in CI.

Logs under `/tmp/stash-native-transition` are `catalog-snapshot-backup.log`,
`catalog-snapshot-full-rehearsal-final.log`,
`catalog-snapshot-independent-reconciliation.log`,
`catalog-snapshot-producer-final.log`, `catalog-snapshot-python312-final.log`,
`catalog-snapshot-focused-final.log` and `catalog-snapshot-package-install.log`.
The native database remains at schema 1000029. Production, workers and n8n have
not switched, and the full transition remains in progress.


## 2026-10-01: Resumable receipt of individual catalog snapshots

Native schema 1000030 now receives the prepared individual catalog bodies through
application-authorized `CatalogSnapshot` services. Each manifest is bound to its
original registry import, source UUID, catalog ID and native collection mapping.
Unknown schema shapes, missing mappings, changed identities and out-of-order or
altered chunks fail. A source/catalog pair cannot silently acquire another frozen
snapshot under a new UUID.

Chunks contain at most 1,000 rows and 16 MiB. Original JSONL bytes, embedded JSON
strings, binary sidecar encodings and row keys survive in indexed temporary
migration records. Chunk receipts and resumable table hashes commit in the same
transaction as their records. Lost responses, restart and exact replay do not
create extra rows; a swallowed write failure still aborts the transaction. The
server checks table/chunk counts and hashes, supported physical columns/keys,
SQLite key ordering, embedded JSON and binary checksums. Retained schema SQL is
never executed. Historical sidecar-view triggers remain supported evidence.

`stash-upload-catalog` verifies the full local snapshot, submits its original
bytes to the explicit native endpoint, checks receipt identity/counts and resumes
from the next chunk. The installed command uses the existing application key;
producer tokens and website credentials are unrelated to this migration access.
The receiver reports `received` with `imported:false` and all record families
explicitly pending. Native graph/capture reconciliation, domain mapping, review
outcomes and retirement of temporary bodies remain subsequent work. Reception
creates no posts, media, ownership or metadata choices, and activates no jobs.

A fresh copy of the completed schema-1000029 registry rehearsal promoted in
0.121 seconds. All 1,697 frozen catalogs were then received as 4,764,236 records in
5,954 chunks, preserving 2,816,108,769 original JSONL bytes. The rehearsal includes
reopening/replaying an acknowledged chunk and checking all receipts after the
final reopen. Receipt staging and these checks took 251.832 seconds; the final
open/consistency check took 29.508 seconds while the full input staging remained.
Inputs are the individually consistent frozen preparation copies, not a live
production cutover boundary. Private input manifests and the complete receipt
set remain under `.local/native-catalog-upload-rehearsal-20261001`.


Independent reconciliation compared the exact staged manifest, every record's
bytes/key/hash, every chunk/table digest and all 146 pre-existing native tables.
All 1,697 catalogs and all 4,764,236 rows matched; existing entity identities,
selected metadata, ownership and prior receipts were unchanged. SQLite integrity
returned `ok` and foreign-key checks found zero violations. This pass took
277.876 seconds. The rehearsal database grew from 1,473,081,344 to 6,986,661,888
bytes while retaining the temporary input rows and their lookup indexes. No
expanded capture/profile copies or live catalog projections were added.

All required fork-gate components passed: 528 v3 tests in 91 files, native client
contracts, 220 producer tests on Python 3.14, backend lint with zero issues, and
all Go tests. The final backend run passed the API
package in 380.579 seconds and SQLite package in 471.006 seconds. The complete
220-test producer suite also passed on Python 3.12 in 6.145 seconds. Focused tests
cover actual Python-to-Go HTTP response loss, restart, exact replay, atomic
rollback, checksum/ordering/lineage rejection, original binary/JSON preservation,
receipt validation and anonymisation. The rebuilt isolated package exposes the
installed `stash-upload-catalog` command. Parent `1b04b94fd` passed lint, build
and preview image publication in CI.

Logs under `/tmp/stash-native-transition` are `catalog-upload-focused.log`,
`catalog-upload-python-focused.log`, `catalog-upload-validation.log`,
`catalog-upload-backend-final.log`, `catalog-upload-python312.log`,
`catalog-upload-promotion.log`, `catalog-upload-full-rehearsal.log`,
`catalog-upload-independent-reconciliation.log`,
`catalog-upload-package-install.log` and `catalog-upload-installed-command.log`.
Production, workers and n8n remain on their existing deployment. The full native
archive transition is still in progress; the next domain import must reconcile
posts/captures and then every remaining physical record family before any
snapshot can be reported as imported.


## 2026-10-01: Native post, profile and capture import

Native schema 1000031 now maps received catalog source evidence through core
services. Application-only checkpoint and outcome routes support
`stash-import-catalog-evidence`; producer tokens cannot run historical imports.
Each transaction handles at most 50 source rows and stops between records after
16 MiB of decoded input. Indexed dependency reads load only the relevant post,
URLs, observation and profiles. Native records, immutable row dispositions and
progress commit together, including protection against swallowed write errors.
The same frozen manifest resumes from the last committed ordinal after restart
or a lost response. Completed mapping receipts replay unchanged.

Qualified post IDs share native identities across physical catalogs. Coomer and
Kemono URLs retain mirror/service namespaces even when old catalog rows called
their platform onlyfans, fansly or patreon. Unknown local keys stay scoped to the
source catalog. Captured Reddit/Twitter identifiers can qualify a local post;
conflicting identities, reused reserved UUIDs and forgotten posts require review.
These mappings never infer performer ownership from names, folders or shared
content. Collection provenance uses the original imported definition even after
a later native edit or retirement.

The Go reader reproduces the legacy Python JSON checksum encoding, verifies
profile hashes and hydration paths, and preserves parent Reddit patch semantics.
Detail rows become original captures; their shared observation receives a shared
disposition without an invented extra timestamp capture. Flat observations each
produce one capture. Core storage deduplicates post revisions, payloads and
profiles, including retention of unreferenced profile bodies. Identical copied
events can share a capture across catalogs; reusing an old capture ID with
different bytes or provenance preserves a separate event.

The complete frozen corpus passed reader reconciliation: 526,348 captures from
1,697 catalogs, including 81,450 flat observations and 18,245 profile references,
matched the Python manifest checksums. Every payload round-tripped through the
native representation without loss. This check took 58.894 seconds. It used
read-only frozen inputs and made no library changes.

A new copy of the schema-1000030 upload rehearsal promoted in 10.372 seconds.
All 1,697 received catalogs then completed the evidence mapping pass: 1,082,119
original rows, 526,348 capture mappings and 781 profile mappings, with zero review
outcomes. The helper used 22,916 bounded transactions, reopened after its first
batch to check the committed checkpoint, and replayed every completed receipt
after the final reopen. Import and these checks took 1,224.670 seconds; the final
database open/consistency check took 28.580 seconds. These are individually
consistent frozen rehearsal inputs, not the coordinated live cutover boundary.
Private source inventories, checkpoints and receipts are retained in
`.local/native-catalog-evidence-rehearsal-20261001/`.

Independent reconciliation then read the original catalogs with the Python source
reader and reconstructed every stored native capture from its database payloads,
profile references and patches. All 526,348 capture payloads, projected metadata,
timestamps, provenance and original header hashes matched. All 781 profile bodies
and every original post-key mapping matched. The 297,999 shared observations
created no additional captures. Native storage contains 256,990 posts, 379,449
shared revisions, 526,348 captures, 781 profile bodies and 18,245 profile
references. One qualified post identity is shared between original catalog rows;
this corpus has no exact copied capture events to coalesce, which the HTTP
fixture tests separately.

All 142 pre-existing tables outside the intended source-evidence writes matched
the baseline, including original snapshot bytes, existing entity metadata and
associations. SQLite integrity returned `ok` with no foreign-key violations.
This comparison took 222.289 seconds. The rehearsal database is 8,975,618,048
bytes while the temporary snapshot input remains present. Original source
catalogs, the upload baseline and production were not modified.

Validation passed all required fork-gate components: backend generation, v3
generation/types/formatting, 528 v3 tests and 71 operation contract files, 223
producer tests on Python 3.14 and Python 3.12, zero final lint issues, and the
entire Go suite. Two initial lint findings were fixed before the successful
backend gate. API tests took 432.370 seconds and SQLite tests 543.334 seconds
while the separate full-copy import was running. Focused tests cover rollback,
reopening/replay, stale checkpoints, forgotten/conflicting posts and historical
collection scope. The real HTTP/Python test loses a committed bounded batch,
resumes it, retains an unused profile, and imports a second physical catalog
with both copied captures and changed data under a reused old capture ID. The
rebuilt isolated package exposes the installed import command. Parent
`b7f61ecb3` passed lint, build and preview publication in CI.

Logs under `/tmp/stash-native-transition` are
`catalog-evidence-reader-reconciliation.log`, `catalog-evidence-promotion.log`,
`catalog-evidence-full-import.log`,
`catalog-evidence-independent-reconciliation.log`,
`catalog-evidence-validation.log`, `catalog-evidence-backend-final.log`,
`catalog-evidence-python312.log`, `catalog-evidence-http-copies.log`,
`catalog-evidence-historical-scope.log` and `catalog-evidence-lint-final.log`.

The evidence pass reports `mapped` or `review` with `imported:false`. It covers
four source families; assets, files, appearances, memberships, sidecar documents,
translations, edits and other retained histories still require native mappings
and final reconciliation. It does not select metadata, associate library media,
construct galleries or activate jobs. Temporary snapshot bodies remain private
migration inputs until the complete importer can retire them. Production and
workers remain on the frozen compatible deployment; the full transition remains
in progress.

## 2026-10-01: Native post link evidence services

Native schema 1000032 adds the core relationship services needed by the next
catalog import pass. `SourcePostLinks` stores each exact post URL once, with
separate observations carrying their original evidence time and provenance. It
also retains evidence for qualified post identifiers and unselected publisher
claims. Identifier changes require the reviewed post revision and cannot take
over another post's identifier. These services do not fetch URLs or infer that
two posts with the same URL are one post.

Publisher claims retain their original account UUID through consolidation and
resolve its current canonical UUID on reads. They cannot choose a capture's
publisher, override its explicit unlink, or assign depicted performers. The
legacy writer inspection showed why this boundary is necessary: an old
`source-id` account can come from a directory label, and native-service-labelled
IDs can represent mirror accounts. The relationship importer must preserve those
claims with their original basis; actual captured publisher evidence and native
review decisions remain separate.

Read-only inventory of the frozen source confirms 1,173 original account rows:
1,095 already have a registry mapping and 78 remain unmapped. The existing
OnlyFans/Fansly source-ID mappings correctly resolve to Coomer namespaces and
Patreon mappings to Kemono; their old platform labels must not create native
service IDs during the next pass. The private grouped inventory is saved with
the post-link rehearsal inputs. A post-association preflight found 218,338 rows
eligible for retained unselected claims, 32,822 whose account is still unmapped,
and 5,831 without a legacy account. Existing mapped post/account namespaces had
no conflicts; eligibility is not a publisher selection or performer attribution.

Evidence writes are immutable and replayable, reject changed request contents,
and refuse new observations for forgotten posts. SQL guards preserve scope and
retirement rules. A managed transaction guard rolls back a late failure even
when a caller ignores its error. Read APIs use bounded indexed cursors. Startup
validates the new tables, indexes, triggers, timestamp column types and evidence
references. Anonymised exports remove these private records before their parent
posts and accounts.

Focused regression tests pass for URL deduplication with separate observations,
post identity conflicts, account consolidation, explicit unlink preservation,
restart/replay, immutable rows, invalid inputs, failed-write rollback, incomplete
startup data and anonymisation. An initial timestamp declaration mismatch was
found and fixed before validation; the final startup guard also verifies its SQL
type. Final focused tests took 22.725 seconds, and final lint reported zero issues.

The corrected schema promoted a new copy of the schema-1000031 full-corpus
rehearsal in 113.983 seconds while the broader validation suite was running.
Independent comparison found all 153 pre-existing data tables unchanged,
including the complete staged snapshots and native post/capture/profile graph.
The four new relationship tables remain empty until their import pass. SQLite
integrity returned `ok` and foreign-key validation found no violations. The
comparison took 398.389 seconds, and the resulting database is 8,975,687,680
bytes. Its private path is
`.local/native-post-links-rehearsal-20261001/verified.sqlite`; the previous
evidence rehearsal and original catalog snapshots were not modified.

All required gate components passed: backend generation, v3 generation/types and
format checks, 528 v3 tests, 71 application operation contracts, 223 producer
tests, final lint and the entire Go package set. The earlier broad run used an
ownership assertion corrected during focused validation; the final complete
SQLite rerun passed in 127.021 seconds. Other Go packages passed in that broad
run, including API tests in 376.154 seconds. Parent `22e96850b` passed lint, build
and preview image publication in CI.

Logs under `/tmp/stash-native-transition` are `post-links-focused-final.log`,
`post-links-lint-final.log`, `post-links-validation.log`,
`post-links-sqlite-final.log`, `post-links-promotion-final.log` and
`post-links-independent-reconciliation-final.log`. The superseded rehearsal
copy with the initial timestamp declaration was removed after the corrected
copy passed reconciliation.

This increment provides core services only. Original `post_urls`, `post_aliases`,
account/handle rows and legacy publisher associations still need their bounded
import passes and per-row reconciliation. No received snapshot becomes imported
through this schema upgrade. The full transition remains active, with production,
workers and n8n on the frozen compatible deployment.

## 2026-10-01: Native catalog relationship import

Schema 1000033 and `stash-import-catalog-relations` now map the original
`accounts`, `handles`, `posts`, `post_urls` and `post_aliases` families through
core services. The pass requires a received snapshot and completed source
evidence mapping. Application-authorized API batches bind the exact manifest
and ordinal, process at most 50 rows, and commit native records, immutable
source-row receipts and progress together. A late error rolls back the entire
batch even if its caller ignores the error. Lost responses resume from the
committed checkpoint, and completed passes replay unchanged.

Account rows use the snapshot's frozen registry mappings and preserve original
account UUIDs through consolidation. They retain legacy keys without promoting
folder-derived or mirror IDs into native-service identities. Handle evidence
keeps its original observation time; mirror handle fields become legacy labels
because they can contain display names. Post URLs share exact native URL rows
while retaining separate evidence. Aliases retain physical-catalog scope and
cannot take over another post's identifier. Old post/account associations become
unselected claims, without selecting publishers or assigning depicted performers.

Every receipt retains the complete original values, including old identity
basis and creation timestamps. Unmapped accounts, invalid values, conflicting
aliases and forgotten posts retain explicit review outcomes. Posts without an
old account are counted as unassigned. Bounded API summaries omit unusually
large keys with an explicit marker; the individual record endpoint retains the
full key and source values. Startup validates source correspondence, progress
continuity and native evidence scope. Anonymised exports remove these records.

Focused tests passed for all five families, interrupted batches, stale cursors,
reopening/replay, mirror scope, alias conflicts, failed-write rollback, forgotten
posts, oversized values, incomplete startup data and anonymisation. The real
HTTP/Python test loses a committed relationship batch, resumes it, and imports a
second physical catalog whose shared post URL retains separate evidence. It also
exercises invalid routes, bounded summaries and individual record inspection.

All required fork-gate components passed: backend generation, v3 generation,
types and format checks, 528 v3 tests, 71 application operation contracts, 226
producer tests on Python 3.14 and Python 3.12, final lint and the complete Go
suite. One initial lint finding was corrected before the successful backend
gate. API tests took 384.817 seconds and SQLite tests 502.426 seconds while the
private full-corpus rehearsal ran. The rebuilt isolated package exposes the
installed `stash-import-catalog-relations` command. Parent `781eb6e72` passed
lint, build and native preview image publication in CI.

A fresh copy of the verified schema-1000032 rehearsal was promoted in 16.210
seconds. Importing all 1,697 frozen snapshots processed 510,366 relationship rows
in 11,497 bounded transactions over 525.312 seconds, including restart and exact
terminal replay checks. Reopening the completed copy took 53.851 seconds while
the broader tests were running. The results are 471,557 mapped rows, 32,978 review
rows and 5,831 unassigned posts. These are relationship dispositions;
`imported:false` remains the whole-catalog status.

The review count consists of 78 original account records without a frozen
registry mapping, 78 dependent handle records and 32,822 dependent post/account
claims. All 250,944 URL rows and 71 aliases mapped successfully, along with 1,095
account references, 1,109 handle observations and 218,338 unselected claims.
Resolving the underlying account mappings and reviewing actual captured
publisher identities remain separate from these immutable migration receipts.

Independent Python reconciliation reread all original snapshot chunks, verified
their hashes, and compared every relationship key, checksum and complete source
value with its native receipt. It independently checked mapped account references,
handle normalization and timestamps, scoped alias hashes, URL identities,
publisher claims and provenance. All 148 unaffected tables matched the baseline
exactly. Existing post/account fields and identities were preserved, with exact
revision increments accounted for by the new evidence. All 504,810 prior post
identifiers, 1,831 account identifiers and 2,296 account-evidence rows survived;
the pass added 71 scoped post identifiers, 1,152 account references and 2,204
account-evidence rows. The 250,944 URL observations share 250,943 native URL rows.
Integrity returned `ok`, and foreign-key validation found no violations. Final
reconciliation took 182.286 seconds; the resulting database is 9,898,385,408 bytes.

Logs under `/tmp/stash-native-transition` are
`catalog-relations-focused-final.log`, `catalog-relations-promotion.log`,
`catalog-relations-full-import.log`,
`catalog-relations-independent-reconciliation-final.log`,
`catalog-relations-validate-fork.log`, `catalog-relations-backend-final.log`,
`catalog-relations-python312.log` and `catalog-relations-package-install.log`.
The final independent checker corrected an initial manifest-field lookup in its
standalone script; the native importer required no change from that check.

The original schema-1000032 baseline and frozen catalog snapshots were not
modified. The new private rehearsal is
`.local/native-catalog-relations-rehearsal-20261001/native-relations-rehearsal.sqlite`.
Captured publisher decisions, assets/files, appearances, memberships, sidecars,
translations, edits and other histories still require their remaining mappings
and final semantic reconciliation. No source jobs are activated, library media
associated, galleries constructed or existing metadata selections changed by
this pass. The full transition remains active; production, workers and n8n
remain on the frozen compatible deployment.

## 2026-10-01: Captured publishers from retained catalog evidence

Schema 1000034 and `stash-import-catalog-publishers` now select publishers from
actual captured account IDs after the evidence and relationship passes complete.
They reuse the native ingestion policy, preserving existing publisher choices
and explicit unlinks. Qualified IDs can resolve an existing account or create
one when the core policy allows it; ambiguous candidates and invalid identities
retain review context. Missing captured identities and forgotten posts remain
unavailable. Feed-owner profiles, folder names and historical catalog links never
supply a publisher, and source publisher selection does not assign depicted
performers or account owners.

The pass processes actual flat/detail captures; shared observation parents do
not create extra events. Immutable receipts reference existing source rows,
captures and decisions, retaining a small decision context without copying the
payload or profile. Original account UUIDs survive consolidation, while reads
also expose their current canonical account. Application-authorized batches bind
the exact manifest and ordinal, process at most 50 rows, and check a 16 MiB
retained-payload threshold between rows. Publisher decisions, identifier evidence,
receipts and progress commit together. Late failures roll back the entire batch,
including account creation, even if the caller ignores the error.

Focused tests cover account creation/reuse, absent and invalid identities,
unmapped source rows, ambiguous names/IDs, forgotten posts, explicit unlinks,
late-write rollback, prerequisites, stale bindings/cursors, restart/replay,
startup integrity and anonymisation. Existing performer and scene associations
remain unchanged. The real HTTP/Python test loses a committed publisher batch,
resumes it, and imports copied captures from another physical catalog: existing
decisions are preserved and only the genuinely new capture gets a new choice.
Bounded summary and individual context routes are covered.

All required fork-gate components passed: backend generation, v3 generation,
types and format checks, 528 v3 tests, 71 application operation contracts, 229
producer tests on Python 3.14 and Python 3.12, final lint and the complete Go
suite. One test-style lint finding was corrected before the successful backend
gate. API tests took 310.908 seconds and SQLite tests 445.270 seconds alongside
the private full-corpus rehearsal. Reinstalling the isolated package exposes
the `stash-import-catalog-publishers` command. Parent `db22eba99` passed lint,
build and native preview image publication in CI.

A fresh copy of the verified schema-1000033 rehearsal was promoted in 71.176
seconds. Importing all 1,697 frozen snapshots processed 526,348 capture rows in
11,820 bounded transactions over 517.000 seconds, including restart and exact
terminal replay checks. Reopening the completed copy took 67.937 seconds. The
results are 33,533 linked publishers and 492,815 unavailable captures lacking
captured account IDs, matching the read-only preflight. Every resolved publisher
already had an account; this corpus required no new accounts and produced no
publisher conflicts. Historical relationship review outcomes remain separate.
All receipts still report `imported:false` for the full catalog migration.

Independent Python reconciliation reconstructed every capture from the original
frozen catalog databases, checked its header checksum and independently derived
the qualified author identifiers. It verified all 33,533 decisions, current
heads and 60,625 captured identifier claims, including original observation
times, source origins and evidence paths. All 152 unaffected tables matched the
baseline exactly. Existing post/account fields remained unchanged apart from
the precisely accounted revision increments. All 2,983 account identifiers and
4,500 prior identifier-evidence rows survived; the pass added 60,625 evidence
rows without adding identifiers. Integrity returned `ok`, with no foreign-key
violations. Reconciliation passed in 256.629 seconds; the resulting database is
10,262,614,016 bytes.

Logs under `/tmp/stash-native-transition` use the `catalog-publishers-` prefix:
`focused-final.log`, `http.log`, `promotion.log`, `full-import.log`,
`independent-reconciliation.log`, `validate-fork.log`, `backend-final.log`,
`python312.log` and `package-install.log`. The private rehearsal and machine-readable
receipts are retained in `.local/native-catalog-publishers-rehearsal-20261001/`.
The original schema-1000033 baseline and frozen catalog snapshots were unchanged.

Assets/files, appearances, memberships, sidecars, translations, edits and other
histories still need their remaining mappings and final semantic reconciliation.
This pass activates no source jobs and constructs no galleries. The full
transition remains active; production, workers and n8n remain on the frozen
compatible deployment.

## 2026-10-01: Catalog attachment evidence and protected source selections

Schema 1000035 and `stash-import-catalog-attachments` now recover explicit source
attachment lists from received captures. The pass requires completed evidence
mapping and validates the captured post against its native identity. It shares
identical manifests, retains each capture reference and combines compatible
partial lists. Source positions, missing slots, declared albums and known counts
remain separate from download availability. Direct Reddit media establishes a
single attachment without declaring an album; Reddit gallery and retained
Twitter extended-entities lists preserve their source order and media-kind hints.

Automatic migration selection now uses the same combination and protection rules
as ingestion while retaining `origin:migration`. Pinned and disabled choices
remain intact. Contradictory lists retain review context and their source
manifests without replacing the current selection. Unsupported or absent lists
remain unavailable; filename/download counters and folders cannot invent source
order. A mismatching existing capture manifest remains a review outcome.

Application-authorized batches bind exact manifests and ordinals, process at
most 50 rows and check the retained-payload threshold between records. Source
lists, selections, immutable receipts and progress commit atomically. Late
failures roll back all changes even if the caller ignores the error. Summaries
are bounded, full source keys are retrieved individually, and conflict samples
are limited to 128 with an explicit truncation marker. Complete manifests remain
intact. Startup validates progress and reference scope; anonymised exports remove
these receipts. Completed passes remain `imported:false` for the full migration.

The read-only preflight examined 526,348 captures in 196.424 seconds. It found
5,250 captures with supported lists across 1,091 posts, including 559 evidenced
albums; all lists for a given post agreed. The other 521,098 captures lacked
supported attachment-list evidence. Older Twitter captures in this frozen corpus
retain individual-file metadata but no original extended-entities lists. The
native producer already preserves those lists for new captures; original legacy
appearances and file associations still require their separate mappings.

Focused SQLite tests passed for shared lists, mixed image/video hints, Twitter
lists, complementary partial captures, ordinary single-media posts, protected
selections, contradictory/invalid evidence, absent evidence, forgotten posts,
unmapped captures, rollback, stale cursors, prerequisites, startup integrity,
replay and anonymisation. Existing media, metadata and performer associations
remain unchanged. The real HTTP/Python test passed lost-response recovery and
cross-catalog reuse: 55 captures share one manifest and selection, with no extra
selection on copied evidence. The installed isolated package exposes the new
command. Parent `5151e78ae` passed lint, build and native preview publication in CI.

The full fork gate passed: backend generation, v3 generation/types/formatting,
528 v3 tests, 71 current application operation contracts, 232 producer tests,
lint and all Go tests. The same 232 producer tests passed on Python 3.12. API
tests took 288.057 seconds and SQLite tests 430.765 seconds during the rehearsal.
Final conflict context uses consistent JSON field names; the focused regression
suite passed again in 30.214 seconds after that adjustment, including a 200-slot
conflict reduced to 128 marked samples. Final lint reported zero issues.

A fresh copy of the verified schema-1000034 publisher rehearsal was promoted
in 141.375 seconds. The full import processed all 1,697 snapshots and 526,348
captures in 11,820 bounded transactions over 508.812 seconds. Restart and exact
terminal replay checks passed; reopening the completed copy took 112.265 seconds.
Results matched preflight: 5,250 mapped captures, 521,098 unavailable captures,
1,091 changed post selections, and no conflicts or protected choices in this
corpus. These are source-list outcomes, not whole-catalog or gallery completion.

A read-only inventory for the next media phase is retained in
`.local/native-catalog-media-assessment-20261001/inventory.json`. Of 776,976
legacy assets, only 890 have a declared digest algorithm (`fclones-blake3`);
the others cannot be treated as verified content hashes. The 778,523 file rows
include present, missing, pending, converted and deduplicated states. Of 422,442
appearances, 1,533 retain both a source media ID and position, 11,471 retain a
position without a media ID, and 409,438 retain neither. Their original path and
association evidence still require explicit mapping or review. That work must
preserve uncertain order, surviving paths and file states rather than fabricate
complete source manifests or available media.

Independent Python reconciliation reconstructed every original capture, verified
its header checksum, and derived the Reddit lists and direct-media references
without calling the Go extractor. It checked each source position, media-kind
hint, known count, completeness flag and evidence path, plus the shared manifest
and selection signatures. The 5,250 capture associations share 1,091 manifests
and selections containing 2,986 attachment entries; 559 posts carry album
evidence. All 153 unaffected tables matched the baseline exactly, including
existing library media, performer links, gallery memberships, publisher choices
and retained source payloads. Every post revision increment was accounted for.
Integrity returned `ok`, with no foreign-key violations. Reconciliation passed
in 248.450 seconds; the resulting database is 10,486,972,416 bytes.

Private copies, helpers and machine-readable receipts are retained under
`.local/native-catalog-attachments-rehearsal-20261001/`. Logs under
`/tmp/stash-native-transition` use the `catalog-attachments-` prefix:
`preflight.log`, `focused.log`, `focused-final.log`, `http.log`,
`python-focused.log`, `promotion.log`, `full-import.log`,
`independent-reconciliation.log`, `validate-fork.log`, `python312.log`,
`lint-final.log` and `package-install.log`. The original schema-1000034 copy and
frozen catalog sources remain unchanged.

Assets/files, legacy appearances, memberships, sidecars, translations, edits and
remaining histories still need native mapping and final semantic reconciliation.
Verified media associations and gallery construction follow that work. No source
jobs were activated and production, workers and n8n remain on the frozen
compatible deployment. The full transition remains active.

## Post media associations with incomplete legacy provenance

Schema 1000036 extends the existing `source_media_evidence` domain rather than
adding a parallel catalog association table. An association always identifies
its source post and library scene/image. Legacy/review evidence can omit an
attachment or capture when the original catalog does not establish that
relationship. Supplying both still requires the attachment's actual presence
in that capture's manifest. Observed/verified file evidence retains its full
attachment, capture and current file-association requirements.

This is needed for the 409,438 frozen appearances with neither a media ID nor
position, and for other appearances whose download counters cannot establish
source order. The repository retains uncertain provenance without manufacturing
captures, attachment IDs, positions, galleries or playable library records.
Evidence alone cannot select media or change performer attribution. A bounded
post lookup includes both general and attachment-specific associations.

Foreign keys enforce post scope even for direct SQL writes. Nullable scope
fields remain immutable; added provenance is separate evidence. A single insert
advances post/attachment review revisions atomically, while exact replay leaves
them unchanged. Forgotten posts reject new evidence but retain historical
replay. Archive UUID adoption and merge/deletion history remain supported.

Focused SQLite tests passed in 35.192 seconds and ingestion tests in 57.277
seconds. They cover unknown scope, multiple posts using one image, targeted
pagination, invalid references, replay, retirement, nullable-field mutation,
atomic rollback after a revision-trigger failure, preserved UUID adoption,
source-choice protection, and real schema-1000035 migration with populated
attachment evidence. Startup rejects corrupt scope before modifying the file.

The isolated 10,486,972,416-byte attachment rehearsal copy promoted to schema
1000036 in 252.940 seconds. Migration retains existing evidence IDs, capture
membership, details, timestamps and review revisions. Independent reconciliation
passed in 399.685 seconds: all 162 unaffected tables match exactly, the earlier
migration ledger is retained, integrity is `ok`, and there are no foreign-key
violations. The result is 10,486,980,608 bytes. This corpus still has zero native
media-evidence rows; the populated schema-1000035 fixture separately verifies
conversion of existing attachment evidence. Normal application startup reopened
the migrated copy successfully in 110.318 seconds.

The required validation set passed. `make validate-fork` completed generation,
v3 types/format/locales, 528 v3 tests, 71 native operation contracts and 232 Python
producer tests. Lint identified the unused revision helper replaced by the SQL
trigger; after its removal, `make lint it` passed with zero lint issues and the
complete Go suite. The API package took 350.572 seconds, ingestion 477.271
seconds, and SQLite 484.752 seconds.

Private copies, the independent row-digest verifier, startup helper and
machine-readable reconciliation are under
`.local/native-post-media-rehearsal-20261001/`. Logs under
`/tmp/stash-native-transition` use the `post-media-` prefix, including
`focused-final.log`, `promotion.log`, `independent-reconciliation.log`,
`validate-fork.log`, `backend-final.log` and `reopen.log`.

A separate read-only path assessment used the historical `/media/porn/` mount
against the frozen Stash copy; it did not register or activate a root. Its
769,643 file rows each have one scene/image owner and no ZIP membership. Of
778,523 catalog file rows, 769,655 present rows have an exact recorded path and
size match, while 6,288 present rows have no matching Stash path. Catalog rows
can repeat a file across catalogs. There are also 1,058 deduplicated rows with
matching survivor paths/sizes, and 179 converted/source-reference rows with a
survivor but no retained original size. Other missing/pending/survivor cases
remain unresolved. This comparison verifies database claims only, not current
filesystem bytes, availability or hash identity. The assessment passed in
6.919 seconds and is retained in
`.local/native-catalog-media-assessment-20261001/path-preflight.json`.

Asset/file binding and appearance import remain the next work, with explicit
root/path review, ambiguity handling, original state preservation and no
replay of historical filesystem actions. No catalog appearances have yet been
associated to native library media by this change. Production and the original
schema-1000035 rehearsal copy remain untouched.

## Shared source claims and unavailable file evidence

Schema 1000037 adds the `SourceFile` domain for shared source content claims,
file observations, guarded matches to existing library files, and post-file
evidence. Original asset references and declared digests remain source claims;
they do not become verified hashes or playable files. Observations preserve
present, missing, pending and deduplicated states, converted-source roles,
original size/modification times and surviving paths. A collection's locations
can share one claim across later collection definition revisions.

Matches retain the current file identity and generation when recorded. ZIP
matches also require the real archive identity/generation and member relation.
Exact paths are literal and reject conflicting reported sizes or modification
times; converted survivors use the shared asset's size. Declared SHA-256 can
match only existing server-verified content for that generation. Historical
replay survives file changes, deletion and UUID adoption. New matches must
validate the current state. Post-file evidence retains unavailable appearances
without inventing attachments, capture membership, source order or galleries.

The focused SQLite suite passed in 8.971 seconds, including shared claims,
offline roots, cross-collection rejection, replay, generation conflicts, ZIP
identity, declared-versus-verified hashes, forgotten posts, startup corruption
and anonymisation of populated private evidence. The final backend suite also
covers the added modification-time conflict case. `make validate-fork` passed
generation, 528 v3 tests, 71 native contracts and 232 producer tests. Lint found
a test helper available only under integration tags; replacing that dependency
allowed `make lint it` to pass with zero lint issues and the complete Go suite
(API 310.169 seconds, ingestion 437.782 seconds, SQLite 449.904 seconds).

The isolated schema-1000036 copy promoted in 232.491 seconds and reopened through
normal application startup in 112.446 seconds. Independent reconciliation passed
in 395.835 seconds: all 163 existing data tables match exactly, the earlier
migration ledger is preserved, the four new tables start empty, integrity is
`ok`, and foreign-key violations are zero. The resulting copy is 10,487,074,816
bytes. Private helpers, the copy and reconciliation report are under
`.local/native-source-files-rehearsal-20261001/`; logs under
`/tmp/stash-native-transition` use the `source-files-` prefix.

This commit supplies the native evidence model, not the asset/file/appearance
import pass. That importer follows with explicit root/mount bindings and
per-record receipts; memberships, gallery construction and the remaining
catalog families still need conversion. No production deployment, live data
migration, source activation or develop merge was performed.

## Catalog assets, file locations and post appearances

Schema 1000038 adds a resumable catalog media import with immutable root/mount
bindings and one receipt per original asset, file and appearance. The API and
`stash-import-catalog-media` client require the frozen manifest digest, logical
root/revision, collection revision and historical library mount. The root can
remain disabled and unbound; this mapping grants no live filesystem access.
Each transaction processes at most 50 records, through assets, files and
appearances in dependency order. The advance cursor is the processed-record
count, while receipt inspection uses original source ordinals. Restart and lost
responses resume committed progress without changing the binding.

Asset rows become shared source claims, preserving their declared digest, size
and original time. File observations retain the original state, role, path,
nanosecond modification time, first-observed value and survivor. Exact path
matches are literal and require agreeing recorded metadata; survivor matching
uses the asset size. Existing verified hashes can establish content matches,
but a catalog's declaration never becomes fresh byte verification. ZIP matches
require actual archive/member identities and both generations. Appearances
retain post-file evidence even when playable library media is unavailable.
An association requires a unique current scene/image owner and rechecks the
matched file generation. Legacy download positions remain evidence rather than
invented source attachment identities or album order.

The timestamp preflight found 73,920 differences caused entirely by subsecond
precision: Stash's existing file timestamps were stored at whole-second
precision. There were no whole-second conflicts. Matching now compares the
precision actually retained by Stash while preserving every original source
nanosecond value. Regression coverage separately rejects real time conflicts.

The isolated schema-1000037 copy promoted in 187.705 seconds. All 1,697 frozen
catalogs then completed the pass in 2,498.429 seconds, including normal startup
reopening in 200.936 seconds and exact completed-receipt replay. The pass used
40,522 bounded transactions and accounted for all 1,977,941 input rows:

| Input | Mapped | Unavailable | Review |
| --- | ---: | ---: | ---: |
| Assets | 776,976 | 0 | 0 |
| File locations | 770,843 | 7,631 | 49 |
| Post appearances | 415,028 | 7,393 | 21 |

Native results are 776,976 shared claims, 778,474 file observations, 770,843
guarded file matches, 422,421 post-file evidence rows and 415,028 post-to-media
associations. Unavailable file rows include 211 pending downloads. These are
historical database matches, not confirmation of current filesystem bytes.
The original source rows remain retained, including invalid paths and affected
appearance references requiring review. Forty-nine file paths contain literal
backslashes rejected by the native relative-path contract; 18 appearances refer
to those files. Three other appearances still name older path-based assets
where the same catalog file now names a deduplicated BLAKE3 asset. Those claims
remain distinct pending reconciliation with the historical dedupe evidence.
This pass changes no media metadata,
performer attribution, selected attachments, existing galleries or media files.

Independent reconciliation against the original frozen SQLite catalogs passed
in 363.477 seconds. It compares every original asset/file/appearance value with
its staged record and native outcome, verifies deterministic identities and
exact path/size/time/generation matches against the pre-import library, and
checks every post/media association against the original unique file owner.
All 159 unaffected tables and the prior migration ledger match exactly; every
post revision increment is accounted for. Integrity is `ok` and foreign-key
violations are zero. The resulting copy is 14,250,086,400 bytes.
Normal startup with the final, strengthened receipt-scope checks also passed
in 156.786 seconds (`catalog-media-current-reopen.log`).

Focused SQLite and real HTTP tests passed, including shared claims, converted
survivors, unavailable media, bounded phase checkpoints, lost responses, replay,
conflicts, ZIP members, stale generations between phases, rollback after an
injected receipt failure, startup rejection and anonymisation. Generation, v3
validation, 528 v3 tests, 71 native contracts and 235 producer tests passed.
The full producer suite also passed under Python 3.12 (7.072 seconds), and the
installed package exposes the new CLI. Go lint reported zero issues. The full
API suite passed in 460.519 seconds; ingestion and SQLite initially reached the
default ten-minute package deadline while the large import was running. Both
passed when rerun with a 25-minute package deadline (564.794 and 589.070 seconds),
with the rest of the full Go suite already passing.

Private copies, import/startup helpers, per-catalog receipts and independent
reconciliation are under `.local/native-catalog-media-rehearsal-20261001/`.
Timestamp assessments are under
`.local/native-catalog-media-assessment-20261001/`. Logs under
`/tmp/stash-native-transition` use the `catalog-media-` prefix, including
`promotion.log`, `full-import.log`, `independent-reconciliation.log`,
`focused-http.log`, `validate-fork.log`, `backend-final.log`,
`backend-retry.log` and `python312.log`.

Memberships, source gallery construction and remaining catalog families still
need conversion, so the snapshot's `imported` result remains false. Production,
source activation and develop remain unchanged.

## Native post collection memberships

Schema 1000039 adds immutable direct post-membership evidence, separate from
capture membership, media intake and performer attribution. The native model
and paged application API retain the post, historical collection revision,
observation time and provenance. Exact replay survives collection renaming or
retirement and post forgetting; forgotten posts reject new evidence. New
memberships advance the post's review revision without changing its metadata.

`stash-import-catalog-memberships` maps the original membership rows after the
snapshot's evidence pass, independently of file availability. Registry-qualified
collection keys identify shared groups across downloaded catalogs. Legacy
`creator` values came from punctuation in folder names and map to directory
groups without owner attribution. Subreddit groups retain their kind. New
groups are disabled with no inferred account, target URL or root. Conflicting
labels and unsupported identities retain the complete original row for review.
Later native edits do not overwrite historical group definitions or receipts.

Each transaction processes at most 50 records, with a between-row byte bound,
and commits native groups, memberships and receipts together. The client resumes
from the last committed original ordinal after lost responses. HTTP inspection
includes bounded receipt lists, full original values, collection-to-post evidence
and post-to-collection evidence. This supplements capture membership; these
groups do not by themselves create source albums or start scrapes.

Focused SQLite and real HTTP tests passed in 11.565 and 5.459 seconds. Coverage
includes bounded restart, shared groups across two independent catalog snapshots,
lost responses, immutable replay, conflicting definitions, original-value
inspection, later collection retirement, forgotten posts, atomic rollback after
an injected receipt failure, corrupt-scope startup rejection and anonymisation.
The full validation set passed generation, v3 formatting/types/locales, 528 UI
tests, 71 native contracts, 238 producer tests and Go lint with zero issues.
All 238 producer tests also passed under Python 3.12 in 6.741 seconds; package
installation and the new CLI entry point were checked.

The full Go run passed ingestion in 446.721 seconds and SQLite in 477.369 seconds.
Its API package initially failed three downloader-worker cases because the direct
Go command selected system Python, which lacked gallery-dl. Those cases passed
in 26.068 seconds with `PRODUCER_PYTHON` pointing at the prepared environment;
the remaining Go tests passed in the original run. Tests used a 25-minute package
deadline while the independent full-copy migration was using disk I/O.

The isolated 14,250,086,400-byte schema-1000038 copy promoted in 547.148 seconds.
All 1,697 catalog snapshots completed membership import in 413.086 seconds,
including 154.108 seconds to reopen through normal startup and exact terminal
receipt replay. The 6,456 transactions mapped all 257,001 original memberships
into 912 shared native groups, with zero review conflicts. The original
schema-1000038 copy and production remain unchanged.

Independent reconciliation passed in 349.851 seconds against all original
frozen SQLite catalog membership rows. It verifies the original values and
source digests, deterministic group and evidence identities, post mappings,
historical collection definitions, timestamps, provenance and terminal receipts.
All 166 unaffected tables and the prior migration ledger match exactly. Every
existing collection definition is unchanged; the only additions are the 912
disabled groups. All 257,001 post revision increments are accounted for.
Integrity is `ok` and foreign-key violations are zero. The resulting copy is
14,483,116,032 bytes.

Private helpers, copies, per-catalog receipts and the independent reconciliation
report are under `.local/native-catalog-membership-rehearsal-20261001/`.
Logs under `/tmp/stash-native-transition` use the `catalog-membership-` prefix,
including `promotion.log`, `full-import.log`, `independent-reconciliation.log`,
`focused-http-final.log`, `validation.log`, `backend.log`, `worker-retry.log`,
`python312-full.log` and `package-install.log`.

Source gallery construction, remaining catalog families, native UI and the
other transition phases remain open. No live migration, source activation,
production deployment or develop merge was performed.

## Imported media matching for source albums

The native gallery service now previews and applies attachment-to-media
associations from an individual post's imported file evidence. Callers choose
qualified retained media IDs or explicitly enable the historical Reddit
`post-id_media-id` filename convention. Matching uses original observations,
including those whose files were deduplicated or converted, and validates the
current library file generations and unique ownership. It never takes album
identity or source order from a filename, folder or download counter.

Duplicate catalog proofs share one media candidate. Existing attachment choices,
including explicit unlinks and undecided reviews, are preserved. Ambiguous
candidates, changed files/ownership and media-kind conflicts remain unresolved.
The complete bounded candidate set participates in preview validation. Applying
the signature records legacy attachment evidence without invented captures,
selects supported media, and uses the existing gallery sync service in one
managed transaction. Manual membership/exclusions, cover and metadata choices,
source gaps, repeated attachments and deletion suppression remain intact.
Pre-commit validation and an atomic-completion guard prevent partial publication.

Focused gallery, file and media SQLite tests passed in 17.798 seconds. The new
cases cover original-versus-survivor paths, explicit Reddit/Twitter IDs, service
qualification, malformed/conflicting IDs, filename boundaries, duplicate proofs,
missing slots, existing decisions, ambiguous ownership, stale previews, forgotten
posts, disabled selections, manual exclusions, deletion, candidate limits,
restart replay and injected/late-write rollback. Generation, all v3 checks
(528 tests in 91 files plus 71 native contracts), all 238 producer tests and Go
lint passed. The complete Go integration suite passed with the prepared producer
runtime: API 325.452 seconds, ingestion 448.220 seconds and SQLite 477.203 seconds.

An isolated copy of the fully imported schema-1000039 library was previewed and
applied with the explicit historical Reddit policy. Of 1,091 selected posts,
559 were evidenced albums and 532 were ineligible single-media posts. The core
created 559 logical galleries, selected 1,253 attachments and added 1,253 gallery
memberships, with zero ambiguous candidates and zero removals. Another 1,201
album attachments remain unselected because the retained evidence did not
establish a usable match.
All posts previewed in 5.198 seconds; application took 13.236 seconds. The whole
rehearsal took 589.541 seconds, including normal startup under concurrent test
load (391.272 seconds), reopening (176.324 seconds), and verifying that replay
reuses every gallery without another membership or media decision.

Independent reconciliation passed in 275.015 seconds. It rederives the matches
from the original observations and post/attachment IDs, checks every new proof
and decision, and verifies ordered source slots, gallery metadata and actual
memberships. The 1,504 new legacy proofs retain their original evidence links
without invented capture membership. All 159 unaffected tables match exactly;
existing records in the changed tables also remain intact. All 3,316 post and
2,757 attachment revision increments are accounted for. Integrity is `ok` and
foreign-key violations are zero. The resulting copy is 14,488,055,808 bytes.

The memberships comprise 1,240 images and 13 scenes. Of the 559 galleries, 266
have matches for every retained attachment, 42 have some matches, and 251 have
none yet. Empty source albums retain their known manifests and metadata without
inventing playable entries. This is distinct from proving that every source
attachment has downloaded or that the source list itself is complete.

Private copies, previews, results, helpers and independent reports are under
`.local/native-source-album-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `album-backfill-` prefix: `focused.log`,
`validation.log`, `backend.log`, `rehearsal.log` and
`reconciliation-final.log`.

The public Apply workflow, durable hook checkpoint/worker, native UI and remaining
transition phases still need implementation. This increment adds no schema
migration and does not expose an HTTP mutation. The original imported copy,
production deployment, source activation and develop remain unchanged.

## Durable historical album Apply and restartable notifications

Native schema 1000040 adds `album.backfill` to the existing durable job service
and an indexed per-resource history lookup. The table rebuild preserves job
identities, submissions, attempts, receipt references and progress/result
checkpoints. File ingestion receipts still require the `media.verify` kind.

The application now exposes read-only album previews and durable Apply,
submission/job status, bounded post/attempt history, cancellation and explicit
retry under `/api/v3/archive`. Requests pin the post UUID, matching policy and
preview signature. Exact lost-response replay returns its original job, even
when the successful application has since changed the preview. Responses use
snake_case names and calendar dates, with one public signature and initial
metadata only for gallery creation.

A metadata-only worker runs independently of FFmpeg and producer configuration.
It rechecks the preview and commits media choices, gallery changes and a compact
publication checkpoint in one transaction. Hooks run afterwards under a renewed
lease. Status distinguishes a committed publication from finished notifications.
Restart and transient failures resume the checkpoint; explicit retry retains
terminal history and the original publication event identity. Cancelling a
queued notification retry also preserves that publication. Later library edits
are never reapplied by a notification retry, and unpublished stale work requires
a fresh preview. Plugin delivery remains at least once, with stable event IDs.
Deleted gallery UUIDs do not resolve to a replacement that reused a numeric ID.

Real SQLite tests cover admission/coalescing, lost acknowledgements, queue
backpressure, targeted history, stale evidence, checkpoint rollback, retry
exhaustion, cancellation before/after publication, lease renewal and lost
ownership, restart recovery, and terminal retry chains. HTTP tests exercise the
actual handlers and worker lifecycle; a JavaScript plugin verifies event IDs
and gallery create/update fields. The populated schema-39 migration fixture
preserves queued, running and terminal file jobs, their attempts/submissions and
ingestion receipt, then verifies the retained guards and indexed album history.
A missing history index is rejected before startup changes database bytes.

The final focused album tests passed (SQLite 38.901 seconds, HTTP 10.489 seconds,
manager hooks 10.345 seconds). Backend generation, all v3 checks (528 tests in
91 files and 71 native contracts), all 238 producer tests, and Go lint passed.
The complete Go integration suite also passed: API 330.924 seconds, ingestion
451.294 seconds and SQLite 490.939 seconds. Final affected tests include the
explicit 404 for submitting work against a missing post.

The complete selected-post rehearsal used a fresh copy of the schema-1000039
membership-import baseline, upgraded through the normal migration path. All
1,091 requests completed: 559 source galleries, 1,253 selected attachments and
memberships, no removals or ambiguous candidates, and 1,201 unavailable album
attachments retained as gaps. The 532 single-media posts completed as ineligible
no-ops. Each original request remains replayable without a second job.

The rehearsal intentionally interrupted notification delivery after publication,
closed/reopened the database and recovered the expired lease. The same original
publication and event identity survived, with one expired attempt followed by
success. There are 1,091 jobs/submissions and 1,092 attempts, with no duplicate
gallery or media decision from recovery. Per-post history also matches every
original submission.

Normal opening/promotion under concurrent test load took 584.474 seconds and
reopening took 149.610 seconds. Previewing took 3.397 seconds, admission 5.573
seconds and post-restart processing 22.490 seconds; the full rehearsal including
replay checks took 766.832 seconds. Startup validation is measured separately
from the targeted preview and durable Apply operations.

Independent reconciliation passed in 364.935 seconds. All 154 unaffected tables
match exactly; every original record in changed domain tables is preserved.
The verifier rederives attachment matches from source evidence and checks the
1,504 new proofs, 1,253 media choices/memberships (1,240 images and 13 scenes),
3,316 post revision increments and 2,757 attachment revision increments. It also
verifies every job's resource/work key, original submission, publication, outcome
and attempt history, including the recovered event. Integrity is `ok`, with zero
foreign-key violations. The final copy is 14,489,649,152 bytes.

Private copies, persisted requests, previews, statuses, interruption evidence,
helpers and independent reports are under
`.local/native-album-jobs-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `album-jobs-` prefix: `final-focused.log`,
`validation.log`, `lint.log`, `backend.log`, `rehearsal.log` and
`reconciliation.log`.

The native album review UI and migration command remain subsequent work, along
with remaining catalog families, native UI/caller conversion, activation,
backup/restore drills and final cutover reconciliation. No production database,
source job, deployment or develop merge was changed by this increment.

## Supported source album migration command

`stash-backfill-source-albums` now prepares and applies historical album work
through the native application API. A private saved plan binds each post to its
matching policy, complete preview, signature and durable request UUID. The command
checks the manifest digest, every record and the explicit Stash origin before
applying. Status verifies the job's post, policy, original signature, publication
counts and retry parent. Interrupted calls reuse the saved submissions; stale
previews remain conflicts instead of being refreshed silently.

Indexed selected-post discovery exposes UUID pagination without materializing
all manifests. Disabled and forgotten posts remain accounted for; unselected
posts do not enter the plan. Forgotten entries and previews requiring review
are retained without submission. Separate commands inspect, cancel and prepare
an explicit retry. Preparing a retry makes no mutation, and a committed
publication retains its original event through later notification retries.
Plan completion remains distinct from full catalog migration or complete media
downloads. No new schema migration is required.

Real SQLite tests verify bounded discovery, exclusions, restart, canonical
cursors and indexed queries. The supported Python CLI runs against the actual
Go HTTP handlers and worker with lost admission, cancellation and retry replies;
restarting the client recovers the original jobs with one POST per mutation.
Producer tests also cover record tampering, changed endpoints, private plan
publication, notification retry chains, review outcomes, contradictory server
responses and HTTP transport boundaries. All ten new tests pass on Python 3.12
and 3.14. Backend generation, all v3 validation (528 tests in 91 files and 71
native contracts), all 248 producer tests and Go lint passed. The complete Go
suite passed: API 411.114 seconds, ingestion 527.513 seconds and SQLite 561.364
seconds. The uppercase-cursor fixture was subsequently made deterministic and
rechecked separately.

The installed command was rehearsed through a private local HTTP server against
a fresh 14,489,649,152-byte copy of the previous schema-1000040 rehearsal. All
1,091 selected posts completed: 559 existing galleries synchronized without new
memberships or media choices, and 532 single-media posts remained ineligible
no-ops. The plan retained 1,253 existing attachment choices and 1,201 unavailable
attachments. Repeated Apply before and after processing returned the same jobs
without another submission. The server loaded no production plugins and used
no production API key, website credentials or media mount.

Copying took 72.119 seconds. Normal database opening under concurrent test load
took 503.493 seconds; previews took 5.879 seconds, admission and pending replay
7.642 seconds, and worker processing 21.552 seconds. The complete command
rehearsal, including finished-status replay, took 539.788 seconds. Startup
validation remains separate from targeted API operation timings.

Independent reconciliation passed in 295.859 seconds. All 172 tables outside
job history match the source copy exactly. Every original job, submission and
attempt is preserved; precisely 1,091 rows were added to each of those three
tables. The verifier checks saved plan hashes, post coverage, work/resource keys,
submission identity, publication counts, results and attempts directly against
SQLite. Integrity is `ok`, with zero foreign-key violations. The resulting copy
is 14,491,217,920 bytes.

Private plans, API reports, copied data, rehearsal helpers and the independent
report are under `.local/native-album-cli-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `album-cli-` prefix: `focused.log`,
`python.log`, `python312.log`, `discovery-final.log`, `validation.log`,
`backend.log`, `rehearsal.log` and `reconciliation.log`.

Native album review UI, remaining catalog families, caller conversion,
activation, backup/restore drills and final cutover reconciliation remain open.
This increment does not migrate live data, activate source jobs, deploy
production, or merge develop.

## Native retained documents and application inspection

Schema 1000041 adds shared original document bytes, distinct parser
interpretations, collection/path/post associations, historical selected-head
claims and revisioned native choices. A single document can serve many paths
or posts; differing interpretations reuse its content bytes. Empty, malformed
and repaired NFO input remains evidence with its original parser status,
warnings and unknown fields. Source times retain their exact spelling and
precision. Paths remain literal historical evidence, including backslashes and
unattributed folder defaults; reading them cannot open a server filesystem path.

The application API exposes one document or byte download at a time, with
indexed bounded post/location association, claim and decision-history pages.
Selection requires the current revision and exact collection/path/claim scope.
Explicit unlinks survive later capture or migration attempts. New evidence
advances post review revisions but creates no media or performer attribution
and applies no scene/image metadata. These operations use application
authentication; source producer routes cannot access them.

Core and HTTP tests cover shared bytes, preserved interpretations, empty and
invalid content, exact replay, conflicting scope, forgotten posts, guarded
choices, lost edit replies, strict inputs, indexed lookups and restart. The
normal SQLite backup retains populated document bytes, claims and explicit
selection history; anonymised exports remove their private content. Startup
refuses missing guards and corrupted bodies/interpretations without rewriting
the database. A real schema-40 upgrade preserves existing entities; an injected
table collision rolls back all new document tables and leaves a dirty migration
that normal startup refuses.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 248 producer tests and Go lint passed. The complete Go suite also
passed: API 356.100 seconds, ingestion 465.573 seconds and SQLite 520.524 seconds.

A read-only assessment passed all 272,556 documents, 415,919 path associations
and 415,613 selected-head timestamps from the 1,697 frozen catalog copies.
This includes 520 invalid, 218 repaired and one empty original document, plus
1,358 associations with no post. Original hashes, parser JSON, paths and times
all satisfy the native retention contract. The source already shares 272,556
distinct byte bodies, totaling 81,558,266 bytes before native compression;
the new system can preserve those shared references. This assessment imports
no catalog records.

A fresh 14,491,217,920-byte copy of the preceding album CLI rehearsal was made
in 40.426 seconds. Promotion from schema 1000040 to 1000041 completed in
265.576 seconds, including the existing native validation before migration.
Normal opening of the resulting schema-41 database passed in 173.131 seconds.
Independent reconciliation passed in 593.804 seconds: all 173 pre-existing data
tables match exactly, every earlier migration-history row is preserved, and
only the schema version and one history entry changed. The six new document
tables remain empty pending the importer. Integrity is `ok`, with zero
foreign-key violations. The resulting copy is 14,491,320,320 bytes.
Private copies, assessment and reconciliation helpers/results are under
`.local/native-documents-rehearsal-20261002/`; logs use the `source-documents-`
prefix under `/tmp/stash-native-transition`: `generation.log`, `validation.log`,
`backend.log`, `http.log`, `migration.log`, `backup.log`, `assessment.log`,
`rehearsal.log`, `reopen.log` and `reconciliation.log`.

To provide rehearsal space, the older schema-39 source-album copy was compressed
from 14,488,055,808 to 3,184,896,088 bytes. Decompression matched the original
SHA-256 before the redundant raw copy was removed. Its archive, checksum report
and original verification artifacts remain in
`.local/native-source-album-rehearsal-20261002/`.

The resumable catalog-document importer, native document UI, remaining catalog
families, caller conversion, activation, coordinated backup/restore and final
cutover reconciliation remain open. Production data, deployments, source jobs
and develop remain unchanged.

## Resumable retained catalog document import

Schema 1000042 adds a bounded document-import pass and immutable per-row
receipts. The supported `stash-import-catalog-documents` command uses the native
application API after the received snapshot's evidence pass. It maps normalized
documents and path associations, or the older flat sidecar format, before
selected-head rows. Original bytes, exact parser evidence, literal paths and
source timestamp spelling survive. Shared documents reuse one byte body and
interpretation while preserving each collection/path/post association.

Explicit historical heads take precedence. A path without one retains the
legacy reader's timestamp-text/hash ordering as a labeled `legacy_fallback`
claim. Invalid explicit heads require review. The importer preserves existing
native choices, including manual unlinks, and retains conflicting historical
claims for inspection. Associations retain the registry-created collection
revision even after later renaming, root binding or retirement. Importing
evidence advances post review revisions without applying entity metadata,
changing performer ownership, creating library media or constructing galleries.

Each transaction processes at most 50 records with a 16 MiB batch threshold.
The processed-record checkpoint commits with domain facts and receipts. A
restarted client reads saved progress after a lost response, and completed
replays issue no further writes. Startup validates phase coverage, counts and
native reference scope. Bounded API summaries keep large original values in
individual record responses. Anonymised exports clear the new private receipts.
Every completed pass still reports `imported:false` pending the remaining
families and final reconciliation.

Focused tests cover dependency-phase resume, rollback after a receipt failure,
shared bytes, exact source values, invalid timestamp review, forgotten posts,
write/checkpoint guards, renamed collections, manual unlinks and later review
changes. The HTTP test runs actual Python preparation/upload/import through
the Go API, loses a committed batch response, replays completion, checks copied
catalogs, and imports real v1 flat storage and an empty document family.
Python receipt checks reject changed bindings, phase regression, stalled
progress and false completion. Python 3.12 validation and package installation
also pass.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 251 producer tests and Go lint passed. The full Go suite passed:
API 357.549 seconds, ingestion 468.535 seconds and SQLite 521.282 seconds.

An isolated 14,491,320,320-byte schema-41 copy was made in 49.179 seconds.
Promotion to schema 1000042 passed in 587.242 seconds while the full test suite
was also running. All 1,697 frozen catalogs then mapped successfully in 23,350
bounded transactions: 1,104,088 source rows and zero review outcomes. The pass
retained 272,556 shared documents, 415,919 path associations and 415,613 explicit
head claims/selections, including 1,358 associations without a post. The shared
original bytes total 81,558,266 before compression. Completed receipt replay
after reopening passed; reopening took 242.983 seconds, and import plus reopen
and replay took 1,196.500 seconds.

Independent reconciliation passed in 271.930 seconds. Fresh row fingerprints
for all 172 unrelated data tables match the preceding baseline's verified
fingerprints. Every physical catalog document/source/head row matches its
original frozen SQLite values, including binary content, exact parser text,
paths, times, post mappings, historical collection revisions and native
decisions. Native byte hashes and document identities were checked independently
of the Go reader. The only changes to pre-existing post rows are the expected
1,243,071 review-revision increments. Earlier migration history and sequence
values are preserved. Integrity is `ok`, with zero foreign-key violations.
The resulting isolated database is 16,420,761,600 bytes.

Private helpers, receipts, copy metadata and independent reports are under
`.local/native-catalog-documents-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `catalog-documents-` prefix:
`focused.log`, `generation.log`, `backend.log`, `validation.log`, `package.log`,
`python312.log`, `promotion.log`, `rehearsal.log` and `reconciliation.log`.

For rehearsal space, two older generated database copies were compressed and
their decompressed SHA-256 digests verified before removing the redundant raw
files. The membership rehearsal changed from 14,483,116,032 to 3,183,657,884
bytes; the album-job rehearsal changed from 14,489,649,152 to 3,185,332,761
bytes. Their `.sqlite.zst` archives, checksum reports and original verification
artifacts remain in their respective private rehearsal directories. The
schema-41 baseline and original frozen catalog inputs remain available.

Native document/album review UI, translations, remaining catalog families,
caller conversion, activation, coordinated backup/restore and final cutover
reconciliation remain open. This checkpoint imports only isolated copies;
production data, deployments, source jobs and develop remain unchanged.

## Shared translation results and retained catalog import

Schema 1000043 stores exact original/output text and nullable language/provider
facts once per shared result, with separate post and historical collection
provenance. Native original-text hashes cover exact UTF-8 bytes. Historical
declared hashes retain their algorithm and do not replace that verified identity.
Unknown languages stay unknown, including during English-language lookups.
Application read APIs page post evidence without repeating text bodies.

`stash-import-catalog-translations` imports the received frozen snapshot after
its evidence pass. Transactions process at most 50 rows with a 16 MiB threshold;
the source-ordinal checkpoint commits with results, assertions and immutable
receipts. The original collection revision survives later renaming or retirement.
Missing hashes/originals remain explicit. Conflicting hashes, invalid provenance
and forgotten posts retain review outcomes. Import neither applies entity
metadata nor starts provider jobs; whole-catalog status stays `imported:false`.

Tests cover shared results, exact Unicode/control characters, nullable identity,
historical Python JSON hashes, language filters, malformed values, stale cursors,
transaction rollback, forgotten posts, corrupt startup data, ordinary backup
restore and anonymisation. The actual Python-to-Go HTTP fixture loses the first
committed batch response, resumes/replays, imports copied/old/empty catalogs,
checks bounded routes and reopens the database. The Python client rejects changed
bindings, stalled progress and false completion. Package installation and its
new command entry point passed.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 254 producer tests on Python 3.14 and 3.12, and final Go lint
passed. The full Go suite passed: API 366.265 seconds, ingestion 472.693 seconds
and SQLite 531.604 seconds. Final focused checks also passed after the validator
cursor cleanup and added backup assertion.

The isolated schema-42 copy took 34.285 seconds and 16,420,761,600 bytes.
Promotion to schema 1000043 passed in 734.252 seconds under concurrent test load.
All 1,697 catalogs mapped 59,023 translation rows in 2,692 bounded transactions,
with zero review outcomes. Import, normal opening, reopening and completed-receipt
replay took 609.322 seconds; reopening accounted for 301.423 seconds. Startup
validation remains a material measured cost for the final performance gates.

Independent reconciliation passed in 448.862 seconds. Every physical source row,
shared result, original UTF-8 hash, declared historical JSON hash, exact timestamp,
post reference and original collection revision matched. There are 52,078 shared
results and 59,023 provenance assertions; all 59,023 historical hashes verified.
Target language/provider remain unknown for 56,168 rows; 2,855 explicitly record
English and translate-shell/Bing. Fresh row comparisons against the preceding
copy confirm all 180 unrelated tables are unchanged. The only existing post-row
changes are 59,023 expected review-revision increments. Prior migration history
and sequences are preserved. Integrity is `ok`, foreign-key violations are zero,
and the final copied database is 16,522,178,560 bytes.

Private copy metadata, importer/verifier helpers, receipts and reports are in
`.local/native-catalog-translations-rehearsal-20261002/`. Validation logs under
`/tmp/stash-native-transition` use the `source-translations-` prefix. To retain
rehearsal space, the unused schema-41 copy was compressed from 14,491,320,320 to
3,185,745,882 bytes; its decompressed SHA-256 was verified before deleting the
redundant raw copy. Its archive, checksum report and earlier evidence remain.

A separate read-only automation assessment is retained in
`.local/native-translation-queue-assessment-20261002/`. It contains 240,397
translation jobs, 396,679 targets and 42,732 cached outcomes, including 40,777
English detections, 1,954 translations and one no-text outcome. Of the targets,
324,195 remain unapplied; retry delays, priorities and original status must
survive the next migration. Input hashes, integrity and foreign keys passed.
Enrichment and discovery tables are inventoried too. This assessment snapshot
is not coordinated with the frozen catalogs or a production cutover.

Translation queue/cache/target migration, remaining catalog histories, native
UI, caller conversion, compatibility removal, coordinated backup/restore and
cutover reconciliation remain open. Production and develop are unchanged.

## Shared translation work and inspection

Schema 1000044 retains shared versioned translation requests, immutable cache
outcomes and per-post/field targets with scheduling history. Exact input text
and output results are shared across targets. Cache outcomes distinguish a
translation, unchanged text already in the target language, and no-text work.
Original provider timestamps remain separate from native receipt times; replay
does not overwrite them or create orphan results for conflicting outcomes.

Held work stays held when a cache arrives or the same target is retained again.
Explicit scheduling requires the current revision and preserves the chosen
priority/deadline. Publication checks the due time and target revision, retains
historical collection scope after renaming/retirement, and atomically records
post provenance. It does not overwrite scene/image fields. Forgotten posts
retain review outcomes; no-text completion invents no translation evidence.

Six application inspection routes expose requests, caches, targets and history.
Target/history pages are bounded and reference shared text. Tests cover cache
sharing, Unicode/control characters, held/delayed work, stale revisions, terminal
replay, missing languages, no-text completion, failed transaction rollback,
forgotten posts, indexed pagination, ordinary backup restoration, anonymisation,
startup corruption refusal and upgrade collisions. The actual Python catalog
import HTTP fixture also passes with the new schema.

The frozen queue assessment was matched read-only against schema 43's retained
post identifiers. Of 396,679 targets, 395,878 have exact imported post references;
801 have no matching post in these copies (793 historically applied and eight
pending). There are no missing collection mappings or ambiguous matches. The
snapshots were taken separately, so these are review inputs for coordinated
reconciliation, not proof that live posts are missing. All 240,397 job inputs
target English; the largest original is 15,711 UTF-8 bytes. The assessment is
`.local/native-translation-queue-assessment-20261002/native-reference-assessment.json`.

A further cache assessment found 371 of the 42,732 cached outcomes labelled
`english` with detected language `en` but output text different from the exact
original. Those historical outputs cannot be passed unchanged to the native
`unchanged` outcome, which requires the original text. The importer must retain
the original rows/results and record an explicit mapping disposition. Counts
and classification are retained in `native-cache-assessment.json` and
`native-cache-conflict-assessment.json` in the same assessment directory.

Execution admission/provider workers and the actual automation import remain
unfinished. This storage checkpoint does not activate translation work, migrate
the live queue, or complete the full archive transition.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 254 producer tests and final Go lint passed. The full Go suite
passed: API 380.380 seconds, ingestion 481.929 seconds and SQLite 554.317 seconds.
Final focused translation/schema/API tests passed after the lint fixes.
Checkpoint `6f74b8846` also passed CI lint, build and native preview publication.

The isolated schema-43 backup took 44.210 seconds and 16,522,178,560 bytes.
Promotion to schema 1000044 passed in 823.506 seconds under concurrent test load.
Independent reconciliation passed in 341.906 seconds: all 185 existing data
tables match the preceding copy row by row, prior schema definitions/history
and sequences are preserved, and the four new work tables are empty. Integrity
is `ok`, with zero foreign-key violations. The resulting copy is 16,522,244,096
bytes. Normal reopening passed in 211.083 seconds.

The startup CPU profile attributes about half the cost to each of two identical
lineage validations: the primary-version lookup and the obsolete fork-version
lookup both create a new validated migrator. This redundant pass is a concrete
performance follow-up; the remaining integrity queries still require budgeting.
Private copy metadata, comparison report, CPU profile and assessment are in
`.local/native-translation-work-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `translation-work-` prefix.

For rehearsal space, the unused schema-42 copy was compressed from 16,420,761,600
to 3,641,362,723 bytes, with its decompressed SHA-256 verified before removing
the redundant raw copy. The compressed archive, checksum report and earlier
reconciliation remain in `.local/native-catalog-documents-rehearsal-20261002/`.
The schema-43 baseline, frozen inputs and production data remain unchanged.

## One integrity pass during native startup

Database startup now reads schema versions through one validated connection.
Native databases no longer open another validated migrator to read an obsolete
fork ledger. Historical import inputs still read their fork version through the
same connection. Every new migrator continues to run the complete pre-write
validation, with no cross-open validation cache or weakened corruption checks.

On the same schema-44 library copy, the measured normal reopen decreased from
211.083 to 136.714 seconds. The CPU profiles show integrity-validation work
decreasing from 204.20 to 107.55 seconds, and the second version-lookup pass is
absent. These are individual rehearsal measurements; the remaining SQL checks
and explicit release performance budgets remain open work.

Focused lineage, historical promotion and corruption-refusal tests pass,
including refusal of an unexpected active fork ledger in a native database.
Backend generation and Go lint pass. The new profile and reopen report are
`reopen-single-pass-cpu.pprof` and `reopen-single-pass.json` beside the original
measurements in `.local/native-translation-work-rehearsal-20261002/`.
The full Go regression suite passed: API 352.198 seconds, ingestion 454.305
seconds and SQLite 512.664 seconds. Logs use the `native-startup-` prefix in
`/tmp/stash-native-transition`. No schema or stored values change in this step.

## Bounded native translation execution

Schema 1000045 adds durable translation jobs bound to at most fifty exact target
revisions. Admission preserves priority, due time and held state, with one active
batch per shared request and a separate translation queue ceiling. Later targets
cannot bypass an existing retry delay or join an already admitted batch. Request,
revision-binding and active-request lookups use indexes; a query-plan regression
test caught and removed SQLite's affinity-induced expression-index scan.

The worker checkpoints cached output under its lease before atomically publishing
its target evidence and outcome. Retries and restored workers reuse that cache.
Cancellation, changed target revisions and lease expiry prevent stale publication;
terminal failures require explicit retry. Forgotten posts retain review without
requiring a provider call. No operation selects scene/image metadata. Application
routes support target creation, scheduling, explicit retry, bounded admission,
status/history and cancellation; scoped producer tokens cannot administer them.

The optional translate-shell/Bing provider retains exact original text, processes
HTML and Unicode chunks, bounds output/runtime, disables init files and terminates
its Unix process group on cancellation. Tests use local fake executables, with no
provider network requests. Server startup leaves execution disabled by default;
missing provider configuration also leaves the worker stopped. Production has
not been activated or migrated.

The historical automation import, automatic capture scheduling, remaining archive
record families, native UI, compatibility removal, coordinated backup/restore and
cutover gates remain open. The full transition goal is still active.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 254 producer tests and final Go lint passed. The full Go suite
passed: API 402.665 seconds, ingestion 496.017 seconds and SQLite 575.472 seconds.
Targeted race tests passed for the worker, repository, API and provider. The
translation package cross-compiles for Windows with the Unix provider disabled.
Final provider tests additionally reject corrupt Unicode/duplicate JSON keys and
prove that stdout copying cannot bypass the output ceiling. Local process tests
verify cancellation also stops the provider's child process.

Migration fixtures preserve preexisting jobs, receipts, attempts and checkpoints;
upgrade collisions roll back the recreated jobs table. Additional tests cover
bounded concurrent admission, priority, backoff across new targets, stale leases,
held/superseded targets, forgotten posts without cached text, atomic admission
and publication failure, restored caches, startup corruption refusal, full job
anonymisation, origin checks and producer-access isolation.

The isolated schema-44 backup took 38.005 seconds and 16,522,244,096 bytes.
Promotion to schema 1000045 passed in 332.487 seconds. Independent reconciliation
passed in 784.869 seconds: all 189 existing data tables match the baseline row
for row, prior migration history and sequences are preserved, and the only old
schema-definition change is the added archive-job kind. The new binding table is
empty. Integrity is `ok`, foreign-key violations are zero, and the resulting copy
is 16,524,034,048 bytes. Normal reopening passed in 352.523 seconds while the
full backend suite and reconciliation were also running; this is verification
under load, not an accepted startup performance budget.

Private copy metadata, independent comparison and startup profile are under
`.local/native-translation-jobs-rehearsal-20261002/`. Validation logs under
`/tmp/stash-native-transition` use the `translation-jobs-` prefix. For space, the
unused schema-43 copy was compressed from 16,522,178,560 to 3,671,721,011 bytes.
Its decompressed SHA-256 was verified before removing the redundant raw copy;
the archive, checksum report and original reconciliation remain retained. The
schema-44 baseline, frozen catalogs/automation snapshot and production remain
unchanged. No historical translation queue has been imported in this step.

## Frozen automation snapshot preparation

`stash-prepare-automation` now prepares and verifies bounded, deterministic input
for all ten maintenance, translation, enrichment and discovery families. The
reader accepts the recognized `SCPC` schema at versions 1–3, inventories retained
DDL without executing it, and records the original database checksum. Frozen
inputs open immutably; active journals, source replacement/writes, unknown schema
objects/columns and corrupt databases are rejected before publication.

Records preserve exact SQLite text, nulls, binary values, integers and finite
real values. Legacy JSON remains an original string even when malformed; broken
foreign-key references remain inventoried review evidence. Primary keys use
SQLite numeric/binary ordering. Private chunks are limited to 1,000 records and
16 MiB, with individual table/chunk hashes and counts. Publication flushes the
files and directory, never replaces an existing destination, and permits
verification after a lost acknowledgement. Empty inputs retain all ten families.
The matching Go parser shares framing/table validation with catalog snapshots,
while preserving the different rules for raw legacy automation values.

The actual frozen automation copy has application ID 1396920387 and version 1.
Preparation retained 895,886 records in 896 chunks (429,249,192 bytes including the
manifest), taking 19.981 seconds. Python verification passed in 10.841 seconds;
an independent direct-SQL comparison matched every original column value and
floating-point representation in 6.748 seconds. Go independently validated all
records, ordering, table totals and hashes in 5.477 seconds. The original source
checksum remains unchanged. The source contains 240,397 translation jobs,
396,679 translation targets, 252,050 enrichment jobs and the remaining seven
families. Inputs, scripts and reports are retained privately in
`.local/native-automation-snapshot-rehearsal-20261002/`.

All 260 producer tests passed. The complete scrape package tests, catalog SQLite
snapshot tests and actual Python/Go HTTP upload regression passed after sharing
the validator; final repository Go lint reports zero issues. Synthetic fixtures
cover lossless binary/JSON/integer/retry values, semantic defects, supported
versions, unknown shapes, empty inputs, changed files, limits and interrupted
publication. The preceding translation execution checkpoint `9f79d3589` also
passed CI build, lint and native image publication.

This checkpoint prepares input only: summaries explicitly report
`imported:false` and every family pending. Native receipt, historical cache/job
mapping, held pending work and reviewed activation are still required. The
automation and catalog copies were captured separately, so they do not establish
the coordinated production cutover boundary. No live schema, queue, service or
configuration has changed, and the full transition remains in progress.

## Resumable native automation receipt

Schema 1000046 and `stash-upload-automation` retain the complete frozen operating
input through the native application API. One snapshot binds to an imported
registry source. Exact manifests and chunks resume after lost responses or
restart; changed bytes, gaps and source rebinding are rejected. Original records,
per-family hashes and receipt checkpoints commit together. Empty snapshots retain
all ten family descriptors without creating work.

Catalog and automation upload now share the bounded transport and transactional
chunk receiver. Stored hash state is checked before appending new records.
Automation startup validation verifies source bindings, exact original bytes,
ordering, chunk receipts and per-table hashes before accepting writes. Indexed
chunk ranges avoid scanning the entire snapshot for every chunk. Anonymisation
removes all four new tables' records.

Receiving remains distinct from native mapping: receipts report `imported:false`
and all families pending. No translation targets, execution jobs, source posts or
provider requests are created. Historical outcome mapping, held pending work,
reviewed activation and the remaining native transition phases stay open.

Backend generation, Go lint, all 263 producer tests and v3 validation passed
(528 tests in 91 files and 71 native operation contracts). The complete backend
run passed ingestion in 543.772 seconds and SQLite in 629.277 seconds. Its only
failure used system Python for the existing gallery-dl worker interop fixture;
rerunning that fixture and the new automation API tests with the prepared producer
runtime passed in 39.794 seconds. Focused race tests passed for SQLite in 27.262
seconds and the API in 8.822 seconds. Fixtures cover concurrent receipt, restart,
exact replay, rejected corruption without database writes, source conflicts,
atomic rollback and migration collisions. The preceding preparation checkpoint
`e18d5b917` passed CI lint, build and preview publication.

The isolated schema-45 copy took 41.976 seconds and 16,524,034,048 bytes. Promotion
to schema 1000046 passed in 709.360 seconds under concurrent test load. The actual
Python client then received all 895,886 records in 896 chunks through the native
HTTP routes in 76.270 seconds, including local verification, lost begin/chunk
responses and completed replay. Startup before that upload took 105.761 seconds.

Independent reconciliation passed in 424.463 seconds: all 190 existing data
tables match the preceding copy row for row, prior schema definitions/history
and sequences are preserved, and every retained automation line matches its
frozen chunk. There are ten family descriptors, 896 immutable chunk receipts
and 895,886 staged records. Integrity is `ok`, with zero foreign-key violations.
The resulting copy is 17,579,802,624 bytes. This proves retained input and library
preservation, not domain import or production cutover.

Normal reopening after receipt passed in 104.798 seconds, including full stored
record validation. Its CPU profile remains with the rehearsal artifacts. These
individual timings do not establish an accepted startup performance budget;
reducing and budgeting the remaining integrity-query cost remains release work.

Private rehearsal artifacts are in
`.local/native-automation-receipt-rehearsal-20261002/`; validation logs under
`/tmp/stash-native-transition` use the `automation-receipt-` prefix. Production
and `develop` remain unchanged.

For rehearsal space, two unused validated copies were compressed and their
decompressed SHA-256 values verified before removing the redundant raw files.
The schema-44 translation-work copy shrank from 16,522,244,096 to 3,671,724,172
bytes; the older catalog-media copy shrank from 14,250,086,400 to 3,126,674,522
bytes. Their compressed archives, checksum reports and original reconciliation
reports remain retained. The schema-45 baseline and frozen source inputs remain
unchanged.

## Frozen translation history and held native work

Schema 1000047 and `stash-import-automation-translations` map the two translation
families from a received frozen automation snapshot. Requests preserve exact
text and the historical English provider policy. Valid outcomes share native
caches; conflicting native outcomes and malformed source values retain explicit
review receipts. Original attempts, errors, state and cached JSON remain linked
to every source ordinal. Older English rewrites stay in that immutable input;
the native unchanged result preserves the exact original text.

Applied targets require an exact source-qualified post and a proven cached
outcome. Completion records migration evidence with unknown provider capture
time. It does not invent execution attempts or copy job update times into capture
timestamps. Unapplied targets remain held with their original priority and retry
deadline. Multiple legacy aliases may share one target; a later historical
completion can promote only an untouched hold created by the same import.
Preexisting native targets and later scheduling edits retain their choices.

Known unfinished catalog evidence imports block the affected batch, preventing
premature unmatched classifications. Missing or forgotten posts remain reviewable.
Requests, caches, target revisions, evidence and receipts commit atomically in
batches of at most 200 records or 16 MiB. An indexed source range excludes the
other eight automation families. Application inspection exposes bounded summaries
and exact source details; producer tokens cannot administer the importer.

Generation, Go lint, the full backend suite and all 266 producer tests passed.
The backend run passed API tests in 390.122 seconds, ingestion in 476.398 seconds
and SQLite in 576.977 seconds. V3 validation passed 528 tests in 91 files and
71 current application operation contracts. Focused race checks passed SQLite
in 42.049 seconds and API interop in 9.717 seconds. Fixtures cover lost committed
HTTP responses, sparse checkpoints, restart/replay, aliases across batches,
native edits/cache conflicts, catalog prerequisites, backup/restore,
anonymisation, late-write rollback, corruption refusal and upgrade collisions.

The full-source semantic assessment accepted all 240,397 original jobs: 197,665
requests without cached outcomes, 42,361 ordinary cached outcomes and 371
normalized English outcomes. That assessment took 5.120 seconds and retained
the original frozen source unchanged. A built wheel installed into a fresh
runtime and exposed the new migration command.

The isolated schema-46 backup took 56.977 seconds and 17,579,802,624 bytes.
Promotion to schema 1000047 passed in 509.282 seconds under concurrent test load.
The actual Python client imported all 637,076 translation rows through HTTP in
422.228 seconds, including deliberately lost responses during both job and
target batches and completed replay. Opening before that run took 231.877
seconds while the backend suite was also running.

There are 240,397 shared requests, 42,732 cached outcomes and 395,826 native
targets: 324,169 held and 71,657 historically completed. The 52 duplicate source
target rows retain receipts against shared targets. All 801 review records are
the source-qualified post references already missing from the separate frozen
catalog boundary; they do not create posts or restart work. New history includes
467,483 target revisions and 71,640 migration evidence records; empty-text
completions create no evidence. Existing album work is preserved, and no
translation execution jobs or pending targets are created.

Independent reconciliation passed in 348.533 seconds. All 187 unaffected tables,
including 895,886 original automation records and 2,182 prior successful album
jobs, match the baseline row for row. All previous translations/evidence,
schemas, migration history and sequences are preserved. Every native request,
cache, target and receipt agrees with its source text, outcome, qualified post,
collection and schedule. The only post changes are the exact evidence-driven
revision increments on 41,095 posts. Selected scene/image metadata and identities
remain unchanged. Integrity is `ok`, foreign-key violations are zero, and the
resulting database is 18,417,156,096 bytes. An initial verifier assumption that
the archive-job table was empty was corrected to preserve and compare its
existing album jobs before the complete comparison was rerun.

Private rehearsal artifacts are under
`.local/native-automation-translation-rehearsal-20261002/`; validation logs under
`/tmp/stash-native-transition` use the `automation-translations-` prefix.
Normal reopening passed in 133.773 seconds, including the imported request,
cache, target, receipt and completion checks. Its CPU profile is retained with
the rehearsal. This timing is an individual measurement; startup performance
budgets remain release work.

For space, the unused schema-45 copy was compressed from 16,524,034,048 to
3,672,096,945 bytes and the older catalog-attachment copy from 10,486,972,416 to
2,248,297,009 bytes. Both decompressed SHA-256 values matched before removal of
their redundant raw files. Archives, checksum reports and prior reconciliations
remain retained; the schema-46 baseline and frozen source inputs are unchanged.

Reviewed activation, automatic scheduling, other operational/history families,
native UI/caller conversion, compatibility removal, backup/restore/export drills,
performance budgets and cutover remain open. Production and `develop` are
unchanged.

## Reviewed translation activation API

Schema 1000048 adds application-only preview/apply/status operations for bounded
translation activation. One immutable receipt releases at most 100 exact held
target revisions, preserving priorities and retry deadlines. The reviewed input,
concrete plan and released revisions commit together. Lost responses replay the
original receipt after restart, completion or later scheduling edits. A changed
member rejects the whole batch, and a failed final receipt write rolls back all
releases even if a caller accidentally swallows the error.

Optional frozen-snapshot bindings restrict activation to its original imported
holds. Indexed candidate paging reports later scheduling choices, completed
targets and forgotten posts without treating those as eligible work. Original
source ordinals and target revisions remain available. Activation creates no
execution jobs or provider calls; ordinary worker admission retains its active
job limit, shared result cache and target deadlines.

Focused archive, SQLite and API checks passed, covering bounds, reviewed hashes,
concurrent replay, backup/restore, anonymisation, source-scoped candidate paging,
forgotten posts, late failures, corrupt receipts and schema upgrade collisions.
The actual Python import fixture also passed with candidate inspection through
HTTP. Race checks passed SQLite in 21.294 seconds and API in 4.494 seconds. Go
lint reported zero issues. One new completion fixture initially tried to publish
before its later activation timestamp; its simulated clock was corrected and
the check passed. The full backend regression run passed, including API in
393.991 seconds, ingestion in 468.332 seconds and SQLite in 575.694 seconds.
Validation logs under `/tmp/stash-native-transition` use the
`translation-activation-` prefix.

The preceding `198de4686` checkpoint passed CI build, lint and preview image
publication. The resumable backlog client and full-source activation rehearsal
are recorded in the following checkpoint. Automatic capture scheduling, remaining
catalog and operational families, native UI, live host/n8n conversion, compatibility removal,
release backup/restore/export drills, performance budgets and cutover remain
unfinished. Production and `develop` are unchanged.

## Resumable bulk translation activation

`stash-activate-automation-translations` now prepares private, hashed candidate
pages and reviewed operation identities before releasing imported holds. Apply
validates the complete saved plan, checks each page again on use and inspects
existing receipts before sending another mutation. Lost committed responses
resume the original operations. Excluded native choices remain excluded; stale
batches are reported without preventing independent batches from completing.
Activation status proves release of the selected holds, not provider execution.
Local page review reads only the manifest and requested page; all pages still
require validation before any Apply.

All 275 producer tests passed in 6.565 seconds. The real Python/Go HTTP fixture
passed in 2.514 seconds, including a lost committed response, status, replay and
database reopening. Its race run passed in 4.471 seconds, and Go lint reported
zero issues. A built wheel installed into a fresh runtime, exposed the command
and read a page from the full saved rehearsal plan. The preceding API checkpoint
`a59d8cd20` passed CI lint, build and native preview publication; its complete
backend regression gate remains the latest full-suite result.

The isolated schema-47 backup took 47.146 seconds and 18,417,156,096 bytes.
Promotion to schema 1000048 passed in 379.687 seconds. The actual CLI then
prepared and activated all 324,169 original holds through the Go HTTP routes in
3,242 batches. It recovered deliberately lost committed responses after the
first batch and at 150,000 targets, then verified status and exact replay. The
complete client sequence took 277.542 seconds after opening in 131.392 seconds.
Provider execution remained disabled. With the final client, single-page review
took 15.771 milliseconds and full saved-plan validation took 8.494 seconds;
these measurements do not establish general UI or release performance budgets.

Independent reconciliation passed in 593.196 seconds. All 194 unaffected tables
match the preceding copy row for row, including original import receipts,
selected scene/image metadata and successful album jobs. Every activation plan,
receipt and exact held-to-pending revision matches its saved source binding.
All 467,483 prior history rows and 71,657 completed targets are preserved;
324,169 new revisions record only the intended releases. There are no translation
execution jobs, integrity is `ok`, and foreign-key violations are zero. The
resulting database is 18,794,524,672 bytes.

Normal reopening passed in 164.042 seconds, including the new activation receipts
and historical revision checks. Its CPU profile is retained with the rehearsal.
Startup cost remains part of the unfinished release performance work.

Private scripts, saved plan, comparisons and installed-wheel evidence are under
`.local/native-translation-activation-rehearsal-20261002/`. Validation logs under
`/tmp/stash-native-transition` use the `translation-activation-cli-` prefix.
For space, the unused schema-36 post-media copy and schema-46 automation-receipt
copy were compressed to 2,248,298,297 and 3,805,953,734 bytes respectively. Both
decompressed SHA-256 values matched before removing the redundant raw files.
Their archives and earlier reconciliation reports remain retained; the schema-47
baseline and frozen source inputs are unchanged.

Automatic capture scheduling, the remaining operational/history imports, native
UI and caller conversion, compatibility removal, coordinated backup/restore and
portable export, performance budgets and production cutover remain unfinished.
The full transition goal remains active; production and `develop` are unchanged.

## Automatic translation scheduling for captured source text

Schema 1000049 adds independent, revisioned collection translation policies and
immutable per-capture decisions. The policy selects title/caption, target
language, provider behavior and priority. Missing policies disable scheduling.
An enabled capture queues at most two targets in the same transaction as its
source evidence and ingestion receipt. Equal text shares requests/cache results;
repeated captures retain existing targets, holds, deadlines and outcomes. Receipt
replay preserves the original decision after policy changes and worker completion.
Translation never selects scene/image metadata.

Collection changes require policy review before new scheduling. Original evidence
still commits, with an explicit review result. Empty text receives a `no_text`
entry. The scheduler reads shared text metadata without reconstructing provider
payloads. Application routes expose policy configuration/history and individual
capture decisions; scoped producer tokens cannot administer these routes.
Startup validation, backup/restore and anonymisation cover the new records.

Backend generation, Go lint and all 275 producer tests passed. Focused tests cover
real capture acceptance/replay, shared provider results, preserved holds,
disabled policies, stale collection revisions, large text, HTTP authorization,
late transaction failures, backup/restore, corrupt-state refusal and migration
collisions. Race checks passed SQLite in 19.354 seconds, ingestion in 16.204
seconds and the API in 8.572 seconds. The complete backend run passed API in
487.656 seconds and ingestion in 555.283 seconds; SQLite exceeded Go's default
ten-minute package timeout while the full-copy backup was running. Its complete
rerun passed in 271.287 seconds. `make test` and `make it` now share an overridable
twenty-minute package timeout; operation, worker and provider deadlines are
unchanged. The preceding `fe3ab0525` checkpoint passed CI lint, build, tests and
native preview publication.

The isolated schema-48 backup took 177.101 seconds and 18,794,524,672 bytes under
concurrent regression-test load. Promotion to schema 1000049 passed in 487.469
seconds. Independent reconciliation passed in 421.142 seconds: all 198 existing
data tables match row for row, including their SQLite value types; earlier schema
definitions, migration history and sequences are preserved. The four new tables
are empty. Integrity is `ok`, foreign-key violations are zero, and the resulting
database is 18,794,569,728 bytes. Existing pending/completed translation work,
selected metadata and original import receipts remain unchanged.

Normal reopening passed in 162.647 seconds, including policy and capture-decision
validation. Its CPU profile is retained with the rehearsal. Startup performance
remains unfinished release work.

Private rehearsal artifacts are under
`.local/native-translation-policy-rehearsal-20261002/`; validation logs under
`/tmp/stash-native-transition` use the `translation-policy-` prefix. For space,
the unused schema-47 copy was compressed from 18,417,156,096 to 4,042,088,994
bytes. Its decompressed SHA-256 matched before removing the redundant raw file;
the archive and prior reconciliation remain retained. The schema-48 baseline
and frozen source inputs remain unchanged.

The next frozen enrichment inventory contains 252,050 jobs, eight cooldowns and
eight source-progress records, with no staged result payloads. Completed, pending,
retry, coalesced, missing-URL, identity-conflict and deliberately excluded states
require separate native outcomes. Their import/execution, remaining histories,
existing policy conversion, native UI, compatibility removal, coordinated
backup/restore/export, performance work and cutover remain unfinished. Production
has not changed, and the full transition goal remains active.

## Metadata-only producer extraction

The supported producer package now includes the isolated extraction component
for native enrichment, replacing its dependency on the old catalog library.
It admits known single-post extractor classes across Reddit, Twitter, Bluesky,
TikTok, Instagram, Kemono/Coomer, Patreon and Fansly. Mirror post/feed classes
are distinguished even when gallery-dl uses the service name as their instance
subcategory. Root feeds remain ineligible. Linked Redgifs/Imgur extraction is
bounded; other children remain explicit unresolved references.

Compact transcripts share post fields with per-record patches, removals and
parent references. Original observation times survive checkpoint replay and
child-only retry. The existing retention policy removes redundant previews and
incidental profile fields before persistence. Temporary child failures discard
partial child results while keeping the parent; a missing child is an explicit
source limitation. Root failures, empty results, changed requests and damaged
checkpoints cannot appear successful.

The helper projects source access/pacing settings without creating download jobs,
archives, postprocessors or cookie writes. Its cache is in memory. Originals
remain selected for Kemono/Coomer metadata. The child suppresses private logs,
disables downloader construction and stops on the first HTTP 429. The parent
bounds input/output and elapsed time, and terminates the process group after
cancellation, timeout or oversized output. Website access values remain local.

All 293 producer tests passed in 8.050 seconds, including 18 new tests for compact
reconstruction, removal/null semantics, parent retries, source configuration,
actual pinned Reddit/Twitter transformations, mirror extractor admission,
unchanged media/cookie files, subprocess isolation, rate limits, cancellation
and limits. Existing native producer/API HTTP fixtures passed in 14.892 seconds.
The final wheel installed into an isolated target and passed all 18 extraction
tests in 0.981 seconds; installed module hashes matched the working tree.
Packaging evidence is retained in `.local/native-metadata-fetch-20261002/`, and
validation logs under `/tmp/stash-native-transition` use the `metadata-fetch-`
prefix. The preceding `81c75c25d` checkpoint passed CI generation, tests, build,
lint and native preview publication.

These transcripts are producer checkpoints, not native captures or completed
jobs. The next work is the native enrichment queue, fenced result publication
against existing post identities, and import of the frozen enrichment and
discovery state. Live service conversion, remaining history/policy migration,
native UI, compatibility removal, coordinated backup/restore/export, performance
budgets and cutover remain unfinished. Production and `develop` are unchanged.

## Native post enrichment targets and retained completion

Schema 1000050 adds revisioned enrichment targets for retained post URLs and
exact collection revisions. Repeated requests preserve holds, exclusions, retry
deadlines, original provenance and completion. Application routes inspect targets
and history and allow explicit revision-checked rescheduling. Indexed readiness
omits future work, forgotten posts and outdated or disabled source bindings.
Producer tokens cannot administer these routes.

Completion binds an immutable operation to the exact pending target revision
and a bounded set of retained gallery-dl captures for that post and collection
revision. Existing valid captures can be reused; an NFO or another post/source
binding cannot certify completion. Receipt, capture references, target transition
and history commit atomically, including when a caller swallows a late error.
Exact replay preserves the receipt after later collection changes and post
tombstones. Backup and anonymisation include the new lifecycle, and startup
validation checks identities, histories, scopes, counts and content digests.

Backend generation, v3 lint/types/locales and all 528 frontend tests passed.
All 293 producer tests and Go lint passed. Focused enrichment tests cover direct
post admission, preserved choices and deadlines, stale source bindings, capture
scope, atomic late failures, HTTP authorization, backup/restore, anonymisation,
corrupt-state refusal, bounded indexes and migration collision rollback. The
complete backend gate passed, with API in 420.301 seconds, ingestion in 488.678
seconds and SQLite in 615.076 seconds. That gate was rerun with durable logs after
the earlier process handles and temporary logs disappeared.

The isolated schema-49 backup took 48.984 seconds and 18,794,569,728 bytes.
It reached schema 1000050 cleanly; the original promotion timing was not retained.
Independent reconciliation passed in 859.199 seconds under concurrent regression
load: all 202 previous data tables match row for row, including SQLite value
types. Existing schema definitions, migration history and sequences are intact;
the four new tables are empty, `quick_check` is `ok`, and foreign-key violations
are zero. The resulting database is 18,794,643,456 bytes. Selected library
metadata, earlier catalog receipts, album jobs and translation state are unchanged.

Normal reopening of the promoted copy passed in 125.846 seconds, with its CPU
profile retained. This verifies startup against the copied data; release startup
performance remains unfinished work.

A read-only check of all 252,050 frozen enrichment jobs admitted 248,862 retained
URLs. The 2,023 missing URLs and 1,165 unsupported URLs all belong to existing
missing-URL outcomes or intentionally excluded sources. This checks URL coverage
only; post identity validation and operational-state mapping remain required.

Private reconciliation and backend-gate results are retained under
`.local/native-enrichment-work-rehearsal-20261002/`; URL coverage is under
`.local/native-enrichment-assessment-20261002/`. The unused schema-30 upload
rehearsal was compressed from 6,986,661,888 to 1,205,156,664 bytes. Its decompressed
SHA-256 matched before the redundant raw copy was removed; the archive and its
earlier reconciliation are retained. Frozen inputs and the schema-49 baseline
are unchanged.

These domain records do not execute extraction or certify a worker attempt.
Fenced extraction/publication, operational cooldowns and fairness, legacy
enrichment/discovery mapping, remaining policies/histories, native UI and caller
conversion, compatibility removal, backup/restore/export, performance work and
production cutover remain unfinished. Production and `develop` are unchanged.

## Selected durable job claims

The shared job repository can claim one explicitly selected job revision without
selecting higher-priority unrelated work or running global expiry recovery.
Readiness, retry deadlines, attempt limits and shared-resource exclusion still
apply. A changed selection is rejected; concurrent callers obtain one owner.
Recovery remains a separate trusted queue operation, and old leases stay fenced.
Both claim paths now reject a transaction that tries to swallow a failed attempt
write after changing the job to running.

This is an internal primitive for the remaining enrichment coordinator. Its
producer service must authorize the selected source and producer within the
same transaction; the primitive adds no generic producer claim endpoint. Durable
enrichment binding, transcript checkpoints, verified capture publication,
cooldowns, fairness and legacy queue mapping remain separate unfinished work.
There is no schema or production configuration change.

Focused durable-job tests passed in 9.167 seconds; the selected and concurrent
claim tests also passed under the race detector in 7.360 seconds. Coverage includes
changed selections, concurrent owners, preserved retry deadlines/checkpoints,
resource exclusion across job kinds, separate expiry recovery, indexed lookup,
and rollback when a caller swallows an injected attempt-write failure.

The first full backend run exhausted disk space while creating fixture databases.
Closed compiler/test scratch was removed after checking for open handles. The
unused schema-31 rehearsal was compressed from 8,975,618,048 to 1,940,025,791
bytes, and schema 49 from 18,794,569,728 to 4,119,869,771 bytes. Each decompressed
SHA-256 matched before its redundant raw file was removed; the archives, hashes
and prior reconciliation receipts remain retained. The current schema-50 copy
and original frozen inputs remain unchanged. Cleanup/validation records are in
`.local/native-enrichment-work-rehearsal-20261002/` under `selected_claim_*` and
`selected-claim-scratch-cleanup.json`.

The complete backend gate then passed in 632.150 seconds, with zero lint issues:
API tests took 424.623 seconds, ingestion 501.975 seconds and SQLite 627.888
seconds. The preceding `a775480d8` checkpoint also passed CI tests, generation,
lint, build and release. Production and `develop` remain unchanged; the complete
transition goal is still active.

## Validated compact enrichment transcripts

The native archive package now validates and reconstructs the metadata
collector's compact checkpoints. Shared base fields, explicit removals, nested
parent context, large numeric IDs and original observation timestamps retain
their semantics. Strict envelope/reference validation rejects malformed,
duplicate and unretained evidence. Resumed checkpoints cannot rewrite earlier
observations or silently discard pending or unresolved children.

Producer and server both enforce the 32 MiB compact limit, 4 MiB per expanded
record and 128 MiB aggregate expansion limit, with bounded record/reference
counts and parent depth. Producer size accounting includes native Unicode
separator escaping; rejected appends leave the prior checkpoint intact.

All archive tests passed in 0.580 seconds. All 296 producer tests passed in
30.097 seconds, and the repository Go lint gate reported zero issues. A shared
Go/Python fixture covers reconstruction and resume, with additional malformed
input, timestamp, retention and compact-amplification cases. Validation logs and
reports are retained under `.local/native-enrichment-work-rehearsal-20261002/`
with the `transcript_` prefix. The preceding `6dc7d53ca` checkpoint passed CI
generation, tests, lint, build, release and native preview image publication.

This adds no schema migration, producer endpoint or execution permission.
Target/job binding, producer-owned attempts, checkpoint persistence, verified
capture publication, cooldown/fairness coordination and legacy enrichment queue
mapping remain next. The remaining plan phases and production deployment are
unchanged.

## Producer-owned enrichment jobs and durable checkpoints

Schema 1000051 adds `post.enrich` jobs bound to exact target and collection
revisions, logical roots, worker policy digests and extractor versions. Every
attempt retains its authenticated producer. The internal coordinator authorizes
historical scope and checks current eligibility, credentials and lease deadlines
through commit. A replacement worker preserves the original producer of earlier
observations. Lost claim/checkpoint responses replay without transferring
ownership or replacing later evidence.

Each job stores one current compact transcript, small immutable acknowledgements
and per-record hashes/provenance. Receipt, body, accounting and progress writes
are atomic. Admission, attempts, checkpoint revisions and total staging bytes are
bounded; failed/cancelled evidence is retained. Target schedule changes cancel
active jobs atomically. Explicit application retry preserves backoff and creates
a new target revision; ordinary submissions cannot release held or exhausted
work. Automatic retries and expired-lease recovery impose exponential backoff.

Focused archive/SQLite tests passed in 23.516 seconds, and concurrency/failover
tests passed under the race detector in 43.266 seconds. The complete backend
gate passed in 700.867 seconds, with API tests in 457.182 seconds, ingestion in
531.981 seconds and SQLite in 674.160 seconds. Final lint reported zero issues.
Coverage includes producer ownership and token rotation, historical root scope,
late transaction expiry, swallowed write failures, storage limits, retry delays,
backup/reopen, anonymisation and corrupted-state refusal. Migration fixtures
preserve all three prior job kinds and attempts across states and roll back on
an unrelated table-name collision.

The schema-50 copy took 48.262 seconds and 18,794,643,456 bytes. Initial validation
under concurrent regression load took 578.193 seconds, followed by migration in
177.350 seconds. The migration harness completed and retained its report; its
outer runner then failed on a duplicate report filename. That reporting collision
is recorded separately, without replacing evidence or rerunning the migration.
Independent reconciliation passed in 256.292 seconds: all 206 prior data tables
match row for row with their SQLite types, existing schema objects differ only
by the added job kind, and prior migration history/sequences are intact. Five new
tables are empty, the storage singleton is zero, `quick_check` is `ok`, and there
are no foreign-key violations. The resulting copy is 18,796,433,408 bytes.

Normal reopening passed in 127.352 seconds with a retained CPU profile. This is
integrity/startup evidence, not a completed release performance budget. Reports,
logs and the migrated copy are in
`.local/native-enrichment-jobs-rehearsal-20261002/`. The unused schema-33 rehearsal
was compressed from 9,898,385,408 to 2,147,424,901 bytes; decompressed SHA-256
matched before its redundant raw file was removed. Its archive, proof and earlier
reconciliation remain retained. Original frozen inputs and the schema-50 baseline
are unchanged. The preceding `2bd35b07d` checkpoint passed CI build, lint and
native preview publication.

The new coordinator has no public producer route or dispatch worker yet.
Checkpoints do not publish captures or complete targets. Verified publication,
shared source cooldowns/fairness, stale-job maintenance, checkpoint cleanup after
publication, legacy queue mapping and remaining history/policy migration are
still required. Native UI, live host/n8n conversion, compatibility removal,
backup/restore/export, performance and production cutover remain unfinished.
Production and `develop` are unchanged; the full transition goal remains active.

## Verified enrichment capture publication

Schema 1000052 adds publication receipts and indexed record-to-capture
associations. The internal coordinator consumes an exact saved checkpoint,
rejects pending child lookups and verifies every reconstructed source identifier
against the existing target post. It preserves the original observing producer
and timestamp across worker handoff. Equal observations within one job share a
capture even when the transcript repeats them as parent context. Unresolved
external references remain retained limitations rather than successful fetches.

Publication reuses the download capture services for shared metadata, source
provenance, publisher matching/review, attachment manifests and translation
scheduling. Those writes commit together with record associations, target
completion and the successful job/attempt result. Generic success cannot bypass
native evidence. Expiry or a source edit before commit rolls back all effects;
lost completion responses replay without rerunning normalization or policy
scheduling. No media files or selected scene/image fields change.

Focused archive, SQLite and capture tests passed in 34.387 seconds. Tests cover
worker handoff, original attribution, duplicate context sharing, pending child
refusal, wrong-post rollback, translation coalescing, injected publication errors,
late expiry/source changes, concurrent replay, backup/reopen, anonymisation and
corrupted associations that still satisfy foreign keys. The race run passed in
35.432 seconds. Migration fixtures preserve staged work, roll back on a name
collision and refuse unverifiable schema-51 success assertions.

The complete backend gate passed in 750.601 seconds with zero lint issues:
API tests took 510.348 seconds, ingestion 577.351 seconds and SQLite 723.973
seconds. The preceding `f71a7bfd9` checkpoint passed CI build, lint and native
preview image publication.

An isolated schema-51 backup took 218.584 seconds under regression load. Initial
validation took 127.221 seconds and migration 127.398 seconds. Independent
reconciliation passed in 206.577 seconds: all 212 prior data tables match row for
row, including SQLite value types; all existing schema definitions, sequences
and prior migration history are intact. The two new tables are empty,
`quick_check` is `ok`, and foreign-key violations are zero. The copy remains
18,796,433,408 bytes. Normal reopening passed in 128.674 seconds, with its CPU
profile retained; release startup performance remains unfinished.

Reports, logs and the migrated copy are under
`.local/native-enrichment-publication-rehearsal-20261002/`. The superseded schema-50
rehearsal was compressed from 18,794,643,456 to 4,119,872,195 bytes. Its decompressed
SHA-256 matched before the redundant raw copy was removed; the archive, hash proof
and earlier reconciliation remain retained. The schema-51 baseline and original
frozen inputs are unchanged.

Public producer routes and dispatch are not enabled. Verified checkpoint-body
cleanup, shared cooldown/fairness, stale-job maintenance, additional post identity
adapters and legacy queue mapping remain required. Native UI, live host/n8n
activation, remaining policy/history migration, compatibility removal,
backup/restore/export, performance and cutover remain unfinished. Production and
`develop` are unchanged; the complete transition goal remains active.

## Verified release of completed enrichment staging

Schema 1000053 releases completed enrichment staging after verifying its native
publication. The internal coordinator commits the release with captures and
job/target completion; failed or cancelled work keeps its staging. Original
acknowledgement hashes, observation/producer associations, native payloads and
unresolved URLs with their parent/depth/reason remain retained. A fixed, versioned
checksum projection binds that evidence without duplicating full source bodies.

Earlier publications keep their staging through migration. Scoped cleanup can
release them after verification, without a new attempt. Lost checkpoint and
completion acknowledgements still replay after cleanup, expiry, source edits,
worker handoff and restore. Startup verifies either the original body or a release
proof and validates native payloads, profiles and collection associations before
opening a writer. Cleanup failure rolls back publication/storage accounting;
swallowing a repository write failure cannot commit a partial release.

The complete backend gate passed in 687.942 seconds with zero lint issues.
Final focused enrichment/capture tests passed in 107.643 seconds after freezing
the checksum projection independently of API structs: SQLite took 103.516 seconds
and ingestion 47.064 seconds under concurrent regression load. Cleanup and
concurrent publication also passed under the race detector in 64.919 seconds;
final lint reported zero issues. Coverage includes old-publication migration,
name-collision rollback, retained failed/cancelled evidence, cleanup failure,
swallowed errors, original-producer acknowledgement replay, backup/reopen,
anonymisation, and corruption that leaves foreign keys valid. The preceding
`ee4b68260` checkpoint passed CI build, lint and native preview image publication.

An isolated schema-52 backup took 38.201 seconds. Initial validation took
134.598 seconds, migration 129.497 seconds and reinitialization 0.134 seconds.
Independent reconciliation passed in 221.153 seconds: all 214 prior data tables
match row for row with their SQLite types; previous schema definitions,
sequences and migration history are preserved. The new release table is empty,
`quick_check` is `ok`, and there are no foreign-key violations. The copy remains
18,796,433,408 bytes. Normal reopening passed in 132.071 seconds with a retained
CPU profile; startup performance budgets remain unfinished.

Reports, logs and the migrated copy are under
`.local/native-enrichment-release-rehearsal-20261002/`. The superseded schema-51
rehearsal was compressed from 18,796,433,408 to 4,120,174,179 bytes, and schema 32
from 8,975,687,680 to 1,940,027,833 bytes. Each decompressed SHA-256 matched before
its redundant raw copy was removed. Both archives, hash proofs and earlier
reconciliations remain retained. The schema-52 baseline and original frozen
inputs are unchanged.

Public producer routes/dispatch, additional post identity adapters, shared source
cooldowns/fairness, stale-job maintenance, legacy queue mapping and remaining
policy/history migration are still required. Native UI, live host/n8n activation,
compatibility removal, backup/restore/export, performance and cutover remain
unfinished. Production and `develop` are unchanged; the full goal remains active.

## Additional source post adapters

Native capture intake and internal enrichment publication now identify Bluesky,
TikTok, Instagram posts/reels, Patreon, Fansly and Kemono/Coomer posts. The Go
server and Python producer share 51 identity/metadata cases. Bluesky identities
include the author's DID; mirror identities include extractor, service and
account. Instagram uses the enclosing post ID rather than an individual file ID.
Disagreeing, incomplete and ambiguous identifiers cannot establish a post.

Metadata uses each extractor's post caption and publication fields. Image alt
text and attachment dates do not replace them. Mirror import dates are not
misreported as publication dates. Current Fansly `account` envelopes and older
retained publisher envelopes both work. Imgur/Redgifs children preserve the
enclosing post's identity, publisher, caption and source evidence paths; social
profile/feed parents do not replace the actual post. Capture provenance retains
the actual mirror extractor independently of its qualified post identity.

All 304 producer tests passed in 100.021 seconds, including offline execution of
the pinned gallery-dl transformations for all seven additional source types.
Backend persistence tests cover original provenance, child attribution, mirror
account separation, collection authority, restart, staging release and exact
publication/receipt replay. Focused archive/enrichment/capture checks passed in
74.824 seconds. Both complete backend gates passed: the final run took 664.086
seconds. The subsequent API capability check passed in 13.761 seconds, and final
lint reported zero issues. Capabilities now list the additional native namespaces
and distinguish Coomer/Kemono namespace prefixes from native service support.
The preceding `7cc3e5b4a` checkpoint passed CI build, lint and native preview image
publication.

A read-only audit of the schema-53 rehearsal reconstructed and verified 17,694
retained captures across Bluesky, Instagram and the mirrored services. It matched
17,681 to their existing native post identifiers, with no conflicting identifiers
or newly required associations. Thirteen Instagram story/highlight captures
remain outside the regular-post adapter; their stored evidence is preserved.
The audit took 7.828 seconds. Existing collection namespace scopes also agree
with the newly supported identities. Reports, exceptions and test logs are under
`.local/native-post-adapters-rehearsal-20261002/`. No schema migration or source
payload rewrite was needed.

Attachment manifests, file selection and source-window adapters beyond
Reddit/Twitter, additional service-specific post/profile partitioning, and
Instagram story/highlight semantics remain required. Post identity support alone
does not enable a complete media download adapter. Public enrichment routes and
dispatch, shared scheduling/cooldowns, operational-history/policy import, native
UI, live host/n8n activation, compatibility removal, backup/restore/export,
performance and cutover remain unfinished. The full transition goal remains
active on `v3-rewrite`; production and `develop` are unchanged.

## Scoped enrichment worker transport

The producer API now exposes revision-pinned enrichment admission, inspection,
claim/renew, compact checkpoint transfer, verified publication and release
receipts. Authentication precedes large request decoding. Domain transactions
recheck historical collection/root grants, current producer ownership and lease
eligibility. Bounded target discovery returns unadmitted work for one selected
collection; it does not claim or discover already queued retries.

Controlled failure outcomes use the existing immutable attempt records.
Temporary errors retain checkpoints and receive server backoff; the eighth
attempt becomes terminal. The same producer can replay an exact failure receipt
after a successor starts without affecting that successor. Credential rotation
preserves producer ownership, while revoked or unrelated credentials cannot
confirm the old attempt. An expired attempt cannot commit a late failure.
Only verified publication can report success or complete the target.

The Python transport validates job bindings, checkpoint hashes/counts and
publication receipts. Its heartbeat uses the HTTP server date and monotonic
request start, and stops further extraction if renewal fails. Checkpoints remain
JSON objects with bounded responses and native Unicode encoding. Original
decimal tokens, exponent spelling, negative zero and large IDs survive HTTP,
resume cloning and extraction subprocesses without changing previous record
hashes. Ordinary event encoding and existing receipt digests are unchanged.

All 318 producer tests passed in 38.081 seconds. The complete backend gate passed
in 726.519 seconds: API tests took 487.998 seconds, ingestion 528.752 seconds and
SQLite 704.202 seconds. Final lint reported zero issues. Focused race checks
passed in 69.958 seconds, covering concurrent failure/publication replay and
real HTTP/Python recovery. Final focused worker/HTTP tests passed in 41.572
seconds after adjusting the race fixture to the actual server clock and the
supported 30-second request timeout. Final client validation checks also passed.

Coverage includes authentication before body reads, collection/producer
isolation, invalid and oversized requests, retry budgets, credential rotation,
checkpoint retention/reopen, stale failure replay, pending-child refusal, and
lost admission/claim/checkpoint/publication replies. The real transport fixture
round-trips a checkpoint larger than 4 MiB with HTML characters, Unicode
separators, exact source decimals and original provenance. Logs and next-worker
notes are under `.local/native-enrichment-worker-rehearsal-20261002/`. The preceding
`fad491032` checkpoint passed CI build, lint and native preview image publication.

No native schema migration was needed; the database version remains 1000053.
Durable local execution/staging, queued-job dispatch, shared source cooldowns
and fairness, stale-job recovery, reviewed metadata-only profiles and legacy
queue mapping remain unfinished. Other operational-history/policy migration,
additional download adapters, native UI, live host/n8n activation, compatibility
removal, backup/restore/export, performance and production cutover still remain.
Production and `develop` are unchanged; the full transition goal remains active.

## Durable selected-job enrichment execution

Producer outbox schema 8 adds a local journal for immutable job definitions,
stable claim requests, returned checkpoint bytes and pending delivery intents.
The executor reserves a full checkpoint allowance against the existing outbox
quota before requesting source data. Downloads and enrichment share that byte
budget. A separate process lock allows one enrichment executor per outbox while
ordinary event delivery continues through short database transactions.

Returned metadata is persisted before a late ownership check can pause execution.
A matching checkpoint acknowledgement releases the local body and atomically
stages publication or the precise pending-child failure. Lost responses replay
the original operations after restart. Expired attempts can deliver unchanged
local evidence through a new claim, subject to the native prefix/revision checks;
divergent successor checkpoints leave the original local body in review. Native
publication is the only completion proof. Review or rejection cannot silently
discard unacknowledged observations.

Reviewed metadata-only profiles bind the source category, pinned runtime,
projected access/pacing configuration and asset identities. They require no media
root, destination locks or download archive. Website credentials remain local
references and stay out of the job, journal and portable policy digest. New CLI
commands validate profiles, execute selected jobs, recover pending deliveries
without website credentials, and inspect retained local state. The executor
requires an already admitted native job UUID.

All 328 producer tests passed in 36.315 seconds, including existing download and
configuration behavior. Focused native HTTP/Python restart tests passed in 27.105
seconds, and the same scenarios passed under the race detector in 35.438 seconds.
Coverage includes lost claim/checkpoint/publication and
failure responses, child-only continuation, ownership loss after extraction,
expired attempts, conflicting successor evidence, rejected checkpoints,
capacity exhaustion before fetch, and no false completion. Local journal tests
also cover process death, concurrent ownership, tampered bytes, receipt mismatch
and preservation/rollback across schema 7 → 8.

The complete backend gate passed in 693.046 seconds with zero lint issues:
API tests took 542.951 seconds, ingestion 519.655 seconds and SQLite 677.023
seconds. The initial sandboxed gate could not access the existing Go cache;
the successful run used the existing cache with permission. The preceding
`7a5b1d19a` checkpoint passed CI build, lint and native preview publication.
Reports and next-dispatch notes are in
`.local/native-enrichment-execution-rehearsal-20261003/`.

Native Stash schema remains 1000053; this increment changes only producer storage.
Queued-job discovery/dispatch, shared source cooldowns/fairness, stale-job
maintenance, review resolution and legacy queue mapping remain required before
enrichment service activation. Remaining historical policy/operational migration,
download adapters, native UI, host/n8n activation, compatibility removal,
backup/export/restore, performance and cutover remain unfinished. Production and
`develop` are unchanged; the full transition goal remains active.

## Scoped enrichment dispatch and native maintenance

The producer can now discover admitted enrichment jobs before admitting fresh
work for an explicitly selected collection. Native discovery uses the existing
partial active-job index, bounded by the 64-job limit, with current source,
policy/runtime and producer-grant checks. Unadmitted target pages preserve
priority/deadline/UUID order. Claim remains the only authority to contact a source.

Producer schema 9 persists separate delivery, local-job, native-job and target
cursors with outage/idle backoff. Selection is saved before network work; lost
admission replies are rediscovered from the native queue. Unsupported URLs do
not stall later pages. Pending delivery precedes new lookups and can run without
a website profile. Expired delivery reports an ownership requirement, or reclaims
with the matching reviewed profile and resends the original bytes. A mismatched
profile leaves the evidence intact. An idle pass is not collection completion.

The HTTP server owns a maintenance loop independent of media/translation workers.
It cancels stale source/target jobs and recovers expired attempts with native
backoff and attempt limits. Checkpoints and receipt history remain retained.
Maintenance stops before database shutdown, makes no website requests and does
not enable held targets. Its writes are atomic even if a caller catches a
repository failure. Existing generic job recovery shares the same expiry logic.

All 339 producer tests passed in 43.410 seconds. Focused native discovery and
maintenance tests passed in 2.736 seconds; actual HTTP/CLI restart and runtime
lifecycle tests passed in 9.221 seconds. The focused race run passed: SQLite took
5.985 seconds and API 17.400 seconds. Tests cover concurrent maintenance, source
changes, backoff, stale-lease refusal, retained checkpoints, rollback after a
caught write failure, lost admission/checkpoint/publication responses, original
metadata reuse after expiry, cursor contention, target pagination, nanosecond
ordering, profile isolation and populated outbox migration/collision rollback.
A historical migration fixture now initializes its own configuration instead of
depending on earlier tests.

The full backend gate passed in 742.076 seconds with zero lint issues. API tests
took 599.056 seconds, ingestion 545.426 seconds and SQLite 715.610 seconds.
A read-only query-plan audit on the schema-53 rehearsal selected
`archive_jobs_active_work`; that copy has zero active
enrichment jobs, so this proves index selection rather than loaded performance.
No native migration was required; schema remains 1000053. Reports, logs and
next-source-scheduling notes are under
`.local/native-enrichment-dispatch-rehearsal-20261003/`. The preceding `9aa831a60`
checkpoint passed CI build, lint and native preview image publication.

Shared download/enrichment pacing and fairness remain required before production
service activation: source_runs and native post-enrichment jobs still have
separate ownership coordination. Legacy enrichment queue mapping, remaining
policy/operational history import, additional download adapters, review UI,
host/n8n conversion, compatibility removal, coordinated backup/export/restore,
performance and cutover remain unfinished. Production and `develop` are unchanged;
the complete transition goal remains active.


## Shared native pacing and enrichment service reservations

Schema 1000054 binds immutable source runs and enrichment jobs to the contacted
service, independently of performer/account ownership. Versioned host rules
recognize service aliases, preserve distinct unknown hosts and keep mirror
services separate from upstream creator namespaces. Claims coordinate cooldowns,
collection ownership and service reservations in their write transaction.
Eligible queued downloads precede enrichment; independent downloads retain their
existing destination/target locks. Blocked enrichment claims no longer cause
successive dispatch passes to keep admitting fresh work.

Each enrichment attempt retains its own service reservations. Claims reserve
known pending child services atomically; a scoped producer route reserves newly
discovered Redgifs/Imgur services before extractor initialization. Its replies are
bound to the current job/fence. A busy child is checkpointed without a request,
retaining its parent for child-only retry. The bounded subprocess exchange keeps
website access settings local and continues preserving native JSON number tokens.
Finished, cancelled or expired attempts no longer hold their services, while
attempt history and checkpoints remain intact.

Native typed rate-limit, timeout and extraction failures pause the affected
service for at least one hour; authentication/challenges use one day. A longer
retry deadline wins. Child failures pause the child service, and old failure
receipts cannot prolong a cooldown. Busy reservations, missing posts, denied
accounts and local worker failures do not declare a service outage. The generic
download adapter still needs typed source-failure reporting and equivalent
reservations for its own linked extractors; its existing scheduling binding
covers the source run's target service. These remain activation requirements.

All 344 producer tests passed in 46.863 seconds. Focused source/enrichment tests
passed, including real Python/native HTTP execution and recovery. Race checks
passed for service identity, concurrent ownership and the HTTP lease boundary
(SQLite 26.650 seconds, API 27.079 seconds). Coverage includes child-only retry,
reservation replay, producer/fence rejection, expired ownership, shared service
cooldowns, independent mirrors, caught-write rollback and historical migration
preservation/collision rollback. The full backend gate passed in 816.074 seconds
with zero lint issues (API 650.095, ingestion 587.493, SQLite 793.301 seconds).

The 18,796,433,408-byte isolated schema-53 copy migrated to schema 1000054.
Initial validation took 202.518 seconds and migration/revalidation 163.429 seconds
while other checks ran; these are not production downtime estimates. Independent
row/type comparisons and digests matched all 215 retained tables (34,810,754 rows),
with zero foreign-key violations and successful integrity checking. Three source
runs gained bindings to two services; this frozen copy has no admitted enrichment
jobs. Populated fixtures cover running attempts and pending children. Query plans
use the intended active-work indexes; the sparse copy is not a load benchmark.

Reports and scripts are under `.local/native-source-pacing-rehearsal-20261003/`.
The comparison passed before deleting the superseded schema-53 copy, reclaiming
another 18.8 GB. The original compatible snapshot, frozen migration inputs and
latest verified native copy remain, with the 50 GiB host reserve checked before
and during copying. No production service or library was migrated.

Download-side linked-service reservations and typed failures, cross-collection
fairness, legacy enrichment queue/cooldown/history import and live conversion
remain. The broader policy/operational migration, adapters, native UI,
compatibility removal, coordinated backup/export/restore, performance and cutover
requirements are still unfinished. The complete transition goal remains active.

## Download linked services and failure attribution

Schema 1000055 adds attempt-scoped requested/held services and immutable typed
failure receipts. The source-run API authenticates the producer, checks the
current owner/fence, and rechecks the definition and lease through commit.
Download extractors reserve their root or supported Redgifs/Imgur child service
before initialization. Busy requests remain durable dependencies without holding
the service. Exact-window retries, including expired attempts, wait for their
recorded dependencies before repeating parent extraction and reserve them in the
claim transaction. Widened traversals discover their own dependency set.

The pinned download adapter now reports controlled source error codes and the
contacted service, without retaining website exception text or URLs. Source
rate-limit and timeout errors stop immediate HTTP retry loops. Child errors
affect the child's cooldown; individual media download failures and intentional
archive stops retain their separate meanings. Lost ownership stops further work.
Lost finish acknowledgements require the exact attempt, outcome, code and service.
The executor requires `source_run_pacing_protocol: 1`.

All 352 producer tests passed in 31.827 seconds. Focused backend checks passed,
covering competing child reservations, authorization/fences, release, typed
failure attribution, busy dependencies across retry/expiry, widened windows,
caught-write rollback and historical migration preservation/collision handling.
Race checks passed (SQLite 44.525 seconds, API 10.296 seconds). The full backend
gate passed in 848.292 seconds with zero lint issues (API 696.224 seconds,
ingestion 636.398 seconds, SQLite 819.788 seconds).

The isolated 18,796,433,408-byte schema-54 copy migrated to schema 1000055 without
increasing its file size. Initial validation took 149.260 seconds and
migration/revalidation 144.357 seconds while other checks ran. Independent
row/type comparisons and digests matched all 219 retained tables (34,810,759 rows)
in 408.890 seconds; integrity checking passed with zero foreign-key violations.
The frozen copy has three source runs but no attempts, so populated fixtures
provide the historical/running-attempt migration coverage. Query plans use the
active-run and attempt-key indexes. These timings and sparse query plans are
not production downtime or loaded-performance estimates.

Reports and scripts are under `.local/native-download-pacing-rehearsal-20261003/`.
The verified schema-55 copy replaces the disposable schema-54 copy under the
50 GiB free-space policy; the original compatible snapshot and frozen migration
inputs remain. The preceding `484533461` checkpoint passed CI build, lint and
preview-image publication. Production and `develop` remain unchanged.

Cross-collection fairness, legacy queue/cooldown/history import, remaining policy
and operational migration, additional download adapters, native UI, host/n8n
conversion, compatibility removal, coordinated backup/export/restore, performance
and production cutover remain unfinished. The full transition goal stays active.

## Bounded download preference and cooperative turns

Schema 1000056 records actual waiting enrichment claims with a 90-second expiry
and per-service scheduling counters. Four download starts or two minutes of
continuously live waiting make an enrichment turn due. The oldest eligible
requester wins across collections, while existing downloads drain. An enrichment
start resets the affected counters and waiting-age budget. Abandoned requests,
held/stale targets and cooling dependencies cannot indefinitely reserve unrelated
services. Claim authority is checked through commit even when the only write is
the waiting hint. Replay does not count another start.

The download API exposes a five-minute `turn_until` derived from the attempt's
recorded start. The worker finishes the current file/checkpoint before yielding,
and heartbeats cannot extend the budget. A resumed traversal may finish replay
and one new checkpoint first, preventing a long saved prefix from trapping it
in repeated replay. This is cooperative scheduling, not a hard timeout. Yield
preserves pending windows and progress, applies the ordinary target cooldown and
does not consume failures or failure backoff. The updated executor requires
`source_run_fairness_protocol: 1` and reports an acknowledged turn as `yielded`.

All 356 producer tests passed in 35.764 seconds. Focused checks passed in 13.880
seconds, including competing claims from different collections, oldest-request
selection, abandoned-interest expiry, held targets, child cooldowns, exact-window
progress, authorization at blocked-claim commit, rollback and restart validation.
Race checks passed in 51.580 seconds overall (SQLite 23.487, API 14.791 seconds).
The full backend gate passed in 814.504 seconds with zero lint issues (API
666.543, ingestion 611.419, SQLite 792.532 seconds).

The isolated 18,796,433,408-byte schema-55 copy migrated to schema 1000056 without
growing its file. Initial validation took 155.899 seconds and migration/revalidation
152.656 seconds while the backend checks ran. Independent row/type comparisons
and digests matched all 221 retained tables (34,810,759 rows) in 282.396 seconds,
with unchanged prior schema objects, append-only migration history, successful
integrity checking and zero foreign-key violations. Two existing service scopes
gain zeroed counters; no historical waiting requests are invented. Populated
fixtures cover real attempts and waiting jobs. Service/expiry lookups use their
indexes; this sparse rehearsal is not a loaded performance benchmark.

Evidence is under `.local/native-source-fairness-rehearsal-20261003/`. The verified
copy replaces the disposable schema-55 rehearsal under the 50 GiB host reserve.
The original compatible snapshot and frozen migration inputs remain. The
preceding `8c69a3fc1` checkpoint passed CI build, lint and preview-image publication.
Production and `develop` remain unchanged.

Dispatch across multiple profiles, legacy queue/cooldown/history import, remaining
policy and operational migration, additional download adapters, native UI,
host/n8n conversion, compatibility removal, coordinated backup/export/restore,
performance and cutover remain unfinished. The complete transition goal is active.

## Worker profile rotation and automatic collection discovery

`stash-ingest dispatch-all --profiles FILE` now rotates across a bounded local
list of download and metadata profiles. Profile selection is persisted before
execution so a restart or busy first profile cannot repeatedly displace its
peers. It first attempts saved metadata delivery without loading website
profiles; a missing access binding therefore cannot strand already fetched
evidence. Unavailable profiles report a fixed code and allow other entries to
run. Native receipts, permissions, leases and shared service scheduling remain
authoritative.

The scoped enrichment API now discovers eligible collections automatically,
including later collections under an existing root grant. Exact collection
grants remain tied to their current root, including unbound collections. Current
grants apply before pagination, and moving a collection does not expose it under
an old grant. Ready collections contain either eligible unadmitted targets or
due queued jobs for the requested policy/runtime. Discovery is read-only.
Metadata profiles retain independent collection rotation and discovery backoff;
the existing collection dispatcher still validates URLs, admissions and claims.

Producer outbox schema 10 adds two cursor tables in one transaction, preserving
all earlier rows, receipts and staged bodies. The native database stays at
1000056. Fixtures verify schema-9 preservation and collision rollback, profile
and collection rotation across restart, competing cursor updates, failure/backoff
handling, current grants, moved sources and job-state boundaries. The actual
Python CLI/native HTTP test automatically discovers its collection, survives a
lost checkpoint response and delivers after restart with the website profile
unavailable; the source is fetched exactly once.

All 368 producer tests passed in 42.409 seconds. Focused checks passed in 16.354
seconds, actual HTTP/CLI checks in 14.109 seconds and race checks in 50.201 seconds
overall (SQLite 6.320, API 12.815 seconds). The full backend gate passed in
752.243 seconds with zero lint issues (API 608.602, ingestion 541.824, SQLite
728.180 seconds).

Evidence is under `.local/native-worker-profiles-rehearsal-20261003/`. A read-only
query check on the verified native copy uses the root, collection-state and job
target indexes. That copy has 2,612 collections but no enrichment targets; it
does not establish loaded performance. This increment needs no new full database
copy. The existing schema-56 copy and frozen inputs remain authoritative, and
135.5 GiB was free after validation under the 50 GiB host reserve. The preceding
`fa297624e` checkpoint passed CI build, lint and preview-image publication.

Legacy enrichment queue/cooldown/history import is next. Remaining policy and
operational migration, additional download adapters, native UI, live host/n8n
conversion, compatibility removal, coordinated backup/export/restore, performance
and cutover still apply. Production and `develop` remain unchanged, and the full
transition goal remains active.

## Legacy enrichment semantics and frozen-input reconciliation

Native preparation policy `automation-enrichment-v1` now interprets the legacy
queue, cooldowns, seed/source progress and catalog enrichment receipts. Retry
deadlines use exact decimal arithmetic and round upward to milliseconds;
platform/account pauses never shorten another preserved deadline. Original
attempt counts and creation/update times remain available without fabricating
native leases or attempts. Contacted services come from the URL, independently
of historical account-platform labels. Unknown versions, states, malformed
staging and unsupported cooldown reasons require review.

Preparation distinguishes held-work candidates, historical completion requiring
a catalog receipt, existing source metadata requiring a gallery-dl capture, and
coalescence requiring a post alias. It preserves explicit exclusions and leaves
missing URLs/identity conflicts reviewable. Staged metadata keeps its exact hash
and requires conversion/review even under a completed queue label. Completed
enumeration can still leave pending jobs; neither seed progress nor the last
source attempt certifies execution completion.

The read-only assessment checked all 252,050 frozen queue rows, eight cooldowns,
eight source-progress rows and all 16,186 retained catalog receipts with matching
source hashes. No frozen seed-progress rows exist; fixtures cover them. The final
pass took 4.894 seconds while validation ran. Queue candidates comprise 227,443
pending/retry holds, 21,368 historical completions, five existing-source outcomes,
44 coalescences, 2,443 exclusions and 747 review states. Every queue post and
catalog resolves to an imported native identity. These are preparation results,
not newly created targets or successful native jobs.

Exact queued URLs are absent for 213,826 held candidates and two historical
completions even though their posts match. The old selector rewrites Reddit and
Twitter URLs to short post routes, so the domain importer must retain those URL
associations with their original queue evidence instead of requiring exact text
already present in `source_post_urls`.

Of the 21,368 `done` rows, 16,186 have a valid exact-key catalog receipt. The other
5,182 have no receipt through their imported post/collection identities either;
all have queue updates newer than their individual catalog snapshots (5,180
OnlyFans-labeled mirror jobs and two Reddit jobs). They remain unproven by this
rehearsal input. This confirms the coordinated fresh-snapshot requirement at
cutover; it does not justify manufacturing missing receipts or silently repeating
that completed source work.

Focused tests cover status/proof distinctions, source mirrors, account-specific
cooldowns, deadline rounding below floating point precision, original staging
bytes, unknown input, receipt timestamps and unresolved children. The full backend
gate passed in 731.215 seconds with zero lint issues (API 583.246, ingestion
517.440, SQLite 704.689 seconds). Final timestamp/cooldown checks passed with the
complete scrape package (0.037 seconds), final lint reported zero issues, and the
full frozen projection was rechecked unchanged.

Evidence is under `.local/native-enrichment-import-preparation-20261003/`.
The native schema remains 1000056 and no database copy was added. Free space was
134.6 GiB after checks under the 50 GiB reserve. The preceding `346083b93`
checkpoint passed CI build, lint and preview-image publication.

Native queue/receipt/cooldown mapping, staged checkpoint conversion, reviewed
activation and coordinated cutover inputs still need implementation. The broader
policy/operational migration, download adapters, UI, host/n8n conversion,
compatibility removal, backup/export/restore, performance and cutover gates remain
unchanged. The complete transition goal remains active.


## Historical catalog enrichment receipt import

Schema 1000057 retains historical enrichment assertions in native receipt storage
with a manifest-bound import ledger and application inspection API. Batches of
50 records resume from the committed source ordinal, including after a lost HTTP
response. Original completion timestamps, version, enriched-link counts and
unresolved-child counts are retained. Each receipt binds the mapped post to the
catalog's migrated collection revision 1; later collection edits/retirement do
not rewrite that scope. Invalid or unsupported receipts, unmapped posts and
forgotten posts retain review outcomes and their source values.

Historical completion does not create a native job, attempt, target or capture,
and does not claim exhaustive child coverage or particular capture IDs. Imported
source provenance remains distinct by catalog. The new client is
`stash-import-catalog-enrichment`; native post/receipt readers provide bounded
inspection. Ordinary backup includes these tables and anonymisation removes
them. Startup checks progress, scope, deterministic identity, retained counts and
both historical completion and import timestamps.

Focused SQLite/API tests cover interrupted batches, stale checkpoints, retired
collection scope, exact times, unresolved children, forgotten posts, caught-write
rollback, migration collisions and startup rejection of altered receipt facts.
The real Python CLI/native HTTP fixture recovers a lost committed completion
response, inspects retained source values, handles absent legacy receipt tables
and rejects invalid requests. All 371 producer tests passed in 118.269 seconds.
The full backend gate passed in 823.868 seconds with zero lint
issues (API 677.598, ingestion 607.518, SQLite 796.750 seconds). The
final stricter import-time validation passed focused checks in 28.368 seconds,
final lint and a fresh full-database open in 131.571 seconds.

The isolated full-copy import mapped all 16,186 receipts across 1,697 frozen
catalogs, with zero review outcomes, in 33.482 seconds
and 2,003 bounded transactions. Every terminal cursor replay matched. Source
counts include 4,657 enriched attachment links and zero unresolved children;
fixtures separately preserve nonzero unresolved counts. This maps historical
assertions only and does not change the unresolved 5,182 later queue completions
whose catalog snapshots predate their proof.

Independent comparison took 275.379 seconds: all 224 pre-existing
tables, retained schema objects, prior migration history and sequences matched.
All 16,186 receipt identities, timestamps, counters and post/collection references
were independently checked against the frozen source rows. Integrity was clean
with zero foreign-key violations. Post, collection/post and receipt-source
lookups use their targeted indexes. The native copy is 18,809,282,560
bytes, about 12.3 MiB larger than its predecessor.

Evidence is under `.local/native-enrichment-history-rehearsal-20261003/`. After
validation, retiring the superseded schema-56 database and derived files reclaimed
18,796,515,328 bytes; 133.4 GiB was free
under the 50 GiB host reserve. The current pointer names the schema-57 copy;
frozen inputs and the original compatible snapshot remain. The preceding
`2f2d91453` checkpoint passed CI build, lint and preview-image publication.

Legacy queue/cooldown/progress mapping, staged checkpoint conversion and reviewed
activation are next. The broader policy/operational migration, download adapters,
native UI, host/n8n conversion, compatibility removal, backup/export/restore,
performance and coordinated cutover remain required. Production and `develop`
remain unchanged, and the complete transition goal is active.

## Frozen enrichment queue import

Schema 1000058 maps the received automation snapshot's enrichment jobs,
cooldowns, seed cursors and source progress into native import records. The
application API and `stash-import-automation-enrichment` client resume bounded
200-record/16 MiB transactions. Original source rows and staged bytes remain
available, and whole-migration completion remains `imported:false`.

Pending/retry work becomes held enrichment targets with original priority and
the later of its job delay and exact account/platform cooldown. Historical
attempt counts do not become invented native attempts. Old `done` status needs
a scoped catalog receipt; `already_native` needs a scoped existing gallery-dl
capture. Historical completion has its own proof basis and zero new capture
associations. Missing proof remains review. Coalesced work needs an explicit
catalog alias; exclusions remain retained. Legacy staging remains review rather
than being discarded to refetch or misrepresented as a native checkpoint.
Existing native choices are preserved; only unchanged holds owned by this import
can combine schedules or finish from historical proof.

The isolated full-copy pass processed all 252,066 operational rows in 1,261
batches and 342.218 seconds. It retained 227,443 held jobs, 16,186 receipt-backed
historical completions, five existing-capture completions, 44 proven coalescences,
2,443 exclusions, eight cooldowns and eight source-progress records. The 5,929
review outcomes comprise 747 original review states and 5,182 completion claims
whose catalog snapshots lack proof. No frozen seed rows exist; fixtures cover
their counters and cursor semantics. The native targets comprise 227,443 holds,
16,191 historical completions and two review targets; unsupported/missing URLs
retain their source decisions without fabricated execution targets. No native
job or pending target was activated.

The previous schema validation and SQL promotion took 771.488 seconds while the
full backend suite ran. Reopening the completed copy passed in 159.694 seconds.
The database is 19,482,345,472 bytes, 641.883 MiB larger than its predecessor.
These measurements do not establish the release's startup or cutover performance
budget. Source-ordinal, import-owned-target, cooldown and historical-proof
lookups use targeted indexes.

The full backend gate passed in 882.809 seconds with zero lint issues (API
729.864, ingestion 650.272 and SQLite 855.299 seconds). All 374 producer tests
passed in 66.285 seconds. Focused fixtures cover sparse cursor restart, exact
account cooldowns, preserved native exclusions, source aliases, staged metadata,
missing proof, forgotten posts, atomic rollback, backup/restore, anonymisation,
native-publication preservation through schema replacement, migration collision
rollback and startup rejection of altered source projections. A real Python
CLI/native HTTP test recovers a committed response being lost and inspects
original progress data without changing its frozen source.

Independent reconciliation took 483.063 seconds and compared 224 pre-existing
tables. All prior media, captures, post bodies, original input ledgers, native
execution state, migration history and sequences were preserved. The only
changes to existing post records are revision increments for 213,826 retained
queued-URL associations, each linked to its original queue row and explicit
snapshot-boundary provenance. All 252,066 operational rows and their post,
collection, deadline, alias and historical completion bindings were independently
checked. Native target/completion identities matched their documented inputs.
Integrity was clean with zero foreign-key violations.

Evidence is under `.local/native-automation-enrichment-rehearsal-20261003/`. The preceding
`3b9a9e1d7` checkpoint passed CI build, lint and native preview-image publication.

After reconciliation and checking that no process held the old files open,
retiring the schema-57 database and derived files reclaimed 18,809,323,520 bytes.
The current pointer now names the schema-58 copy; original compatible and frozen
input snapshots remain. Free space was 131.1 GiB under the 50 GiB host reserve.

Reviewed enrichment activation, staged checkpoint conversion, review resolution,
remaining policy/operational migration, additional download adapters, native UI,
host/n8n conversion, compatibility removal, backup/export/restore, performance
and coordinated cutover remain required. Production and `develop` remain on
their existing releases; the complete transition goal remains active.

## Reviewed enrichment activation and collection handoffs

Schema 1000059 introduces saved-preview activation through the native application
API and `stash-activate-automation-enrichment`. Original frozen holds are listed
once per target using indexed ordinal pages, including when legacy aliases share
a target/revision. Preview binds the original target, current post revision,
retained URL, chosen collection revision, release target, policy and schedule.
Apply rechecks the preview atomically, preserves priority and exact delays, and
retains receipts that replay after subsequent completion, edits or forgetting.
It does not start collectors or assert their completion.

Imported collections are disabled at revision 1. Reviewing and enabling a
collection creates a new revision; activation now explicitly carries the held
work to it. The old target remains excluded with `activation_rebound`, and the
new pending target retains the reviewed source scope. Existing native destination
work is preserved as a conflict. Disabled/retired collections, changed original
holds and forgotten posts require review. General application activation also
supports holds already bound to an active collection revision.

Translation and enrichment clients share bounded HTTP and immutable saved-plan
handling; translation plan format and receipt semantics are unchanged. Plans
validate every page before Apply, preserve operation UUIDs after response loss,
and distinguish activation from execution. Enrichment review candidates prevent
an empty eligible selection from claiming completion. A packaged-wheel smoke
check exercised both installed command entry points from the built artifact.

The full backend gate passed in 885.167 seconds with zero lint issues (API
727.260, ingestion 634.416, SQLite 867.252 seconds). All 379 producer tests passed
against the current source in 124.096 seconds. Focused SQLite/API fixtures cover
scope changes, occupied destinations, original alias deduplication, delayed work,
late-failure rollback, historical receipt replay, backup/anonymisation, migration
collision and corrupt-receipt rejection. The real Python/native HTTP fixture
imports 205 holds, reviews their collection, hands work to revision 2 and recovers
a committed batch response being lost while preserving its saved plan.

The full-copy rehearsal reviewed 815 imported collection definitions from
disabled revision 1 to active revision 2, then activated all 227,443 original
holds through the real Python CLI and native HTTP API in 2,275 batches. Lost
responses after the first batch and after 100,000 committed targets recovered
from their original receipts. Saved plan bytes remained unchanged across resume,
status and replay. This was confined to the disposable copy; no collector job,
source fetch or new capture was started.

Independent reconciliation passed in 968.249 seconds. All 225 unaffected tables,
original import ledgers, previous collection definitions, target history,
migration history and sequences match the baseline. Each original hold was
consumed exactly once, and each replacement has the reviewed collection revision,
retained URL, priority and exact deadline. All 16,193 other historical targets
remain unchanged. The report verifies all 2,275 receipts and their hashes,
227,443 replacement identities, and 454,886 new history rows. Integrity was
clean with zero foreign-key violations.

The database is 20,085,633,024 bytes, an increase of 575.340 MiB. The activation
workflow, including plan preparation, two response-loss recoveries, status and
replay, took 478.411 seconds. Opening the migrated copy took 158.286 seconds;
fresh startup after activation passed in 192.032 seconds. These measurements do
not satisfy the release's startup or cutover performance requirement. The
indexed candidate query returned 100 records in 0.001661 seconds.

Evidence is under `.local/native-enrichment-activation-rehearsal-20261003/`.
The schema-59 copy replaces its disposable predecessor under the documented
retention policy; original compatible and frozen input snapshots remain. Free
space stayed above 110 GiB while both copies were present, with the 50 GiB reserve
enforced for planned rehearsal writes. Production and `develop` are unchanged.
The preceding `c9c9be9af` checkpoint passed build, lint and preview-image
publication CI.

The full transition goal remains active. Staged checkpoint conversion, import
review resolution, remaining operational/policy families, additional download
adapters, native UI, host/n8n conversion, compatibility removal,
backup/export/restore, performance and coordinated cutover still require work.

## Retained legacy enrichment checkpoints

Schema 1000060 adds resumable conversion and application inspection of saved
legacy `enrichment_jobs.staged_json`. Each outcome binds the frozen manifest,
original source ordinal and source/staged hashes. Converted documents share
equal metadata bodies, optional shallow deltas and explicit parent references,
while retaining every ordered post/media record, pending child and unresolved
reference. Exact large JSON numbers, explicit nulls and the historical retention
policy are preserved. Equal converted documents are shared across source rows.
Malformed and unknown formats retain their original values with a review reason.

Legacy staging did not record observation times or owned producer attempts.
Conversion explicitly records observation time as unrecorded, retains the
reported extractor version separately, and leaves unscoped unresolved URLs
unscoped. It cannot impersonate a native worker transcript. Conversion does not
fetch sources, create captures, change target choices or release review holds;
reviewed handoff into native execution remains required.

`stash-import-enrichment-checkpoints` verifies frozen input and the exact number
of staged rows, sends bounded batches and resumes from committed source ordinals
after response loss. Application routes expose progress, paged summaries and
selected original/converted evidence. Completed replay is read-only. Native
startup validates deterministic conversion and source bindings, backup retains
the new evidence, and anonymisation removes it with its source records.

The full backend gate passed in 890.153 seconds with zero lint issues (API
731.068, ingestion 621.203, SQLite 872.698 seconds). All 382 producer tests passed
against current source in 36.094 seconds. Focused fixtures cover sparse cursors,
shared bodies with distinct original hashes, exact source reconstruction,
unsupported formats, caught late-failure rollback, schema collision, backup,
anonymisation and startup rejection of altered evidence. The real Python/native
HTTP fixture converts 205 staged rows in resumable batches, retains one malformed
row for review, and recovers a committed batch response being lost. A packaged
wheel smoke test exercised the new command and both related enrichment commands
from the built artifact.

The full-copy rehearsal used SQLite's online backup to include the baseline's
committed WAL data. Schema-60 opening/migration took 837.629 seconds under
concurrent backend checks. The real CLI then completed response-loss recovery,
resume and replay in 62.456 seconds. The frozen operational input contains zero
non-null staged values, so this rehearsal verifies the empty-input result and
whole-library preservation; populated conversion semantics are exercised by the
fixtures above. It does not claim that a fresh cutover snapshot will also have no
staging. No collector or provider execution started, and the migration-wide
`imported` flag remains false.

Independent reconciliation passed in 364.765 seconds. All 231 pre-existing
tables, original schema objects, earlier migration history and sequence counters
match the baseline. Captures, media, source bodies, held/released work and prior
activation receipts remain unchanged. The three new tables contain only the
verified empty-input progress receipt. Integrity is clean with zero foreign-key
violations. The verified database is 20,085,768,192 bytes; it replaces the
schema-59 disposable copy under the existing retention policy, while original
compatible and frozen source inputs remain available.

Fresh startup passed in 156.991 seconds. Startup and cutover performance remain
release requirements; these results do not close them. Evidence is under
`.local/native-enrichment-staging-rehearsal-20261003/`. The preceding `7535da1a7`
checkpoint passed CI build, lint and native preview-image publication.

The full transition remains active. Reviewed native execution of imported
staging, import review resolution, remaining operational/policy families,
additional download adapters, native UI, host/n8n conversion, compatibility
removal, coordinated backup/export/restore, performance and cutover still require
work. Production and `develop` remain unchanged.

## Unknown historical observation times

Schema 1000061 lets retained source evidence distinguish an unknown original
observation time from the time it entered the archive. Known captures keep their
original timestamp and signature, with a null recording time. Undated captures
have a null observation time and a required archive recording time under a
separate signature domain. Identical capture UUID replay cannot change that
meaning. JSON emits null for an unknown observation, and capture pagination
uses the applicable timestamp plus UUID through a dedicated index.

Undated publisher evidence requires review. An explicit account association is
retained without fabricating first/last-observed dates for identifiers. Existing
publisher choices remain selected. The normal producer boundary continues
requiring actual observation times. This is a prerequisite for imported staging
handoff; it does not itself turn a legacy checkpoint into an owned worker result,
release a held target or start a collector.

The full backend gate passed in 893.177 seconds with zero lint issues (API
715.939, ingestion 618.804, SQLite 874.963 seconds). Fixtures cover nullable JSON,
known/unknown capture paging through the index, exact replay, changed-time
rejection, direct SQL ambiguity guards, publisher review, backup, anonymisation
and startup rejection of altered evidence without writing to the database.
An actual schema-60 fixture migrates while retaining signed observations and a
noncontiguous rowid; a destination-name collision rolls back the table rebuild.
Other historical migration fixtures now copy their historical column sets
explicitly rather than assuming every later schema has identical columns.

SQLite online backup created one replacement rehearsal copy, including committed
WAL state. Normal opening, migration and reinitialisation passed in 704.150
seconds while the backend checks ran. No extra full database archive was retained.

Independent reconciliation passed in 381.105 seconds. All 234 existing data
tables retain their original values and types, including all 526,348 captures'
UUIDs, rowids, timestamps and signatures. The new recording-time column is null
for these known observations. Earlier migration history, sequence counters and
unrelated schema objects are unchanged. The new capture pagination index is
used; integrity is clean with zero foreign-key violations. The verified database
is 20,265,979,904 bytes. Undated behavior is covered by populated fixtures; no
unknown historical times were inferred or added to the frozen library.

Fresh startup passed in 162.056 seconds. Startup and cutover performance remain
release requirements. Evidence is under
`.local/native-capture-time-rehearsal-20261003/`. Both copies left about 138.6 GiB
free during the final checks, above the 50 GiB reserve. The verified copy replaces
its schema-60 predecessor under the existing retention policy; original compatible
and frozen source inputs remain available. The preceding `4a69a1e4b` checkpoint
passed build, lint and preview-image publication CI.

The full transition remains active. Reviewed execution of imported staging,
import review resolution, remaining operational/policy families, additional
download adapters, native UI, host/n8n conversion, compatibility removal,
coordinated backup/export/restore, performance and cutover still require work.
Production and `develop` remain unchanged.


## Reviewed checkpoint evidence acceptance — 2026-10-03

Schema 1000062 adds application preview, acceptance and receipt lookup for one
converted legacy checkpoint. It records shared native captures with unknown
observation times and the actual acceptance time, while preserving original
record slots, both parent references, exact numbers, pending children and
unscoped references. Duplicate source slots share a capture. It neither applies
a newer retention policy to old source bytes nor invents producer ownership,
missing extractor versions or source URLs.

A preview binds the frozen snapshot and manifest, original ordinal and target
revision, post revision, source hashes and reconstructed capture identities.
When a post only has a catalog-local identifier, its first service identifier is
an explicit reviewed change. Conflicting identifiers and another post's existing
claim are rejected. Acceptance, captures, optional identifier and historical
collection associations are atomic. Original review state, scheduling/history,
worker state, publisher decisions and scene/image metadata remain unchanged.
A lost response can be recovered by receipt UUID or exact replay after later
review decisions. This is evidence acceptance; reviewed child execution remains
to be implemented.

The preceding `c42b84d9e` build and lint CI passed, but its preview publication
failed during a migration test. A two-connection regression reproduced the cause:
SQLite could expose an old column count to the comparison helper before stepping
a statement after another connection rebuilt the table. Commit `983e9b4d8` pins
a read transaction and refreshes its schema before preparing the row comparison.
The new test failed before that fix and passed afterward; comparison assertions
were retained.

The full backend gate passed in 838.666 seconds with zero lint issues (API
666.155, ingestion 557.603, SQLite 819.757 seconds). Coverage includes shared
bodies and deltas, both parent references, large numeric IDs, missing historical
fields, invalid graphs and oversized payloads, conflicting post identities,
stale previews, caught late-write rollback, duplicate acceptance, backup/restore,
anonymisation and startup refusal of missing evidence bindings without writes.
The application API fixture imports frozen registry/catalog/automation inputs,
loses a committed acceptance response, recovers the receipt, and verifies that
the original target stays in review. A schema-61 fixture preserves staging and
captures across upgrade; a destination-table collision rolls back the migration.

One SQLite online backup produced the replacement copy. Normal opening,
migration and reinitialisation passed in 808.922 seconds. Independent comparison
passed in 437.223 seconds: all 234 existing data tables retain their exact values
and types, including all 526,348 capture UUIDs, rowids, observation/recording times
and signatures. Existing schema objects, prior migration history and sequence
counters remain unchanged. The two new evidence tables are empty, as expected:
the frozen operational snapshot has no saved legacy checkpoint bodies. Populated
acceptance and recovery behavior is demonstrated by fixtures, not inferred from
that empty historical input. Integrity is clean with zero foreign-key violations.
The database remains 20,265,979,904 bytes.

Fresh-process startup passed in 158.812 seconds. Startup/cutover performance
remains a release gate. Evidence is under
`.local/native-checkpoint-evidence-rehearsal-20261003/`. The large rehearsal was
assigned idle disk-I/O priority while other tests and services ran. Free space
stayed above 136 GiB with both database copies present. The verified replacement
supersedes schema 61 under the retention policy; original compatible and frozen
source inputs remain available.

The full transition remains active: reviewed staging execution, import review
resolution, remaining operational/policy families, additional download adapters,
native UI, host/n8n conversion, compatibility removal, coordinated backup/export/
restore, performance and final cutover are still required. Production and
`develop` remain unchanged.


## Retained context and child retry prerequisites — 2026-10-03

Schema 1000063 introduces signed capture context bindings. A newly observed
capture can retain exact older parent metadata while identifying the original
capture UUID at each parent path. Capture and bindings are atomic, and missing
or replaced bindings fail integrity checks even if another capture contains the
same text. The new retention policy applies current reduction to fresh child
fields and verifies older embedded parents against their original captures.
No existing capture, publisher decision or identifier date is rewritten.

Publisher assessment follows the original bound observation. Known dates remain
the parent's dates; unknown dates still require review and never become today's
first/last observed dates after an explicit link. Nested media-host context is
supported without confusing a feed owner with a post's actual author. Ordinary
captures without bindings take a fast path with no additional reconstruction.
Backup/restore and anonymisation include the new relationships.

The Go and Python collector contracts now support a retained-context prefix for
future reviewed child retries. It preserves accepted capture UUIDs, exact large
numbers, both parent branches and historical payload bytes without inventing
observation times, source URLs or producer identities. A retry can fetch only
saved children; it cannot append more historical captures, copy old fields as a
new delta, or attach unbound inline parents. The seed retains pending parent/depth
associations, deduplicates identical child work and leaves original unscoped
references in the frozen acceptance. Invalid/oversized or ambiguous graphs remain
review cases. Offline Go-to-Python-to-Go tests exercise the real collector, its
inherited source settings, child reservation and error recovery.

This is a prerequisite checkpoint, not an enabled execution handoff. Native job
admission, checkpoint storage/startup validation and publication still reject
this new execution format. Evidence acceptance keeps its review hold. The next
step must bind an application-reviewed handoff to the exact evidence, target,
post and active collection revisions; deliver its seed without a fabricated
lease/receipt; and connect worker/outbox recovery, publication and versioned
release proofs. Existing v1 argument, receipt and release hashes remain intact.
Context provenance also needs to be applied to newly created native child
publications through that versioned contract.

The full backend gate passed in 844.413 seconds with zero lint issues (API
667.981, ingestion 560.558, SQLite 826.582 seconds). All 387 producer tests passed
in 36.539 seconds. A subsequent small publisher fast path passed the relevant
publisher/context/recording-time tests (18.169 seconds) and a final clean lint
check. Tests cover unknown and known parent times, exact old payload retention,
changed bindings, missing parents, future parent clocks, unretained fresh fields,
caught late-write rollback, read-only startup refusal of deleted/rebound context,
backup/restore, anonymisation and schema-62 upgrade/collision rollback. The
migration comparison helper now orders WITHOUT ROWID tables by their primary
keys, while retaining the earlier schema-refresh fix.

An online backup took 43.852 seconds. Normal open, migration and reinitialisation
passed in 705.276 seconds. Fresh-process reopen passed in 242.653 seconds while
independent reconciliation was also running; this is not an isolated startup
benchmark, and startup performance remains a release gate. Independent
reconciliation passed in 506.582 seconds: all 236 existing data tables retain
exact values and types, including all 526,348 capture UUIDs, rowids, observation/
recording times and signatures. Existing schema objects, migration history and
sequence counters are unchanged. The new context table is empty, as expected for
these frozen inputs; populated context behavior is covered by the fixtures.
Integrity is clean with zero foreign-key violations, and the database remains
20,265,979,904 bytes. This verified replacement supersedes schema 62 under the
retention policy, subject to the final host-visible open-file check.

Obsolete paged review exports were retired after host-visible open-file checks,
retaining their small manifests. This reclaimed another 580,788,224 bytes without
removing frozen source inputs. With both large database copies present, free
space stayed above 135 GiB; the existing guard reserves 50 GiB plus estimated
peak work space. Evidence is under
`.local/native-checkpoint-handoff-rehearsal-20261003/`.

The full transition remains active. Reviewed execution, migration review,
remaining operational/policy families, additional adapters, native UI, host/n8n
conversion, compatibility removal, coordinated backup/export/restore, performance
and final owner-reviewed cutover are still outstanding. Production and `develop`
remain unchanged.

## Exact checkpoint handoff review — 2026-10-03

Schema 1000064 adds an application review receipt for child-only execution from
accepted legacy captures. Preview binds the acceptance and plan hash, original
target, current post revision, active destination collection revision, runtime
and configuration hash, capture policy and exact resume document hash/size/counts.
The destination target identity is explicit when a newer collection definition
requires a replacement. Requests cannot invent an acceptance or silently reuse a
preview after post, target, collection or runtime changes.

Receipt and seed lookup recover a committed review after a lost response or
later edits. The seed is reconstructed from frozen evidence, with original
capture identities, payloads, extractor versions and recording times verified.
The database stores only the small review plan. Original unscoped references
remain in the acceptance and are counted separately; null historical observation
times do not acquire an observing producer or a new timestamp. Backup/restore,
anonymisation and read-only startup verification include the review.

This increment does not consume the review, release the target or create a worker
job/checkpoint/lease. Native job arguments, checkpoint storage and publication
remain gated to their existing execution format. The next increment must bind
native admission to this receipt, deliver the exact seed through producer scope,
include saved child services in scheduling/fairness, preserve prefixes during
retry, publish retained and new observations with distinct provenance, and
version release proofs. New ordinary child captures also need bound context.

Targeted tests cover retained bodies and unknown clocks, stale post/target/
collection/runtime review, caught late-error rollback, missing/corrupt seed
bindings, backup/restore, anonymisation, and schema-63 upgrade/collision rollback.
The real HTTP fixture loses both committed evidence and handoff responses,
recovers their receipts, parses the seed through the Python collector contract,
and confirms that the original target remains in review. An initial broad run
found an invalid namespace in the new stale-post test fixture; correcting that
fixture passed all handoff tests with integration settings in 7.232 seconds.
The corrected full backend gate passed in 870.028 seconds with zero lint issues
(API 688.716, ingestion 579.586, SQLite 860.681 seconds).

One online backup took 40.931 seconds. Normal opening, migration and
reinitialisation passed in 1,058.161 seconds with idle disk-I/O priority during
other host work. Fresh-process opening passed in 560.044 seconds while independent
comparison and host work were running; this is not an isolated benchmark. The
independent comparison passed in 1,377.083 seconds: all 237 existing data tables
retain their exact values and types, including 526,348 capture UUIDs, rowids,
observation/recording times and signatures. Existing schema objects, prior
migration history and sequence counters are unchanged. The new handoff table is
empty, as expected for the frozen inputs; populated review behavior is covered
by the fixtures. Integrity is clean with zero foreign-key violations, and the
database remains 20,265,979,904 bytes. Startup/cutover performance remains a
release gate. Evidence is under
`.local/native-checkpoint-review-rehearsal-20261003/`. The verified replacement
supersedes schema 63 under the retention policy after the host-visible open-file
check; original compatible and frozen source snapshots remain available. Free
space stayed above 135 GiB with both rehearsal copies present.

The full transition remains active, including execution handoff, migration
review, remaining operational and policy families, adapters, native UI, host/n8n
conversion, compatibility removal, coordinated recovery/export, performance,
owner-reviewed cutover and retirement.

## Execute reviewed checkpoint handoffs — 2026-10-03

Schema 1000065 connects the reviewed seed to native admission, worker execution
and publication. Admission rechecks the exact reviewed post, target, collection,
root, runtime and policy, then atomically releases the planned target and creates
one job. The small binding tables contain original capture identities, digests
and pending service names. They do not duplicate payloads or invent a producer
attempt/checkpoint receipt. A repeated admission recovers the original job even
after completion or later collection edits.

New native jobs pin version-2 capture semantics. Existing native v1 jobs and
released proofs retain their original identities and checksums. The producer
uses the verified seed only before its first real checkpoint, and recovers saved
delivery before another fetch. Pending child services participate in pacing and
fairness before any checkpoint exists. A child-only failure with no saved child
failure evidence does not pause the parent service.

Publication reuses original retained captures, including unknown observation
times and extractor versions. It associates them with the destination collection
without treating them as fresh publisher/translation evidence. Fresh child
captures receive their real observation times and signed parent-capture bindings;
ordinary new child enrichment uses the same contract. Version-2 release proofs
bind the review and seed to original signatures, new captures and submitting
producer records. Original unscoped references remain in accepted evidence.
Startup, backup/restore and anonymisation include these relationships.

The real HTTP fixture drops committed admission, checkpoint and publication
responses, then recovers through three fresh Python worker processes. It fetches
the saved child once, never refetches the retained root, and finishes with a
single attempt. Exact large integers and missing historical clocks survive the
round trip. Other fixtures cover stale review, late-error rollback, child
cooldowns and fairness across restart, rewritten prefixes, completion outside
publication, missing projections, and migration of existing v1 released proofs.

Both build pipelines for the preceding commit failed twice while fetching the
pinned gallery-dl repository from Codeberg (HTTP 504 before code tests). The
dependency now uses the upstream GitHub repository at the exact same commit;
its local installation passed. This changes the transport URL, not the pinned
extractor version or runtime identity.

The full backend gate passed in 850.368 seconds with zero lint issues (API
681.549, ingestion 562.567, SQLite 841.071 seconds). All 388 producer tests passed
in 38.226 seconds. The real HTTP review/execution fixtures passed in 6.153 seconds.
The first complete backend run found one older migration fixture creating a new
v2 job before reconstructing schema 57; switching that fixture to a historical
v1 job passed its targeted check in 8.468 seconds and the subsequent full gate.

One SQLite online backup took 70.908 seconds. Normal opening, migration and
reinitialisation passed in 784.913 seconds, and fresh-process opening passed in
157.299 seconds. The large-copy operations used idle disk-I/O priority during
other tests and host activity; these are not isolated startup benchmarks.

Independent comparison passed in 869.229 seconds. All 238 existing data tables
retain their exact values and types, including 526,348 capture UUIDs, rowids,
observation/recording times and signatures. Prior migration history and sequence
counters are unchanged. Only the expected release-table definition and three
scope triggers changed; the three new execution tables are empty. These frozen
inputs contain no saved legacy checkpoint bodies, so populated handoff execution
is demonstrated by fixtures. Integrity is clean with zero foreign-key violations,
and the database remains 20,265,979,904 bytes. Startup/cutover performance remains
a release gate.

Evidence is under `.local/native-handoff-execution-rehearsal-20261003/`. This
verified replacement supersedes schema 64 under the retention policy after the
host-visible open-file check. Free space stayed above 133 GiB with both copies
present. After the checks, 763 closed temporary SQLite test files were removed,
reclaiming 973,881,344 bytes. Original compatible and frozen source inputs remain.

The full transition remains active: refreshed coordinated snapshots, migration
review, remaining operational/policy families, additional adapters, native UI,
host/n8n conversion, compatibility removal, coordinated backup/export/restore,
performance, owner-reviewed cutover and retirement are still required.
Production and `develop` remain unchanged.

## Native file edit and deduplication history — 2026-10-03

Schema 1000066 promotes catalog metadata edits, file-state changes and
deduplication assertions into native history, with source-file observations and
declared content claims as its references. Events preserve source IDs, original
timestamps and locations, including archive members. The receipt timestamp stays
separate from an unknown source clock. Path-specific edits remain separate;
legacy null means inherit, relationship values remain source names, and unknown
fields or representations remain inspectable extensions. Importing this history
does not change selected Stash fields, create files or execute old deletions.

The catalog import uses the completed media mapping's frozen root/collection
revisions and commits bounded 50-record/16-MiB batches. Its CLI resumes from the
native cursor after a lost response. Application APIs expose import receipts,
individual events and bounded observation/claim history. Startup verifies event
signatures, complete children, scope and exact source receipts; backup and
anonymisation include the graph.

Populated fixtures cover path-specific alternatives, inherit, unknown fields,
large integers, missing members, archive paths, prepared/finished assertions,
replay, swallowed late errors, altered receipts, read-only corruption rejection
and migration collisions. The real HTTP fixture drops a committed batch response
and resumes through fresh CLI processes. The frozen inputs contain no metadata
edits or file-state events, so their populated behavior is demonstrated by those
fixtures rather than claimed from an empty production sample.

The backend gate passed in 977.652 seconds with zero lint issues (API 774.741,
ingestion 647.853, SQLite 939.224 seconds). After strengthening exact receipt
verification, all affected history/HTTP tests passed in 35.640 seconds and final
lint passed in 17.157 seconds. All 391 producer tests passed in 73.095 seconds.

One SQLite online backup took 55.076 seconds. Normal migration and
reinitialisation passed in 1,070.266 seconds. Importing all 1,697 frozen catalogs
and verifying completed receipt replay took 210.964 seconds, including 194.690
seconds to open the database. All 927 deduplication assertions mapped without
review failures. A fresh process reopened the populated database in 209.793
seconds. Large-copy checks used idle I/O priority alongside other host work;
these are not isolated startup benchmarks. Startup/cutover performance remains
a release gate.

Independent comparison passed in 719.399 seconds. All 241 existing data tables
retain their exact values and types, including 526,348 capture UUIDs, rowids,
clocks and signatures. Existing schema objects, prior migration history and
sequence counters remain unchanged. The 927 assertions preserve 913 source
event IDs and 2,047 locations, including separate catalog copies and their
original timestamps. Integrity is clean with zero foreign-key violations. The
database remains 20,265,979,904 bytes.

Evidence is under `.local/native-file-history-rehearsal-20261003/`. The verified
replacement supersedes schema 65 under the retention policy after commit/push
and the host-visible open-file check. Original compatible and frozen source
inputs remain available. With both rehearsal copies present, free space stayed
above 133 GiB; each import batch also checks the 50-GiB host reserve plus its
write allowance.

The full transition remains active: historical metadata review/application,
remaining operational/policy families, refreshed snapshots, additional adapters,
native UI, host/n8n conversion, compatibility removal, coordinated recovery and
export, performance, owner-reviewed cutover and retirement still require work.
Production and `develop` remain unchanged.

## Reviewing historical metadata choices — 2026-10-03

Schema 1000067 adds immutable receipts for explicitly selecting a retained
catalog edit. The application API discovers alternatives through a selected
scene/image's indexed file associations, without scanning all catalogs. Preview
shows the current value, protection, proposed value and source context without
writing. Applying a ready preview commits the typed field decision and its
source-history/file-match receipt together. Unknown fields remain inspectable
evidence and cannot select arbitrary database columns. Migration creates no
reviews and changes no selected metadata.

Performer candidates include canonical names and aliases without prioritizing
either. Missing or ambiguous names block the complete relationship replacement
until explicitly resolved; bounded results report truncation. Studio, tag and
group selections also use native identities and revisions. Apply rechecks the
entity, selected field, candidate revisions, file ownership and file/ZIP
generations. A historical inherit choice releases protection while retaining
the current value until an allowed policy evaluates it. Different historical
paths remain separate choices even when their files were deduplicated.

Clients can recover the original receipt after a lost response by replaying the
same saved request UUID and body. Later edits, UUID adoption or deletion of the
matched file do not reapply a committed request. Changed bodies are rejected.
Late errors roll back the field decision and receipt together, including when a
caller swallows the error. Successful new changes use the existing after-commit
notification contract; retries do not register another notification. Current
field summaries expose compact provenance without duplicating their values or
including plugin settings. Native UI controls are still pending.

Populated SQLite and HTTP fixtures cover exact and deduplicated alternatives,
protected fields, inherit, URL normalization, name ambiguity and overflow,
unmatched names, target revisions, ZIP containers, source ownership, retry,
adoption/deletion, rollback, corrupt receipts, migration collisions and origin
protection. The full fork gate passed in 1,080.440 seconds, including 528 v3
tests, 391 producer tests, generated contracts, zero lint issues and the full Go
integration suite. Final candidate-overflow/migration-collision and HTTP summary
checks passed in 12.065 and 32.794 seconds. The last review-specific replay-error
change passed affected API/SQLite tests in 23.704 seconds and lint in 16.735
seconds. These final focused checks followed compilation of the full gate.

One online backup took 132.558 seconds. Normal migration and reinitialisation
passed in 996.212 seconds; a fresh process reopened the database in 205.753
seconds. The reopen profile places most CPU time in SQLite stepping during
earlier catalog validation. Startup performance remains a release gate; the
checks used idle I/O priority alongside other host activity and are not isolated
benchmarks.

Independent reconciliation passed in 791.971 seconds. All 248 existing data
tables retain their exact values and types, including capture rowids, clocks and
signatures. Existing schema objects, previous migration history and sequence
counters are unchanged. A separate generated-column comparison also verified
all 1,927 performer primary-name flags and their table definitions. Integrity is
clean with zero foreign-key violations. The database remains 20,265,979,904 bytes.
The frozen catalogs have no metadata-edit rows, so populated review behavior is
demonstrated by fixtures; the new receipt table is correctly empty in this copy.

Evidence is under `.local/native-metadata-review-rehearsal-20261003/`, with the
full fork gate report in the preceding file-history rehearsal directory. The
verified copy supersedes schema 66 under the retention policy after commit/push
and the host-visible open-file check. Free space was 178 GiB with both copies
present, above the 50 GiB host reserve. Original compatible and frozen migration
inputs remain available.

Remaining work includes native review controls, other operational/policy import
families, refreshed snapshots, additional adapters, host/n8n conversion,
compatibility removal, coordinated recovery/export, performance and the reviewed
production cutover. Production and `develop` remain unchanged.

## Scene and image metadata review controls — 2026-10-03

Scene and image pages now expose native Metadata review through the shared
layout's desktop tabs and mobile section menu. The panel loads only the selected
entity's current fields and a bounded page of file-linked historical edits.
Current protection and field decisions are expandable; individual alternatives
and their previews load on demand. Applying a choice refreshes active affected
library queries. Unrelated configuration and plugin settings are not included.

Previews compare current and proposed values and explain replacement of a
protected choice or release to automatic updates. Performer candidates show
canonical names, disambiguation and local library IDs. Ambiguous or missing
names require explicit choices, including a search for a differently named
existing performer. Studios, tags and groups use the same typed selection path.
Unknown retained fields remain unsupported. This is depicted metadata review,
not source-account ownership or a performer merge.

A deployment-scoped IndexedDB journal saves each exact Apply body before any
transmission. Transactions coordinate tabs and retain one unresolved choice per
scene/image. Opening the panel does not send mutations. Recovery checks the
original receipt before replaying the saved body, including after a page reload.
Mismatched receipts, uncertain responses and storage failures retain the pending
request. Only a definitive stale-preview refusal permits starting a new review.
Changed files and ZIP containers now report that refusal consistently, while
preserving the underlying repository error. Display refresh failures remain
separate from a successfully committed choice.

The final v3 validation gate passed with 540 tests, generation, type checks,
formatting, locales and retained contracts. The embedded v3 build and Go asset
checks passed. Eighteen browser checks passed in 28.7 seconds using the cached
version-matched Playwright image,
covering scene/image pages on desktop/mobile, ambiguous and unmatched names,
lost-response recovery, stale previews, multiple tabs and two deployment prefixes
on one origin. Chromium and WebKit screenshots were inspected. The initial host
browser invocation lacked its browser binaries; the cached container supplied
both engines without another browser installation.

The full fork gate passed in 976.756 seconds, including Go lint/integration tests
and all 391 producer tests. A subsequent UI refinement uses secure random bytes
for request UUIDs on LAN HTTP deployments where randomUUID is unavailable; the
final v3 gate, browser run, build and Go embedding checks above include it.
Schema stays 1000067 and this change requires no additional full-size rehearsal
database. Evidence is under `.local/native-metadata-ui-20261003/`.

Cleanup removed the closed schema-66 predecessor and temporary tests, reclaiming
20,307,513,344 bytes. Five unused intermediate native n8n rehearsal image tags
were removed while retaining the latest verified image. After the checks, 29
closed temporary test files were also removed; about 189 GiB was free with the
50 GiB reserve in force. Original compatible/frozen migration inputs and the
verified schema-67 copy remain.

Broader source/account/collection management, remaining import/policy families,
source adapters, live host/n8n conversion, compatibility removal, coordinated
recovery/export, performance and reviewed production cutover remain open.
Production and `develop` remain unchanged.

## Startup validation with stale import statistics — 2026-10-03

Catalog receipt validation now requires the existing unique snapshot index when
joining a publisher, attachment, media or document receipt to its import
checkpoint. The rehearsal's publisher/attachment planner statistics still
described one checkpoint after 1,697 catalogs had been imported. SQLite chose a
complete checkpoint-table scan for every receipt. Validation runs before writes,
so updating database statistics is not a prerequisite for opening it safely.
All validation predicates, counters, reference checks and rejection behavior
remain unchanged; no schema or stored-data migration is needed.

Read-only probes of the retained schema-67 database measured publisher receipt
validation at 26.464 seconds before and 0.837 seconds with the indexed lookup;
attachment validation measured 27.349 and 0.564 seconds. Both returned the same
valid result. Media/document lookups already chose that index in this snapshot;
they receive the same protection against stale statistics, without claiming a
measured query-plan improvement here. These are sequential checks under other
host activity, not isolated benchmarks or production startup guarantees.

The regression fixture builds the real native schema and installs the observed
one-row planner estimate. Removing the index requirement through a temporary
compiler overlay reproduces all four unwanted parent scans; the final code
passes. Existing import, damaged-receipt and before-write rejection fixtures
also pass. Complete read-only lineage validation passed before and after the
change, taking 300.363 and 113.474 seconds respectively. Its sampled CPU cost
fell from 175.08 to 112.03 seconds. Concurrent host load and cache state also
affect elapsed time; the profiles identify removal of the repeated scans.
The full fork gate passed in 979.024 seconds, including Go lint/integration tests,
540 v3 tests and 391 producer tests. The final one-row-statistics regression also
passed independently. Evidence is under
`.local/native-startup-performance-20261003/`.

The checks reuse the latest verified 20,265,979,904-byte native database through
read-only connections. No extra database archive is created. The 50 GiB host
reserve remains in force. Startup performance and the rest of the transition
plan remain release gates; production and `develop` remain unchanged.

## Native account ownership review API — 2026-10-03

Schema 1000068 adds durable receipts for explicit account ownership choices.
Application routes now page canonical accounts, qualified identifier candidates,
retained claim evidence and ownership history. Cards summarize at most eight
identifiers and mark truncation. Individual previews and links read the selected
account and performer; they do not scan catalogs or rebuild the library.

Linking requires an explicitly selected performer UUID and revision. Shared
names or identifiers remain candidates. Preview binds the account revision,
current resolved owner and proposed performer; changes after preview require
review again. Account owners remain separate from depicted media performers.
Unlinked and undecided choices are distinct. Retrying the same request returns
its original receipt after later links, unlinks, account consolidation, performer
UUID adoption, merges or deletion, without restoring a superseded choice.

Focused repository and HTTP checks pass, covering ambiguity, bounded discovery,
read-only previews, stale targets, response-loss retries, rollback after receipt
failure, identity changes, anonymisation and corrupt/missing schema rejection.
Migration fixtures preserve existing decisions and refuse destination collisions
without modifying the original file. The full fork gate passed in 1,027.769
seconds, including Go lint/integration tests, 540 v3 tests and 391 producer tests.
The full-copy migration passed in 1,099.134 seconds, and a fresh reopen passed in
165.359 seconds. On that copy, a 25-account page took 10.618 ms and selected
account details plus an unlinked-choice preview took 0.242 ms, without applying
a choice. These are local repository timings, not browser/network benchmarks.
Independent reconciliation passed in 635.003 seconds: all 249 existing tables,
their schema, history and sequence counters are preserved, including 1,182
accounts and four saved ownership decisions. Generated performer-name values
also match. The new receipt table is empty, integrity is clean and foreign-key
violations are zero. Evidence is under
`.local/native-account-review-rehearsal-20261003/`.

The 50 GiB host reserve applies to the candidate copy and validation work. Once
verified, the candidate replaces the preceding rehearsal rather than retaining
another full database archive. Native account review controls, other management
UI, remaining import/policy families, caller conversion, compatibility removal,
backup/export/restore and reviewed cutover remain unfinished. Production and
`develop` have not changed.

## Native account ownership review controls — 2026-10-03

Account review is now a native route in the desktop utility menu and mobile
navigation drawer. It pages canonical accounts and filters ownership status,
while identifiers, retained evidence and ownership history load on demand.
Existing links offer Change link. The performer picker displays disambiguation
and local IDs and requires an explicit selection before preview. Linking,
explicit unlinking and returning an account to review are separate choices;
account ownership does not assign depicted scene/image performers.

The browser durably saves an exact request before Apply. Recovery checks the
original receipt before resending, coordinates competing tabs and preserves
uncertain requests across reloads. Stale previews require fresh review. Successful
writes refresh the selected account and update its queue card; the existing
page cursor and scroll position survive returning to the queue. A failed refresh
does not erase confirmed success or permit another write from stale state.
Shared transport/storage helpers preserve the prior metadata-review journal.

Focused protocol tests cover response loss, concurrent tabs, endpoint/account
isolation, failed storage, wrong receipts and late completion. All 558 v3 tests,
type/locale/contract checks and the real embedded UI build pass. All 38 account
and metadata browser checks pass in Chromium and WebKit, including mobile
navigation and scroll restoration. Desktop/mobile screenshots were inspected.
The full fork gate passed in 979.647 seconds, including Go
lint/integration checks and 391 producer tests. The preceding `583e88f48` account
API checkpoint also passed CI lint, build and native image publication. Evidence
and the two retained UI screenshots are under
`.local/native-account-ui-20261003/`.

This increment changes no database schema and creates no additional full database
copy. The verified schema-68 rehearsal remains current, and the 50 GiB host
reserve remains enforced. Account equivalence review, broader source/collection
management, remaining import/policy families, host/n8n activation, compatibility
removal, native backup/export/restore and reviewed production cutover remain open.
Production and `develop` remain unchanged.

## Native account consolidation review — 2026-10-03

Application routes and Account review controls now expose explicit consolidation
of duplicate account records in one qualified service namespace. The selected
account's search is bounded and can resolve an exact archive UUID. Preview shows
both components, retained identifier evidence and the proposed owner. Conflicting
ownership requires an explicit choice; different stable IDs require acknowledgement.
Separate service accounts can still share a performer without being consolidated.
No consolidation changes depicted performers or media files.

The existing event's request digest supports exact read-only receipt checks and
replay after later consolidation, ownership edits, performer UUID adoption,
merges and deletion. The original domain request serialization stays unchanged.
There is no new schema migration, duplicate receipt body or full rehearsal copy.
The browser saves before sending, recovers pending requests after reload, updates
the affected cards, retains the queue and loads consolidation history on demand.
Stale review disables the old form before refreshing; the shared performer input
also honours disabled state while pending requests are checked.

Focused backend tests and all 24 consolidation client/storage checks pass. All
50 account, consolidation and metadata browser checks pass in Chromium/WebKit,
covering mobile layout, conflict resolution, response loss, exact retries and
stale-review recovery. Desktop/mobile screenshots were inspected. The full fork
gate passed in 998.494 seconds, including all 582 v3 tests, type/locale/contract
checks, Go lint/integration tests and 391 producer tests. The real embedded v3
build also passed. Evidence is under
`.local/native-account-consolidation-20261003/`. The preceding `df3534c90` UI
checkpoint passed CI build, lint, browser checks and native image publication.

The schema-68 rehearsal remains current and the 50 GiB host reserve still
applies. Broader source/collection management, remaining import/policy families,
live caller conversion, compatibility removal, backup/export/restore and reviewed
production cutover remain open. Production and `develop` remain unchanged.
