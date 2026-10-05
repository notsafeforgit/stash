# Portable native archive format

`integrations/archive` provides the independent `stash-archive` command. It
preserves the complete native SQLite database and original artwork, plus
explicitly selected configuration and operating-state files. UUIDs, redirects,
remote IDs, captures, documents, selected metadata, gallery membership, jobs and
schema history remain in their original database representation. Binary values
are preserved exactly. The package uses Python 3.11 or newer and its standard
library; inspecting a restored archive requires neither Stash nor a catalog
plugin.

The current coverage is **declared components**. Each database has a consistent
SQLite snapshot, and each restored component is verified. Production backup
still needs a coordinated producer receipt boundary, configuration/filesystem
state, a matching media manifest, verified remote publication and the complete
restore drill in the [transition plan](native-archive-transition-plan.md).
Supplying several live database paths does not establish that shared boundary.

Export snapshots all declared download archives first, then every producer
outbox, then the native library, regardless of their order in `--components`.
It freezes these SQLite copies before compressing any library/artwork objects;
later producer activity cannot move the saved queues ahead of the saved library.
The native gallery-dl adapter commits a file-completion event before adding its
download-archive entry, and a producer acknowledgement follows the native
receipt commit. This ordering preserves those causal relationships without
stopping downloads for the potentially long compression step.

For producer-aware exports, require the corresponding receipt check before
packing or publishing the archive manifest:

```sh
stash-archive export --database /srv/stash/native.sqlite \
  --blobs /srv/stash/blobs --components /backup-work/components.json \
  --producer-origin https://stash.example --output /backups/native-archive
```

Missing registered outboxes, a wrong origin or inconsistent acknowledgements
abort the export before packing. The flag does not discover every download
archive, certify third-party writers' ordering, freeze configuration or preserve
the filesystem recovery/media boundary. Those remain coordinator requirements.
The portable coverage stays `declared-components`. Verification after restore
rechecks receipts and binds its proof to the final archive/library/outbox hashes.

The optional producer receipt check verifies capture/file events, source-run
admissions and scraper job journals after a complete verified restore:

```sh
stash-archive verify /backups/native-archive --temp-parent /restore-work \
  --producer-origin https://stash.example
```

Every registered producer must have exactly one matching outbox snapshot for
that origin. An acknowledged event whose payload has been discarded must have
the same native receipt, including its digest, scope, result and commit time.
Pending, sending and review events retain their original bytes. If Stash already
accepted an event but its response was lost, matching native acceptance is
reported separately and the retained payload remains available for replay.
Missing or conflicting receipts fail verification; the check changes no queue
state. It also rejects newer outbox versions until their receipt contract is
supported. Retired producers still need their retained outboxes until an explicit
retirement/checkpoint protocol allows their removal.

Source requests retain their original template and window after admission.
Verification reconstructs their submitted bytes and checks Stash's separately
normalized request digest. Coalesced requests retain their own identities; a
later run state does not rewrite an earlier admission. Unsubmitted windows,
leased requests and requests awaiting review remain pending work. Admission
does not certify scrape completion.

The enrichment, listing and detail-verification journals retain their immutable
job definitions, original claim/attempt owners and acknowledged checkpoints,
publications, comparisons or failures. Verification compares these with native
history, including timestamp precision. It allows an older acknowledgement when
the native job has since advanced. It checks a pending page/checkpoint's original
bytes and separately counts bodies already accepted before a response was lost;
`accepted_pending_bodies` is not an execution-completion count. It never renews
leases, resets retry deadlines or makes pending/review work complete.
If another worker completed the job after the saved attempt expired or ended
for retry, `superseded_pending_bodies` identifies that distinct case. Both the
native result and the producer's unacknowledged bytes remain retained for review.

