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
new or reused S3 metadata object requires full-object SHA-256, exact length and
Standard storage class. ETag or size alone cannot pass. Immutable archive objects
and per-run manifests precede the current JSON pointer. Remote obsolete tagging
follows that commit; verification failures preserve the preceding generation.

After publication and the last use of the retained view, the host releases the
server checkpoint, artwork pins, ZFS snapshot and external component copies.
It reclaims the run's compressed archive objects and copied ledgers, keeping
manifests and release receipts. Unknown files are preserved. Failed unpublished
runs retain original data for recovery. The private `active.json` journal now
selects the original run, checkpoint UUID, configuration, destination and options
after restart. It is recorded before capture and removed only after publication
and release finish. Once the media selection is saved, retries skip rescanning
and compaction. A current `--defer-cleanup` request still takes precedence.

An invocation with a published but unfinished attempt only completes its cleanup;
a subsequent invocation captures newer state. Cleanup reopens the permanent
release records without requesting a new capture or requiring already released
media/artwork views. Finished run identities remain reserved. Changed
configuration/destination or ambiguous unfinished runs fail for inspection.
Attempts that never sealed still need the abandonment/pruning workflow before
production installation; removing their identity files is not a recovery method.

Interrupted packing and verification use private scratch directories with
durable inode ownership records. A fully sealed bundle is promoted without
re-export; incomplete copies can be rebuilt from the retained original view.
Cleanup rejects replaced scratch roots and nested mounts, and does not follow
symlinks into source data. Upload and native-validator child processes inherit
the existing backup lock, so losing their Python parent cannot let another run
remove scratch data while those children are still using it.

The current-manifest update records its original S3 ETag and uses a conditional
write. ETag is only a concurrency token; SHA-256 remains the content check. A
retry may adopt its exact already-published bytes after a lost reply, but cannot
replace a newer publication. Immutable per-run records remain available.

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

Installation remains gated on complete worker/config inventory, unsealed-attempt
abandonment/retention, media-generation reconciliation, full artwork capture
timing and writer/WAL measurements, and the relocated restore and owner review in the
[transition plan](../../docs/native-archive-transition-plan.md). This conversion
performs no live cloud writes or installed host-script changes.
