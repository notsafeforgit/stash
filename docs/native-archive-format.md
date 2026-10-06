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
archive or certify third-party writers' ordering. The server checkpoint described
below adds application configuration and deletion recovery capture; complete
media coverage and external-writer coordination remain publisher requirements.
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

Producer snapshots through schema 16 include attachment-download reports. Their
capture dependencies, and completed-file dependencies for downloaded outcomes,
must survive in the same queue and source scope. Acknowledged reports require
their original native report and matching capture receipt; pending reports keep
their exact bytes even after a lost server response. These checks preserve
reported transfer history without declaring files verified or available.

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
coverage is `capture-file-download-run-and-job-receipts`. This does **not** certify
download archives, media, configuration or filesystem recovery. Those remain
required parts of the coordinated production backup. The copied
library used for the large restore rehearsal currently has no native producers;
nonempty receipt checks use the real producer outbox, request queue and native
receipt table definitions in SQLite regression fixtures, plus real producer
processes talking to the Go API. The backend and Python
verifier also check the same source-request digest corpus, including offsets,
fractional timestamps and year limits.

## Commands

For a running native application, select `--server` instead of `--database`:

```sh
stash-archive export --server https://stash.example \
  --api-key-file /private/stash-backup-api-key \
  --request-id "$CHECKPOINT_UUID" \
  --recovery-roots /backup-work/recovery-roots.json \
  --blobs /srv/stash/blobs --components /backup-work/components.json \
  --producer-origin https://stash.example --output /backups/native-archive
```

The enclosing backup job should persist a new checkpoint UUID for each backup
and reuse it on retries. If omitted, the CLI generates one. Recovery roots are
a JSON array of `{"name":"media","path":"/media"}` objects interpreted in
the server's filesystem. Include every root needed by pending deletion journals,
including any separate trash root. Roots must be explicit and nonoverlapping;
they do not request a recursive backup of all ordinary media under them.

After copying the declared download archives and producer outboxes, the exporter
requests a server checkpoint and downloads its sealed components. It verifies
the request identity, inventory, lengths, digests and database deletion markers,
then checks receipt correspondence before packing. It copies the server's library
only once and checks component hashes again while packing. Server exports that
include producer/download state require `--producer-origin`; redirects are
refused and the application API key is sent only in a request header. Producer
tokens cannot create or retrieve these private application backups.

The authenticated API is `POST /api/v3/backups/checkpoints`, with `uuid`,
`recovery_roots` and optional `reserve_bytes`; `GET /checkpoints/{uuid}` and
`GET /checkpoints/{uuid}/components/{name}` under the same prefix retrieve sealed
results. Concurrent capture requests receive 409. An identical request UUID
reuses and revalidates its original sealed capture, even if live settings have
changed. A different request or an incomplete existing directory is rejected.
Failed new requests remove only their own output. The manifest is written last.

An authorized host coordinator can request `external_boundary` with
`{"timeout_seconds":30}` (1–120 seconds). After capturing configuration and
deletion recovery, the server keeps its database writer guard while it streams
an NDJSON `boundary_ready` event containing the checkpoint UUID, request digest,
one-use token and expiry. The coordinator captures its external filesystem view
and posts `{"token":"…","details":{…}}` to
`/api/v3/backups/checkpoints/{uuid}/boundary`. Details must be a nonempty JSON
object no larger than 64 KiB after canonical encoding. The server records the
accepted confirmation in `filesystem-boundary.json`, then releases ordinary
writes before the large database copy. The stream ends with a `sealed` event or
an error; a confirmation alone is not a sealed checkpoint.

The Python `ServerCheckpoint` client accepts a trusted local `boundary`
callback, `boundary_timeout`, and optional `boundary_release` and
`boundary_validate` callbacks. It validates the challenge before invoking the
callback, sends its returned evidence, and checks the sealed component against
the acknowledgement. An identical sealed retry returns ordinary JSON and
reuses the original evidence without invoking the callback again. Matching
acknowledgements are retryable during the copy and from the sealed component;
changed evidence and stale tokens are rejected. Expiry, cancellation or a
disconnected client cannot leave the server waiting indefinitely or seal an
unconfirmed capture.