Listing receipts require the matching retained native page. Metadata checkpoint
receipts require the current retained checkpoint body or, for published enrichment,
its native release receipt. Native schema validation remains responsible for
the complete release/capture provenance proof; this receipt check does not
reconstruct a deliberately released transcript. Completed local journal rows
may have been pruned by the producer's normal retention policy; native history
is retained independently.

The returned proof identifies the archive, canonical manifest digest, exact
library/outbox component hashes and bounded per-producer counts/digests. Its
coverage is `capture-file-run-and-job-receipts`. This does **not** certify
download archives, media, configuration or filesystem recovery. Those remain
required parts of the coordinated production backup. The copied
library used for the large restore rehearsal currently has no native producers;
nonempty receipt checks use the real producer outbox, request queue and native
receipt table definitions in SQLite regression fixtures, plus real producer
processes talking to the Go API. The backend and Python
verifier also check the same source-request digest corpus, including offsets,
fractional timestamps and year limits.

## Commands

The portable verifier can run the matching native Stash executable after its
transport checks and combine that result with the producer receipt check:

```sh
stash-archive verify /backups/native-archive --temp-parent /restore-work \
  --native-validator /opt/stash/stash \
  --producer-origin https://stash.example
```

Both checks use the **same single temporary restore**. Native validation must
return the exact library SHA-256, byte count and schema from that archive's
inventory. The resulting `native_snapshot` proof includes the archive UUID,
canonical manifest digest and library component identity; `ingestion_receipts`
is bound to the same archive. The command prints a success result only after
every requested check succeeds, then removes the temporary restore. Failed,
malformed, mismatched or unsupported reports do not produce a partial success.
The coverage remains `declared-components`; neither proof certifies filesystem
recovery or a matching media/download/configuration boundary.

`--native-validator` is an explicit trusted local executable path, never a
program selected by an archive. It receives only `--verify-native-snapshot` and
the isolated library path, with no shell interpretation. Output is bounded to
64 KiB per stream and validation has a one-hour deadline; use
`--native-validator-timeout SECONDS` for a different positive deadline. Invalid
options or an unavailable executable fail before restoring any data. Omitting
this option keeps the standalone Python transport/SQLite checks available
without an installed Stash binary.

After restoring a bundle, the matching native Stash binary can verify the
database's full schema, source evidence and retained provenance:

```sh
stash --verify-native-snapshot /restore-work/library.sqlite
```

This command runs before application/configuration initialization and outputs a
JSON receipt containing the database's SHA-256, byte count, schema version and
pending file-deletion marker count. It reuses the native startup validators,
including the complete release/capture proof checks. It requires this binary's
exact clean native schema and never upgrades an older snapshot. Missing, foreign,
dirty, newer or logically inconsistent databases fail with a nonzero exit status.

Use a closed, SQLite-aware snapshot or isolated restored copy. A nonempty WAL or
rollback journal is rejected. The validator holds a read transaction, compares
the database's bytes before and after validation, and rejects a changed file.
It does not call the application database's normal open/recovery path: pending
deletion markers and staged media remain available for the coordinated restore.
`filesystem_recovery_verified` stays false; successful database validation does
not certify the media, deletion-journal or producer boundary.

