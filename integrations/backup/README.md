# Host backup integration

This directory tracks the existing host daily backup, restore and audit tools
while they are converted to the native archive contract. `baseline.json` records
the exact installed files captured before conversion. The baseline commit is a
rollback point, not a second supported runtime or an installation instruction.

The copied `host/s3-backup-README.md` describes the installed policy. Preserve its
daily cadence, storage classes, missing-mount/empty-source guards, upload retry
ledger, pending/last-copy protections, restore behavior and publication-before-
cleanup ordering. S3 credentials and remote operations remain host-owned.

## Native publication

The tracked publisher now replaces the catalog upload with a native archive.
It acquires inventoried worker barriers and retains the native checkpoint,
original artwork (performer photos, logos and covers), producer/download state
and ZFS media view before scanning. Media uploads read that retained view. Live
mount, nonempty-source and returning-file protections still inspect the live
source. Dry runs create no native checkpoint, artwork pins or ZFS snapshot.

`current_manifest.json` version 3 binds the media selection to one native archive
and checkpoint UUID. A selection digest excludes timestamps and publication
references, avoiding a circular hash. The same selection is packed inside the
archive with the sealed filesystem-boundary digest. Content restore, native
binary validation and producer receipt verification precede publication. Every
new or previously unverified S3 metadata object requires full-object SHA-256,
exact length and Standard storage class. Publication records that proof in a
private local SQLite index. Subsequent runs use a complete paginated LIST and
reuse the proof only when the expected checksum/length and the object's ETag,
modification time and storage class still match. LIST and ETag are change checks,
not content hashes. Changed or unknown objects require a new checksum HEAD;
missing immutable objects are uploaded and verified again. Immutable archive objects
and per-run manifests precede the current JSON pointer. Remote obsolete tagging
follows that commit; verification failures preserve the preceding generation.

After publication and the last use of the retained view, the host releases the
server checkpoint, artwork pins, ZFS snapshot and external component copies.
It reclaims the run's compressed archive objects and copied ledgers, keeping
manifests and release receipts. Unknown files are preserved. Failed unpublished
runs retain original data for recovery. The private `active.json` journal now
selects the original run, checkpoint UUID, configuration, destination and options
after restart. It is recorded before capture and removed after publication and
release finish, or after fenced abandonment of an unsealed attempt. Once the
media selection is saved, retries skip rescanning and compaction. A current
`--defer-cleanup` request still takes precedence.

An invocation with a published but unfinished attempt only completes its cleanup;
a subsequent invocation captures newer state. Cleanup reopens the permanent
release records without requesting a new capture or requiring already released
media/artwork views. Finished run identities remain reserved. Changed
configuration/destination or ambiguous unfinished runs fail for inspection.
For an attempt that never sealed, a resumed run reads the authenticated server
status under backup/dedupe and worker exclusion, then records server abandonment
before cleaning its known components, artwork pins and owned ZFS snapshot. A
missing server record still requires this fence: it prevents a delayed capture
request from reusing the UUID. Sealed attempts continue toward publication.
The abandoned run exits with an error and no backup-success claim; the next
invocation gets a new UUID. Both server and host keep permanent failure receipts.
Unknown/legacy files without ownership evidence require inspection, and identity
records must never be removed as a shortcut to recovery.

Interrupted packing and verification use private scratch directories with
durable inode ownership records. A fully sealed bundle is promoted without
re-export; incomplete copies can be rebuilt from the retained original view.
Cleanup rejects replaced scratch roots and nested mounts, and does not follow
symlinks into source data. Upload and native-validator child processes inherit
the existing backup lock, so losing their Python parent cannot let another run
remove scratch data while those children are still using it.
ZFS commands retain the same lock through a host supervisor, including when
`sudo` closes child descriptors or the caller times out. Closing the caller's
descriptor does not unlock a surviving command. Retirement rejects replaced
directories, symlinks, nested mounts, mismatched snapshot properties/GUIDs,
foreign holds and clones; it never uses forced or recursive ZFS destruction.

The current-manifest update records its original S3 ETag and uses a conditional
write. ETag is only a concurrency token; SHA-256 remains the content check. A
retry may adopt its exact already-published bytes after a lost reply, but cannot
replace a newer publication. Immutable per-run records remain available.

## Storage and request costs

The database, original record photos/logos/covers and small restore manifests
remain in Standard; bulk media stays in Deep Archive. Unchanged encoded chunks
share immutable keys across runs. New archives use a maximum 64 MiB raw chunk
instead of 1 MiB, reducing requests for changing database snapshots. Readers
continue to accept earlier 1 MiB archives and enforce each manifest's limit.
The larger chunk is a storage/request tradeoff: an edit replaces its whole
chunk, while reducing the number of upload requests. Retention and real daily
change volume still need production-scale measurement before activation.

