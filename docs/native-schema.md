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
and decisions. Account equivalence and catalog import are described in the
later migration sections; native ownership review uses the API described below.

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

Discovery pages canonical accounts and bounded identifier summaries. Account
details, identifier evidence and ownership history use selected-record lookups
with independent pagination. Ambiguous identifiers remain multiple candidates.
Startup validates receipt integrity before writes, and anonymisation removes
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
continue requiring the worker's observation time. This schema change does not
release imported collector checkpoints or start work; reviewed execution handoff
remains separate. Migration retains all original capture UUIDs, rowids, signed
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

## Source file history

Schema 1000066 promotes catalog metadata edits, file-state changes and
deduplication assertions into `source_file_history`, with typed child tables
for locations, edits, states and deduplication. An event pins the original
collection/root revisions. Its source timestamp may be unknown; `observed_at`
records receipt of the assertion, and `created_at` records native insertion.
Signatures cover its provenance and complete child values. Partial event writes
cannot commit, even when a caller discards a late error. Startup validates the
graph, signatures and import receipts before normal opening.

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

`catalog_file_history_imports` and `catalog_file_history_records` provide bounded,
resumable mapping of the frozen `metadata_edits`, `file_events` and
`dedupe_events` families after completed evidence/media mapping. Each source
row receives one receipt, including invalid rows retained for review. The pass
uses 50-record/16-MiB batches and the source ordinal as its cursor. It never
executes retained SQL or reconstructs a filesystem operation. A completed pass
retains `imported:false` until overall migration reconciliation is complete.

Application-authenticated API routes:

- `GET/POST /api/v3/archive/catalog-snapshots/UUID/file-history-import` reads or
  advances progress. POST requires `expected_manifest_sha256` and `after`.
- `GET .../file-history-import/records?after=ORDINAL&limit=N` returns summaries;
  `GET .../records/ORDINAL` returns one receipt and its original source values.
- `GET /api/v3/archive/file-history/UUID` returns one complete event.
- `GET /api/v3/archive/file-observations/UUID/history?after=UUID&limit=N` returns
  event summaries for that observation.
- `GET /api/v3/archive/content-claims/UUID/history?after=UUID&limit=N` returns
  deduplication event summaries for that claim.

List limits are 1–100. There is no public history endpoint that deletes files or
applies an edit. Ordinary database backup retains this graph; anonymisation
removes the source history, import receipts and identifying values together.

### Reviewing a retained metadata edit

Schema 1000067 adds `metadata_file_edit_reviews`. Each immutable receipt joins
one application request to its native field decision, source history entry,
source field and file match. Import still leaves selected metadata unchanged.
The explicit review operation uses the normal typed field-decision writer and
commits the chosen value, provenance and retry receipt in one transaction.

Discovery starts at a selected scene/image's file associations and pages by
`(history_uuid, match_uuid)`; it does not read the whole catalog library. Edits
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
startup also checks source scope, normalized scalar choices and preserved UUID
redirects. A late failure cannot commit a field without its receipt, even when
the caller discards the error. Application notifications use the existing
after-commit contract and are not registered again for a committed retry.