Ordinary [SQLite readers in WAL mode](https://www.sqlite.org/wal.html#read_only_databases)
can create empty WAL/shared-memory sidecars. These are rebuildable reader state,
not additional archive components. The validator never reads or recovers the
separate Stash file-deletion journal.

The backend's `sqlite.WithNativeCheckpoint` provides the writer exclusion needed
by the server/filesystem coordinator. It opens an existing exact-schema native
database without application initialization or deletion recovery, obtains
`BEGIN IMMEDIATE`, and exposes the corresponding journal path and committed
deletion IDs to a capture callback. `CopyDatabase` uses SQLite's online backup
API through a separate read-only connection while that guard remains held.
It refuses existing outputs/sidecars, flushes the new copy, supports per-step
disk-reserve checks and removes its failed output. The checkpoint connection
does not run close-time optimization or commit application changes.

The callback must finish capturing matching configuration, raw journal and
staged/trash bytes before returning, and must not write to the guarded library
or invoke recovery. Cancellation makes capture fail but keeps writer exclusion
until that callback and any in-flight database copy have actually stopped;
callbacks must honor their capture context. Lock acquisition has a five-second
SQLite busy limit, with cancellation checked before and after acquisition.
Full native validation can run on the copy after releasing
the writer lock. `CopyDeletionSnapshot` captures recovery state under the same
guard and commit-marker view. These primitives are not yet wired into the
production export coordinator. Configuration, external media writers, complete
media/download inventories and activation remain separate requirements.

`CopyDeletionSnapshot(destination, roots, checkSpace)` creates a private regular
ZIP file suitable for a `file_journal` component. Roots explicitly authorize
nonoverlapping source directories; they cannot authorize the whole filesystem.
The component contains each required file body once per filesystem identity,
plus a final canonical JSON manifest. ZIP uses stored entries so the enclosing
portable archive performs compression. SHA-256 and byte counts identify file
contents. Raw gob journals, byte-encoded paths and symlink targets preserve
non-UTF-8 filenames. The manifest also retains commit markers, directory modes,
file times, hard-link identities and explicit trash-root alias bindings.

Capture follows nested staged parent directories and includes staged trees,
replacement files, reserved trash directories, unfinished copies and completed
trash destinations. Ancestors are directory skeletons; unrelated media files
are not included. Prepared journal fragments remain opaque. Unknown/corrupt
journals, unsupported special files, undeclared paths, unsafe symlink ancestors,
overlapping roots and observed changes stop capture. Native writer exclusion
does not freeze external writers; the coordinator must still establish that
boundary. No source recovery occurs during capture.

`file.RestoreDeletionSnapshot` requires the matching copied database's commit
markers and a new destination outside the original roots. It validates the
manifest and object inventory, streams and verifies file contents, recreates
hard links, and rewrites journal paths and file identities into private
`roots/<name>` directories. Symlink targets are preserved literally and never
followed during extraction. Missing expected identities become deliberately
unmatchable sentinels, reported as `UnresolvedIdentities`, so a replacement never
becomes the original merely because a new inode number was allocated. Existing
replacement conflicts remain pending for normal recovery to report.

Restore does not activate a database or execute deletions. Its returned journal
directory must be paired with the restored database's journal path only after
all library/media bindings and remaining components are restored and checked.
This component is not a complete media backup, a general library-root migration,
or proof that every pending deletion can complete. Source path syntax must be
supported by the restoring operating system. Production export still needs the
assembled configuration/filesystem coordinator, full media inventory and remote
publication; opening an unmatched database can otherwise prune deletion markers.

Install into a prepared Python environment:

```sh
python3 -m pip install ./integrations/archive
```

Export a native library and its original artwork:

```sh
stash-archive export --database /snapshots/native.sqlite \
  --blobs /snapshots/blobs --output /backups/native-archive
stash-archive inspect /backups/native-archive
stash-archive verify /backups/native-archive --temp-parent /restore-work
stash-archive import /backups/native-archive --output /restore-work/new-installation
stash-archive list /restore-work/new-installation
stash-archive list /restore-work/new-installation --table performers --limit 20
```

`export` uses the [SQLite Online Backup API](https://www.sqlite.org/backup.html),
including committed WAL data. It checks database integrity, foreign keys, native
lineage and the clean migration ledger. External artwork is resolved from the
snapshot's blob references; repeated `--blobs` options add supplementary stores.
Missing or corrupt originals abort the export. Other selected files must remain
unchanged while read. Original configuration bytes can include secrets, so the
archive and restored files use private filesystem permissions.

`inspect` reads the inventory and reports sizes; `contents_verified: false`
distinguishes that inspection from a content verification. `verify` performs an
actual restore in a temporary directory, checks every chunk and database, then
removes that temporary copy. `import` requires a new destination and writes
`restore.json` only after all checks and durability steps succeed. It preserves
pending producer work exactly. Server and worker activation are separate steps.

The restored directory contains `library.sqlite`, `blobs/`, and `components/`.
Select the restored database and corresponding artwork in the native server's
configuration. Use a binary that supports the recorded native schema. Existing
absolute media locations and configuration values remain unchanged; reviewed
root remapping is a separate restore step. The old compatible binary cannot
open a promoted native database.

`list` without a table lists the native tables. With a table it returns a
bounded page using its declared primary key. The returned `next` array can be
passed to `--after`; composite keys are supported. Default columns include
identities and human-readable names/titles. `--columns` selects additional
columns, with binary values represented as base64. Output is bounded to 8 MiB;
the restored SQLite file also supports independent SQLite tools.

The tools reserve 50 GiB of free space by default, check additional space before
snapshot/restore and recheck while writing. `--reserve-bytes` changes that reserve
for another installation. A failed operation removes only its newly created
output. A killed process can leave an unsealed directory; an existing destination
is always refused. Preserve that evidence or remove the abandoned directory
before choosing a new destination.

## Additional components

`--components components.json` accepts an array of explicit local inputs:

```json
[
  {"role":"config","name":"config.yml","path":"/snapshots/config.yml"},
  {"role":"producer_outbox","name":"worker.sqlite","path":"/snapshots/outbox.sqlite"},
  {"role":"download_archive","name":"downloads.sqlite","path":"/snapshots/downloads.sqlite"}
]
```

Supported additional roles are `config`, `import_rules`, `file_journal`,
`producer_outbox`, `download_archive`, `media_manifest`, `worker_profile` and
`operating_state`. Producer outboxes and download archives use SQLite snapshots.
Producer application IDs, bindings and schema versions are retained and checked.
Other roles preserve exact file bytes. Components restore beneath
`components/<role>/<name>`; they do not overwrite a running service's files.
The input paths are omitted from the portable inventory.

## Representation and integrity

Format version 1 is a directory containing:

- `manifest.json`: format/transport version, archive UUID, creation time,
  coverage, chunk size and the inventory's SHA-256, byte length, entry count
  and total uncompressed size. Its publication is the final commit point.
- `artifacts.jsonl`: one streamed record per component. A record identifies its
  role/name, complete uncompressed SHA-256 and size, and ordered chunk descriptors.
  Database entries also carry their native or producer identity/version metadata.
- `objects/<sha256>.gz`: compressed chunks addressed by their encoded SHA-256.
  Each descriptor also records encoded size, uncompressed size and uncompressed
  SHA-256. Chunks contain at most 1 MiB of raw data; deterministic gzip headers
  allow identical chunks to share an object across artifacts and future uploads.

The stream inventory avoids retaining hundreds of thousands of artwork records
in memory. SQLite snapshots retain page layout, allowing unchanged chunks to be
reused. Objects are immutable: an existing object is checked before reuse.
Restore validates the inventory before trusting its space estimate, rejects
duplicate names and unsupported formats, verifies both encoded and decoded
hashes, and validates complete artifact size/hash/order. It checks original
artwork against its retained MD5 identity as well as the transport SHA-256.
Symbolic-link objects and paths outside the fixed output layout are rejected.

An archive digest detects corruption and identifies the exact archive. Trust in
a downloaded manifest comes from the enclosing backup's verified publication
record. Common-boundary certification and S3 current/latest publication are not
implemented by this component transport.

## Validation

`make validate-archive` runs temporary SQLite/WAL, restore, artwork, pending-work,
corruption, schema refusal, publication failure, path safety and offline-pagination
tests. It is included in `make validate-fork` and CI. Full-copy rehearsals must
also reconcile the complete native database and original artwork, measure peak
space/runtime, reopen with the native binary and exercise the remaining shared
boundary and media-root cases before production cutover.
