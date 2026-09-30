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
Archive UUIDs, source/provenance models, and the catalog import are separate
remaining work.
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
