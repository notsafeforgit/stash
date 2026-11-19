# Native schema promotion

This is a versioned schema reference, not a deployment checklist. Sections
describe individual migration phases; staging, activation and domain publication
have separate receipts even when all three are implemented. Start with the
[system overview](ARCHITECTURE.md) for current ownership and execution paths.
The startup-versus-full-audit boundary below applies to the historical proof
validation described throughout this guide. Deployment and one-time import
completion belong to the installation's operations records.

## Import history retirement (schema 1000142)

Schema 1000142 removes the frozen catalog and automation import history from the
application. The retired workflows are the catalog and automation staging uploads
and their import passes, the catalog association repair, catalog payload
retention, reviewed activation of imported translations, enrichment and checkpoint
work, enrichment rebinding, enrichment handoffs, the discovery workflow, the
`/import-history` page and API, the `imports` pagination resource and the
`nfo-cleanup` command. The migration drops the staging, receipt, activation and
handoff tables and views, together with `source_post_import_labels` and
`source_cleanup_intents`. It also deletes the discovery archive jobs
(`account.list_page` and `post.verify_candidate`) with their attempts and policy
approvals.

The native data the import produced stays ordinary data: posts, captures,
identifiers, URLs, publisher decisions, attachments, post-media decisions,
documents, translations, source files, observations and matches, and collection
membership groups. `source_enrichment_receipts` remain because completions refer
to them. `source_file_history` and its child tables are frozen evidence for
metadata file-edit reviews; nothing writes new rows. Held translation targets are
unchanged, since translation targets have no excluded state. Ordinary enrichment
jobs, publications, checkpoints, pacing and fairness are unaffected.

**Lookup carry-over.** A legacy-only post can still be enriched by its original
finder lookup. The migration copies the mapped `lookup` finder rows of finished
automation imports into `enrichment_identity_lookups`, keyed by post, collection
and URL and holding the original finder values. It replaces
`enrichment_discovery_resolutions` with `enrichment_identity_resolutions`, which
references the retained `lookup_id`. Earlier discovery resolutions are dropped;
the identifiers and evidence they published remain. Enrichment still corroborates the native
identifier through that lookup, under the `retained-discovery-identity-v1` policy
and its basis rules (`scrape.MatchDiscoveryLookup`). The enrichment job
description carries the result as `identity_resolution`, which
`GET /api/v3/archive/enrichment-jobs/{job}/identity-resolution` reads.
A corroborated identifier that already belongs to another post merges the
duplicate into the first stored post, using the reviewed post merge inside the
publication transaction. When that merge's own checks refuse it, for example
because the merged gallery needs review, the job fails with
`post_identity_conflict` and keeps its checkpoint for review. Retrying would
prepare and refuse the same merge again.

**Target exclusion.** Unreleased held imported enrichment targets (origin
`migration`) move to `excluded` with reason `import_retired`. Their history is
kept, and they cannot be activated.

**Space.** The migration does not shrink the database file. Freed pages become
reusable. A database rebuild (Settings → Tasks → Optimise database, which runs
`VACUUM`) returns the space. Older backups remain the only way to audit the
October import.

## Versioned migration notes

Schema 1000141 stops retaining routine field-value history. Replacing a field
choice deletes its superseded value, relationship references and automatic
provenance links in the same transaction. The upgrade removes existing obsolete
choices while preserving current heads, values, protection, UUIDs and revisions.
Only decisions explicitly referenced by historical file-review/keep receipts
remain as that operation's replay proof; they are not returned as field history.
Ordinary repeated saves reuse an equivalent explicit library choice, including
empty values. Changing an inherited value to an explicit choice remains a real
change. The UI shows the current choice; the existing history endpoint returns
at most that current choice, using its original sequence cursor. Freed SQLite
pages become reusable; this migration does not run a whole-library VACUUM.

Schema 1000139 permits a managed post consolidation to retain a gallery already
shared by one of its members. It changes the insertion guard only; existing
media, galleries and their metadata remain intact. Ordinary gallery adoption
continues to enforce source ownership.

Schema 1000138 expands the existing attempt-failure constraint to retain explicit
account availability and Redgifs deletion codes, including provider history-limit
failures. It preserves every existing failure and request. Fenced confirmed
Redgifs deletion retires its source through the existing collection/subscription
model; other account states keep recovery checks. The migration itself retires
no accounts and changes no downloaded media.