The release callback frees producer barriers after immutable views are retained,
before acknowledging and waiting for the large server copy. A sealed JSON replay
also invokes it before component downloads, without running capture again. Error
cleanup releases any remaining barrier; an enclosing host context still owns
setup/acquisition failures. Release never means deleting the retained views.
The sealed filesystem component is downloaded and passed to the validator before
the large library download. `HostFilesystemCapture` checks the original worker
barrier inventory, pin manifest and held media snapshot at that point, including
on replay; changed caller inventory cannot silently reuse the checkpoint.

This handshake does not implement or certify a filesystem provider. The trusted
host must establish its producer barrier **before** requesting the native writer
guard, validate and retain the actual immutable views, and clean up failed
external captures. Otherwise a producer waiting on the server while holding the
host's barrier can deadlock the capture. The server never runs provider commands
from a request. The command-line exporter does not yet select a provider;
complete production inventory and publication integration remain required.
Checkpoint coverage remains
`database-configuration-deletion-recovery`.

Native artwork writes now publish flushed replacement inodes instead of
truncating a file that a reader or backup might retain. Orphan cleanup rechecks
references under a write transaction and uses the deletion journal, sharing the
checkpoint guard.

The host-side `ArtworkPins` provider retains those inodes in a private cache on
the same filesystem as the original artwork. Invoke it from the authenticated
boundary callback and return its record as `details.artwork`. Pass that same
provider to `export_archive(..., artwork_pins=pins)`, without live `blob_paths`.
The exporter verifies the record against the sealed server component and reads
only the retained files. It verifies each packed artwork's checksum against the
captured database. A retry can reuse the pins even after their live source tree
has disappeared; it cannot silently fall back to later live artwork.

Pins use flat private directories, retaining the original inodes without another
copy of all artwork bytes or all hash-prefix directories. Flushed inventories
record their identities, lengths and modification times. The provider bounds
capture by the server challenge's deadline and the normal disk reserve; a failed
capture removes only its own attempt. It requires the native atomic artwork
writer and journaled cleanup. Other tools must not modify original artwork in
place. Capture duration still needs measurement against the full live inventory.

After verifying durable enclosing publication, the host publisher calls
`release_published_artwork(archive, client, pins)`. It checks the archived server
and filesystem components, obtains the matching permanent server release,
records the local binding, then unlinks only the listed retained inodes. An
interrupted cleanup resumes from that binding. It retains small identity/release
records and removes inventories after durable completion; unexpected files and
changed inodes are preserved. Export never calls release automatically. This
provider does not upload to S3 or establish the ordinary-media/producer boundary.

Native gallery-dl workers now cooperate with the host's
`stash_ingest.publication_lock.PublicationBarrier`, using the existing shared
worker lock directories. The barrier waits for active file mutations and stops
new ones at a gate. Acquire it across all inventoried worker roots before asking
for Stash's database guard; release it once the immutable views are retained.
Acquisition is bounded and fails on timeout. Source fetching outside a file may
continue, but downloads, postprocessors, archive completion and filesystem
callbacks share exclusion. The adapter holds a file's lock across its download,
so long current downloads can delay capture. The protocol does not cover legacy
workers, manual filesystem changes or dedupe; retain their existing exclusion
and inventory requirements.

