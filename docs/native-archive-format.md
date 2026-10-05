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

The optional producer receipt check verifies capture/file events and source-run
admissions after a complete verified restore:

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

The returned proof identifies the archive, canonical manifest digest, exact
library/outbox component hashes and bounded per-producer counts/digests. Its
coverage is `capture-file-and-run-admission-receipts`. This does **not** certify
enrichment/discovery journals, download archives, media or config.
Those remain required parts of the coordinated production backup. The copied
library used for the large restore rehearsal currently has no native producers;
nonempty receipt checks use the real producer outbox, request queue and native
receipt table definitions in SQLite regression fixtures. The backend and Python
verifier also check the same source-request digest corpus, including offsets,
fractional timestamps and year limits.

## Commands

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