Normal database backups retain the receipts; anonymisation removes them before
their private source history. Migration creates no reviews and changes no
selected fields. The [application API](native-ingestion.md#historical-metadata-review-api)
and scene/image review controls are implemented. The review controls use the
shared mobile section menu and retain uncertain Apply requests across browser
reloads. Broader account, source and migration review interfaces remain
transition work.

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
Reviewed activation and automatic capture scheduling are documented below.
Native review UI and production activation remain separate transition work.

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

### Automatic source translation policies

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
The producer receives its decision in the capture receipt. Startup validates
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
certify that all linked media are locally available. Startup validation checks
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
| `PUT /enrichment-targets/{target_uuid}/schedule` | Replace scheduling fields using `expected_revision` and `schedule`; stale revisions or ineligible pending bindings return 409. |
| `POST /enrichment-targets/{target_uuid}/retry` | Using `expected_revision`, release a held target or retry pending work whose bound job failed or was cancelled. Preserve priority and the later target/job retry deadline. Other states return 409. |
| `GET /enrichment-completions/{completion_uuid}` | Original completion input and timestamp; 404 when no receipt committed. |

Producer tokens cannot inspect or administer these routes. Cross-origin
mutations are refused. There is no HTTP completion mutation. Retaining a pending
target does not yet start execution: extraction dispatch, fenced publication,
cooldown/source fairness, and legacy queue mapping remain transition work.

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
anonymisation removes them. Startup validates bindings, provenance, record hashes,
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
Cross-collection fairness and legacy queue conversion
remain required before activation.

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
metadata scheduling. Startup checks the job result, all associations and every
capture against the saved source evidence, including shared-body/profile hashes.
Migration preserves queued, running, failed and cancelled schema-51 work; a
schema-51 success assertion is refused because that schema had no verified
publication path. Backup retains publication/provenance; anonymisation removes it.

Schema 1000053 adds verified staging release below. The scoped producer routes
and worker are implemented; remaining adapters and legacy queue conversion still
precede production activation.

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

Startup requires either the original staging body or a valid release for every
publication. It rechecks the proof, bounded native payloads/profile references,
post identifiers, collection provenance, receipt histories and storage accounting
before opening a writable connection. A missing or altered release, reference,
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
are reconstructed for historical attempts. Startup checks bindings and required
objects. Ordinary backups retain scheduling state; anonymisation removes it with
work history. This does not import legacy catalog cooldowns or scheduling history.

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
all previous rows and schema objects. Startup validates dependencies and failure
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
start. The oldest eligible requester wins, with UUID as the tie-breaker. Required
services include checkpointed children. Current target/source definitions,
cooldowns and existing enrichment ownership must still allow the work. Held or
stale targets and cooling dependencies cannot reserve an unrelated parent.
Existing downloads drain before enrichment starts; further downloads wait for
the due turn. Starting enrichment resets the affected service counters and age
budget, preserving download preference between metadata turns. All admission and
counter updates commit with the actual attempt; replaying ownership or a held
reservation does not count another start.

Download attempts expose `turn_until`, five minutes after their recorded start.
The worker checks that server-derived deadline at source boundaries; heartbeats
cannot extend it. It finishes the current file and saves its checkpoint before
yielding. Replaying a saved cursor may itself exceed five minutes, so a resumed
worker may reach one new checkpoint before yielding. This is a cooperative
budget, not a hard execution timeout. Yield uses `retry` with the controlled
`source_turn_complete` code, keeps pending windows/progress and the normal target
cooldown, and does not consume the failure/backoff budget.

Migration starts a new fairness epoch with zero counters; it does not invent
historical scheduling interest or rewrite existing work. Startup validates
service coverage, current queued-job bindings and the complete required-service
set. Ordinary backups retain this state; anonymisation removes it. Producer
outbox schema 10 adds rotation across local download/metadata profiles and
permitted collections without changing the native database schema. Legacy
scheduling import and live conversion remain required before activating
production schedules.


## Historical enrichment receipts

Schema 1000057 adds `source_enrichment_receipts`, `catalog_enrichment_imports`
and `catalog_enrichment_records`. The manifest-bound importer reads the optional
legacy `enrichment_receipts` family after native post evidence has been mapped.
Transactions use 50-record batches and a 16 MiB input budget, committing
receipts and their cursor together. Exact replay returns the existing
progress; stale cursors or changed manifest digests conflict.

A receipt preserves its source version, exact completion timestamp, enriched
attachment-link count and unresolved-child count. It binds the mapped native post
to the catalog's historical collection revision 1, even if that collection is
later edited or retired. The `catalog-enrichment-v1` policy identifies a legacy
worker assertion. It does not imply complete child coverage, identify particular
captures, or claim execution by a native worker. No jobs, targets, attempts,
captures, post revisions or Stash media fields are created or changed.

Malformed/unsupported receipts, unmapped posts and forgotten posts retain
review outcomes and their original source values without a native receipt.
Receipt identity includes its source/catalog, native scope and assertion; equal
values in different catalogs do not silently acquire shared provenance. The
snapshot importer currently accepts one frozen snapshot per source/catalog.
Ordinary backups include the receipts and original source ledger; anonymisation
removes both. Startup checks progress, source scope, receipt identity, counts and
original timestamps. Catalog-wide completion remains `imported:false`.

Application APIs expose resumable progress and source records at
`/api/v3/archive/catalog-snapshots/{snapshot}/enrichment-import`, its `/records`
list and `/records/{ordinal}` detail. Native receipts are available at
`/api/v3/archive/enrichment-receipts/{receipt}` and
`/api/v3/archive/posts/{post}/enrichment-receipts` (UUID cursor, limit 1–100).
These are historical inspection/import routes. Queue/cooldown migration and
reviewed activation are separate operations.

## Frozen enrichment work

Schema 1000058 adds `automation_enrichment_imports` and
`automation_enrichment_records`. The `automation-enrichment-v1` pass maps
`enrichment_jobs`, `enrichment_cooldowns`, `enrichment_seed_progress` and
`enrichment_source_progress` from the received automation snapshot. Catalog
post evidence and historical receipt imports must finish first; coalesced work
also requires completed catalog relationship mapping. Each transaction processes
at most 200 source records or 16 MiB. Progress binds the exact manifest digest
and source ordinal; replay reads terminal progress without repeating writes.

Pending/retry jobs become held native enrichment targets. Priority, original
attempt counts and deadlines remain available. The deadline includes the later
of the job delay and its exact platform/account cooldown. Account pauses do not
become service-wide native pauses. Unknown cooldown semantics require review.
Seed completion remains a traversal cursor; source last-attempt timestamps
remain progress evidence. Neither certifies completed enrichment. No native
jobs or attempts are created, and no target is activated by this pass.

Old `done` jobs require a catalog receipt for the same native post and historical
collection revision. `already_native` requires an existing gallery-dl capture
bound to that same scope. Completed imported targets reference those assertions
through `enrichment_completions.legacy_receipt_uuid` or `legacy_capture_uuid`;
they have zero new capture associations. The completion API reports `basis` as
`legacy_receipt` or `legacy_capture`. Ordinary native completions retain their
capture requirements and report `basis: captures`. These historical completions
are private to the importer and do not invent native execution or collection
coverage. Missing proof becomes review, preserving the original status.

`coalesced` requires an explicit imported catalog alias to the same native post.
It retains that alias proof without claiming the parent executed. Unsupported,
excluded and unresolved work retains its original disposition. Legacy staged
JSON stays in the immutable source record with its digest and a review outcome;
it is neither discarded nor represented as a native owned checkpoint.

Matching uses source-qualified catalog keys and imported aliases. A valid
queued URL missing from the native post is retained with migration evidence
whose observation time is explicitly the frozen snapshot boundary. This can
advance the post's URL-evidence revision; it does not create a post-body revision
or alter media metadata. New work does not overwrite existing native choices.
Only holds created by this import at their unchanged revisions can combine
their priority/deadline or finish using historical proof.

Application APIs expose GET/POST
`/api/v3/archive/automation-snapshots/{snapshot}/enrichment-import`, a `/records`
list (ordinal cursor, limit 1–100), and `/records/{ordinal}` with original source
values. Backups include these records; anonymisation removes them. Startup
checks progress, typed projections, source bindings, scoped proofs and native
history. Overall migration remains `imported:false`; review resolution and
reviewed activation are separate release requirements.

### Reviewed enrichment activation

Schema 1000059 adds immutable `enrichment_activations` and
`enrichment_activation_targets` receipts. The application API provides
`POST /api/v3/archive/enrichment-activations/preview`,
`POST /api/v3/archive/enrichment-activations`, and
`GET /api/v3/archive/enrichment-activations/{activation}`.

Preview accepts an operation `uuid`, optional paired `snapshot_uuid` and
`manifest_sha256`, and up to 100 selections containing `target_uuid`, `revision`
and the explicitly chosen `collection_revision`. Snapshot selection requires a
finished enrichment import and one of its unchanged original holds. An active
post and the collection's current active revision are required. Preview includes
the post revision, retained URL and URL UUID, original and destination collection
revisions, release target/revision, policy, priority and exact retry deadline.
Apply sends `{input, expected_plan_sha256}` and rechecks that entire preview in
one transaction. Changed evidence, schedules, source definitions or destinations
conflict without releasing part of a batch.

Imported collections begin disabled. Review their definitions before activating
work; enabling one creates a new collection revision. When the chosen active
revision differs from the held target's historical revision, activation consumes
the original hold as `excluded` with reason `activation_rebound` and creates a
`review` target for the same post, URL, collection UUID and policy at the selected
revision. Its priority and deadline are unchanged. A pre-existing destination
is a conflict, preserving native choices. If the source revision is unchanged,
the original target simply advances from held to pending. Receipts retain both
the consumed and released target histories. They replay after completion, later
holds, changed collection definitions or a forgotten post, without rescheduling
work. Activation does not create a collector job or claim source execution.

`GET /api/v3/archive/automation-snapshots/{snapshot}/enrichment-import/held-targets`
uses `expected_manifest_sha256`, sparse `after` ordinals and `limit` (1–100).
Indexed lookup emits the last original hold for each target once, even when
several legacy aliases share it or coalescing did not change its revision.
Candidates identify changed/completed work, forgotten posts, disabled/retired
collections and occupied replacement targets. A later native hold cannot be
selected merely by substituting its new revision in a snapshot-bound request.

Plans and responses allow 8 MiB so a complete page of retained URLs, including
JSON escaping, fits. Startup validates receipt hashes, original import ownership,
source/destination identity, active historical collection definitions and exact
held-to-pending or held-to-excluded/replacement histories. Backup retains the
receipts and anonymisation removes them with source evidence. The bulk application
client is `stash-activate-automation-enrichment`; source fetches remain separate
metadata-only worker operations.

### Imported enrichment staging

Schema 1000060 converts saved `enrichment_jobs.staged_json` into native migration
evidence using `legacy-enrichment-staging-v1`. The original automation record,
its source hash and the exact staged-text hash remain available. Conversion is
resumable in batches of at most 100 records and 4 MiB of source data; sparse source
ordinals and the frozen manifest digest fence each request. Both valid and
malformed non-null staged values receive a conversion or review outcome.

`automation_checkpoint_imports` records progress. Immutable
`automation_checkpoint_records` bind each outcome to the original enrichment
mapping. `automation_checkpoint_bodies` shares equal converted documents by hash.
Converted bodies contain ordered source records referring to shared metadata
bodies, optional shallow deltas, and explicit `_parent`/`_reddit` references.
Expansion reproduces the retained source values, including exact JSON numbers,
nulls and removed fields. The converter does not apply a newer source-retention
policy to old staged bytes. Unknown fields/formats stay in review with their
original evidence; partial conversion is not published.

Observation times are explicitly `unrecorded`. The saved extractor version is a
reported legacy value and may be absent. Pending child URLs retain their original
parent metadata, depth and reason. Unresolved references stay unscoped because
the old collector did not retain their parent; missing reasons stay absent.
Neither job update times nor the snapshot boundary become invented observation
times. This format is separate from owned native worker transcripts and is
rejected by that transcript parser.

The application routes are `GET` and `POST`
`/api/v3/archive/automation-snapshots/{snapshot}/enrichment-checkpoints`, with
`GET .../records?after=0&limit=100` and `GET .../records/{ordinal}` for inspection.
POST accepts `{expected_manifest_sha256, after}` after enrichment mapping has
finished. Record lists omit payloads; a selected record returns its converted
body and exact original values. Repeating the completed cursor is read-only.
`stash-import-enrichment-checkpoints` verifies the local frozen snapshot and the
exact staged-record count, and resumes a committed response being lost.

Conversion leaves posts, media, target choices, deadlines, captures and worker
state unchanged. It does not resolve the original review hold or mark the full
archive migration imported. Reviewed handoff of this retained evidence to native
execution remains a separate operation. Backup includes the converted evidence;
anonymisation removes it. Startup checks complete source bindings, progress,
hashes and deterministic conversion semantics, including review outcomes.

### Accepting retained checkpoint evidence

Schema 1000062 adds `checkpoint_evidence_acceptances` and
`checkpoint_evidence_captures`. Application review can materialize the shared
bodies of one converted legacy checkpoint as ordinary source captures. Original
record slots, pending children and unscoped unresolved references remain in the
converted evidence. Repeated slots share the same capture; each binding retains
its original body index and exact reconstructed payload digest.

These captures use origin `legacy-enrichment` and policy `legacy-retained-v1`.
Their observation time is null; their recording time is when the archive accepted
the evidence. The old reported extractor version remains optional. Missing source
URLs, reasons, timestamps and producer ownership are not invented. Both parent
references, shallow deltas, explicit nulls and exact JSON numbers are preserved.
Every body must have a supported post identity and all must identify the same
post. Unsupported or contradictory evidence remains available in migration review.

The preview identifies the exact original snapshot/manifest, ordinal, target
revision, source/staging/body hashes, post revision, shared capture UUIDs and
counts of original records, pending children and unscoped references. A post
with only catalog-local identifiers may acquire its first service identifier;
`assign_post_identifier`, `post_namespace` and `post_value` show that change in
the preview. An identifier already assigned to another post, or a different
service identifier on the selected post, is a conflict. Acceptance cannot merge
posts or silently replace a native identifier.

All routes use application authentication under `/api/v3/archive`:

| Route | Contract |
| --- | --- |
| `POST /checkpoint-evidence/preview` | Input: `uuid`, `snapshot_uuid`, `manifest_sha256`, `ordinal`, `target_revision`. Returns the review plan with `plan_sha256`; writes nothing. |
| `POST /checkpoint-evidence` | Body: `input` with those same fields and `expected_plan_sha256`. Atomically accepts only the unchanged preview. |
| `GET /checkpoint-evidence/{acceptance}` | Reads the acceptance by its input UUID, including `created_at`. Returns 404 if absent. |

Only one acceptance can own an original snapshot ordinal. A duplicate operation
UUID replays the original receipt after later target decisions; a different UUID
cannot replace it. Changed previews return 409. Requests are bounded to 16 KiB,
plans to 1 MiB, and reconstructed evidence to the existing per-payload and total
expansion limits. Capture creation, optional post identifier assignment, original
collection association and receipt bindings commit together, including when a
caller catches a late write error.

Acceptance leaves the target's review state, schedule and history unchanged. It
does not create a worker job, complete enrichment, select publisher accounts or
apply Stash metadata. Reviewed child execution remains a separate handoff to
implement. Backup preserves acceptance and source evidence; anonymisation removes
both. Startup recomputes the original conversion and reconstructed capture hashes,
checks the original review history and collection associations, and rejects altered
evidence before opening a writable database.

### Original observation context and reviewed retry format

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

The Go/Python parser and isolated collector also understand the future reviewed
retry format `stash-metadata-fetch-v2`. Its immutable prefix contains context
records with `retained_capture` UUIDs, null observation times, full original
payloads and no delta/parent indices. The accepted checkpoint converter builds
this prefix and preserves scoped pending children. Original record slots and
unscoped references remain in the frozen acceptance, without invented parents.
Missing historical failure reasons become the handoff state `legacy_pending`;
the historical reason itself remains unchanged in the acceptance.

New child records have real observation times, obey current reduction and refer
to earlier context records. They cannot refetch a root, use old evidence as a
delta base, add unreviewed historical captures during retry, or insert another
unbound inline parent. Repeated pending work is deduplicated without removing its
original evidence. Both original parent branches survive; incompatible category
chains, excessive depth and oversized documents remain review cases. Child-only
retry inherits gallery-dl parent settings and reserves the contacted child
service before initialization.

Schema 1000065 connects this storage/collector contract to reviewed native
execution, as described below. Evidence acceptance alone still leaves the target
in review. Existing v1 receipts and release proofs retain their byte-for-byte
checksum contracts.

### Reviewing an exact checkpoint handoff

Schema 1000064 adds immutable `checkpoint_handoffs`. This is an application
review receipt for the starting context of child-only execution. It binds an
existing evidence acceptance and its plan hash, the original target revision,
the post's current revision, an active destination collection revision, the
worker configuration hash and extractor version, the context retention policy,
and the exact resume document's hash, size and counts. The preview identifies
the target UUID/revision that subsequent admission would release, including a
replacement target when the collection revision changed.

Under `/api/v3/archive`, all four operations require application authentication:

| Route | Contract |
| --- | --- |
| `POST /checkpoint-handoffs/preview` | Input: `uuid`, `evidence_uuid`, `evidence_plan_sha256`, `target_revision`, `collection_revision`, `policy_sha256`, `extractor_version`. Reads current post revision and returns `plan_sha256`; writes nothing. |
| `POST /checkpoint-handoffs` | Body: `input` with those same fields and `expected_plan_sha256`. Saves only the unchanged review. |
| `GET /checkpoint-handoffs/{handoff}` | Recovers the committed review, including `created_at`, using the input UUID. |
| `GET /checkpoint-handoffs/{handoff}/seed` | Returns `handoff_uuid`, `plan_sha256`, `sha256` and `body`, reconstructed from frozen evidence and verified native captures. |

The database stores the bounded plan, not another copy of the source payloads
or resume document. Seed reconstruction verifies accepted capture UUIDs, raw
payload hashes, historical extractor versions, metadata and original recording
times. Missing or altered evidence is an integrity failure. A new runtime or
configuration requires a new preview. Post, target and collection changes
invalidate an uncommitted preview; an exact replay of an already committed
operation recovers its original receipt after later edits. Historical receipt
and seed lookup do not authorize executing a stale target.

Pending children remain scoped to their retained parents. The plan separately
counts original unscoped references; those stay in the frozen acceptance and
are not silently given invented parents in the new worker document. Retained
records keep null observation times and have no claimed observing producer.
Requests are bounded to 16 KiB, plans to 1 MiB and reconstructed seeds to the
collector's 32 MiB / 1,024-record limits. Invalid ancestry or oversized legacy
evidence stays in review without truncation. Clients must preserve JSON numbers
losslessly when decoding and replaying the seed; converting source IDs or numeric
lexemes through floating point can change its exact checksum.

Accepting this review does **not** release the original target, create a job,
write a worker checkpoint/lease/receipt, or complete enrichment. The separate
admission operation below consumes the review and binds its exact seed.
Backup retains the review and its source evidence; anonymisation removes the
execution bindings before the review. Startup
reconstructs the seed and verifies historical collection and evidence bindings
before opening for writes.

### Executing a reviewed checkpoint handoff

Schema 1000065 adds `enrichment_handoff_jobs`, `enrichment_job_retained_records`
and `enrichment_job_seed_services`. These contain one consumption receipt per
handoff/job, bounded original capture UUIDs and record digests, and supported
pending child services. They duplicate no source payloads. Migration does not
release review holds or create jobs from saved legacy state.

Producer admission revalidates the exact reviewed post/target/collection, logical
root, runtime, configuration and plan hash. One transaction either releases the
same target revision, or excludes the old target and creates the planned target
for the newer collection revision, then submits and binds one job. A generic
submission cannot omit the consumption receipt. A lost response replays the
original job even after later completion, cancellation or collection changes;
that recovery does not restart it.

New native enrichment jobs use argument version 2 and pin
`capture_policy: source-retention-v1+capture-context-v1`. A handoff job additionally
pins `handoff: {uuid, plan_sha256, seed_sha256}`. Ordinary new jobs use the same
capture contract without a handoff. Existing native v1 jobs, request UUIDs,
acknowledgements, capture identities and release proofs remain valid; this is
native execution history, not upstream UI/plugin compatibility.

The scoped seed endpoint reconstructs and checks original evidence against that
job. It is not a checkpoint acknowledgement or an invented producer attempt.
Before the first checkpoint, scheduling and fairness account for pending Imgur
and Redgifs services using the small service projection. A child-only failure
without saved child failure evidence cannot pause the parent website. Once a
real checkpoint exists, its pending services govern retries.

The first checkpoint must extend the exact seed. Later checkpoints retain its
immutable prefix and ordinary retry constraints. Each prefix record's
`retained_capture` identifies its original native observation; `producer_uuid`
on checkpoint/published records identifies the submitting producer, not an
invented observer of historical data. Original missing observation/extractor
information stays missing. Publication reuses those captures and associates them
with the destination collection without rerunning their publisher/translation
assessment as new observations. Retained evidence can complete a target only
inside the bound job's verified atomic publication.

Fresh child captures have their actual observation time, current extractor
version, and a signed context binding to an earlier capture. Parent UUIDs affect
capture identity even when their text is equal. Ordinary newly executed child
captures receive the same context bindings. Version-2 staging release binds the
job/seed/review hashes, original capture signatures, record delivery provenance,
new observations and unresolved references. Unscoped legacy references remain
in the original acceptance linked by the review; no parent or new failure is
invented for them. Completed staging is released only after this proof verifies.

Startup checks both the retained seed projections and current checkpoint or
released publication. Backups preserve these records and anonymisation removes
them in dependency order. Review UI and coordinated production activation remain
transition work.

## Frozen discovery and maintenance history

Schema 1000069 adds `automation_discovery_imports` and
`automation_discovery_records`. The `automation-discovery-v1` pass reads
`discovery_accounts`, `discovery_targets`, `discovery_candidates` and
`maintenance` from the received automation snapshot. The same snapshot's
enrichment mapping must finish first. Each transaction processes at most 200
source records or 16 MiB, committing typed associations and the exact manifest/
source-ordinal checkpoint together. Failed writes roll back the whole batch,
including when an outer transaction caller catches the error. Terminal replay
returns the original progress without writes.

Discovery is about identifying source posts for historical media, separately
from performer ownership. Account continuations preserve their validated service,
profile URL, listing/matching phase, historical attempts/pages and the later of
their retry deadline and imported platform/account cooldowns. Cursor and staged
page/detail digests reference original snapshot strings. The original account
mapping remains available through later account consolidation; this pass cannot
establish account equivalence, ownership or depicted-performer attribution.

Pending targets bind their original account job, source-qualified post reference
and historical collection revision. Candidates retain their URL, matching basis
and payload digest, even when several URLs remain possible. Neither is an
accepted post match. Existing lookup work references the corresponding enrichment
record and exact URL instead of creating another target. Historical completion
requires a same-post/collection receipt or capture; `already_native` requires a
capture, and `coalesced` requires the imported alias proof. A completed listing
does not certify that any target matched. Missing scope/proof and staged results
requiring conversion receive explicit review outcomes.

Known maintenance entries retain typed inventory watermarks, translation seed
times, seed counts, source exclusions and pruning summaries. Nanosecond
watermarks are decimal strings in the API to preserve integer precision.
These are historical records, not live scheduling/retention policies, executable
cursors or proof that native jobs completed. Unknown or malformed entries remain
in the original ledger with a review reason.

Application-authorized GET/POST
`/api/v3/archive/automation-snapshots/{snapshot}/discovery-import` exposes the
import; POST requires `expected_manifest_sha256` and `after`. `/records` accepts
an ordinal cursor and limit 1–100. `/records/{ordinal}` returns original values
on demand. Paged results contain compact projections and native associations;
payloads are not copied from the snapshot into another document store.

Foreign keys and immutable records retain source-family scope. Startup validates
progress, recomputes typed projections and digests, and checks native account,
post, collection and enrichment-proof bindings before writes. Normal database
backups include the original snapshot and these associations; anonymisation
removes the import before its dependencies. No jobs, captures, performer links,
post identifiers or selected metadata are changed by this pass. The client is
`stash-import-automation-discovery`; migration remains `imported:false` and
reviewed discovery activation/execution remain separate transition work.

## Verified legacy discovery identities

Schema 1000070 adds `enrichment_discovery_resolutions` and an indexed lookup of
mapped discovery inputs. During enrichment publication, a legacy-only post can
receive a native Reddit/Twitter identifier without changing its UUID. Exactly
one completed discovery mapping must bind the original post, collection UUID
and exact candidate URL. Reviewed collection revision changes retain that scope.

The `retained-discovery-identity-v1` policy checks the fetched post's service ID
against the candidate URL and requires corroboration: every retained strict
Twitter filename identifies that post; its captured publisher matches a saved
account identifier; or original text/title and its UTC publication date match
the historical evidence. Text normalization preserves the original finder's
HTML removal, NFKC case folding and Unicode word comparison, with minimum
lengths of 40 characters for text or 20 for titles. A feed's profile object,
translated text, a URL alone or approximate similarity cannot prove identity.

The resolution references the frozen snapshot ordinal, published checkpoint
record and identifier evidence, retaining the post's prior revision and the
versioned matching basis. It does not copy source payloads. Original observation
time comes from the capture; resolution time records the transaction. The
identifier, captures, resolution, job result and target completion commit
together. A deferred foreign key and pre-commit verification prevent a caller
from committing the identity mutation without the complete publication.

Unknown or multiply mapped inputs and IDs already belonging to another post
require review. Publication rolls back and the controlled failure retains its
checkpoint for later inspection. No post consolidation or performer assignment
is inferred. Reopening verifies the original snapshot bytes, reconstructed
native capture, matching basis, identifier receipt and publication binding even
after compact staging has been released. Anonymisation removes resolutions
before their dependencies; ordinary database backups retain them.

The migration itself leaves existing jobs, identifiers and selected metadata
unchanged. It does not activate workers. Account-listing historical coverage,
detail execution, reviewed post consolidation and production caller conversion
remain transition work.

### Account-listing candidate evidence

The pure `retained-discovery-listing-v1` matcher compares retained held targets
with compact Reddit/Twitter listing records. It has no database writes. A
corroborated candidate requires exact original title plus UTC source date,
original text plus date, a distinct title and longer original text, or an
original qualified post URL plus date. Text uses the retained Unicode word
normalization and minimum lengths of 20 for titles, 40 for dated text and 64
for text corroborating a distinct title. Duplicate title/text does not count as
two facts. Media URLs, scrape/profile URLs, translated text, observed capture
time and approximate captions cannot supply that proof. A contradictory captured
publisher ID rejects the candidate.

An exact title without corroboration stays `title-needs-verification`. Detail
responses must be checked against the unchanged original target: confirming the
inferred post URL or publisher cannot upgrade it through the separate lookup
policy's filename/account shortcuts. Neither result accepts a post identity.
The eventual publication service must check complete enumeration, all competing
candidates, current native choices and identifier ownership atomically.

Page matching groups media and parent-context records by qualified post ID,
retaining their original record ordinals. It does not copy post/profile payloads.
Several attachments from one post produce one candidate; separate posts with
the same caption remain separate candidates. A stronger record can corroborate
the same post's weak title, while a final or empty page never becomes a match
on its own.

Schema 1000072 binds each reviewed held target in `discovery_match_targets` to
its original frozen record SHA-256, existing legacy-backed listing and exact
native post revision. Unconverted historical candidate rows block binding;
resuming from an old cursor cannot silently omit them. Migration creates no
bindings or jobs. Bindings are idempotent, and later source or post edits stop
fresh comparison without invalidating historical receipts.

`discovery_match_pages` records each target/page comparison and its original
page and result hashes. `discovery_match_evidence` keeps record ordinals and the
comparison basis; it does not copy the source payload. The original page body
is shared by every target that compares it. `discovery_match_candidates` groups
qualified post IDs across pages, retaining first, strongest and latest evidence.
Further media from one post do not become competing candidates. A stronger
record can promote that post's provisional result, while all earlier evidence
and distinct competing IDs remain available.

Receipt, evidence, grouping and target cursor commit in one managed transaction.
An interrupted write rolls back all four, even if its caller catches the error.
Independent callers and retries return the original receipt. Each listing
allows at most 10,000 bound targets; each target allows 4,096 distinct candidate
posts. Exceeding a limit preserves the source page and prior progress and fails
without truncating candidates or advancing the cursor.

The application runs a comparison worker over explicitly bound targets and
already retained pages. Its pending index bounds inspection to 32 target rows
per step, including waiting and stale targets. Decoding and comparison use a
read transaction; the subsequent short write rechecks the original target,
page, native post revision and active collection/account/root before committing.
The saved target cursor survives restart. This worker makes no source requests,
does not admit listing jobs and does not publish identities or selected metadata.

`enumeration_complete` on a match target means comparison reached the retained
listing's final cursor. It does not certify coverage of earlier historical
pages, choose a unique identity, complete an import, or authorize release of
source evidence. Historical-coverage reconciliation and weak-candidate detail
execution remain necessary before those candidates can publish.
Startup verifies original target bindings, page/result digests, record
references and candidate aggregates. Normal backups retain the entire graph;
anonymised exports remove it before removing its source evidence.

The application review derives coverage and blockers from these indexed records
in one read transaction. It distinguishes received and compared batches, missing
history before a saved cursor, weak/competing candidates and changed native
choices. It also reports when a sole candidate's identifier already belongs to
another post, including a forgotten one. It loads no source bodies and creates
no stored review state. An empty blocker list is not identity acceptance or
metadata publication. See the [review response](native-ingestion.md#reviewed-discovery-activation).

### Reviewed discovery activation

Schema 1000073 retains immutable `discovery_activations` and
`discovery_activation_targets` receipts. A preview selects an exact imported
manifest, legacy-backed listing definition and up to 1,000 original target
ordinals/hashes. It resolves current native post revisions and pins them in the
reviewed plan. The original account hash, saved cursor, historical page count,
retry deadline, collection/root revision, worker policy and extractor remain
part of the same review. Existing compatible listing and target bindings can be
reused by another explicitly reviewed batch.

Apply requires the preview hash and repeats its source/revision checks. It
commits the shared listing, every selected target and the immutable operation
receipt atomically. A late failure rolls back the whole batch; a changed native
post or collection requires another preview. The same operation UUID and request
return their original receipt after a lost response, later comparison progress,
post forgetting or collection retirement. Different inputs cannot reuse that
operation UUID. Backup/reopen validation checks the original receipt graph and
anonymised exports remove it before removing comparison bindings.

Activation creates no page jobs, fetches no source data and chooses no post
identity. Scoped producers separately admit due listings. Application-only
routes expose preview/apply/replay, retained listing summaries and the comparison
targets, candidates and original record references. Producer tokens cannot use
these review routes. See [activation API](native-ingestion.md#reviewed-discovery-activation).
The operator command saves reviewed activations and recovers their original
receipts. Missing-history searches can use reviewed recovery below; the review
UI, detail execution and staging-release workflow remain open.

### Verified discovery publication

Schema 1000074 adds immutable `discovery_match_publications` and
`discovery_published_records`. Application publication accepts only a completely
compared search retained from its beginning, one strong candidate, unchanged
native choices and an unclaimed qualified post ID. Preparation decodes the
original frozen target and strongest page under a read transaction. The write
rechecks those bindings and commits identity/URL evidence, native capture-domain
effects and the receipt together. Caught errors and failed commit checks roll
back partial identities and metadata. Shared capture services retain publisher,
album and translation behavior without a synthetic producer delivery receipt.

The receipt pins the target revision, strongest page hash, matching basis and
actual corroborating record ordinal. Record associations preserve original
observing producer, times and parent contexts through deterministic capture IDs.
All observations for the selected post on that page are published; unrelated
posts remain staged. Native captures retain normalized shared bodies, profiles
and patches. Original imported post UUIDs and legacy keys remain valid, and a
source ID already owned by another post requires explicit consolidation.

Replay returns the same receipt after lost responses, restarts and later native
edits. Startup reconstructs the original proof and verifies every capture,
collection association, identity/URL evidence row and record mapping. Ordinary
backups retain the graph; anonymisation deletes publication children before their
dependencies. Publication does not release page bodies, finish catalog import or
assign depicted performers. Automatic dispatch uses the same service. Actual
source coverage, weak-candidate resolution and verified staging release remain
transition work.

### Reviewed discovery history recovery

Schema 1000075 adds `discovery_listing_recoveries` and
`discovery_recovery_targets`. Each original legacy search with missing historical
batches may have one replacement beginning at the first page. The new definition
pins the original listing UUID/digest and keeps the same frozen account reference,
source account, collection and profile URL. Explicit review chooses the current
collection/root and worker policy; original retry deadlines remain lower bounds.
Definitions without recovery retain their original serialized form and digest.

The ordinary activation preview/apply service commits the new definition,
selected target links and immutable receipt atomically. It waits for running
producers, cancels queued predecessor work and prevents further predecessor
admissions/pages. Existing attempt/page/candidate evidence remains retained, and
its pending comparisons may finish. Each replacement target preserves the same
original source ordinal, hash and post UUID; it cannot invent a new association.

Read-only reviews expose both search references. A new complete strong match
cannot publish while the predecessor has uncompared retained pages or a differing
candidate. Publication reconstruction checks those same facts on reopen, after
which later native edits can still replay the original receipt. Foreign keys,
unique predecessor constraints, mutation guards and startup graph checks preserve
these relationships. Backups include both searches; anonymisation removes recovery
links before their dependencies. New fetching supplies current evidence, not a
claim that missing historical bodies were recovered or all past posts still exist.

## Durable account listing pages

Schema 1000071 stores immutable discovery definitions in `discovery_listings`.
Each binds a native source account, exact collection revision and logical root,
Reddit submitted or Twitter timeline URL, worker policy/runtime, initial cursor
and not-before deadline. A fresh listing starts without a cursor. Resuming a
frozen listing requires a `discovery_listing_legacy` reference proving the
original account, profile, collection, cursor, historical page count and mapped
cooldown. Unconverted staged results cannot be discarded by starting a listing.
Historical page counts remain historical assertions, not native page receipts.

Each `account.list_page` archive job fetches **one page**: one batch of source
posts and the cursor for requesting the next batch. The
`discovery_listing_jobs` binding retains its generation and requested page
ordinal; `discovery_job_attempts` records the authenticated producer for each
fenced attempt. Failed attempts retry the same page with backoff. An explicit
retry of exhausted or cancelled work retains earlier pages and deadlines.
Successful nonfinal pages permit a new job for the next cursor. A final page
ends enumeration, including when that page contains no records.

`discovery_pages` retains one compact `stash-discovery-page-v1` body per page,
with an immutable digest, original producer/attempt and small receipt. The page
must continue the exact saved cursor, profile and runtime. Repeated cursors,
gaps, future observation times and writes after completion are rejected. Page
retention, job success and reservation release commit together; generic job
success cannot bypass that receipt. Lost acknowledgements replay the original
receipt without restoring the old lease or contacting the source again.

Listings share service reservations, cooldowns, collection exclusion and bounded
download preference with enrichment and downloads. Releasing after each page
allows a download turn before the next page. The original job and pacing rows,
indexes and guards survive migration. Admission allows at most 16 active listing
jobs, 10,000 new pages per listing and 2 GiB of retained discovery bodies across
the database. Each page retains the 4,096-record, 32 MiB compact and 128 MiB
expanded limits. Capacity exhaustion is an explicit failure, never a completed
listing or discarded page.

The internal coordinator authenticates collection/root grants and rechecks the
original lease deadline, credential and source definition through commit.
Descriptions return the requested job's original input cursor even after later
pages advance the listing. Controlled failure receipts preserve the original
attempt and cannot prolong cooldowns on replay. Startup validates definitions,
import provenance, generations, cursor chains, page bodies and completion proofs.
Anonymisation removes these records before their dependencies; ordinary database
backups include them.

Page success proves retained enumeration data only. It does not match candidate
posts, publish native captures, assign performers or complete catalog migration.
The [scoped worker API](native-ingestion.md#account-listing-discovery) exposes
admission of existing definitions, owned attempts, page delivery and failure
receipts. It cannot create definitions or activate imported work. Durable
producer delivery, selected-job execution and shared profile/collection dispatch
are implemented, along with the native candidate comparison worker described
above. Reviewed activation and publication of complete, unique strong matches
are implemented, through both the application endpoint and a bounded server
worker. Both use the same atomic publication checks and receipts. Historical
coverage, weak-candidate detail execution, verified staging release and reviewed
post consolidation remain required.