Schema 1000134 adds independent account profile observations and normalized
profile links, with `account.profile` ingestion receipts. The receipt migration
preserves existing capture, file and attachment receipts and their references.
Profile observations do not create source posts or claim media coverage; see
[performer profile links](native-ingestion.md#performer-links-from-captured-profiles).
The direct initialization baseline remains pinned. Backups include this evidence;
anonymized exports remove it.

Schema 1000126 adds bounded durable commands and per-item checkpoints for ordinary
[bulk edits](native-bulk-updates.md), including fenced recovery and notification
attempts. Existing ordinary job history is preserved; pending old executors
without commands retain their interrupted-task behavior. No library entities
are edited by the migration, and the direct initialization baseline stays pinned.

Schema 1000125 captures historical cover frame selections from generated caches
into canonical SQLite metadata, with a finite upgrade cohort and bounded
unresolved evidence. See [cover selection ownership](preview-images.md#storage-and-compatibility-boundary).

Schema 1000124 adds the empty `file_moves` commit-marker table for
[recoverable file moves](file-moves.md). Completed moves retain no journal or
marker history. No existing files or library metadata are changed by this
migration; the direct fresh-install baseline remains independently pinned.

The [versioned migrations](../pkg/sqlite/migrations) define the current native
schema; this guide describes the individual changes by their version. See the
[architecture and upgrade boundaries](native-archive-transition-plan.md). New migrations are
rehearsed on copies before deployment. Schema 1000118 adds a transaction-local
work set for source galleries to inherit performers from their images and
scenes. It creates no historical assignments; normal member/performer edits and
album resynchronization refresh affected galleries while preserving explicit
choices. Schema 1000117 added source-gallery consolidation and alternate titles.

Schema 1000114 adds opt-in monitoring
subscriptions and immutable associations for scheduled runs, separate from
source definitions, downloader configuration and manual imports. Existing
sources default to monitoring off; importing an installation's subscription
list is an explicit operation. See [source collections](native-source-collections.md).

Schema 1000113 makes library folders and ingestion roots one configuration.
Schema 1000110 permits independent source workers to share a media root while
retaining account, target and file fences.

Routine startup validates lineage, the clean supported schema version, required
tables/columns/indexes/triggers, and unfinished write contexts. It does not scan
all retained catalog receipts, captures, decisions or job history. The required
partial index bounds the check for unsealed metadata decisions. Each domain
mutation continues to enforce its transaction guards and constraints.

Explicit migration constructors and `stash --verify-native-snapshot <path>`
retain the full domain/history audit. Snapshot verification additionally checks
SQLite integrity, foreign keys and file identity without writing to the source.
Separating startup from this audit requires no schema migration or data rewrite.

Schema 1000109 records verified image-to-video transitions in `media_conversions`.
The immutable record binds the original image and file lifetime to the new scene
and verified final file lifetime. It permits only that image-to-scene identity
redirect; normal merges still require matching kinds. Source/gallery intent and
chosen covers follow the redirect. Common metadata choices are copied to the
scene with their source/policy provenance, while the original image's field
history and photographer attribution remain retained. See
[conversion admission and publication](native-ingestion.md#completed-file-events).

Scene activity dates now permit NULL for previously undated image counts.
History and last-date queries exclude NULL; counts, decrements and resets include
it. Scene merges and JSON imports/exports retain the undated count separately,
without manufacturing timestamps. Existing dated events migrate unchanged, and
the migration itself converts no images or media files.

Schema 1000099 retains a canonical HTTP origin for newly observed download
dependencies. The existing `source_run_attempt_pacing` rows gain a nullable
`source_origin`; historical root/Imgur/Redgifs bindings remain unchanged with no
invented origin. New dependencies must match their original `SourceScopeV1`
scope. Triggers keep the origin immutable and carry it into exact-window retry
attempts. Signed paths, query parameters and fragments are not retained there.

Schema 1000098 distinguishes configured source scans from publication windows.
The existing run/attempt window envelopes carry optional `basis: "traversal"`;
new triggers reject invalid bases, mixed run coverage and traversal enrichment.
Existing published runs, requests and attempts retain their original bytes.
The version advance prevents an older binary from discarding the basis and
mistaking a completed scan for historical publication coverage. No table copy
or rewritten source evidence is required.

Schema 1000097 adds `account_profile_urls` and
`performer_profile_url_suppressions` for [captured profile links](native-ingestion.md#performer-links-from-captured-profiles).
The former shares link evidence across captures of one account; the latter
preserves explicit URL removals across scrapes, restarts and performer merges.
The migration creates empty tables and retains all existing performer URLs;
it cannot infer historical removals from missing links. Schema 1000096 added
provider metadata import receipts, following schema 1000095's
[file deduplication receipts](native-file-deduplication.md).
Installation-specific migration checkpoints and release verification records
belong to the deployment's operations repository.

The independent schema begins at primary migration 1000000 and identifies itself
with `native_schema.lineage = org.notsafeforgit.stash.native-archive`. New
databases use this lineage. Existing compatible databases require explicit
migration; normal startup does not silently promote them.

Fresh installations create native schema 1000121 directly from the generated
[bootstrap baseline](../pkg/sqlite/bootstrap/1000121.sql), then run 1000122 and
later native migrations normally. The baseline's complete schema and initial
data are checked against historical migration replay in CI. Its version stays
pinned independently of the application's current version; new migrations do
not require another snapshot. See the [completed bootstrap follow-up](../pkg/sqlite/migrations/README.md#completed-follow-up-direct-native-database-bootstrap)
for regeneration and recovery details.

Only untouched databases qualify: no schema objects and no schema/user/application
version markers. The baseline, lineage and clean version commit in one SQLite
transaction after rechecking freshness under the writer lock. Interruption before
commit permits a fresh retry; after commit, remaining migrations use ordinary
explicit upgrade/recovery. Existing libraries are never reinitialized. An old
driver's empty ledger retains historical initialization recovery.

Before a writable migration connection opens, a read-only inspection rejects
dirty migration state, a foreign or missing native marker, newer native schemas,
unsupported legacy primary/fork versions, and missing authoritative native
tables. The native version is distinct from both upstream's 1–86 input sequence
and historical private schemas 998/999. Production deployment must still use a
separate native database path; a marker cannot teach arbitrary future upstream
binaries about this fork.

Startup reads primary and historical-fork versions through a single validated
connection. Native databases have no active fork ledger, so their version lookup
does not repeat the complete integrity pass. Each newly opened migrator still
validates the database before writes; validation is not cached across opens.

Promotion completes the legacy fork migrations and their final reconciliation
once, snapshots existing migration history before any historical compaction,
checks foreign keys, and rejects unknown `fork_*` objects or destination-table
collisions. A single SQL transaction establishes the lineage, preserves history
in `legacy_schema_history`, and promotes the authoritative tables:

| Compatible table | Native table |
| --- | --- |
| `fork_video_file_metadata` | `video_file_metadata` |
| `fork_image_file_metadata` | `image_file_metadata` |
| `fork_scene_cover_sources` | `scene_cover_sources` |
| `fork_shares` | `shares` |
| `fork_share_sessions` | `share_sessions` |
| `fork_file_deletions` | `file_deletions` |
| `fork_saved_filter_state` | `saved_filter_state` (transitional canonical-model input) |
| `fork_performer_autotag_ignored_names` | `performer_autotag_ignored_names` (transitional canonical-model input) |

Rows retain their IDs, checksums, source validity information, historical deleted
file IDs, share tokens/snapshots, and foreign-key behavior. The large scene/image
covering indexes retain their historical names to avoid unnecessary rebuilding.
Repositories and filter joins use the promoted table names. The active fork
migration ledger is removed and legacy migrators cannot modify a native schema.

Migration 1000001 validates and moves each saved-filter AST onto
`saved_filters.filter_ast`, then removes `object_filter` and `saved_filter_state`
in one transaction. The repository reads and writes that single representation.
Invalid ASTs stop conversion before the old state is changed. Existing pending
legacy conflicts retain their exact input strings in `saved_filter_import_conflicts`
for review; the canonical AST stays selected. This evidence survives deletion of
the saved filter. `native_migration_history` records each native conversion.

Current saved-filter imports accept only `filter_ast`. Flat `object_filter`
files are rejected. Historical conversion lives in `pkg/filtermigration` and
is restricted to database promotion and checkpointed configuration promotion.
The `LEGACY-UPGRADE-ONLY` markers, package documentation and caller-boundary test
keep these historical codecs separate from native runtime behavior. Their input
semantics are frozen; new UI/API features must not change historical conversion.
The [legacy upgrade policy](../PROJECT.md#legacy-upgrade-code) defines permitted fixes
and removal prerequisites. Native bootstrap bypasses these readers; supported
historical database/configuration upgrades still need them.
The implemented native migrations are described below.
The full [transition plan](native-archive-transition-plan.md) remains the
acceptance requirement.

Migration 1000002 consolidates `performers.name`, `performer_aliases`, and
`performer_autotag_ignored_names` into `performer_names`. Position zero is the
canonical name; subsequent positions order aliases. The primary flag is derived
from that position, and each row owns its auto-tag policy. The performer's global
ignore flag remains on `performers`. Lookups, sorting, filtering, auto-tagging,
and writes use the native name set. Name selection moves an existing name and
its policy together; replacement is transactional.

Names are not globally unique, and display disambiguation is optional. Name
matches must therefore return candidates rather than establish identity. Exact
duplicate canonical/alias entries are represented once; migration history keeps
their original evidence and any unmatched old policy. Case variants are retained
as distinct spellings. The old name column, alias table, and policy table are
removed. Full native audits reject a missing canonical name; ordinary writes
enforce the native name constraints.

Migration 1000003 adds `default_filters`, one native record per view with a
revision that survives clearing the default. Default filters no longer live in
the configuration file. The UI configuration response includes their derived
values, but generic UI-setting writes cannot modify them. Conflict resolution
requires the reviewed revision and rejects stale browser actions.

`configuration_migrations` first commits the exact filter-only input, converted
records, and any pending alternatives together. The manager then atomically
publishes the cleaned configuration and its source digest, and marks publication
complete. Retrying either interruption point reuses the checkpoint; it cannot
reimport a cleared or edited default. A changed source configuration stops
publication for reconciliation. Invalid canonical criteria stop import before
any records are staged. Historical string pagination is converted explicitly.
Other settings and credentials do not become migration evidence.

New database creation skips historical configuration rewrites; those apply only
to actual older database inputs. Configuration publication uses a flushed
temporary file, atomic replacement, and directory sync while retaining existing
permissions and symlinks. Automatic migration backups are retained on success.

Migration 1000004 adds `archive_entities`, a UUID identity registry for performers,
scenes, images, and file records. Every active identity has exactly one checked,
typed foreign key to the existing row; partial unique indexes enforce one UUID
per row. Existing integer IDs and media relationships remain unchanged. UUIDs
identify records, independent of names, paths, and content fingerprints.

Schema-owned lifecycle triggers allocate identities on creation, advance
revisions on edits, and retain deletion tombstones. A reused local integer ID
receives a new UUID. Performer and scene merges write redirects in the same
transaction that transfers relationships and removes the source records.
Redirects retain their meaning if the survivor is subsequently deleted.
Cross-kind redirects, cycles, and resurrection of retired identities are refused.
Lookup by local ID uses the appropriate partial index; UUID resolution is bounded
to 128 redirects and rejects an invalid chain.

The archive repository can adopt a pre-existing catalog UUID with a revision
check, retaining the generated UUID as a redirect and cascading existing UUID
references. A UUID already assigned elsewhere requires reconciliation. Native API/UI exposure remains separate work; creating this registry has not modified library data. Anonymised exports
replace UUIDs while preserving the graph of redirects.

Migration 1000005 adds native `source_accounts`, qualified
`source_account_identifiers`, and their retained evidence. Accounts have UUIDs
independent of library performers. Identifier lookup uses a covering index and
bounded keyset pagination; identifiers are deliberately not globally unique.
Reused handles and disputed IDs remain candidates rather than silently merging
accounts. Known native handles are normalized for matching; opaque IDs, mirror
user IDs, and unknown service identifiers retain their spelling. Offline profile
URL parsing preserves native/mirror scopes and does not infer an author from a
post, feed, or directory.

Evidence has a stable key, basis, origin, retained JSON object, and observation
interval. Replays reuse a claim and may widen its interval with nanosecond
precision. Reusing an evidence key with different contents is rejected. New
evidence increments the account revision; identical repeats do not invalidate
review. Numeric identifiers are not decoded through floating point, and
duplicate JSON keys or invalid UTF-8 cannot silently lose evidence.

`account_performer_decisions` stores immutable linked, explicitly unlinked, and
undecided choices. `account_performer_links` points to the current decision
through a checked composite foreign key. Decisions require the reviewed account
revision and, when linking, the reviewed active performer revision. Profile
discovery cannot reverse an existing link or explicit unlink. Source identifiers
never assign scene/image performers. Choosing a newer decision preserves history;
moving the head backwards or attaching it to another account is refused.

Ownership references the typed archive performer identity. UUID adoption follows
foreign keys, merges resolve through retained redirects, and deletion retains
the decision against the tombstone for review. New choices cannot use stale or
deleted performer identities. Anonymised exports remove source account evidence
and decisions. Account equivalence is described in the later migration sections; native ownership review uses the API described below.

Migration 1000068 adds `account_ownership_reviews`, immutable receipts for
explicit ownership choices through the native application API. Each receipt
retains its original request and reviewed digest, and references the exact
account/decision pair. It commits in the same transaction as that decision.
Identical retries return the original receipt even after another link, unlink,
account consolidation, performer UUID adoption, merge or deletion. Reusing a
request UUID with different contents is a conflict. Existing ownership history
is preserved without manufacturing review receipts for old choices.

Preview binds the account revision, selected performer revision and current
resolved owner. A changed identity claim, renamed or merged performer, or newer
ownership decision requires another preview. Evidence timestamp updates alone
do not invalidate it. Linking an account records its owner; it does not assign
depicted performers, alter source payloads, or consolidate service accounts.

The account list pages canonical accounts and bounded identifier summaries. Account
details, identifier evidence and ownership history use selected-record lookups
with independent pagination. Ambiguous identifiers remain multiple candidates.
Full native audits validate receipt integrity before writes, and anonymisation removes
receipts before the private account evidence they reference. The native
[Account review screen](native-ingestion.md#account-ownership-review-api) uses
these selected-record endpoints, with explicit performer choices, ownership
history and browser request recovery on desktop and mobile.

Migration 1000006 adds source posts and retained evidence. A post has its own
UUID and qualified service identifiers; native and mirror identifiers cannot
collide. An existing identifier cannot silently move to another UUID. Additional
identifiers require the reviewed post revision. None of these records is
required for a directly scanned scene, image, file, or performer.

| Record | Meaning |
| --- | --- |
| `source_posts` / `source_post_identifiers` | The source post and its explicit identifiers, independent of library media |
| `source_post_revisions` | Shared post body and normalized title/text/date/language projection |
| `source_captures` | Immutable retained evidence for a post revision, with observation time when known, origin, platform, extractor version, retention policy, and per-file/provenance patch |
| `source_profile_bodies` | Shared meaningful profile JSON, identified by namespace and content hash |
| `source_capture_profiles` | Typed references from each capture's shared body or patch to a profile body |
| `source_payloads` | Content-addressed JSON bytes, optionally compressed without changing their checksum |

A multi-image post reuses its post revision while keeping each attachment's
capture details. Identical profiles are reusable across both captures and posts.
A meaningful profile edit selects another profile body; it does not copy the
post body. The revision signature excludes profile references, while the capture
signature includes them. Changed post counters can still produce distinct post
evidence; profile pruning does not imply a general post-field allowlist.

`gallery-dl-retained-v1` implements the existing catalog policy for new input:
remove known credential/runtime fields, keep meaningful Twitter/Reddit profile
fields, and keep Reddit originals or the best available preview instead of
preview ladders. Animation and stream manifests survive; a blurred fallback is
not relabelled as an original. Unknown extractors retain their source fields
apart from credential/runtime filtering. Malformed or missing media IDs do not
establish duplicate content. Normalization works on copies and never fetches
URLs or alters extractor working metadata.

`legacy-retained-v1` is reserved for trusted historical evidence. It partitions old
retained evidence without applying new pruning retroactively. Network ingestion
must not expose this escape hatch. Unknown payload shapes stay whole rather
than guessing which fields belong to a file. 

Shared bodies and patches contain null profile placeholders. Separate RFC 6901
references identify those placeholders; source JSON cannot impersonate an
internal sentinel. Reconstruction checks all references before substituting
anything, verifies body hashes, rejects overlapping/unused references, and
enforces a 4 MiB bound on stored and expanded capture data. Metadata projections
are bounded at 256 KiB, depth at 64, and profile references at 1,024 per capture.
Oversized historical records require an explicit rejection/report or a versioned larger-object design, never silent truncation.

The native `json-v1` hash representation uses sorted map keys, UTF-8 without HTML
escaping (except U+2028/U+2029), and exact numeric tokens. Duplicate keys, invalid
UTF-8, and unpaired surrogate escapes are rejected. It is not RFC 8785 and is
independent of producer event-byte digests. Profile hashes include their
namespace and a versioned domain separator. Revisions and capture signatures
use ordered JSON tuples with separate domains. The 26 synthetic reference
fixtures in `pkg/archive/testdata/source-retention-v1.json` cover the existing
Python policy; native tests additionally cover malformed IDs and reference
integrity. These files contain no library data.

Capture writes are transactional. Replaying an identical capture UUID returns
the original record, including after restart; different contents under that
UUID fail. Reads verify decompressed length, payload checksums, profile hashes,
and revision/capture signatures. This also detects lost profile references.
Summary queries omit bodies and use bounded indexed keyset pagination. Retained
evidence is immutable; a forgotten-post tombstone rejects new captures and
resurrection. Purge/forget commands, account/capture associations, media appearances, source merging, and API/UI exposure remain subsequent work. Anonymised exports remove source evidence and vacuum free pages.

Schema 1000061 allows trusted imports to retain a missing observation time.
Known observations keep their exact `captured_at` value and existing signatures;
their new `recorded_at` column is null. Undated evidence has null `captured_at`
and a separate, required `recorded_at` identifying when the archive received it.
That time does not establish when the source was observed. Exactly one timestamp
is populated. Unknown observations use a distinct signature domain binding their
recording time; identical UUID replay cannot change either timestamp's meaning.
JSON represents the unknown observation as null. Capture summaries order by
observation time when known, otherwise recording time, with UUID as the indexed
tie-breaker; cursors retain the applicable timestamp.

Publisher matching sends undated evidence to review. A reviewed account link
can be saved without assigning invented first/last-observed times to its handles
or IDs. Existing explicit decisions remain selected. Network producer captures
continue requiring the worker's observation time. This schema change does not release collector checkpoints or start work. Migration retains all original capture UUIDs, rowids, signed
values and relationships. Backup retains both forms of evidence; anonymisation
removes them with the other source records.

Migration 1000007 extends the same archive identity registry to galleries.
Existing gallery IDs, memberships, covers, names, and filesystem associations
remain intact; each receives a portable UUID. The migration rebuilds the checked
registry in one transaction, preserving all prior UUIDs and references while
adding the typed `gallery_id` foreign key. Incoming ownership links and existing
redirects survive the rebuild. Foreign-key deferral is scoped to that transaction.

Gallery creation, rename, deletion, UUID adoption, and guarded redirects use the
same lifecycle as other archive entities. Changes to images/covers, linked
scenes, files, performers, tags, URLs, custom fields, and chapters advance the
gallery revision. Moving a relation between galleries invalidates both revisions.
A deleted gallery retains its identity tombstone, and a reused integer ID gets
a different UUID. Anonymised exports rekey gallery identities too.

This is a prerequisite for source-post albums, not automatic album creation.
Post-to-gallery associations, synchronization, manual membership decisions,
and the mixed-media album UI remain subsequent work under
the [album requirements](native-archive-transition-plan.md#source-post-albums-and-galleries).

Migration 1000008 stores source attachments independently of local downloads.
An attachment belongs to one source post and keeps a qualified, opaque source
identifier. Immutable `attachments-v1` manifests share ordered entries across
captures with the same list. Each capture names one manifest; replay with a
different list is rejected. Complete lists have contiguous zero-based positions
and a matching count. Partial lists may have gaps and an independently known
total; they cannot erase an older complete snapshot. Multiple source positions
may refer to the same attachment, but a position itself is unique.
An explicit album flag or multiple declared/observed entries identifies an
album, independent of how many files have downloaded. Lists are bounded to
4,096 entries and 4 MiB, with source positions and expected counts up to one
million. Oversized lists are rejected, never silently truncated.

`source_media_evidence` records candidate scene/image and optional file UUIDs.
Composite foreign keys prove that the cited capture actually contains the
attachment. Typed identity checks reject performer/gallery targets as media.
Observed-file and verified-bytes evidence additionally require a current
scene/image-to-file relationship. This trusted repository boundary records
proof supplied by core ingestion; it is not permission for an HTTP producer to
assert verification. The ingestion service still needs to validate the root,
path, and bytes. Legacy/review evidence can preserve historical associations
without making them automatic matches.

Migration 1000036 extends this same evidence table with an explicit post UUID.
Legacy/review evidence may omit the capture, attachment, or both when those
relationships are unknown. Supplying both still requires actual membership in
that capture's manifest. Post, capture and attachment scope is enforced by
composite foreign keys; observed-file/verified-bytes evidence still requires
all three identities and a file. A filename or download number cannot fill an
unknown source position. `PostMediaEvidence` provides bounded, indexed retrieval
including associations without attachment evidence. Such associations neither
select attachment media nor create galleries. They can connect several posts
to an existing scene/image without duplicating that library entity.

The migration preserves existing evidence UUIDs, details, timestamps and capture
membership. Nullable scope fields remain immutable; newly established provenance
requires new evidence. Insertion advances the post and, when present, attachment
revision in the same SQL statement. Retirement prevents new evidence while
historical replay and archive UUID adoption remain supported. Full native audits validate
the new scope, target kinds and required guards. 

Media choices are separate immutable decisions with a current head. They require
the reviewed attachment and target-media revisions. Ingestion may select only
one active candidate, supported by a current observed/verified file association,
while no explicit link or unlink is selected. Ambiguous evidence, deleted
candidates, or more than 1,000 evidence rows require review. Later captures do
not overwrite explicit choices. Merged UUIDs resolve to the surviving identity;
adoption cascades references, and deletion retains historical links to tombstones.
Evidence replay is idempotent across those identity changes, while changed
evidence content under an existing UUID is rejected. A future transport receipt
must separately validate exact producer event bytes.

The new repositories use bounded, indexed lookups and the caller's transaction.
Late insert/head failures roll back the evidence, choices, and revisions together.
Reads validate manifest counts and signatures; missing entries are corruption,
not an empty album. Anonymised copies remove manifests and association history
before rekeying identities. Gallery construction is added by migration 1000010.
Ingestion endpoints and album UI remain separate work; current source selection
is added by migration 1000009.

Migration 1000009 adds audited post attachment selections. An automatic selection
combines compatible partial manifests by source position, preserving known
counts, album declarations, and media hints. Unknown hints can become known;
different attachment IDs, conflicting known types/counts, or positions outside a
known count require review. Capture timestamps never authorize silently removing
or reordering items. For example, positions 0 and 2 from one capture can combine
with position 1 from another. Even when all three positions are known, the list
is marked source-complete only when a contributing capture explicitly observed
a complete list. This remains independent of local download completeness.

Selections retain only the immutable manifest references needed to establish the
combined result. Redundant source lists are omitted from the new selection,
while their original captures and previous decisions remain intact. Header and
entry loading uses two bounded, indexed queries for the selected post's lists;
it does not scan all posts or issue a query per capture. At most 4,099 supporting
lists and 16,384 input entries are processed, and the combined list retains the
4,096-entry/4-MiB bounds. Exceeding a bound fails explicitly and leaves the prior
choice intact. Review can select one complete source list directly.

`PreviewSelection` is read-only and returns the current source-post revision,
conflict kinds/positions, proposed entries, and whether an existing choice is
protected. Automatic decisions require that revision and cannot replace a
`pinned` or `disabled` choice. Review can pin an exact capture's manifest, disable
automatic source selection, or choose a capture as the new automatic starting
point. None of these actions deletes captures, media, or galleries. Equivalent
automatic replay keeps the previous decision and revision.

Automatic migration decisions use the same combination and protection rules as ingestion, with `origin:migration` retained in the audit.

`post_attachment_decisions`, their manifest-reference rows, and the current head
commit together. Foreign keys scope every reference to its source post, history
is immutable, and a head cannot move backward. Counts and a selection signature
detect missing/changed evidence on current-selection reads. New selections are
blocked for forgotten posts. Anonymised exports remove the new history and
references. Source-to-gallery synchronization and durable membership intent are
added below. Native review APIs are added in schema 1000085; their UI and general
field-decision coverage remain separate transition work.

Migration 1000085 adds `attachment_selection_reviews`, binding one immutable
request to its original post attachment decision. Its composite foreign key and
insertion guard require the same post, current decision/revision, review origin,
mode, capture and reason. Apply checks an existing receipt before current guards,
then recomputes the preview and records the decision and receipt in one managed
transaction. A precommit guard rejects a partially completed operation even when
a caller catches a late error. The shared read-only selection builder also serves
ordinary ingestion; its compatible-list merge and protected/no-op behavior stay
unchanged.

Receipts preserve original input across subsequent choices. Full native audits verify
canonical request bytes, request/decision signatures, the original preceding
decision, and capture-to-manifest scope. Anonymisation removes receipts before
source evidence. The migration adds no choices and rewrites no existing media,
source evidence or gallery memberships. Native SQLite backup/export retains the
new table. See [source-list review APIs](native-ingestion.md#reviewing-source-attachment-lists)
for the explicit separation between selecting source order and applying gallery
membership changes.

Migration 1000010 distinguishes manual, filesystem, and source origins on
galleries. Existing rows are classified from their folder/file associations
without changing their UUIDs, revisions, metadata, or memberships. Origin is
immutable; source-created galleries cannot acquire a folder or ZIP. Title-based
manual-gallery lookup excludes source albums.

`post_gallery_decisions` and `post_gallery_links` record explicit post-to-gallery
associations and disable decisions. One current post owns a gallery association.
Source creation cites the selected attachment decision; reviewed adoption
requires the current post and gallery revisions and a pathless manual/source
target, with no file associations even when no primary file is selected. Titles,
performer names, and folders never imply adoption. Disabling or changing an
association leaves the former gallery, its memberships, and its files intact.
A deleted gallery suppresses recreation; a redirected gallery requires review.
UUID adoption cascades into the association and its history.

Migration 1000086 adds `gallery_association_reviews` and
`attachment_media_reviews`. Each immutable receipt binds a canonical request to
its original decision through scoped foreign keys and insertion guards. Preview
and Apply share the ordinary association validators. Revisions, preview digests
and managed-transaction completion guards prevent stale or partly committed
choices. Full native audits validate original request bytes, signatures, previous decisions
and their scopes. Existing successful requests survive later choices, deletion
and identity adoption without rewriting their original input. Anonymisation
removes receipts before source evidence; native backup/export retains them.
The migration creates no choices or gallery memberships and changes no files.

Gallery adoption now checks claims throughout the target's bounded reverse
identity group, including historical UUIDs after merges. The original post can
explicitly reassociate its own gallery; a different post cannot bypass a retained
claim by choosing the survivor UUID. Explicitly disabling the original claim
releases it. Attachment review respects post-wide rejection/conflict decisions,
while allowing converted media to have a different library type from its source
hint. See the [association review APIs](native-ingestion.md#reviewing-gallery-and-attachment-associations).

`SourceGallery.Preview` computes changes for the selected post using bounded,
indexed bulk lookups. It retains source order and repeated attachment slots,
while deduplicating actual scene/image memberships. Attachment choices resolve
media redirects; deleted entities, undecided/unlinked attachments, and explicit
membership exclusions remain distinct states. A `linked` entry identifies a
library entity, not proof that its file is locally available. Producer download
state and actual file availability remain separate ingestion work. Ordinary
single-media posts are ineligible; explicit albums and known multi-item lists
remain eligible with partial or absent local media.

`Sync` verifies the preview signature, including current post, gallery, and
selected media revisions, in the caller's write transaction. It creates at most
one source gallery and changes only the planned memberships. A fresh replay
does not create another gallery, advance revisions, or append duplicate membership
history. One scene/image can be shared by several posts without copying media.
An old preview becomes stale after creation; future transport receipts must
separately make delivery replay idempotent.

Membership insertions/removals are recorded in `gallery_membership_events`, with
`gallery_membership_heads` selecting current intent. Source events cite the
post and attachment selection. Ordinary gallery/image/scene edits are library
choices, including an explicit add of an already-present member. Tracking
continues for previously adopted manual galleries even after disabling the
association. Manual inclusions and exclusions take precedence across media
redirects. Source synchronization removes only obsolete memberships it owns;
pre-existing/manual additions and explicit image covers remain protected.
Source order is retained in the attachment selection; manual mixed-media order
and its UI are subsequent work.

An ephemeral `source_gallery_write_context` row identifies source writes within
one transaction. A pre-commit guard requires all such markers to be removed;
startup refuses a database containing a leftover marker. This distinguishes
source membership changes from library edits without requiring plugin hooks.
Failures roll back the gallery, identities, association, membership events, and
revisions together. History is immutable, heads only advance, and typed/scoped
foreign keys protect the links. History pages are bounded to 100 records;
membership previews reject more than 8,192 members or decision heads rather
than silently dropping any. Identity resolution follows at most 128 entries.

New source galleries initialize title, description, and a valid date from the
capture cited by the attachment selection. This initial implementation never
overwrites existing gallery metadata or assigns depicted performers from the
publisher. The scalar choice checkpoint below records the initial field
provenance. Automatic reevaluation, relationship policies, URLs, mixed-media
ordering controls, native API/UI, durable after-success delivery, and ingestion integration remain required. Anonymised copies remove source association
and membership history before rekeying identities.

Migration 1000011 extends `archive_entities` to tags, studios, and groups. These
relationship targets need portable references before field-decision history can
refer to them. Existing UUIDs, revisions, redirects, and incoming foreign keys
survive the registry rebuild. New identities use checked typed foreign keys and
partial unique indexes, with the same deletion, ID-reuse, adoption, and redirect
rules as media identities. Tag merges now redirect former UUIDs to the survivor
within the relationship-transfer transaction.

Changes to a tag, studio, or group definition, including its own URLs, aliases,
remote IDs, custom fields, tags, and hierarchy as applicable, advance its
revision. Moving a relationship advances both affected definitions. Unrelated
media usage is not part of that revision: services reviewing usage transfers
must also validate the relevant associations. Full native audits verify the new lifecycle
and relationship guards. Anonymisation rekeys these identities and preserves
their redirect graphs. These identities supply stable relationship targets for
the later relationship-field decisions.

Migration 1000012 adds scalar metadata choices for scenes, images, and galleries.
The native field schema allows title, code, details, date, rating100, organized,
scene director/production_date, and image/gallery photographer. IDs, fingerprints,
playback counters, arbitrary source JSON, and unrelated settings are not targets.
Strings, dates with retained precision, nullable integer ratings from 0 to 100,
and booleans are checked by the repository. A value must fit within 4 MiB of
canonical JSON. Migration 1000013 extends these choices to collections and
relationships as described below.

`metadata_field_baselines` marks existing entities without copying their fields.
Both populated and empty legacy values remain protected; migration does not
invent user intent or allow a first scrape to fill a previously empty title.
Before schema 1000141, the first edit materialized the prior value as history.
Current writes retain only the newly selected choice. Newly
created empty fields can inherit; nonempty initial values of unknown origin are
conservatively protected. New source albums record their known initial title,
description, and date as inherited choices with capture references. An untitled
album's generic title is a policy value, not an invented source caption.

`metadata_field_decisions` retains immutable individual field values and their
origin, mode, capture reference, reason, and time. `metadata_field_heads` selects
the current choice through a scoped foreign key. Set and clear protect the field;
inherit explicitly permits automatic selection again. Filename fallback cannot
replace a nonempty selection from another origin. Normal column updates record
library choices even when a user reaffirms the same empty value. Timestamp-only
or playback-counter updates do not create metadata decisions.

The repository updates the browsing field and its selected choice in one transaction,
checks the entity revision, verifies the resulting value, and detects equivalent
replay. `metadata_field_write_context` distinguishes these writes from ordinary
library edits. An ignored late error leaves the marker and prevents commit;
startup refuses leftover markers. Decisions advance the archive revision,
history uses bounded indexed pagination, and UUID adoption cascades references.
Deleted entities retain history; reused integer IDs receive independent state.
Anonymised copies remove decision values and source references before rekeying.

Migration 1000013 adds performers, tags, studio, URLs, and custom fields for all
three media kinds, plus groups with scene indexes for scenes. Reference values
use typed archive UUIDs: studio is a UUID or null, performers/tags are UUID sets,
and groups are `{uuid, scene_index}` objects. A new nonempty choice supplies the
reviewed revision of every target. Missing, stale, redirected, deleted, or
wrong-kind targets require a fresh preview. Set membership has canonical UUID
ordering; source album ordering remains in the attachment manifest.

`metadata_field_references` stores historical targets through foreign keys to
the archive registry rather than unchecked IDs inside JSON. A reference decision
is built and sealed within the transaction; only a sealed decision can become a
field head. Counts, contiguous positions, scope, and immutability are checked.
UUID adoption cascades these references. Merge redirects remain intelligible in
history; a later integer-ID reuse cannot change the meaning of an old reference.

Ordinary relationship edits can require many join operations. Database triggers
retain the first previous value in `metadata_field_pending`, and the native
transaction manager records one final library choice per affected field before
commit. Target retirement captures the old UUIDs before the local foreign key
is nulled. Explicit empty sets, removing an absent value, and reaffirming an
already-present value also record intent through the shared relationship stores.
During the editing transaction, State reports the current value as pending and
protected; history excludes that unfinished choice. Failed flushes roll back the
transaction. Startup refuses pending writes or unsealed decisions. Anonymised
exports remove references and pending state with the other private history.

URLs retain order, discard exact duplicates, and require absolute HTTP(S) URLs
within 8192 bytes. Custom fields accept native primitive string/number/boolean
values; SQLite stores booleans as 0/1. Nulls and nested values are rejected, and
omitting a key from the replacement object removes it. Integers must fit signed
64-bit storage; larger exact identifiers should be strings. Floating-point
values retain the native double precision on replay. Collection choices accept
at most 4096 items and 4 MiB of canonical JSON. These limits do not rewrite
existing fields during migration. UUID, owner, reverse-target, and history
indexes bound lookups to the relevant entities, including a new scene-to-group
index. Existing scalar decisions, entity revisions, and the last issued history
sequence survive migration intact, including when later rows were removed.

These are core repository operations. Source authorization, candidate/policy
selection, full creation-intent conversion, review API/UI,
and durable after-success delivery still belong to the subsequent domain/API
work. No new producer-facing endpoint exposes the repositories directly.

Migration 1000014 adds reviewed consolidation of duplicate source-account
records within one qualified service namespace. Accounts on different services,
including native and mirror namespaces, remain independent records that can
share a performer owner. A matching name does not consolidate accounts.

`source_account_consolidations` retains the source/destination UUIDs, committed
revisions, chosen ownership decision, review signature, request digest, origin,
reason, and time. Original account rows, identifiers, evidence, and ownership
history remain in place. Current ownership and identifier lookup follow the
canonical account; ownership history remains scoped to its original account.
Consolidation history exposes the events touching the requested record, including
the retained intermediate UUIDs in a sequence of consolidations.

Canonical account UUIDs are indexed on both accounts and identifiers. A composite
foreign key ties each identifier's original account and canonical account to the
account row, and consolidation updates cascade atomically. This supports indexed
candidate pagination and direct resolution without walking redirect chains.
Original evidence ownership does not change. Additional identifier evidence on
an old account also advances the canonical account's review revision.

Preview is limited to the two account components, at most 4096 account records
and 8192 identifiers. Its signature covers membership, identifiers, ownership,
and the current revisions of referenced performers. Compatible existing choices
can be preserved, including explicit unlinks. Contradictory ownership requires
an explicit resulting choice. Conflicting stable IDs, including TikTok `secUid`
values and mirror user IDs, require acknowledgement and remain retained evidence.
These operations do not assign depicted performers
to media. A performer merge or new identity evidence invalidates a stale preview.

An optional consolidation UUID supplies replay identity: exact repeated input
returns the existing event, and changed input is rejected. Failed writes leave
transaction context that prevents an accidental partial commit. Startup checks
for unfinished contexts; full audits also verify agreement between canonical
indexes and consolidation history.
Identifiers and evidence claims cannot be rewritten; repeated observations may
extend their time intervals. Anonymised exports remove the new history and
context with the other account evidence. Migration begins with every existing
account as its own canonical identity and invents no consolidations.

Application preview/apply routes and native account review controls now expose
explicit consolidation. A read-only receipt-check POST verifies the entire saved
request against the existing event's request digest; it does not require another
receipt table. The original domain input serialization remains unchanged so
retained requests are replayable. Receipt lookup precedes current-state checks,
including after subsequent consolidation and performer identity changes. The
separate ownership-review API and UI manage a performer association without
implicitly consolidating accounts or equating identifiers. See the
[consolidation review contract](native-ingestion.md#account-consolidation-review-api).

Migration 1000015 adds logical `media_roots` and `source_collections`, each with
an immutable definition history and a current revision. Root UUIDs survive mount
changes; a nullable server binding records the local absolute path and directory
identity. Collections describe account targets, feeds, subreddits, searches,
manual batches, directories, legacy catalogs, or other collections. They may
reference a qualified account and a root-relative directory prefix. These
relationships do not establish depicted performers, gallery membership, or
permission to ingest files.

Updates compare the reviewed revision and retain origin/reason. Repeating an
unchanged definition at its current revision is a no-op. Disabled definitions
can be reactivated; retirement is permanent. The root and collection creation
transactions must publish revision one before commit: deferred foreign keys
prevent an ignored late error from committing an incomplete identity. SQL guards
require consecutive immutable revisions; full audits reject missing definitions.
A collection account foreign key includes its service namespace. Consolidated
account references retain their original identity and resolve through the
account service when current ownership is needed.

Target URLs are optional, exact HTTP(S) evidence without embedded credentials,
not unique identities. Indexed lookup searches historical targets and returns
current definitions, including disabled/retired definitions and multiple
collections using the same URL. Matching a URL does not merge collections or
permit automatically restarting one. Current lists and history have bounded
keyset pagination.

`source_collection_captures` pins each capture to the collection revision used
for it. The same capture can belong to several collections or historical
revisions; pagination includes both capture UUID and revision. Direct media
intake instead uses `source_collection_media_intake`: a stable event UUID, pinned
collection revision, typed scene/image UUID, origin, and reason. It needs no
invented source post or account and makes no metadata changes. Exact replay
returns the retained event; a changed payload using that UUID is rejected.
Media references follow archive UUID adoption while the submitted UUID is
retained for original-event replay. Deletion tombstones preserve provenance.
These are evidence records, not editable collection membership or proof of a
finished download. Retired definitions may receive late historical evidence.

The filesystem helper probes an open root directory and compares that identity
when opening a file. It rejects an inactive/unbound root, a replaced mount,
noncanonical relative paths, `.part` names, escaping symlinks, and nonregular
files. Returned descriptors stay bound to the opened file through path renames.
Unix opens are nonblocking to prevent a replacement FIFO from hanging a worker.
New bindings and reactivation require a successful probe; disabling an offline
binding or changing its label does not require its mount to be available.
Portable imports can retain unbound logical roots for later deployment binding.

Producer authorization, pinned work scopes, final-file stability/hash/probe
checks, durable receipts, folder/batch metadata defaults, API/UI remain subsequent integration work. Producer capture intake is described
below and in [native ingestion](native-ingestion.md). The migration fabricates no collections or intake
history from existing paths. Anonymised exports remove root bindings, collection
definitions, and intake provenance with the other private source evidence.

Migration 1000016 adds `capture_publisher_decisions`, current
`capture_publisher_heads`, and references to the identifier evidence used by each
choice. Capture publishers are source accounts, independently of their performer
owners and media attribution. Decisions retain linked, unlinked, and undecided
states, origin, policy, reason, and replay identity. Original account UUIDs remain
historical associations; current reads follow account consolidation.

The publisher service verifies and reconstructs one capture, derives qualified
claims, and uses targeted identifier queries. Unique captured IDs can reuse an
account; unmatched IDs with no locator candidates can create one. Handle-only,
ambiguous, contradictory, and cross-namespace evidence remains reviewable.
Explicit review may select an account, create a distinct account for a reused
handle, unlink, or return to automatic selection. Existing explicit unlinks are
protected. No operation assigns depicted performers or changes ownership.
See [publisher decision semantics](native-source-identity.md#publisher-decisions).

The new `source_account_identifiers_canonical_kind` index begins with canonical
account UUID so checking one account's claimed ID kinds cannot scan an entire
service. Post-account queries constrain the capture range before joining current
choices. Preview signatures omit unrelated observation counters while retaining
relevant identity changes, current selections, and candidate ambiguity.

SQL guards publish consecutive immutable decisions and checked heads atomically.
Deferred foreign keys require a head for every decision. A durable write context
blocks partial commits after account allocation or evidence recording fails;
startup rejects unfinished writes, and full audits check publication consistency. Request digests
make exact replay return the original result, without restoring an old choice
over a newer one. Anonymised exports remove these private histories before
removing captures and accounts. Migration creates no publisher choices for
existing captures; ingestion must establish their evidence.

Migration 1000017 adds `ingest_producers`, `ingest_credentials`,
`ingest_credential_scopes`, and immutable `ingest_receipts`. Credentials store
verifiers rather than token secrets and grant collection/root pairs. Revocation
is permanent; rotation preserves producer identity. Receipt foreign keys require
the committing credential's scope, the recorded collection revision, and the
actual capture/post association. SQL guards also require collection capture
provenance and matching logical roots.

The native HTTP service commits source capture, automatic publisher resolution,
album manifest/selection, collection provenance, and receipt in one transaction.
Receipt lookup uses the producer/event primary key; exact replay returns the
original result and changed event bytes conflict. Historical collection
definitions allow delayed metadata delivery within explicitly granted scope.
Anonymised exports remove credentials and receipts before private source records.
Migration creates no credentials, receipts, or imported data.

The [protocol documentation](native-ingestion.md) lists current supported events
and limits. File intake and its durable worker are described below. Producer outboxes, external worker lease enforcement and native administration/review UI remain subsequent work.

Migration 1000018 adds `files.generation`, `media_contents`, and immutable
`file_content_versions`. These identities have separate purposes:

| Identity | Meaning |
| --- | --- |
| File UUID | One file record's lifetime; deletion retains a tombstone |
| File generation | Monotonic fence for observed location/byte changes |
| Content UUID | Shared server-verified SHA-256 bytes and their length |
| Scene/image UUID | The library item, its metadata, relationships, and history |

File generations advance on basename/folder/ZIP association, size, modification
time, and MD5/oshash changes. Folder path changes also invalidate their files.
Unchanged fingerprint rows are preserved during updates; captions, codec metadata,
and perceptual hashes do not advance the counter. Updates require the generation
read by the caller, so a delayed scanner or generation task cannot replace newer
file state. An explicit compare-and-swap advance handles new byte evidence that
size/mtime alone did not reveal. Counter values are fences, not a count of
downloads or content revisions.

Content verification records retain their file UUID/generation, reviewed root revision,
relative path, descriptor identity, precise modification time, and change token.
That snapshot describes the descriptor when it was verified. A metadata-only
change (removing another hard link, chmod, or an identical copy with a new
inode) changes ctime or identity but not the bytes: re-registration re-hashes
the held descriptor and keeps the generation's original verification, without
failing or advancing the generation. Changed bytes remain a content conflict.
Current-content reads require an active file at that exact generation. Old
verifications survive changes/deletion and canonical UUID adoption. Identical
verified bytes share one content UUID without merging file rows, scenes, images,
edits, or playback history. Content-location lookups start with the indexed
content UUID; history pagination uses file UUID and generation.

The server prepares one descriptor, then `PreparedMedia.RecordContent` records
the proof in the caller's managed transaction and rechecks the current root and
file before commit. A later generation change or closed/replaced descriptor
rolls back publication. Generations reflect changes observed by Stash; they do
not make filesystem writes atomic or eliminate descriptor revalidation.
[Native file intake](native-ingestion.md#durable-archive-work) connects durable
jobs and completion receipts to association/gallery publication; production
producer activation remains separate.

Migration assigns generation 1 to existing files and leaves both new content
tables empty. Existing fingerprints, UUIDs, and media rows remain intact; no
historical checksum is relabelled as newly verified SHA-256 evidence. Anonymised
exports remove these hashes and private path/descriptor histories before source
roots are removed. Startup requires the generation column, guards, and lookup
indexes.

Migration 1000019 adds durable archive jobs, stable submission acknowledgements,
and per-attempt history. Active equivalent work has one job; each submitting
request keeps its association after completion so retries cannot schedule a new
run accidentally. A unique running-resource index serializes shared destinations.
Claiming advances a persistent ownership fence; heartbeat/progress/result writes
require the unexpired owner and fence. Recovery is bounded and respects attempt
limits. Job revisions protect cancellation, and terminal jobs and finished
attempts are immutable.

An explicit `ClaimByID` checks the selected job revision and indexed readiness
without selecting or recovering unrelated work. It preserves retry delays,
attempt limits and shared-resource exclusion. Both queue and selected claims
prevent an incomplete attempt write from committing its running job head.
Domain authorization remains the calling service's responsibility within the
same transaction; this storage primitive grants no producer access.

`job.Durable.Publish` combines domain writes and the attempt outcome in one
managed transaction and rechecks expiry before commit. Repeat submissions can
promote priority but preserve an existing retry delay. Bounded indexed queue and
attempt reads avoid scanning media/catalog tables. The migration leaves all job
tables empty and preserves existing ingestion receipts and verified file history;
legacy journals require the separate importer. Anonymised exports remove jobs,
arguments, acknowledgements, and attempts. The [ingestion guide](native-ingestion.md)
describes the connected file worker and separate source-run coordinator. External
producer conversion remains unfinished.

Migration 1000040 extends the job kind constraint with `album.backfill` and adds
`archive_jobs_resource_history(kind,resource_key,id)` for bounded per-post
history. The jobs table is rebuilt under its original name, retaining every job,
submission, attempt, receipt reference and progress/result checkpoint. Existing
receipt guards still require `media.verify`; producers cannot submit album work.
Startup checks the required index and schema objects. Historical album
publication commits its gallery/media decisions and compact checkpoint together;
hooks then use the original publication UUID across automatic recovery or a new
explicit retry. Terminal history is retained. See the [album Apply API](native-ingestion.md#historical-source-album-backfill).

Migration 1000020 retains `file_path_fences`, a local removal history independent
of file UUID lifetimes. Deleting or moving a regular file increments the original
folder/basename key; moving a folder records its regular files' old paths. Normal
metadata updates and ZIP member paths do not add removals. Recreating a file
never resets the counter. No historical removals are invented during migration.
The folded lookup index uses the same collation as existing file lookups and
sums spelling variants so any new removal invalidates a captured path fence.

Ingestion captures this state before inspection and checks it during publication
and before commit. An ordinary scan may create a previously absent file while
inspection runs; publication can reuse it only when the path fence remains valid.
Stored and submitted path spellings must open the same verified filesystem
object. Active current-generation content lookups find at most two distinct
scene/image owners, enough to distinguish a unique association from an ambiguity
without scanning the library. Existing independent media identities are never
merged merely because their bytes match. Anonymised exports clear removal
history after path rewrites, including private paths captured by those rewrites.

The filesystem deletion journal currently derives its directory from the
database filename. Promotion in place retains that association, but a production
path change must first drain pending deletions or transfer the exact journal
with its database. Do not rename a live database and assume its pending file
operations moved with it.

Migration 1000021 extends immutable `ingest_receipts` to `file.completed`.
A file receipt requires one `media.verify` job and a non-null scoped root; its
post/capture pair is optional for manual media. A source capture receipt retains
its required post/capture pair and has no job. Capture provenance is required
whenever that pair is present. The job reference is unique, while producer/event
identity remains shared across event kinds. Existing source receipts keep their
values exactly and acquire a null job column.

Admission commits the accepted receipt and job in one transaction. Server-owned
arguments pin the collection revision, root-relative path, lifetime/generation,
removal counter, and optional acknowledged source attachment. The producer's
size/SHA-256 remain claims until the worker verifies them. Receipt replay precedes
current file/definition checks and returns the original acknowledgement within
the caller's permitted scope. Token rotation does not erase queued work.

Publication uses a durable checkpoint: verified file, media, provenance, source
choice, album changes and progress commit together. Restart resumes preview and
notification work using that checkpoint and re-verifies the file. Final success
requires current lease, scope, descriptor, generation and ownership checks.
Status distinguishes an earlier committed registration from a completed intake.
Anonymised exports delete receipts before jobs to respect the new foreign key;
startup checks require the receipt job column and lookup index. Migration creates
no jobs or invented file evidence for existing library rows.

Migration 1000022 adds `metadata_policies`, immutable `metadata_policy_revisions`,
and `metadata_decision_policies`. Policies pin the reviewed collection definition;
field decisions reference the exact policy revision with foreign keys. Root-path
and collection-directory indexes support targeted ordinary-scan matching.
Startup requires the policy tables, guards and indexes and rejects unfinished
heads. Anonymised exports remove policy provenance and definitions before their
referenced field decisions and collections. Existing library and source rows are
unchanged, and no policies or historical authorship are inferred. See the
[native policy contract](native-ingestion.md#native-metadata-policies).

Migration 1000023 adds `source_runs`, immutable `source_run_requests`, fenced
`source_run_attempts`, persistent target cooldowns and owner review history.
Coalescing preserves requested date windows separately from claimed and completed
ranges; failed attempts restore their range without bypassing backoff or permanent
deferral. Foreign keys retain producer, collection and root revision provenance.
Unique active-work, collection, target and destination indexes serialize ownership;
claim additionally excludes overlapping directory scopes. Scoped pagination and
expiry indexes bound operational lookups independently of library size. Full native audits
validate the schema and current attempt/lease agreement. Anonymisation removes
run state before its producer and source definitions. Migration invents no runs
or legacy journal completion. See the [run protocol](native-ingestion.md#source-run-coordination).

Migration 1000024 adds immutable `ingest_credential_roots` for explicitly issued
Stash API-token grants. Each selected logical root permits ingestion for its
registered collections, including later additions. Existing named collection
grants retain their original scope; migration creates no root grants.

The receipt table retains every value, including accepted file-job identities.
Its insert guard accepts either a matching collection/root grant or a matching
explicit root grant, while preserving collection revision, capture provenance
and job-kind constraints. A null receipt root cannot match a root grant.
Grant deletion guards retain permission evidence while receipts reference it;
token revocation leaves those records intact. Anonymisation removes receipts
before both kinds of grants. A source-target/root index supports bounded current
collection lookup. Normal database backups include the grants and receipts.

Migration 1000025 adds `source_backfill_decisions` and its retained
`source_backfill_requests` proof links. Decisions belong to a logical media root
and a qualified source account, independently of performer ownership. Current
component definitions preserve the eight modes used by the existing Reddit and
Twitter n8n runner. A composite subject/component index bounds status lookups;
neither performer names nor a full-library scan participates.

Historical completion and skip imports use stable UUIDs derived from the input
database UUID, source table and original primary key. They retain the complete
original row, including the exact `result_json` string, user confirmations,
unknown provenance fields and timestamp spelling. A changed row under the same
identity requires review; it cannot overwrite earlier evidence. Imported
acceptance is distinct from verified native source-window coverage and does not
create runs, requests or file receipts.

Native decisions reference the authenticated producer's immutable request rows
with foreign keys. The request digest, root, exact target URL, policy and actual
completed ranges must cover every URL and the whole requested window. Proof
links and decisions commit together, and replay preserves the original receipt.
Startup requires the tables/indexes/guards; full audits also reject missing or
inconsistent proof links. Anonymised exports remove these private decisions before their
source-run and producer references. Ordinary database backups include them.

## Retained scan journal snapshots

Migration 1000026 adds `scan_journals` and `scan_journal_records`. A snapshot
has a stable UUID, the original database UUID, a logical media root, its captured
time, an exact document digest and a table inventory. The entire snapshot and
every row commit together. Reusing its UUID with a changed root, source or
document conflicts; another explicitly identified snapshot can preserve a later
state of the same mutable journal.

Records retain their original table/key, values, embedded JSON strings and a
deterministic UUID derived from the snapshot and key. Indexed summaries keep
target/context, retry state, legacy lower bounds, extractor positions, deferrals
and exact scope references available without reading all raw command evidence.
Original commands are retained only for recognized non-secret argument forms;
they are never executable server input. Unknown options require review of the
private source snapshot before import. Older extractor column variants remain
identifiable in their original evidence.

Dispositions are `pending_binding`, `historical` and `review`. These describe
migration evidence, not source-run state. A saved PID, boot identity or systemd
unit never becomes a native lease. Unbound/manual extractor scopes and handoffs
remain reviewable. Per-scan hashes remain opaque historical identifiers; they do
not prove a native window completed. Collection confirmations remain distinct
from account backfill decisions. Retention alone creates no source runs or
completion proof; the separate schema-27 activation receipt records a reviewed
operational binding.

Startup checks the tables/indexes/guards. Record-count reconciliation runs during
explicit full audits.
Snapshots and rows are immutable; anonymised exports remove this private history
before deleting source roots. Normal database backups include it.

Migrations run against copies during development. SQL failure leaves a dirty
migration state that startup refuses; restore the migration backup or use a
validated recovery procedure. Do not force a schema version to hide a failure.
For a historical promotion, retain the original compatible database until the
reviewed upgrade completes. Routine native updates do not repeat that cutover.

## Reviewed scan activation

Migration 1000027 adds `scan_journal_activations` and
`scan_journal_activation_jobs`. An immutable plan binds an exact snapshot target
to a native collection/root revision, worker policy, cutoff, consolidated window,
retry state and optional legacy checkpoint. Its receipt references the created
source run. Each original scan key is unique within its original database UUID,
so later snapshots cannot activate the same pending job again. SQL guards and
full-audit reconciliation protect the record-to-plan bindings; anonymised exports
remove them before deleting the retained journal and source runs.

Activation creates a queued or deferred source run without producer requests,
attempts or completion evidence. The first real lease can seed the reviewed old
cursor. Run responses expose a recovery policy derived from the immutable plan:
when coalescing expands its window, the worker must traverse archived items without
its normal archive stop rule. Exact-window retries preserve native checkpoints.
The original snapshot remains immutable and normal database backups include both
evidence and activation state. See the [application contract](native-ingestion.md#activating-retained-scan-requests).

## Source file evidence

Migration 1000037 adds four normal domain tables for file provenance:

| Table | Meaning |
| --- | --- |
| `source_content_claims` | A source's declared asset reference, optional digest/size and original timestamp. Multiple locations share one claim within a collection, including across collection definition revisions. |
| `source_file_observations` | The reported path, root revision, state, role, size, modification time and survivor path. ZIP observations identify the archive path separately from its member path. |
| `source_file_matches` | Evidence that an observation matched a particular library file generation, using an exact path, survivor path, verified content or explicit review. |
| `source_post_file_evidence` | A source post's association with a file observation, including files unavailable to the library. |

These immutable facts separate original source claims from current library
availability. A path-derived asset ID is not a checksum, and recording an
observation never creates playable media or verified content. Declared SHA-256
matches require existing server-verified content for the same file generation.
Exact/survivor matches retain the reviewed historical library mount prefix;
that prefix does not register or authorize a live filesystem mount.
Exact-path matches also reject disagreement with a reported size or modification
time, comparing timestamps at the library's persisted whole-second precision
while retaining the source's original nanoseconds. Survivor matches use the
shared asset's size, since a converted input can have different bytes and
timestamps from its output.

New matches require active file identities and current generations. ZIP matches
also require the current archive identity/generation and its actual member
relationship. Recorded evidence remains valid history after movement, byte
changes, UUID adoption or deletion. Post-file evidence advances the post's
review revision once and does not select attachments or assign performers.

Historical collection/root definitions scope every observation, so offline or
disabled roots can retain evidence without activating workers. Full native audits check
scope, target kinds and canonical relative paths before writes. Normal database
backups include these tables; anonymised exports remove their private evidence.

## Source file history

Schema 1000066 promotes catalog metadata edits, file-state changes and
deduplication assertions into `source_file_history`, with typed child tables
for locations, edits, states and deduplication. An event pins the original
collection/root revisions. Its source timestamp may be unknown; `observed_at`
records receipt of the assertion, and `created_at` records native insertion.
Signatures cover its provenance and complete child values. Partial event writes
cannot commit, even when a caller discards a late error. Full native audits validate the graph, signatures and receipts during migration and full snapshot verification.

Locations refer to source file observations, including unavailable files and
ZIP members. Deduplication points to a declared content claim, which need not be
a verified digest. A member absent from that catalog, or currently associated
with a different claim, keeps its declared path without an invented observation.
The retained stage and survivor describe the historical assertion; neither is
an instruction to mutate the filesystem. Copies in separate catalogs retain
their respective scopes and timestamps, even when their source event IDs agree.

Edit entries retain their source key, target field, representation and value.
Known fields map as follows:

| Source key | Native target | Representation |
| --- | --- | --- |
| title, details, director | same field | text |
| date | date | calendar date |
| urls | urls | URL strings |
| actors | performers | source names |
| tags, studio | same field | source names |
| movie | groups | source name |

Legacy `null` means `inherit`, not an explicit clear. Unknown fields and invalid
known representations retain their exact JSON values as `unmapped` extensions,
including large integers. Relationship names do not become native entity UUIDs.
An exact-path edit remains separate from edits to other files sharing the source
claim; importing history cannot replace it with a later duplicate-path value.
Resolving and applying alternatives belongs to explicit metadata review. The
existing Stash scene/image selections remain unchanged by this import.

Schema 1000142 retired the historical file-history import. These tables remain frozen evidence read by the metadata file-edit reviews below; nothing writes new rows.

Application-authenticated API routes:

- `GET /api/v3/archive/file-history/UUID` returns one complete event.
- `GET /api/v3/archive/file-observations/UUID/history?after=UUID&limit=N` returns
  event summaries for that observation.
- `GET /api/v3/archive/content-claims/UUID/history?after=UUID&limit=N` returns
  deduplication event summaries for that claim.

List limits are 1–100. There is no public history endpoint that deletes files or
applies an edit. Ordinary database backup retains this graph; anonymisation removes the source history, receipts and identifying values together.

### Reviewing a retained metadata edit

Schema 1000067 adds `metadata_file_edit_reviews`. Each immutable receipt joins
one application request to its native field decision, source history entry,
source field and file match. The migration still leaves selected metadata unchanged.
The explicit review operation uses the normal typed field-decision writer and
commits the chosen value, provenance and retry receipt in one transaction.

The review starts at a selected scene/image's file associations and pages by `(history_uuid, match_uuid)`; it does not read the whole history. Edits
at an exact path and at a deduplicated predecessor remain separate alternatives.
The user selects the scene/image explicitly, including when several existing
entities share a file. Review does not merge those entities or propagate the
choice to other owners. Historical disabled roots need no live filesystem grant
to review already retained metadata.

Preview rechecks the original file-match basis, current file generation, ZIP
container generation and selected entity's ownership of the file. Its digest
also binds the entity revision, current field decision/value, proposed value,
name candidates and chosen relationship revisions. Apply repeats these checks
inside its write transaction. Unrelated matches, changed files, removed
associations, renamed/deleted relationship targets and stale previews cannot
silently apply a previously displayed choice.

Relationship names use bounded lookups. Performer matching includes every
canonical name and alias, without giving either priority. Missing/ambiguous names
block the whole relationship replacement until explicitly selected; candidate
overflow is visible. A reviewed selection may link a source name to a differently
named native entity. Several names selecting one UUID produce one relationship.
This choice changes depicted metadata only, not source-account ownership.
Unsupported source fields/representations remain inspectable and cannot be
selected as native targets.

For legacy `inherit`, review releases the override while retaining the current
selected value until a permitted native policy evaluates it. It neither clears
the value nor invents a source/filename fallback. The preview shows that exact
result. Ordinary source mappings continue to respect explicit set/clear choices.

Clients save a request UUID and exact apply body before sending it. A retry
recovers the original receipt even after later edits, UUID adoption or deletion
of the matched file; it does not rerun the old change. Reusing a request UUID
with a different body fails. Receipt signatures bind the request and decision;
full native audits also check source scope, normalized scalar choices and preserved UUID
redirects. A late failure cannot commit a field without its receipt, even when
the caller discards the error. Application notifications use the existing
after-commit contract and are not registered again for a committed retry.

Normal database backups retain the receipts; anonymisation removes them before
their private source history. Migration creates no reviews and changes no
selected fields. The [application API](native-ingestion.md#historical-metadata-review-api)
and scene/image review controls are implemented. The review controls use the
shared mobile section menu and retain uncertain Apply requests across browser
reloads.

Schema 1000094 adds `metadata_file_edit_keeps` for an explicit **Keep current
value** outcome. It retains the exact request, reviewed entity revision, current
field-decision identity, source field and file match. It creates no field
decision and changes neither the selected value nor its protection, provenance
or entity revision. Unsupported fields and unresolved relationship names may be
declined without choosing a replacement. Keeping a value emits no metadata
update notification.

Keep repeats the same preview digest, current ownership and file-generation
checks before commit. Its signed receipt remains replayable after later edits,
UUID adoption, merges and file deletion. Apply and Keep cannot share a request
UUID; triggers enforce that distinction across both receipt tables. A late
receipt failure prevents the surrounding transaction from committing. Full native audits
validate scope, signatures, redirects and cross-table request uniqueness.
Anonymisation removes Keep receipts before their private source history.

The shared review queue considers each retained source field separately. An
Apply or Keep receipt settles that field for the selected media and its retained
merged identities, while other fields remain pending. Later library edits do
not reopen a previously reviewed historical field. Historical evidence remains
available, including for a later explicit Apply with a new request identity.

## Retained source documents

Migration 1000041 adds native document evidence without creating files, media,
performers, or selected scene/image metadata. The six tables have distinct roles:

| Table | Meaning |
| --- | --- |
| `source_document_contents` | Exact original bytes, shared by SHA-256 and compressed when smaller. Empty and malformed documents are valid retained evidence. |
| `source_documents` | An immutable parser interpretation: original encoding, parser identifier, parse status, warnings and parsed JSON. Different interpretations can reference the same bytes. |
| `source_document_sources` | A document observed at a literal historical path in a collection revision, optionally associated with a post. Several paths/posts can share one document. |
| `source_document_head_claims` | Evidence that a source writer selected a particular document at that location and time. |
| `source_document_head_decisions` | Revisioned native selection history, including explicit unlinks. |
| `source_document_heads` | The current decision for each collection/path pair. |

Content is bounded to 16 MiB; parser warnings and results are each bounded to
4 MiB. JSON must be unambiguous, with valid Unicode and no duplicate object keys.
The document identity includes exact parser JSON text so retained bytes are not silently reserialized, reinterpret, or drop unknown legacy fields. Byte digests
and interpretation identities are checked when read and during full native audits.
Source timestamp spelling and precision are retained; an empty timestamp means
unknown. Recorded-at timestamps separately identify native receipt time.

Paths are evidence strings, including literal backslashes. These services never
open the historical path or resolve it against a current filesystem mount.
Documents without a post remain accessible by collection and path, including
historical folder defaults. A source path claim alone does not activate a
metadata policy or assign a performer.

Post/path/source lookups use bounded UUID pagination and indexed queries.
Selected-head history uses revision pagination. Foreign keys and SQL guards
check collection revisions, claim scope and contiguous decision history. New
post evidence cannot resurrect a forgotten post; exact prior evidence can be
read or replayed. New evidence advances the affected post's review revision.
Review choices and migration selections cannot be overwritten by later automatic
captures. Migration cannot replace any existing choice; a reviewer can change
it by supplying its current revision. Historical head claims remain separate
from the current native selection.

These records live inside the normal library database and its SQLite snapshots.
Anonymised exports remove all six tables' contents. 

### Document application API

The following routes are under `/api/v3/archive`, behind application
authentication. Producer tokens do not authorize these operations.

| Method and path | Result |
| --- | --- |
| `GET /documents/{document_uuid}` | One parser interpretation and content hash/size; no raw bytes. |
| `GET /documents/{document_uuid}/content` | Exact bytes as an attachment download, including empty or invalid documents. |
| `GET /document-sources/{source_uuid}` | One original path/post association. |
| `GET /document-claims/{claim_uuid}` | One historical selected-head assertion. |
| `GET /posts/{post_uuid}/documents` | That post's document associations, with `after` UUID and `limit`. |
| `GET /collections/{collection_uuid}/documents?path=...` | Associations at this literal path, with `after` UUID and `limit`. |
| `GET /collections/{collection_uuid}/document-head?path=...` | Current decision, or `null` if none exists. |
| `GET /collections/{collection_uuid}/document-head/claims?path=...` | Historical assertions, with `after` UUID and `limit`. |
| `GET /collections/{collection_uuid}/document-head/history?path=...` | Native decisions, with `after` revision and `limit`. |
| `PUT /collections/{collection_uuid}/document-head` | Select or explicitly unlink a document, requiring the current revision. |

List limits default to 50 and accept 1–100. Percent-encode the literal path in
query parameters; it is not a route segment. Association lists reference shared
document UUIDs without repeating parser results or bytes. Unknown resources
return 404; a known location with no selection returns `null`.

PUT accepts `relative_path`, `expected_revision` (zero for an undecided location),
`state` (`linked` or `unlinked`), `source_uuid`, optional `claim_uuid`, and `reason`.
A linked source must belong to that exact collection/path; a supplied claim must
refer to that source. Unlink requires empty or omitted source and claim UUIDs.
The server records origin `review`; callers cannot assert source observations
through this operation. A stale revision returns 409. After a lost response,
read the current decision/history before choosing whether another edit is needed.
Selecting document evidence does not apply its values to a scene or image.

## Retained source translations

Schema 1000043 shares translation results in `source_translations`. Identity
includes exact original text, output, source and target languages, and provider.
Unknown values remain null, distinct from empty strings. Original/output text
is valid UTF-8, bounded to 4 MiB each, with no whitespace or Unicode normalization.
The native original-text SHA-256 hashes its exact UTF-8 bytes; null original text
has no hash. Reading a result and full data validation verify its identity/hash.

`source_translation_evidence` separately retains each post association,
optional historical collection revision, provenance, source timestamp and
declared input hash/algorithm. Identical results can serve multiple posts and
catalogs without repeating their text. Historical hash assertions remain separate
from the native verified original-text hash. Source timestamp spelling and
precision survive; native receipt time is recorded separately. New evidence
advances the post's review revision once. Exact replay remains valid after a
post is forgotten, but new assertions cannot resurrect it.

Application-authenticated routes under `/api/v3/archive` are:

| Route | Result |
| --- | --- |
| `GET /translations/{translation_uuid}` | One shared result, including exact text and nullable language/provider facts. |
| `GET /translation-evidence/{evidence_uuid}` | One post/provenance assertion. |
| `GET /posts/{post_uuid}/translations` | Bounded evidence summaries referencing results without repeating text. |

Post queries accept `after` UUID, `limit` (default 50, range 1–100), optional
`original_sha256`, and exact `target_language`. Unknown language never matches
an English filter. These routes retain evidence; they do not select translations
or replace titles/details. Translation execution and provider state
have a separate migration step. Normal SQLite backups include these records;
anonymised exports clear private translation data.

## Translation requests, cache and targets

Schema 1000044 stores work independently of execution-job admission. A large
historical backlog can retain every target without filling the bounded execution
queue. `translation_requests` shares exact UTF-8 original text, target language
and versioned provider/preprocessing policy. Requests preserve whitespace,
Unicode and control characters; their original-text SHA-256 differs from the
historical JSON hash. Changing provider behavior requires a new policy identity.

`translation_cache` retains one immutable outcome per request:

- `translated` references a shared `source_translations` result.
- `unchanged` references the exact original text with a detected source language
  matching the requested target language. Missing language is insufficient.
- `no_text` records completion without inventing a translated string, provider
  or language.

Exact cache replay retains its first provenance and source timestamp. A changed
outcome conflicts before any new result is stored. The timestamp can be empty
when the source did not record it; native receipt time remains separate.

`translation_targets` associates a request with a post, title/caption field and
optional historical collection revision. Each target is `held`, `pending`,
`completed` or `review`, with priority 0–100 and a `not_before` deadline. Repeated
retention does not reset scheduling, release held work or reopen completion.
Explicit schedule edits require the current target revision; every change is
retained in `translation_target_history`.

Publishing a due pending target atomically records the cache reference and
post provenance. Multiple targets share the result but keep their own evidence;
no-text completion creates no translation evidence. A forgotten post retains
`review` with `post_forgotten` instead of receiving new assertions. Publication
does not select or overwrite entity fields. Scheduled callers must additionally
fence the transaction through their durable job lease.

Schema 1000120 adds `translation_policy_defaults` for optional namespace defaults
and `source_post_translation_display` for a post's selected display language and
bounded propagation cursor. Translation publication wakes an already selected
display in the same transaction. The worker then updates matching inherited text
in short transactions; a restart resumes the cursor. These records do not copy
captions or retain caption versions. Original metadata and shared translated
results remain separate. Source albums also use the selected translation at
creation, and their derived alternate titles are deduplicated when translated.
See [automatic translations and explicit text backfill](native-ingestion.md#automatic-source-translations).

Application-authenticated inspection under `/api/v3/archive` is available at:

| Route | Result |
| --- | --- |
| `GET /translation-requests/{request_uuid}` | One request, including its exact original text and policy. |
| `GET /translation-requests/{request_uuid}/cache` | Its cached outcome/result reference, or `null` while uncached. A missing request returns 404. |
| `GET /translation-requests/{request_uuid}/targets` | Bounded targets referencing this request. |
| `GET /posts/{post_uuid}/translation-targets` | Bounded targets for this post. |
| `GET /translation-targets/{target_uuid}` | One target and its current schedule/completion. |
| `GET /translation-targets/{target_uuid}/history` | Bounded scheduling and publication history. |

Target lists accept `state`, UUID `after` and `limit` (default 50, range 1–100).
History uses a revision `after` cursor and the same limits. Summaries reference
shared requests/results without repeating their text. All these records survive
ordinary SQLite backup/restore and are removed from anonymised exports.

### Translation execution

Schema 1000045 adds the durable `text.translate` job kind and immutable
`translation_job_targets` bindings. Each job freezes one request and at most
50 target UUID/revision pairs; its arguments reference shared text rather than
copying it. Admission and all bindings commit together. The database also
rejects a submitted translation job whose complete bindings were not committed.

Admission selects due pending work by priority and keeps at most 64 active
translation jobs by default, within the overall durable-job ceiling. One active
batch per request protects cache generation and retry delays across new targets.
Request-specific target selection, target bindings and active-request exclusion
use indexed lookups. Later targets wait for another bounded batch; completed
batches share their cached result.

The worker renews its lease during provider work, checkpoints a valid result in
a fenced transaction, then atomically publishes its still-current targets and
job outcome. A later publication failure reuses that cache after restart.
Changed or held target revisions are not published by the old worker. Forgotten
posts retain `review/post_forgotten`, even without a cache, and a batch containing
only forgotten/superseded targets makes no provider request. These operations
retain translation evidence; they do not change selected scene/image fields.

Failures retain machine-readable codes without raw stderr or original text.
Transient failures retry after five minutes, doubling up to 24 hours, with ten
attempts per admitted batch. Cancellation and terminal provider failures hold
the exact still-pending target revisions owned by that job. Generic lease
recovery may leave a pending target bound to an exhausted job; it remains
excluded from automatic admission. Explicit retry releases a held target or
advances a target bound to a cancelled/failed job. It preserves priority and
`not_before`, and cannot retry a successful completion. Old jobs, bindings and
attempts remain inspectable.

The HTTP server owns an optional translate-shell/Bing worker. Configure
`translation_worker_enabled: true` and `translation_shell_path` (default `trans`)
only when provider execution should start, then restart the server. The default
is disabled, including on migration/rehearsal copies. An unavailable executable
leaves the worker stopped. The current provider requires a Unix host; inspection,
storage and migration are independent of that executable. Website credentials
remain in the existing scraping environments.

Native container images include Translate Shell and its runtime dependencies.
Installing the executable does not enable the worker; provider requests still
require the explicit configuration switch above. Standalone Unix installations
must provide `trans` themselves or configure its absolute path.

Policy `translate-shell-bing-text-v1` extracts text from recognized HTML, splits
it into chunks of at most 1,000 UTF-16 units and disables translate-shell init
files. Emoji outside the basic multilingual plane count twice; a leading
HTTP/file URL's protective space also counts toward the limit. The URL is passed
as literal text. The provider receives no shell-expanded
command. Each subprocess has a 30-second deadline and 1 MiB output ceiling; the
whole request has a five-minute deadline, at most 128 chunks and the existing
4 MiB result limit. Unsupported inputs are retained for review instead of being
truncated. Unix cancellation kills the process group, including descendants.
No-text work avoids the subprocess; confirmed target-language text preserves
the exact original, including HTML and whitespace, as `unchanged`.
When Bing rewrites a chunk before translating it, its response adds a
`translationSrcText` element naming the text it actually translated. It spells
out numerals ("3" becomes "three") and reads romanised words as Chinese ("Jin"
becomes 金, translated as "gold"). That output translates other text, so the
chunk keeps its readable source text and the result claims no source language.
An element that only echoes the chunk keeps Bing's translation.

Additional application-authenticated operations under `/api/v3/archive` are:

| Route | Result |
| --- | --- |
| `POST /posts/{post_uuid}/translation-targets` | Retain a request and a target together. Body: `request` with `original_text`, `target_language`, `policy`; `field` (`title` or `caption`); optional paired `collection_uuid`/`collection_revision`; explicit `schedule` with `state`, `priority`, `not_before`. Origin is application review. Replay preserves the existing target schedule. |
| `PUT /translation-targets/{target_uuid}/schedule` | Revision-checked `schedule` replacement using `expected_revision`. Zero/omitted `not_before` means server time. |
| `POST /translation-targets/{target_uuid}/retry` | Explicit release/retry using `expected_revision`, preserving the deadline. |
| `GET /translation-targets/{target_uuid}/job` | Binding for the current target revision, or optional historical `revision`; unbound revisions return `null`. |
| `POST /translation-jobs/admit` | Admit one bounded batch; body `{}`. Returns 202 with its job, or 200/null when no batch is admitted. Admission alone is not completion. |
| `GET /translation-jobs/{job_uuid}` | Durable job, frozen target references, checkpoint, state and revision. |
| `GET /translation-jobs/{job_uuid}/attempts` | Bounded attempts using fence `after` and `limit`. |
| `POST /translation-jobs/{job_uuid}/cancel` | Cancel using `expected_revision` and hold its unchanged pending targets atomically. |
| `GET /translation-requests/{request_uuid}/jobs` | Bounded execution history using sequence `after` and `limit`. |

Producer tokens cannot administer this work or choose executable paths.

## Automatic source translation policies

Schema 1000049 stores collection policies in `translation_policies` and immutable
`translation_policy_revisions`. Each revision binds a reviewed collection
revision, enabled state, provider/preprocessing policy, target language, title
and caption switches, and priority. Policy writes require the expected policy
and collection revisions. A missing policy disables automatic scheduling.

`capture_translation_decisions` records the first scheduling decision for an
exact collection/capture/revision tuple. `capture_translation_entries` records
each enabled field as `created`, `retained` or `no_text`, with foreign keys to
the exact target-history revision when present. Captures with equal text share
requests and cached outcomes; targets remain qualified by post, collection
revision and field. Existing holds, due times, priorities and outcomes survive
recapture. Original text remains exact, including whitespace.

The capture, decision, requests, targets and ingestion receipt commit together.
Provider execution occurs later through the existing bounded worker. Retry of an
accepted capture returns its original receipt after later policy/target edits,
restart or post forgetting. No scene/image metadata is selected. Shared source
metadata is read directly; scheduling does not reconstruct the provider payload.

Application routes under `/api/v3/archive`:

| Route | Contract |
| --- | --- |
| `GET /collections/{collection_uuid}/translation-policy` | Current policy or null when never configured. |
| `PUT /collections/{collection_uuid}/translation-policy` | `expected_revision`, `expected_collection_revision`, `definition`, and optional `reason`; origin is application review. |
| `GET /collections/{collection_uuid}/translation-policy/history` | Immutable revisions after numeric `after`, at most 50 per page. |
| `GET /captures/{capture_uuid}/translation-decision` | Requires `collection_uuid` and `collection_revision`; returns the saved decision or 404. |

Producer tokens cannot configure these policies or inspect unscoped decisions.
The producer receives its decision in the capture receipt. Full native audits validate
policy definitions, capture/collection membership, exact source text and saved
target history independently of current policy and target states. Backup/restore
preserves these records; anonymisation removes them with the source evidence.
See [configuration and scheduling behavior](native-ingestion.md#automatic-source-translations).

## Post enrichment targets and completion

Schema 1000050 adds `enrichment_targets`, `enrichment_target_history`,
`enrichment_completions` and `enrichment_completion_captures`. A target binds
one existing post, one of its retained URL records, an exact collection revision
and the versioned `gallery-dl-metadata-v1` extraction policy. Its deterministic
UUID coalesces repeated requests without using a performer, account handle,
folder label or caption as identity. The first review/migration origin is retained.
Admission accepts supported direct post routes; it does not establish that a URL
uniquely identifies the intended post. The publishing worker must independently
verify extracted identity against that post.

Targets have revisioned `held`, `pending`, `review`, `excluded` and `completed`
states. Explicit rescheduling can move between the first four states; completed
targets are immutable. Priority is 0–100, `not_before` is a UTC deadline, and
`reason` is a machine code of at most 64 lowercase letters, digits or underscores,
starting with a letter. Review/exclusion require a reason. Repeated retention
returns the existing target, preserving its schedule, origin and completion.
It cannot release a hold, restart excluded work or replace a retry deadline.
An omitted scheduling deadline means server time, so clients preserving a delay
must send the previously read `not_before`.

New pending work requires an active post and the current, active collection
revision. Held/review/excluded work can retain an older or disabled binding.
An indexed, collection-scoped readiness query omits future deadlines, forgotten
posts, disabled roots and outdated/disabled collection revisions. It also omits
target revisions already bound to jobs and targets with other active jobs.
A later source edit does not silently redirect an existing target. Reviewing a
new binding retains a distinct target with its own history; the old record remains
inspectable.

Completion is an internal domain operation with an operation UUID, exact target
revision and 1–1,024 distinct capture UUIDs. Every capture must belong to the
target post and have retained provenance for that exact collection revision,
with `gallery-dl` or `gallery-dl-enrichment` origin. Legacy NFOs, an unrelated post,
or a capture from another collection/revision cannot certify completion. Existing
valid captures may be reused. Capture order is irrelevant; the immutable receipt
binds the normalized set and original target revision. A second operation cannot
replace a completed target, and exact replay returns the first receipt even after
later collection changes or a post tombstone.

The completion receipt, capture links, target transition and history commit in
one managed transaction. A deferred foreign key prevents a receipt without its
completed target; a failed late write also aborts a caller that swallows the
error. The invoking worker must fence that same transaction with its execution
lease and perform source identity validation. These records alone do not assert
that a worker ran, authorize downloads, change selected scene/image fields, or
certify that all linked media are locally available. Full native audits check
identities, scopes, histories, counts and receipt digests. Ordinary database
backup retains the records; anonymised copies remove them with source evidence.

Application-only routes under `/api/v3/archive` are:

| Route | Contract |
| --- | --- |
| `POST /posts/{post_uuid}/enrichment-targets` | Retain work with `url_uuid`, `collection_uuid`, `collection_revision`, `policy`, and an explicit `schedule`. Origin is application review; the body cannot supply a new URL or claim migration provenance. |
| `GET /posts/{post_uuid}/enrichment-targets` | UUID-keyset page with optional `state`, `after` and `limit` (1–100). |
| `GET /collections/{collection_uuid}/enrichment-targets` | The same bounded listing scoped to one collection. |
| `GET /enrichment-targets/{target_uuid}` | Current target, original binding, retained URL and optional completion UUID. |
| `GET /enrichment-targets/{target_uuid}/history` | Immutable schedule revisions after numeric `after`, using `limit` (1–100). |
| `PUT /enrichment-targets/{target_uuid}/schedule` | Replace scheduling fields using `expected_revision` and `schedule`; stale revisions or ineligible pending bindings return 409. A pending target that stays pending keeps the retry deadline of its queued, previously attempted job, so `not_before` may come back later than requested. |
| `POST /enrichment-targets/{target_uuid}/retry` | Using `expected_revision`, release a held target or retry pending work whose bound job failed or was cancelled. Preserve priority and the later target/job retry deadline. Other states return 409. |
| `GET /enrichment-completions/{completion_uuid}` | Original completion input and timestamp; 404 when no receipt committed. |

Producer tokens cannot inspect or administer these routes. Cross-origin
mutations are refused. There is no HTTP completion mutation. Retaining a pending target does not start execution; the scoped producer API and worker below perform that work.

## Enrichment jobs and checkpoint ownership

Schema 1000051 adds the `post.enrich` job kind, preserving existing jobs, attempts
and submissions. `enrichment_job_targets` binds one job to an exact target-history
revision. Its canonical arguments also pin the post, collection revision, logical
root, worker policy digest and extractor version. Mount paths and website
credentials are excluded. Collection resource keys serialize enrichment jobs
within a source. Schema 1000054 below adds shared coordination with downloads.

`enrichment_job_attempts` records the authenticated producer for each fenced
attempt. Submission and claim cannot commit without their corresponding bindings.
The internal ingestion coordinator authorizes against the job's recorded scope,
checks current target/source eligibility, and rechecks credentials and lease
ownership before commit. A producer can recover a lost claim response or rotate
its Stash token without transferring ownership. Another producer needs a new
attempt; knowing the old owner's UUID does not grant its lease.

Checkpoint storage consists of one current body per job in
`enrichment_checkpoints`, immutable small acknowledgements in
`enrichment_checkpoint_receipts`, and per-record hashes in
`enrichment_checkpoint_records`. Each record retains the producer and attempt
that first stored it, including after a different worker resumes the transcript.
Prior records and unresolved references cannot be rewritten. Expected-revision
checks protect concurrent updates. Receipt, new record references, body and job
progress commit atomically; job progress contains the receipt rather than another
full transcript.

Replaying the same checkpoint returns its original acknowledgement. The original
attempt can confirm that acknowledgement after expiry, cancellation or target
edits, provided its producer still has valid access. This read does not restore
ownership or replace the current body. New checkpoint writes require the current
producer-owned lease and unchanged, eligible target/source binding.

Admission permits at most 64 active enrichment jobs. Each job permits eight
attempts and 128 checkpoint revisions; transcript limits remain 32 MiB and 1,024
records. `enrichment_checkpoint_usage` enforces a 2 GiB total staging budget,
including bodies retained by failed, cancelled or older published jobs. Exhaustion
refuses new storage rather than discarding evidence. Verified publication now
releases completed staging as described below. Ordinary database backups retain these records;
anonymisation removes them. Full native audits validate bindings, provenance, record hashes,
checkpoint histories and exact storage accounting.

A real target schedule change cancels its active job in the same transaction.
Explicit retry creates a new target revision and preserves existing backoff;
repeated admission cannot restart an exhausted attempt. Automatic retry and lease
recovery impose exponential backoff starting at five minutes. Old checkpoint
evidence stays with its original job. Collection/root changes prevent old workers
from continuing. The server's bounded maintenance loop cancels stale jobs and
recovers expired ownership while retaining checkpoint evidence.

The scoped [producer API](native-ingestion.md#producer-enrichment-api) and durable
worker expose this coordinator. Checkpoint acceptance verifies the transcript and
execution ownership. Publication is the
separate operation below that verifies post identity and commits native captures.

## Verified enrichment publication

Schema 1000052 adds `enrichment_publications` and
`enrichment_published_records`. Foreign keys bind a publication to its exact
checkpoint, publishing attempt and target completion receipt. Every transcript
record maps to a native capture and retains its original observing producer
through the checkpoint records. A capture index supports provenance lookup
without scanning other jobs. Immutable source bytes and observation timestamps
determine capture UUIDs; equal observations by one producer within a job can
share a capture even when they occur as both post and parent-context records.

The internal coordinator accepts only a saved checkpoint revision and digest.
It refuses pending child lookups, and every reconstructed record must resolve
through an existing native post identifier to the exact target post. A URL,
caption or another returned post cannot authorize creating or changing that
identity. [Post adapters](native-ingestion.md) cover Reddit, Twitter, Bluesky,
TikTok, Instagram posts/reels, Patreon, Fansly and Kemono/Coomer, including retained
Imgur/Redgifs child context. Download activation still requires attachment and
source-window adapters; post identity support alone cannot enable it.
Unsupported external references stay in the retained checkpoint and their count
is exposed in the publication. Completion certifies extraction within the
supported policy, not successful resolution of every external link.

Metadata comes from the same source-specific fields as the download producer:
Bluesky text, TikTok description, Instagram post description/date, Patreon
publication date, and the existing content/selftext/title fields. Kemono/Coomer
require `published`; their `date` can be the mirror's import time. Original text
and observation times are preserved. A media host's caption does not replace its
enclosing post's caption. Existing native partitioning continues unchanged;
broader service-specific post/profile partitioning remains transition work.

Publication reuses the download capture transaction for source evidence,
collection provenance, publisher review/matching, attachment manifests and
translation scheduling. Captures, their record associations, target completion,
job result and attempt outcome commit together. This path creates no fictional
HTTP producer receipt and does not download files or select scene/image fields.
Original capture producers can differ from the worker publishing the result.
Credentials, the original lease deadline, and current source eligibility are
checked through commit; expiry or a source edit rolls back all effects.

An ordinary job success assertion cannot bypass publication. Lost completion
responses replay the original receipt for the publishing producer/attempt, even
after later source edits or lease expiry. They do not rerun normalization or
metadata scheduling. Full native audits check the job result, all associations and every
capture against the saved source evidence, including shared-body/profile hashes.
Migration preserves queued, running, failed and cancelled schema-51 work; a
schema-51 success assertion is refused because that schema had no verified
publication path. Backup retains publication/provenance; anonymisation removes it.

Schema 1000053 adds verified staging release below. The scoped producer routes and worker are implemented; remaining adapters still precede production activation.

## Completed enrichment staging release

Schema 1000053 adds `enrichment_checkpoint_releases`. The internal publication
coordinator verifies the saved transcript against every published native capture,
then releases its body in the same transaction as successful publication. Storage
accounting decreases only when that deletion commits. Failed, cancelled, running
and unpublished jobs keep their evidence and continue counting toward the limit.

Each release retains the original byte count, timestamp, unresolved references
(URL, parent record ordinal, depth and reason), and a versioned SHA-256 proof.
The proof binds immutable job arguments, publication and checkpoint receipts,
original record digests/producer associations, and native capture signatures.
Source payloads remain in the shared native capture/profile stores. Small original
acknowledgements, observation attribution and record-to-capture associations remain
unchanged. This releases intermediate encoding choices; it does not promise to
reconstruct the exact compact transcript after cleanup.

The migration creates the table without deleting any older staging. The scoped
internal `ReleaseCheckpoint` operation verifies older successful publications
before releasing them; it needs current access to the job's recorded source/root
scope, without claiming a new attempt or requiring an expired lease. Publication
and checkpoint acknowledgement replays continue returning their original receipts.
`CheckpointHead` returns null after release; `CheckpointRelease`, publication
status and published records describe the retained result and unresolved links.

Full native audits require either the original staging body or a valid release
for every publication. They recheck the proof, bounded native payloads/profile
references, post identifiers, collection provenance, receipt histories and storage
accounting during migration or explicit snapshot verification. A missing or altered release, reference,
capture association or payload is rejected. Ordinary database backups retain
releases and their native evidence; anonymisation removes both. Releasing staging
makes SQLite pages reusable; it does not vacuum the database or delete media.

## Shared source scheduling

Schema 1000054 adds `source_pacing`, `source_run_pacing`,
`enrichment_job_pacing` and `enrichment_attempt_pacing`. Immutable work bindings
identify the contacted service through the versioned `source_scope_v1` function.
Known domain aliases share a service; Coomer/Kemono use their mirror scope rather
than the creator's upstream service. Unknown extractors remain separate by host.
These scopes are independent of performer identity, account aliases and website
credentials. Changes to equivalence rules require an explicit migration/version.

Download and enrichment claims read shared cooldowns and ownership inside their
managed write transaction. Running enrichment excludes other claims for its services and collection. Pending child services are checked and reserved atomically with a
retry claim. The current producer can also reserve a supported newly discovered
child through the fenced worker API. Attempt reservations persist for history,
but only the current running fence holds the service. Failure, cancellation and
expiry release that ownership without deleting metadata checkpoints. Replaying
an existing reservation does not consume an attempt or rewrite its start time.

Eligible queued downloads initially precede enrichment, with the bounded
preference described below. Independent downloads still obey
the existing target, destination and collection locks. Typed failures impose
shared service cooldowns: at least one hour for rate limits/timeouts/extraction
failures and one day for authentication/challenges. Missing posts, denied accounts,
busy sources and local worker failures do not pause an entire service. Child
failures use the child's service. Longer delays win and old failure receipts
cannot extend the deadline when replayed.

Migration derives bindings from existing immutable source definitions and post
URLs while preserving jobs, attempts, checkpoint bytes and prior cooldowns.
Existing running checkpoints gain child reservations; original root reservations
are reconstructed for historical attempts. Full native audits check bindings and required
objects. Ordinary backups retain scheduling state; anonymisation removes it with
work history. 

Schema 1000055 adds `source_run_attempt_pacing` and
`source_run_attempt_failures` for download-side linked services. Each attempt
holds its root service. Before initializing an extractor, the download adapter
reserves that service through the current producer, owner and fence. The supported
linked services are Redgifs and Imgur; unrelated service reservations are rejected.
Requested but busy services persist with `reserved=0`, without holding them.
An exact-window retry checks all prior dependencies before touching the parent
and atomically reserves them with the new attempt. This includes expired attempts
whose worker never reported the blocked dependency. A widened or new traversal
discovers its own linked services. Terminal attempts retain their history without
holding any service.

Typed failure receipts bind the failing service to the matching terminal attempt,
error code and completion time. A network or authentication failure requires a
held service; `source_busy` may identify a requested service. Only that service
receives any applicable cooldown. Local storage/configuration failures and
individual media download failures retain their existing generic attempt errors.
No source URL, website response or access value enters these tables. The additive
migration binds historical attempts to their existing root scope and preserves
all previous rows and schema objects. Full native audits validate dependencies and failure
receipts; ordinary backups retain them and anonymisation removes them.

## Bounded source preference

Schema 1000056 adds `source_service_turns`, `source_enrichment_waiters` and
`source_enrichment_waiter_scopes`. Actual blocked enrichment claims register
interest for 90 seconds; polling refreshes that deadline without resetting the
original wait time. A queue with no requesting worker does not reserve services.
Interest is removed when the job revision changes, and expired interest is
ignored and pruned by subsequent registrations.

A waiting enrichment job becomes due after four download starts on any required
service, or two minutes of continuously live interest since the last enrichment
start. An eligible requester actively polling its own due turn can claim it;
an older hint belonging to another caller does not reserve the turn exclusively.
This lets persisted collection rotation make progress without waiting for an
absent requester. Concurrent claims still grant only one metadata owner. Required
services include checkpointed children. Current target/source definitions,
cooldowns and existing enrichment ownership must still allow the work. Held or
stale targets and cooling dependencies cannot reserve an unrelated parent.
Existing downloads drain before enrichment starts; further downloads wait for
the due turn. Starting enrichment resets the affected service counters and age
budget, preserving download preference between metadata turns. All admission and
counter updates commit with the actual attempt; replaying ownership or a held
reservation does not count another start.

Historical `post.enrich` attempts receive a fixed 180-second turn measured from
the current attempt's start. Heartbeats do not extend it. Once it ends, ready
downloads can claim the same service and metadata yields at its next HTTP
boundary without consuming its failure allowance. With no queued download it
can continue using idle capacity. Shared request pacing and provider cooldowns
remain authoritative during the turn, including for linked services.

Download attempts expose `turn_until`, five minutes after their recorded start.
The worker checks that server-derived deadline at source boundaries; heartbeats
cannot extend it. It finishes the current file and saves its checkpoint before
yielding. Replaying a saved cursor may itself exceed five minutes, so a resumed
worker may reach one new checkpoint before yielding. This is a cooperative
budget, not a hard execution timeout. Yield uses `retry` with the controlled
`source_turn_complete` code, keeps pending windows/progress and the normal target
cooldown, and does not consume the failure/backoff budget.

Migration starts a new fairness epoch with zero counters; it does not invent
historical scheduling interest or rewrite existing work. Full native audits validate
service coverage, current queued-job bindings and the complete required-service
set. Ordinary backups retain this state; anonymisation removes it. Producer
outbox schema 10 adds rotation across local download/metadata profiles and
permitted collections without changing the native database schema. 

## Background metadata (schema 1000143)

Metadata turns take whole-service time from downloads, while a running download
often leaves its shared request slots unused, for example while it fetches
media files. Schema 1000143 adds `enrichment_background_attempts`, which marks
current `post.enrich` attempts that use that spare request capacity instead.

A worker opts in by claiming with `background: true` when capabilities report
`enrichment_background_protocol: 1`; it should run only
metadata work, so no download it would otherwise start waits for it. Such a
claim never registers turn interest or changes turn counters. It takes an
ordinary metadata turn only when one is available without waiting. Otherwise it
starts a background attempt only beside a download already running on the
service, with no other metadata job on that service, no running download of the
same collection, no cooldown on the service or its retained child services,
and no collection cooldown.

Downloads and turn scheduling ignore background attempts, except that metadata
for the same collection stays exclusive. Each background request still uses the
shared request spacing, Retry-After budget and backoff probes. It also waits
(`wait_ms` 5000) while a download request is waiting for that service, or while
an ordinary metadata attempt holds it. Once the attempt has been running for a
metadata turn length (180 seconds) and still has to wait, it receives a zero
wait and yields with `budget_yield`, keeping its checkpoint and failure count.
The marker records only the job and fence; it is immutable, requires the
current running attempt, and is removed by anonymisation. Full native audits
check that every marker belongs to a metadata attempt.

## Retained enrichment receipts

Schema 1000057 adds `source_enrichment_receipts`, the ledger of historical
enrichment completions. A receipt keeps its source version, exact completion
timestamp, enriched attachment-link count and unresolved-child count. It binds the
mapped native post to the collection's historical revision 1, even if that
collection is later edited or retired. The `catalog-enrichment-v1` policy marks a
legacy worker assertion. It does not imply complete child coverage, identify
particular captures or prove execution by a native worker. Existing completions
that cite a receipt keep that reference. Nothing writes new receipts. The
enrichment import pass and its receipt routes were retired in schema 1000142.

## Reviewed post-to-media associations

Schema 1000081 adds `post_media_decisions`, `post_media_links`,
`post_media_supersessions` and `metadata_decision_post_media`. Immutable review
decisions and replacement edges preserve explicit post links and unlinks without
requiring attachment identity. Current heads are indexed by post and media;
bounded reverse redirects expose conflicting choices after media merges. A fresh
review replaces all reviewed heads in that merged group, retaining each original
decision and the replacement relation. The request UUID and digest provide exact
response-loss replay, including after restart or retirement.

New decisions require the current post revision, current active scene/image
revision and complete reviewed decision set. Capture selection validates that the
chosen decision still governs that post/media pair. Selected field history keeps
the direct association reference as well as capture and policy provenance. All
references have foreign keys, immutable history guards and full data validation;
UUID adoption cascades references without rewriting the recorded review.

The migration creates empty association tables and preserves existing attachment
choices and source evidence. It selects no historical links, adds no album and
activates no policy. Ordinary snapshots include these records; anonymisation
removes the association history and metadata references with other source data.
See [the review API](native-ingestion.md#reviewed-post-to-media-associations).

## Historical post-to-media matching receipts

Schema 1000082 adds `post_media_backfills`, `post_media_backfill_decisions` and
`post_media_decision_evidence`. A bounded matching request retains its UUID,
reviewed post revision, policy, preview signature, input digest and outcome
counts. Its selected migration decisions reference the original media evidence,
post-file evidence and file-match records through foreign keys. No source payload
or post body is copied into these records.

Receipts and proof references are immutable. SQL guards enforce post/decision
scope and the original appearance/file chain; full native audits also verify decision
counts, required proofs and all cross-record references. The shared service
checks current file/archive generations and ownership before selection and again
before commit, preserving explicit choices and merge conflicts. Exact request
replay returns the original outcome without rerunning matching. Anonymisation
removes these dependent references before their source records.

The migration creates empty tables and schedules no matching work. Native SQLite
snapshots include the records with the other archive data. See
[historical matching](native-ingestion.md#historical-post-to-media-matching) for
the application API and saved-plan client.

Schema 1000091 updates only the file-proof scope trigger. A new post/media
matching decision can use an original member's evidence when both posts resolve
to the same current post identity. The evidence, appearance and file match must
still form their original chain. Full native audits validate that distinction, including
after another merge or portable restore. Historical schema versions retain
their original same-post validation before migration.

No tables, columns or existing rows change. Original requests, decisions and
proof references keep their owners and remain exactly replayable. The migration
does not merge posts, select media or start jobs; new requests separately require
the current post UUID and a revalidated preview.

## Original observation context

Schema 1000063 adds `source_capture_contexts`. A new capture can embed an exact
older capture at `/_parent` or `/_reddit`; the relationship records both capture
UUIDs. The child and its bindings commit together. The signed capture includes
every path and parent UUID, so deleting or replacing a binding fails integrity
verification even if the replacement has identical metadata. Both captures must
belong to the same post. A known parent observation cannot occur after the child.
Existing capture rows, signatures, publisher decisions and identifier dates are
unchanged by migration; it creates no inferred relationships.

These captures use `source-retention-v1+capture-context-v1`: current reduction
rules validate the newly fetched fields, while each embedded parent must exactly
match its bound capture. This permits a saved older parent to retain its original
reduction policy without allowing fresh child data to bypass today's rules.
Capture reads expose the bindings as `Contexts`; normal captures omit the field.
Native backup preserves the relationships and anonymisation removes them.

Publisher review follows the bound context selected by the source extractor.
Its optional `Observation` identifies the original capture and its observation
time, including null when unrecorded. Identifier evidence uses that original
time, rather than the newer child's fetch time. An undated original remains a
review case; explicit linking does not fabricate first/last observed dates.
Feed-owner context does not replace the actual author of a social post.

## Attachment download reports

Migration 1000087 extends ingestion receipts with `attachment.download` and adds
`source_attachment_downloads`. It preserves all prior receipts and adds no report
for historical files. Each small immutable row binds a producer/event receipt to
an existing download attempt, source capture and attachment. An optional final
file event must belong to that exact attachment and capture, with matching run,
collection revision and root; it retains its separate verification job.

Unique indexes bound a transfer to one start and one terminal report. A durable
producer sequence orders transfers within the original attempt independently of
clock differences or delayed delivery. Foreign keys, insertion guards and a
managed-transaction check prevent a report or receipt committing alone. Full native audits
check retained scope and transfer consistency. Anonymisation removes reports
before receipts and source evidence; native backups and portable exports retain
them. Original report and receipt contents survive restart and relocated restore.

History uses an attachment/sequence index and pages at most 100 original reports.
Its derived transfer state respects any retained terminal report, even when the
start arrived later. An unfinished transfer is active only if its original
producer/owner/fence still holds a live server lease. Neither a completed download
report nor an old successful verification establishes current file availability.
The read does not mutate run state or synthesize missing producer reports.
See [the wire contract](native-ingestion.md#attachment-download-reports).

## Reviewed post merge receipts

Migration 1000092 preserves the existing `archive_jobs` rows, constraints,
indexes and transition guards while adding the `post.merge_notify` kind. The
`archive_jobs_work_history` index supports bounded newest-first lookup by kind
and immutable work key. Four authoritative tables retain each completed review:

| Table | Preserved state |
| --- | --- |
| `post_consolidation_reviews` | Exact canonical request/result JSON, digest, selected source/gallery decisions and original notification job |
| `post_consolidation_review_members` | Every original post's previous canonical UUID and revision |
| `post_consolidation_review_media` | Ordered post-media decisions created by this review |
| `post_consolidation_review_attachments` | Ordered attachment decisions created by this review |

Foreign keys and immutable/scope triggers bind these records to the original
identity event and reviewed decisions. Full native audits check canonical request/result
bytes, signatures, original members, decision ownership and original job scope;
corrupt receipts or missing authoritative tables are rejected. Anonymization
removes dependent receipt rows in dependency order. Portable exports and restored
native databases carry all four tables and unfinished delivery work.

The managed merge transaction publishes identity, source-list selection,
gallery and media choices, actual album effects, receipt and notification job
together. Required notification delivery begins after commit, retains the
original event identity across explicit retries, and cannot substitute a newly
created gallery that happens to reuse an old local integer ID. Historical
captures, revisions and successful producer/backfill receipts keep their
original ownership and recovery semantics.

## Captured reply relationships

Migration 1000105 adds `source_post_threads` for incoming Twitter conversation,
parent-post and numeric publisher IDs. One row references its supporting capture;
repeat captures reuse that row. A later capture may fill a missing reply-target
account ID, while contradictory identity evidence marks the relationship for
review without overwriting it. Conversation and author indexes support bounded
thread reads and gallery planning. Historical captures are not scanned.

The post-gallery index now permits multiple evidenced self-replies to share a
gallery. Database guards reject unrelated sharing. The gallery service retains
explicit per-post decisions, deduplicates physical membership, and respects
manual exclusions and deletion. Thread sync validates every participating plan
and commits all associations and membership changes in one transaction. It does
not merge post identities or silently combine independently created galleries.
Anonymization removes thread rows before their capture dependencies; native
backups include the new table and normal gallery records automatically.

## Retrievable share links (1000122)

`shares.token` retains new and regenerated public link keys directly, allowing
owner management to copy or preview the same URL without rotating it. Existing
`token_hash` values and guest sessions remain valid; older rows keep an empty
`token` until explicitly regenerated. The authenticated `MediaShare.url` is null
for these rows and inactive shares. Public media DTOs never expose the stored key.

Recipient previews use the ordinary link. The migration removes unused
`share_sessions.exchange_only` bootstraps before dropping that column and
`preview`; existing open sessions keep their original expiry and version.
Revocation removes the stored key and all guest sessions. Backups retain active
link secrets as well as their protected media/library data.

## Recipient share lookup (1000133)

`shares_token_hash` uniquely indexes the existing `shares.token_hash` column so
recipient URLs can identify a grant by its secret alone. The migration changes
no keys, internal IDs, membership, versions, visit history or guest sessions.
Old query/fragment links and hash-only grants remain valid; new management links
use `/share/<secret>/`. Browser media delivery retains ID-scoped guest cookies.

## Search and count read caches

Schema 1000131 adds a single transactional `library_search_state` token and
triggers covering every indexed text dependency. The disposable search database
records that token and the SQLite schema version with each completed index
transaction. Restart reuses a matching index; interrupted updates, external edits,
schema changes and missing cache tables require rebuilding. Reads use ordinary
SQL until an index matches their pinned snapshot. Divergent writes after a restore
produce different random tokens even when their write counts match. Search rows
remain outside the library; this migration does not rebuild library data.

Count cache entries record the tables SQLite reports while compiling the query.
Transactional temporary triggers collect changed tables; only dependent entries
expire. Rollbacks retain cached counts. Schema changes, untracked external writes
and tracking failures conservatively invalidate the cache. Relative-time and
nondeterministic queries remain uncached. Worker bookkeeping does not invalidate
unrelated library totals.