`state_directory/object-receipts.sqlite3` is a rebuildable upload index, not
required restore state. It is scoped by bucket and full object key. It contains
only independently verified checksums and the remote identities needed for
subsequent LIST comparisons. Entries are committed in small batches; interruption
can require verifying a few uploads again. Missing index state triggers a new
verification pass without reuploading matching objects. An invalid index or
incomplete/denied inventory stops publication. The normal backup lock excludes
cooperating writers; an inventory is not a new checksum audit or a guarantee
against unrelated concurrent cloud mutations.

Publication uses listings for unchanged archive chunks. Explicit checksum audit
and download commands still verify remote checksums directly. At the measured
258,014-object baseline, a daily listing takes approximately 259 requests rather
than one HEAD per object. At the October 2026 Oregon rates that is about $0.04
per 30 days for those listings; new uploads, unknown/changed objects, small
manifest operations and explicit audits incur additional requests. The regression
suite checks actual fake-transport operation counts across publisher restart.
See [AWS request pricing](https://aws.amazon.com/s3/pricing/) and
[LIST pagination](https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjectsV2.html).

The unreleased prototype requiring enabled bucket versioning and version-read
permissions was withdrawn. Cold-media replacement identity still needs the
immutable-key integration before cutover; stable filename keys are not proof
that an older backup will restore the original bytes. Do not activate this
staged publisher until that and the remaining cutover gates are verified.

## Configuration and development

Run `make pre-backup`, then `make validate-backup`. Tools install only in the
isolated `.local/native-backup/bin` runtime. This does not replace installed
host scripts or change any service. CI runs the same tests against fake cloud
transports; the old catalog Python package is no longer a dependency.

The package uses the existing host boto3 credential chain and rclone remotes.
Stash does not import boto3 or upload to S3. An application API key file is a
separate input for checkpoint capture, retained as a private configuration
component. Never put its value in command arguments.

Non-dry publication requires `--native-config /private/host-backup.json` or
`STASH_NATIVE_BACKUP_CONFIG`. Its JSON fields are:

| Field | Meaning |
| --- | --- |
| `format`, `version` | `org.notsafeforgit.stash.host-backup`, `1` |
| `server`, `api_key_file` | Native endpoint and private application key file |
| `state_directory` | Private persistent staging directory outside disposable `run_*` workspaces; artwork pins must share the original artwork filesystem |
| `artwork_sources` | Complete list of original Stash blob directories |
| `media` | Explicit `dataset`, `guid`, `mountpoint`, and media `relative_path` within that dataset |
| `worker_lock_roots` | Every native worker publication-lock root |
| `components` | Complete `{role, name, path}` inventory of outboxes, download archives, profiles and referenced private files |
| `recovery_roots` | Native deletion-recovery `{name, path}` bindings |
| `producer_origin` | Original endpoint used to validate producer receipts |
| `native_validator` | Trusted local native Stash executable |
| `reserve_bytes` | Optional disk reserve, default 50 GiB |
| `boundary_timeout`, `validator_timeout` | Optional deadlines, default 120 and 3600 seconds |
| `zfs_command` | Optional trusted host command vector, default `["/usr/sbin/zfs"]` |

The configuration and application key file are included automatically. Declaring
some files does not prove completeness. Reconcile production components,
including layered profile references and environment assets, against every active
worker before installation. The daily 03:00 America/Los_Angeles schedule remains
host-owned and unchanged.

## Restore and audit

`stash-s3-restore-media` validates v3 native/media references before selecting a
subset. A subset is a media-only plan and cannot inherit a whole-library native
binding. Historical text/v2 backup inputs remain readable.

`stash-s3-restore-native --manifest /private/current_manifest.json` audits native
metadata and all object SHA-256 headers. It never requests Glacier restores.
`stash-s3-audit --remote --native-checksums` includes this in the coverage report;
without the extra flag, native coverage is reported as a manifest reference only.

Download and independently verify a bundle with:

```sh
stash-s3-restore-native --manifest /private/current_manifest.json \
  --download-to /restore-work/native-archive \
  --native-validator /trusted/bin/stash \
  --producer-origin https://original-stash.example
stash-archive import /restore-work/native-archive \
  --output /restore-work/new-installation
```

The download directory must be new. All downloaded bytes are checked; native
validation and producer receipt checks run on an isolated restored copy. Neither
command activates a server or worker. Restore matching media through the media
restore tool. Mount rebinding and deletion recovery require the reviewed cutover
procedure. Retained publication proofs do not replace validation by the selected
local native binary.

Installation remains gated on complete worker/config inventory,
media-generation reconciliation, full artwork capture
timing and writer/WAL measurements, and the relocated restore and owner review in the
[transition plan](../../docs/native-archive-transition-plan.md). This conversion
performs no live cloud writes or installed host-script changes.
