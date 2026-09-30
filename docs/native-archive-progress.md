# Native archive implementation progress

This is the implementation record for the
[full transition plan](native-archive-transition-plan.md). It does not narrow
that plan's scope or replace its completion criteria. Development remains on
`v3-rewrite`; merge into `develop` requires verification and the owner's success
review. Production has not been migrated.

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

## Phase status

| Phase | Status |
| --- | --- |
| 0 Baseline and contract | In progress: source tagged, runtime pinned, all compatible images preserved, independent-fork policy updated. Full backup boundary, fixtures, scoped API contract and performance budgets remain. |
| 1 Native schema and services | Schema promotion, canonical saved/default filters, durable config import, unified performer names, portable archive identities including galleries, native account/ownership storage, shared post/profile/capture storage, ordered attachment manifests, and audited media associations are implemented. Source equivalence, album construction, review APIs/UI and remaining domain services are in progress. |
| 2 Ingestion and producer adapter | Not yet implemented |
| 3 Catalog importer and full-copy reconciliation | Not yet implemented |
| 4 Native UI and client conversion | Not yet implemented |
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

Automatic source-gallery construction and the native album UI remain required
work. Ordered attachment associations are described below. No production
galleries or files have been modified.

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

Source manifest selection, source-to-gallery construction and synchronization,
manual album decisions, importer integration, and native UI remain outstanding.
Production remains on the compatible release.
