# Native schema promotion

The independent schema begins at primary migration 1000000 and identifies itself
with `native_schema.lineage = org.notsafeforgit.stash.native-archive`. New
databases use this lineage. Existing compatible databases require explicit
migration; normal startup does not silently promote them.

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

Legacy filter files can still be converted at the import boundary while their
remaining callers are retired. They do not create live projection/shadow data.
The source/provenance services and catalog import remain separate work; the
implemented native migrations are described below.
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
removed. Startup refuses a native database with a missing canonical name.

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
references. A UUID already assigned elsewhere requires reconciliation. Actual
catalog import and native API/UI exposure remain separate work; creating this
registry has not imported or modified the live catalogs. Anonymised exports
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
and decisions. Source-account equivalence plans, native review APIs/UI, capture
references, and importing actual catalogs remain subsequent work.

Migration 1000006 adds source posts and retained evidence. A post has its own
UUID and qualified service identifiers; native and mirror identifiers cannot
collide. An existing identifier cannot silently move to another UUID. Additional
identifiers require the reviewed post revision. None of these records is
required for a directly scanned scene, image, file, or performer.

| Record | Meaning |
| --- | --- |
| `source_posts` / `source_post_identifiers` | The source post and its explicit identifiers, independent of library media |
| `source_post_revisions` | Shared post body and normalized title/text/date/language projection |
| `source_captures` | An immutable observation of a post revision, with capture time, producer, platform, extractor version, retention policy, and per-file/provenance patch |
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

`legacy-retained-v1` is reserved for trusted catalog import. It partitions old
retained evidence without applying new pruning retroactively. Network ingestion
must not expose this escape hatch. Unknown payload shapes stay whole rather
than guessing which fields belong to a file. The import and network ingestion
boundaries themselves are still subsequent work.

Shared bodies and patches contain null profile placeholders. Separate RFC 6901
references identify those placeholders; source JSON cannot impersonate an
internal sentinel. Reconstruction checks all references before substituting
anything, verifies body hashes, rejects overlapping/unused references, and
enforces a 4 MiB bound on stored and expanded capture data. Metadata projections
are bounded at 256 KiB, depth at 64, and profile references at 1,024 per capture.
Oversized historical records require an explicit import rejection/report or a
versioned larger-object design, never silent truncation. The all-catalog import
rehearsal must assess these limits before production cutover.

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
resurrection. Purge/forget commands, account/capture associations, media
appearances, source merging, API/UI exposure, and actual catalog import remain
subsequent work. Anonymised exports remove source evidence and vacuum free pages.

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
historical replay and archive UUID adoption remain supported. Startup validates
the new scope, target kinds and required guards. Catalog path/asset matching and
appearance import remain subsequent work.

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