`stash_archive.zfs_media.ZFSMedia` provides an actual retained media view for one
explicitly configured Linux dataset, dataset GUID and physical mountpoint. It
checks ZFS topology and the process's mount namespace; child datasets, nested
bind/other mounts, missing mounts and changed dataset identities are rejected.
The [ZFS snapshot](https://openzfs.github.io/openzfs-docs/man/master/8/zfs-snapshot.8.html)
name contains the checkpoint UUID and one-use token. Creation also writes their
binding as snapshot properties, and a [ZFS hold](https://openzfs.github.io/openzfs-docs/man/master/8/zfs-hold.8.html)
retains the view. The provider verifies snapshot GUID, creation transaction,
request digest, hold and a read-only `.zfs/snapshot` path before acknowledging.

Private local intent precedes ZFS effects. Failed or uncertain attempts retain
that identity and any created snapshot; they never silently recapture under the
same challenge. The sealed `details.media` record must equal the retained local
manifest on replay. Changed/missing snapshots, absent holds and pending deferred
destruction prevent use. `MediaView.resolve(relative)` rejects paths and symlinks
escaping the view. The provider supports explicitly configured host commands;
it does not install sudo/delegation rules or run commands chosen by the server.

The host coordinator joins both providers with the native worker barrier:

```python
# The existing host backup/dedupe lock must already be held. pins and media are
# configured ArtworkPins/ZFSMedia providers; all worker lock roots are inventoried.
with HostFilesystemCapture(worker_lock_roots, pins, media) as capture:
    client = capture.client(server, api_key, checkpoint_uuid, recovery_roots,
                            boundary_timeout=120)
    stage = capture.prepare(component_cache, client, components)
    stage.seal()
view = media.open_bound(client.boundary_receipt).verify()
media_source = view.resolve("porn")
# Build/upload the matching media manifest from media_source, under the same
# enclosing backup/dedupe lock. Then package the captured native components:
export_archive(None, bundle, components=generated_components, producer_origin=origin,
               server_checkpoint=client, artwork_pins=pins, media_snapshot=media,
               component_stage=stage)
```

`ComponentStage` persists every declared download-archive SQLite snapshot before
any producer-outbox snapshot. It also copies the declared external profiles,
configuration and other operating files with size/identity checks, retaining
exact private bytes. A durable manifest records component identities, original
byte-encoded paths, SQLite metadata, lengths and SHA-256 digests; its digest is
sealed as `details.components` and the manifest itself is an archive component.
Names cannot override required server components. Stage directories are private
and named by checkpoint UUID; existing files are never overwritten or silently
recaptured. This records a caller-supplied inventory, not proof that every active
worker and configuration dependency was inventoried.

Only the invocation that prepared a new stage, while still holding its original
worker barrier, may request a new native capture. The request intent is durable
before transport. A reopened stage or uncertain request uses the existing
checkpoint GET route exclusively. Missing or incomplete server captures stop the
retry; a new coordinated attempt needs a new UUID. Incomplete local preparation
also needs a new UUID. A lost response after server sealing can reopen that
original view. Changed inventory, request options, component bytes, server
manifest or receipt binding prevents reuse. Current live sources may change or
disappear without changing the retained stage.

`ServerCheckpoint.seal` retrieves the sealed manifest and filesystem evidence
without downloading the large library. The host can therefore establish its
retained media source before media publication. Later export downloads that same
native view and packs the staged external files directly, without recopying live
SQLite inputs. Both actual packed digests and the existing native/producer
receipt proof are checked. Additional components in this mode are generated
media manifests or operating-state artifacts, not replacement live inputs.

After durable enclosing publication, `release_published_components(archive,
client, component_cache)` verifies the archived stage inventory and every exact
declared artifact before requesting server release or removing local copies. It
persists release intent, removes only unchanged numbered stage files, and can
resume after interruption. It retains small manifests, identities and receipts;
unknown files and all original live source paths are untouched. This helper
does not verify remote publication itself or retire abandoned incomplete stages.

The exporter validates the sealed media association but does not pack ordinary
media into the metadata archive. The host publisher must read that retained view
when building/uploading the matching media manifest. Once both media and native
archive publication have been durably verified, it may call
`release_published_media(archive, client, media)`. The helper binds release to the
archived checkpoint and permanent server release, persists local intent, removes
only its own hold and destroys only the exact verified snapshot. It never forces,
recurses or defers destruction; other holds/clones block cleanup. A crash after
hold removal or destruction is retryable. Identity/release records remain, and
ordinary export never releases snapshots. The host daily caller coordinates
publication and unsealed-attempt retirement. Complete worker/configuration
inventory, media-generation reconciliation and restore/capture measurements
remain required before production use.

The server captures the native database, raw deletion recovery trees, main
configuration, runtime overrides and configured TLS certificate/key assets.
Settings and overrides remain separate, and private values appear only in the
private backup components. Original configured paths and working directory are
retained as byte-encoded metadata for later relocation. Other external settings,
worker profiles, source-access files, artwork and ordinary media still require
the enclosing publisher's complete inventory and restore bindings.

Checkpoint files live in `native-checkpoints/{uuid}` under the configured backup
directory, with private directories/files. Export/download does not release them.
The server flushes `attempt.json` before any capture effects and retains it even
on failure. An incomplete request cannot recapture newer state under that UUID.
The publisher releases temporary components only after verifying durable archive
publication, using `POST /api/v3/backups/checkpoints/{uuid}/release`. Its body
contains `checkpoint_sha256`, `archive_uuid` and `archive_manifest_sha256`.
The first digest binds the exact canonical Go checkpoint manifest, not just the
request parameters; the archive digest uses the portable format's canonical JSON.

The server atomically publishes a private `release.json` without replacing a
competing release, and flushes that record before removing any listed component.
It permanently retains both `checkpoint.json` and `release.json`. Preserve this
metadata when relocating the checkpoint cache; do not prune the whole directory
after removing the large files. Capture/report/component requests for a released
UUID return 410, preventing a retry from capturing later live state. The same
release can be repeated to finish interrupted cleanup; a different archive
binding is rejected. `GET .../{uuid}/release` retrieves the retained binding but
does not assert that an interrupted cleanup has finished. Unexpected files and
nonregular replacements are preserved.

The Python publisher helper `release_published_checkpoint(archive, client)`
checks the complete artifact inventory, the saved server report and every
required checkpoint component's digest/size binding before sending the release.
It checks the returned binding as well. This helper does not independently
verify remote storage: its caller must first complete content/native/producer
verification and durable publication/readback. The normal export command never
calls it. Production S3 publication must reach its verified master-manifest
commit point before release.

`GET /api/v3/backups/checkpoints/{uuid}/status` returns `missing`, `partial`,
`sealed`, `released` or `abandoned`, with the original request digest when known
and the checkpoint digest for sealed/released states. Active capture or cleanup
returns 409. Status serializes with the native checkpoint guard; malformed or
unknown legacy directories fail rather than appearing missing.

For an unsealed attempt, the host sends `POST .../{uuid}/abandon` with its exact
`request_sha256`. The server refuses sealed snapshots and active capture, writes
and flushes a permanent `abandoned.json`, then removes only fixed regular
component files belonging to its recorded attempt. Missing UUIDs can also be
reserved this way to fence delayed capture requests. Unknown files, symlinks and
directories are preserved. Repeating the same request completes interrupted
cleanup; different request identities fail. Capture/read/component requests for
an abandoned UUID return 410. This receipt is never a publication certificate.

The host records the filesystem challenge before provider effects, retains
artwork admission evidence before linking, and keeps its existing ZFS intent
before snapshot creation. With backup/dedupe and worker barriers held, a resumed
unsealed run obtains the server fence and retires only those owned resources.
Missing manifests are permitted for partial output; missing ownership records
require inspection. Provider cleanup preserves original media/artwork and unknown
files, refuses unexpected mounts and foreign snapshot holds/clones, and retains
small identity/failure records permanently. ZFS subprocess supervision keeps the
backup lock alive through `sudo`, caller cancellation and timeout until the actual
command exits. Abandonment ends the current invocation with a failure, allowing
the following invocation to allocate a fresh run without reporting a backup.

The default reserve is
50 GiB, checked on both output and live database volumes during capture. A pinned
WAL reader allows ordinary writes during the database copy but retains newer WAL
pages until the copy ends. Copy duration, source WAL growth and the shorter
configuration/recovery-tree writer exclusion still need production-scale
measurement. This API and client do not by themselves certify complete archive
coverage, remote publication or readiness to activate restored workers.

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
Full native validation can run on the copy after releasing the writer lock.
`CopyDeletionSnapshot` captures recovery state under the same guard and
commit-marker view. The application coordinator uses `CaptureNativeSnapshot`:
it fixes a separate WAL read transaction under that guard, captures configuration
and recovery trees, then releases the writer before copying the pinned view.
Non-WAL sources are refused. Filesystem capture cannot run after writer release.
The coordinator takes the database guard before the configuration read lock and
releases the latter after copying the small configuration assets. External media
writers, complete media/download inventories and activation remain separate
requirements.

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
complete media/configuration inventory and remote publication; opening an
unmatched database can otherwise prune deletion markers.

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
output (server checkpoints retain their admission records as described above).
A killed portable export/import process can leave an unsealed directory; an existing destination
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
  SHA-256. New archives declare a 64 MiB maximum raw chunk, reducing request
  counts for database snapshots. Readers also accept earlier archives declaring
  1 MiB and enforce the declared limit. Deterministic gzip headers allow identical
  chunks to share an object across artifacts and future uploads. A small edit
  changes its entire chunk; changing the writer's chunk size does not rewrite
  or invalidate an existing archive.

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
record. The [host backup integration](../integrations/backup/README.md) now binds
the retained native/filesystem view to a v4 media manifest and verifies S3
Standard object checksums before committing its current manifest. Repeated
publication can reuse a durable checksum receipt when a fresh paginated inventory
reports the same remote object identity. Explicit audit and restore always check
remote checksums; listing metadata alone cannot establish initial integrity. Credentials,
scheduling and S3 publication belong to that host package, not this component
transport or the Stash server. Complete production inventory and cutover proof
remain separate gates.

The host publishes version 4 media manifests, whose version 2 selection
binds each restore path to a distinct object descriptor and explicitly records
the cold bucket/prefix. Multiple paths may share one checksum-verified object.
The complete native archive binding includes those descriptors, paths and store;
subset media plans drop the whole-library native claim. Historical selection
version 1 remains readable. Immutable uploads, durable cold-object receipts and
path-aware cleanup preserve replacement identities while sharing unchanged bytes;
see [immutable media publication and restore](../integrations/backup/README.md#immutable-media-publication-and-restore).
This does not require S3 object versions or change the portable bundle format.

The host transport distinguishes large media masters from the portable bundle's
small manifest. Host master JSON is bounded at 512 MiB, and its archived media
selection permits another 1 MiB for the binding envelope. The host permits a
1 GiB complete `artifacts.jsonl` inventory and streams it to a new verified file
in 1 MiB reads; each individual record retains the portable format's existing
limit. Encoded-object downloads use the same digest-checked streaming path.
Only a complete matching file is published; failed downloads remove their
temporary output and do not overwrite existing destinations. See the
[manifest-capacity rehearsal](native-backup-manifest-scale.json) for the measured
272,373-video shape, original cap failure and isolated metadata round trip.

## Validation

`make validate-archive` runs temporary SQLite/WAL, restore, artwork, pending-work,
corruption, schema refusal, publication failure, path safety and offline-pagination
tests. It is included in `make validate-fork` and CI. Full-copy rehearsals must
also reconcile the complete native database and original artwork, measure peak
space/runtime, reopen with the native binary and exercise the remaining shared
boundary and media-root cases before production cutover.
