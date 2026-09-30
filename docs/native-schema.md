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
Ordered attachments, post-to-gallery associations, synchronization, manual
membership decisions, and the mixed-media album UI remain subsequent work under
the [album requirements](native-archive-transition-plan.md#source-post-albums-and-galleries).

The filesystem deletion journal currently derives its directory from the
database filename. Promotion in place retains that association, but a production
path change must first drain pending deletions or transfer the exact journal
with its database. Do not rename a live database and assume its pending file
operations moved with it.

Migrations run against copies during development. SQL failure leaves a dirty
migration state that startup refuses; restore the migration backup or use a
validated recovery procedure. Do not force a schema version to hide a failure.
The compatible database and deployment stay pinned until the production cutover
and restore gates pass.