Automatic migration decisions use the same combination and protection rules as
ingestion, with `origin:migration` retained in the audit. The
[catalog attachment importer](native-source-identity.md#mapping-catalog-attachment-lists)
uses these services to recover source lists before file/media association.

`post_attachment_decisions`, their manifest-reference rows, and the current head
commit together. Foreign keys scope every reference to its source post, history
is immutable, and a head cannot move backward. Counts and a selection signature
detect missing/changed evidence on current-selection reads. New selections are
blocked for forgotten posts. Anonymised exports remove the new history and
references. Source-to-gallery synchronization and durable membership intent are
added below; API/UI exposure and general field decisions remain required.

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
ordering controls, native API/UI, durable after-success delivery, and ingestion/catalog
import integration remain required. Anonymised copies remove source association
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
must also validate the relevant associations. Startup verifies the new lifecycle
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
The first edit materializes the prior field value as preserved history. Newly
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

The repository updates the browsing field and its history in one transaction,
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
the context and agreement between canonical indexes and consolidation history.
Identifiers and evidence claims cannot be rewritten; repeated observations may
extend their time intervals. Anonymised exports remove the new history and
context with the other account evidence. Migration begins with every existing
account as its own canonical identity and invents no consolidations.

Producer evidence matching, account-review API/UI, and catalog import remain
separate required integration work; no new public endpoint exposes these stores.

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
require consecutive immutable revisions and startup rejects missing definitions.
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
checks, durable receipts, folder/batch metadata defaults, API/UI, and catalog
import remain subsequent integration work. Producer capture intake is described
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
startup rejects unfinished writes and inconsistent publication. Request digests
make exact replay return the original result, without restoring an old choice
over a newer one. Anonymised exports remove these private histories before
removing captures and accounts. Migration creates no publisher choices for
existing captures; ingestion and catalog import must establish their evidence.

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
Migration creates no credentials, receipts, or imported catalog data.

The [protocol documentation](native-ingestion.md) lists current supported events
and limits. File intake and its durable worker are described below. Producer
outboxes, external worker lease enforcement, catalog import and native administration/review UI
remain subsequent work.

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
Startup checks the new index and supported kinds before writes. Historical album
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
expiry indexes bound operational lookups independently of library size. Startup
validates the schema and current attempt/lease agreement. Anonymisation removes
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
Startup requires the tables/indexes/guards and rejects missing or inconsistent
proof links. Anonymised exports remove these private decisions before their
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

Table/index/guard checks and record-count reconciliation run at startup.
Snapshots and rows are immutable; anonymised exports remove this private history
before deleting source roots. Normal database backups include it.

Migrations run against copies during development. SQL failure leaves a dirty
migration state that startup refuses; restore the migration backup or use a
validated recovery procedure. Do not force a schema version to hide a failure.
The compatible database and deployment stay pinned until the production cutover
and restore gates pass.

## Reviewed scan activation

Migration 1000027 adds `scan_journal_activations` and
`scan_journal_activation_jobs`. An immutable plan binds an exact snapshot target
to a native collection/root revision, worker policy, cutoff, consolidated window,
retry state and optional legacy checkpoint. Its receipt references the created
source run. Each original scan key is unique within its original database UUID,
so later snapshots cannot activate the same pending job again. SQL guards and
startup reconciliation protect the record-to-plan bindings; anonymised exports
remove them before deleting the retained journal and source runs.

Activation creates a queued or deferred source run without producer requests,
attempts or completion evidence. The first real lease can seed the reviewed old
cursor. Run responses expose a recovery policy derived from the immutable plan:
when coalescing expands its window, the worker must traverse archived items without
its normal archive stop rule. Exact-window retries preserve native checkpoints.
The original snapshot remains immutable and normal database backups include both
evidence and activation state. See the [application contract](native-ingestion.md#activating-retained-scan-requests).

## Reviewed registry imports

Migration 1000028 adds `catalog_identity_imports` and
`catalog_identity_import_records` for frozen performer identities, saved local
bindings, redirects, explicit account choices and older plugin evidence. Migration
1000029 adds `catalog_registry_imports`, `catalog_registry_import_records`,
`catalog_account_mappings` and `catalog_collection_mappings` for the complementary
account, catalog and routing families. Both phases retain immutable original rows
and guarded apply receipts; domain changes and receipts commit atomically.

The second import references the first, validates their complementary inventories,
and creates disabled logical collections without scrape targets or root bindings.
Original source-qualified keys remain migration references to native accounts and
collections. Captured IDs establish accounts, while uncertain locators remain
provisional or unresolved. Imported publishers do not assign depicted performers.
SQL guards and startup validation reconcile receipts, evidence counts and mappings.
Normal database backups include these records; anonymisation removes them before
source accounts and collections. See the [registry import contracts](native-source-identity.md#importing-the-performer-registry).

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
disabled roots can retain evidence without activating workers. Startup checks
scope, target kinds and canonical relative paths before writes. Normal database
backups include these tables; anonymised exports remove their private evidence.

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
The document identity includes exact parser JSON text so an import does not
silently reserialize, reinterpret, or drop unknown legacy fields. Byte digests
and interpretation identities are checked when read and during startup.
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
Anonymised exports remove all six tables' contents. The coordinated producer
backup, portable export and UI remain subsequent parts of the transition.
Applying this schema alone has not imported old NFO catalogs.

### Historical document import

Schema 1000042 adds `catalog_document_imports` and immutable per-row
`catalog_document_records`. The application-authorized
`stash-import-catalog-documents` command requires a received frozen snapshot and
a completed evidence pass. It processes normalized documents, their path
associations (or older flat sidecar rows), then explicit selected heads. Each
transaction processes at most 50 rows, with a 16 MiB batch threshold. It commits
domain facts and receipts together. Resume uses the **processed-record count**;
the source ordinal resets between dependency phases.

Original bytes and parser interpretations are shared across catalogs while
collection/path/post associations remain distinct. The imported association
refers to the registry-created collection revision 1, even if that collection
has since been renamed, bound to a root, or retired. The importer reads retained
snapshot data; it does not open media or NFO paths. Invalid native interpretations
and missing post mappings retain review receipts with the original staged values.

An explicit historical head takes precedence over the legacy reader's fallback.
For a path without a head, that fallback orders `captured_at` text descending,
then the content hash descending. Its claim is labeled `legacy_fallback`; an
invalid explicit head requires review instead of silently selecting a fallback.
Existing native selections and explicit unlinks are preserved, with differing
historical claims retained for review. Later manual changes do not rewrite
completed import receipts or their original decision references.

Under `/api/v3/archive/catalog-snapshots/{snapshot_uuid}/document-import`,
`GET` returns progress and `POST` advances with `expected_manifest_sha256` and
`after`. `GET /records` returns bounded summaries, using source-ordinal `after`
and a 1–100 `limit`; `GET /records/{ordinal}` includes one original source row.
Exit 0 means mapped, exit 2 means completed with review outcomes, and exit 1
means failure or an unavailable response. Completed passes still report
`imported:false`; other catalog families and final reconciliation remain required.
Startup verifies phase coverage, counts and native reference scope. Normal
SQLite backups retain these records; anonymised exports clear them.

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
has no hash. Reading a result and startup validation verify its identity/hash.

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

### Historical translation import

`stash-import-catalog-translations` requires the same received frozen snapshot
and completed evidence pass used by the other importers. It records immutable
row outcomes in `catalog_translation_records` with resumable progress in
`catalog_translation_imports`. Each transaction handles at most 50 records and
a 16 MiB batch threshold. The checkpoint is the last source ordinal; results,
provenance and receipts commit together. Collection scope stays at the original
registry-created revision 1 across later renames, root binding or retirement.

Legacy `input_hash` uses `catalog-json-sha256-v1`: SHA-256 of the original text's
historical Python JSON encoding, not its raw UTF-8 bytes. The importer classifies
it as `verified`, `missing`, `unverifiable` (no original text), or `mismatch`.
Mismatches retain both the result and original assertion for review. Invalid
provenance and missing/forgotten post mappings retain review receipts and any
valid result without creating invalid post evidence. No missing language or
provider is guessed, and importing does not start translation work or apply
scene/image metadata.

Under `/api/v3/archive/catalog-snapshots/{snapshot_uuid}/translation-import`,
`GET` returns progress and `POST` advances with `expected_manifest_sha256` and
`after`. `GET /records` returns bounded summaries with source-ordinal `after`
and a 1–100 `limit`; `GET /records/{ordinal}` includes one original row.
Completed passes remain `imported:false` until the entire migration is reconciled.
Startup validates receipts against their staged source, result, post and
collection references and recomputes historical input-hash classifications.

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

Policy `translate-shell-bing-text-v1` extracts text from recognized HTML, splits
it into 1,800-code-point chunks and disables translate-shell init files. A leading
HTTP/file URL is passed as literal text. The provider receives no shell-expanded
command. Each subprocess has a 30-second deadline and 1 MiB output ceiling; the
whole request has a five-minute deadline, at most 128 chunks and the existing
4 MiB result limit. Unsupported inputs are retained for review instead of being
truncated. Unix cancellation kills the process group, including descendants.
No-text work avoids the subprocess; confirmed target-language text preserves
the exact original, including HTML and whitespace, as `unchanged`.

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

Producer tokens cannot administer this work or choose executable paths. The
frozen automation importer below maps historical requests, results and targets.
Reviewed activation, automatic capture scheduling and native review UI remain
separate transition work. Adding execution does not activate production.

## Frozen automation snapshot receipt

Migration 1000046 retains a frozen automation database before native mapping or
activation. `automation_snapshots` binds an exact manifest and original database
checksum to an imported registry source. A source accepts one frozen snapshot
in the target database. Exact replay resumes it; different snapshot identities
or manifests cannot replace the retained source.

`automation_snapshot_tables` records progress and hash state for all ten source
families. `automation_snapshot_chunks` stores immutable chunk receipts, and
`automation_snapshot_records` retains each original line, source key and ordinal.
Chunk records, table hashes and the receiving checkpoint commit atomically.
Indexed chunk/table ranges support bounded verification and later domain mapping.
Startup verifies manifest identity, registry references, original bytes, ordering,
chunk receipts and table digests before accepting writes. Source schema SQL is
retained as evidence and never executed. Anonymisation removes these records.

Application-authenticated operations under `/api/v3/archive` are:

| Route | Result |
| --- | --- |
| `POST /automation-snapshots` | Begin or replay the exact JSON manifest. Requires `X-Stash-Manifest-SHA256`; maximum 8 MiB. |
| `PUT /automation-snapshots/{snapshot_uuid}/chunks/{chunk_index}` | Receive the next NDJSON chunk, or replay an already committed exact chunk. Requires the same manifest digest; maximum 16 MiB and 1,000 records. |
| `GET /automation-snapshots/{snapshot_uuid}` | Inspect the immutable input identity and committed progress. |

Encoded request bodies are rejected. Producer tokens cannot administer these
routes. The supported client is `stash-upload-automation`; see
[preparation and upload](../integrations/gallery-dl/README.md#frozen-automation-input).
Both interrupted and completed receipts survive ordinary database backups.

`receiving` and `received` describe transport only. Every family remains pending
and `imported` remains false, including for a valid empty snapshot. Uploading
creates no native translation, enrichment, discovery or maintenance work, and
does not change library metadata. Historical domain mapping and explicit
activation remain separate transition requirements.

## Frozen automation translation mapping

Migration 1000047 adds `automation_translation_imports` and immutable
`automation_translation_records`. Each mapping references its original staged
ordinal and, where proven, a shared request/cache, source-qualified post,
registry collection revision and immutable target revision. A partial source
index restricts bounded reads to the two translation families; unrelated
automation rows do not become translation work.

Requests use exact original text and the legacy English provider policy.
Cache mapping checks the original job hash and typed schedule/result values.
Invalid inputs and conflicting native cache choices receive explicit review
outcomes. The raw source preserves attempt counts, errors and English rewrites;
native unchanged results preserve original text. Provider capture time remains
unknown rather than borrowing a job update timestamp.

Unapplied targets enter `held`, retaining priority and a retry deadline rounded
up to native millisecond precision. Proven applied targets become completed with
migration evidence and no fabricated worker execution. Empty-text completion
has no translation evidence. Exact post aliases coalesce to one target, and a
later applied alias can complete an untouched hold created by the same import.
Existing native targets and later revisions are preserved. Known catalog evidence
imports must finish before resolving their post references; unresolved or
forgotten posts retain review receipts.

All domain writes and their receipts commit atomically in batches of at most
200 source rows or 16 MiB. Startup checks source scope, checkpoint counts,
deterministic request/cache identities, original schedules, historical revisions
and migration completion provenance. Ordinary backup/restore includes the source
and its mappings; anonymisation removes both before dependent native work.

Application-authenticated operations under
`/api/v3/archive/automation-snapshots/{snapshot_uuid}/translation-import` are:

| Route | Result |
| --- | --- |
| `GET` | Inspect the committed checkpoint and mapped/review counts. |
| `POST` | Advance using `expected_manifest_sha256` and exact `after` ordinal; maximum body 4 KiB. |
| `GET /records` | Bounded summaries with `after` and `limit` (1–100). |
| `GET /records/{ordinal}` | Mapping and exact original source values. |

Producer tokens cannot administer these routes. The supported command is
`stash-import-automation-translations`. `mapped` or `review` completes this
mapping pass only: `imported` remains false, and no active jobs, provider calls
or selected library-field changes are created. Other automation families and
final reconciliation remain transition work. Releasing imported holds is a
separate reviewed operation, described below.

## Reviewed translation activation

Schema 1000048 adds immutable `translation_activations` and
`translation_activation_targets`. Each activation identifies at most 100 exact
held target revisions. Preview records their request, post, optional collection
revision, field, priority and deadline in a hashed plan. Apply requires the same
input and reviewed hash; every target becomes pending in one transaction while
keeping its priority and deadline. A changed target or forgotten post rejects
the entire batch. Activation does not admit execution jobs or invoke a provider.

The caller chooses and durably saves the activation UUID before applying. Reuse
that UUID, exact target revisions and plan hash after interruption. The original
receipt remains valid even if a worker completes the targets or someone later
holds them again; replay never repeats the release. A different input or hash
cannot reuse an existing UUID. New scheduling choices require a new preview and
operation UUID. The existing worker admits due targets through its normal limits
and shared request cache.

Application-authenticated routes under `/api/v3/archive` are:

| Route | Contract |
| --- | --- |
| `POST /translation-activations/preview` | Input: `uuid`, `targets` containing `target_uuid` and `revision`, and an optional paired `snapshot_uuid`/`manifest_sha256`. Returns the normalized `input`, concrete `entries` and `plan_sha256`. |
| `POST /translation-activations` | Body: saved `input` and `expected_plan_sha256`. Returns the immutable plan, activated target revisions and original `created_at`. |
| `GET /translation-activations/{activation_uuid}` | Inspect the original receipt; 404 if no activation committed. This is release status, not execution completion. |
| `GET /automation-snapshots/{snapshot_uuid}/translation-import/held-targets` | Requires `expected_manifest_sha256`; pages original hold receipts using source-ordinal `after` and `limit` (1–100). Reports current revisions and `eligible`, `changed`, `completed` or `post_forgotten`. |

Supplying a snapshot restricts activation to its completed translation mapping
and the exact revisions that mapping originally held. A later native hold cannot
be released through the old migration receipt. Ordinary application review can
preview a later held revision without the snapshot binding. Candidate paging
uses an index over original held receipts; it neither scans other operational
families nor guarantees that candidates remain unchanged until Apply.

Requests are bounded to 128 KiB and duplicate target UUIDs are rejected. Producer
tokens cannot administer these routes. Startup verifies plan/input hashes,
source bindings and the historical held-to-pending transitions, independently
of current target states. Backup/restore retains activation receipts;
anonymisation removes them before their referenced source/target histories.
`stash-activate-automation-translations` saves bounded previews and operation
identities in a private plan before applying. It validates every saved page,
inspects existing receipts on resume and reports release separately from
execution. See the [command and recovery contract](../integrations/gallery-dl/README.md#activate-imported-translation-holds).
Automatic scheduling for new captures remains separate transition work.
