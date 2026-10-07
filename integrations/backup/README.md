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

`current_manifest.json` version 4 binds the media selection to one native archive
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
and per-run manifests precede the current JSON pointer. A successful pointer
commit receives an immutable backup-history receipt before local release or
cleanup is eligible. Remote obsolete tagging follows that receipt and retention
verification; verification failures preserve the preceding generation.

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

## Manifest capacity and streamed downloads

Publication, run recovery, history, audit and restore share a **512 MiB** limit
for complete host master JSON. The embedded `s3-media.json` selection has another
1 MiB of envelope allowance. Portable `artifacts.jsonl` inventories have a
separate **1 GiB** host transport limit; their records are already validated as
a stream. The portable archive's small manifest, verification document and
per-inventory-record bounds remain unchanged. These limits are in
`host/manifest_limits.py` and do not change the stored format versions.

Native metadata and encoded-object downloads stream in 1 MiB reads within one
GET per object. Each download verifies the declared size and complete SHA-256,
flushes its temporary file, then publishes the new destination. Truncation,
excess bytes, wrong hashes, network failures and insufficient reserved disk
space discard that temporary file. Existing destinations are never replaced.
JSON masters still need an in-memory parsed representation; streaming the
artifact inventory does not make all backup memory use constant.

The [2026-10-05 scale rehearsal](../../docs/native-backup-manifest-scale.json)
copied the current ledger and video manifest under the backup lock for 1.22
seconds, then released it. Its 272,373 video paths and sizes, 1,328 archive units
and 281,421 representative objects produced valid masters of 137,128,906 bytes
with SHA-256 descriptors and 128,967,697 bytes with CRC64NVME. Content identities,
checksums and tar sizes were synthetic; these are supported representations of
the library's shape, not a remote-object audit. The SHA-256 representation was
rejected by the old 128 MiB cap.

The larger case then passed real JSON/archive codecs, publication/history and
streamed download/portable restore through a fake S3 transport, restoring the
137,128,318-byte embedded selection with the exact original hash. That check
took 63 seconds and peaked at about 2.12 GiB RSS. It used the existing tiny
native-schema verification fixture; the separate populated-database restore
rehearsal remains required evidence. No media bodies, cloud objects or live
configuration were changed.

## Snapshot history and media retention

Native backups retain the seven most recent successful snapshots by default,
including the current publication. Configure `retention` as
`{"keep_last": 7, "pins": ["<archive UUID>"]}` to retain additional named snapshots.
Pins use the native archive UUID from `native_archive.archive_uuid`, not a run
name or checkpoint UUID. `keep_last` accepts 1–365; pins are additional to that
count. Capture timestamps order snapshots, with UUIDs breaking ties and the
current publication always counted. No snapshot expires merely because several
scheduled runs failed or did not execute.

Immutable receipts live under `native-archives/history/publications/<uuid>.json`
in the metadata bucket. Each binds the successful run's exact master manifest
and native archive reference. A prepared per-run manifest alone does not prove
successful publication. If the current pointer succeeds but the receipt write
fails, the host keeps its original capture and retries the receipt before
releasing it; it does not create a newer snapshot under the same identity.

Retention first verifies the active receipts and selected media graphs, including
the cold bucket/prefix and presence in the run's complete cold inventory. It then
records immutable `history/retirements/<uuid>.json` decisions for old snapshots.
The current snapshot and pins cannot retire. Unknown or already-retired pins,
changed/missing history, invalid inventories and mismatched storage bindings
stop cleanup. A retired snapshot is no longer promised to be restorable;
increasing `keep_last` or adding a pin later cannot resurrect it. Existing
unregistered native backups need explicit history reconciliation before this
writer replaces an earlier native publisher. Compatible media-only backups do
not become native snapshots automatically.

Cold-media cleanup protects the union of every retained snapshot's video,
base-tar and delta-tar keys. A file removed from the live library stays protected
while an older retained snapshot needs it. Its cleanup intent and source-path
evidence remain queued until the last reference retires. All existing optional
legacy/orphan cleanup paths honor that protection too. `--defer-cleanup` records
the new successful publication but defers snapshot retirement and media tagging.
Retirement changes no cold object bytes or storage class, and leaves the existing
Deep Archive obsolete-object lifecycle in charge of physical expiration.

`state_directory/snapshot-history` is a private, destination-scoped, rebuildable
cache. A complete paginated history LIST verifies unchanged receipt identities;
only new/changed receipts and uncached master graphs are downloaded. Large
derived graphs are removed after retirement; small identity receipts remain.
Losing the cache rebuilds protection from the remote receipts and verified
masters. Corrupt caches stop cleanup and require inspection/rebuilding.

Set `standard_cleanup: true` only after reviewing and installing the metadata
bucket policy below. The default is false until that activation review. Cleanup
verifies the bucket's lifecycle and versioning configuration on every run; it
never modifies either. Extra enabled lifecycle rules require review rather than
being silently accepted. Versioning does not have to be enabled. The daily
publisher needs read access to these two bucket configurations, plus object
tag reads/writes for owned native keys. It does not need permission to change
bucket policies or delete objects.
For an already-versioned bucket, tag requests target the checked version and
also require the corresponding object-version tagging permissions; this does
not enable versioning or copy existing data.

Standard cleanup protects every active snapshot's full native object graph,
including pins. It tags only chunks found in retired snapshots and their four
per-run metadata files (master, native manifest, inventory and verification).
Unknown uploads are left alone. Chunk-retirement receipts under
`native-archives/cleanup/completed/<uuid>.json` are permanent and precede tagging
the old inventory. A failed/lost tag request can therefore resume, and rebuilding
the local cache does not require an inventory that has already expired.

Reusing a retired chunk removes its retirement tag and checks its checksum and
presence before publication. Unrelated tags survive. If expiration won the race,
the publisher uploads the retained local bytes again. Receipt schema 2 caches
verified live/retired tag state together with object identity; cleanup durably
invalidates this state **before** each tag change. A lost cache or schema-1
upgrade requires one reconciliation pass. Normal unchanged runs use paginated
listings, without per-object tag or checksum requests. The host backup lock and
exclusive ownership of the retirement tag are required.

Released local runs lose only their verified `master.json`,
`prepared-master.json`, `catalog.json` and `archive/artifacts.jsonl` after
retirement. A small durable intent lets that cleanup resume after a crash.
Identity, release, completion and publication receipts remain, as do unknown
files and every unfinished/active attempt. Large derived native graphs are
pruned as well. Native publication already captures the exact host ledgers in
the portable archive, so it no longer uploads duplicate `ledgers/latest/` and
`ledgers/runs/` copies. Existing compatible ledger backups remain untouched for
historical restores and need separate inventory/retirement review.

Generate the reviewable policy for the exact native metadata prefix (empty here):

```bash
python -c 'import json; from native_cleanup import lifecycle_rules; print(json.dumps({"Rules": lifecycle_rules("")}, indent=2))'
```

Run that command in the installed host backup environment. It only prints JSON.
The dedicated `stash-native-retired=true` rule expires tagged objects after
one day of **object age**, not one day after tagging. Additional rules bound
noncurrent versions and expired delete markers under the native archive,
per-run manifest and current-pointer prefixes. They do not change Deep Archive
media lifecycle rules. See the checked-in
[empty-prefix example](native-lifecycle.example.json) and AWS's
[expiration and tag evaluation rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-expire-general-considerations.html).
Do not install this example over unrelated rules without review.

The inspected metadata bucket has no lifecycle rule, and its versioning status
still needs authorized verification. Policy installation, production activation
and observed expiration remain cutover gates; a retirement receipt alone does
not prove that storage bytes have expired.

## Storage and request costs

The database, original record photos/logos/covers and small restore manifests
remain in Standard; bulk media stays in Deep Archive. Unchanged encoded chunks
share immutable keys across runs. New archives use a maximum 64 MiB raw chunk
instead of 1 MiB, reducing requests for changing database snapshots. Readers
continue to accept earlier 1 MiB archives and enforce each manifest's limit.
The larger chunk is a storage/request tradeoff: an edit replaces its whole
chunk, while reducing the number of upload requests. Standard reclamation now
has retry/reuse and request-count tests; cloud-policy activation and real daily
change volume still need validation.

The [2026-10-05 encoder measurement](../../docs/native-backup-cost-measurement.json)
used a disposable copy of the verified schema-1000077 rehearsal database. It
made no cloud requests and left the source unchanged. The 20,265,979,904-byte
database compressed to 5,242,807,047 bytes in 302 objects. Controlled SQL image-title
updates produced these additional objects relative to earlier scenarios:

| Scenario | New objects | New compressed bytes |
| --- | ---: | ---: |
| Initial snapshot | 302 | 5,242,807,047 |
| One title edit | 8 | 188,399,162 |
| 100 spaced title edits | 24 | 600,577,622 |
| 1,000 spaced title edits | 24 | 600,012,930 |

The edit batches are cumulative; these are synthetic examples, not measured
daily production churn. Each encoding took about 159–187 seconds. If all 302
database objects changed daily, 30 days of their PUTs and verifying HEADs would
cost about **$0.049** at the inspected Oregon rates ($0.005 per 1,000 PUTs and
$0.0004 per 1,000 GET/other requests). This excludes listings, other components,
retirement operations, retries, audits and restores; it is not the entire S3 bill.
Rates come from the [official regional price list](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonS3/current/us-west-2/index.json)
(publication 2026-09-28). Actual retained storage also depends on completing the
Standard reclamation policy above.

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
permissions was withdrawn. Cold publication now preserves replacement bytes under
separate keys and reuses verified existing uploads. Production inventory, retention,
cost measurements and restore/cutover review still precede activation.

The [2026-10-07 operational drill](../../docs/native-backup-operational-rehearsal.json)
used the actual host session and S3 in an isolated prefix, then restored the
native fixture database, producer queues and real host ledgers. After reopening
the publisher, its 23 unchanged encoded objects needed one LIST and no individual
checksum/tag reads or uploads. Three small per-run metadata files still needed
nine HEADs and three tag reads. The separate complete production bucket inventory
used 315 LIST requests, projecting about $0.05 per thirty daily inventories at
the observed Oregon price. This excludes other operations and storage, and is
not a measurement of native daily churn. Full populated restore and production
activation remain pending. The inventory also found 57,725 required older videos
without additional S3 checksums. Their adoption now passes regression tests and
three real read-only samples; whole-library reconciliation remains a release gate.

## Immutable media publication and restore

The staged publisher writes version 4 manifests. Readers also retain historical
text/v2/v3 support; existing backups are not rewritten. A retained unpublished
v3 native attempt must finish with its original writer before this writer is used.

A version 4 manifest separates each video's relative restore `path` from its
S3 `key`. Several paths can reference one object; planning, status, thaw and
download operate once per distinct key. Each required video/base/delta key has
exactly one descriptor in `objects`: size, cold storage class, verified checksum
algorithm/value, and an optional independently checked local SHA-256.
Video descriptors require that local SHA-256. New content keys use
`media/sha256/<sha256>` and require full-object S3 checksums. Historical filename
keys remain valid when their checksums can be checked against complete local
bytes. Unsupported evidence is an error for review, never authorization to
upload existing media again.

For old videos with no additional checksum headers, the video adoption path can
use a matching single-part MD5 ETag or a multipart MD5 ETag with verified 5 MiB
or 16 MiB part boundaries. It also records the independently computed local
SHA-256, which all subsequent byte verification requires. These descriptors use
`s3-etag-md5` or `s3-etag-multipart-md5`; the latter includes its `part_size` in
bytes. Shape, size, part count and uploader-supplied metadata cannot establish
the match. Only plaintext/SSE-S3 objects qualify; SSE-KMS and SSE-C are rejected.
See the [S3 ETag contract](https://docs.aws.amazon.com/AmazonS3/latest/API/API_Object.html)
and [multipart calculation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/checking-object-integrity-upload.html).
Malformed, unsupported or composite additional checksums do not fall back to
ETags. New uploads, new content-key adoption and archive publication retain
the full-object S3 checksum requirement, including interrupted-upload recovery.

The existing `TAR_DELTA_DB` ledger now contains the cold-store binding, verified
object receipts, current video path bindings and pending path associations.
One complete paginated inventory serves verification and maintenance for each
run. A previously verified, unchanged object needs no HEAD, upload or tag request.
Lost checksum receipts require re-verification; matching bytes are reused.
Do not discard path associations or pending cleanup as if they were disposable
checksum cache entries. The ledger is included in the native backup.

New or replaced videos use content keys and explicitly upload to Deep Archive
with rclone's immutable mode. Identical files and renamed paths reuse one object;
historical filename keys are adopted only after comparison with local bytes.
New tar bases/deltas keep their append-only keys and receive the same independent
full-checksum verification. Checksum metadata written by the uploader alone is
not accepted as proof. Known objects with changed bytes stop publication.

Cleanup starts after publication and protects every retained snapshot's object
keys, including objects shared by multiple paths. Pending upload intentions retain their paths
even after interruption. A file that returns or changes after the captured view
defers obsolete tagging; the path is rechecked after the remote tag read. Removed
path bindings are retired independently of shared objects. Superseded bytes remain
subject to the existing obsolete-object lifecycle; this is not indefinite retention
of every historical backup. Activation of Standard-object reclamation remains a
cutover gate.

The local NUL video manifests contain source paths. `current_manifest.txt` is
only a deduplicated object allowlist; content-addressed keys require the JSON
manifest to reconstruct paths. Historical filename-based text restores remain
supported. Retrying a retained v4 selection verifies every selected video and
archive without rescanning newer files or silently repairing missing objects.

`media_store` binds the bucket and prefix. A full native manifest uses
`scope: native` and includes those bindings, object identities and restore paths
in its version 2 media-selection digest. A selected subset uses `scope: media`
and retains only its required descriptors, without a whole-library native claim.
No S3 VersionId or bucket-versioning permission is required by this format.

Offline reconstruction verifies every required object's bytes before creating
output, then checks source-file identities through reconstruction. Duplicate
paths, unsafe paths and collisions with archive members fail. A failure during
extraction can leave a partial new output directory; it never reports a complete
restore or activates that directory. Remote downloads verify response checksums
and streamed bytes before retaining a file. Missing/changed objects fail without
substituting newer content, requesting a thaw or uploading a replacement.

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
| `worker_inventory` | Optional path to the worker dependency declaration described below; resolved under publication barriers and retained for retries |
| `retention` | Optional `{keep_last, pins}` policy; default seven successful snapshots with no extra pinned archive UUIDs |
| `standard_cleanup` | Optional boolean, default false; enable after reviewing/installing the native lifecycle policy and granting configuration/tag reads and tag writes |
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

Declare an external SQLite database such as n8n's `database.sqlite` with
`{"role":"operating_database","name":"n8n.sqlite","path":"/private/n8n/database.sqlite"}`.
The component stage takes a WAL-aware SQLite snapshot under the worker barriers,
retains its integrity/foreign-key checks and reuses its original bytes on retry.
Do not copy the live database as `operating_state` or include its WAL/SHM files.
Include n8n's encryption/configuration files and any external execution payloads
separately. These inputs must match the restored workflow database and producer
state before workflows resume. The native worker barriers do not stop unrelated
n8n nodes, pruning or configuration edits; coordinate those writers for a full
recovery boundary. An archive export never activates or resumes n8n.

### Worker dependencies

`stash-s3-inventory --inventory /private/workers.json --output /private/inspection.json`
checks a declaration without starting workers, contacting Stash/S3, or printing
resolved credentials. The output must be a new file. A declaration has this shape:

```json
{
  "format": "org.notsafeforgit.stash.worker-inventory",
  "version": 1,
  "workers": [
    {
      "name": "host",
      "profile": "/private/host-worker.json",
      "home": "/home/archive",
      "working_directory": "/media",
      "outboxes": ["/private/outbox.sqlite"]
    }
  ]
}
```

Each entry represents a declared deployment/profile. Multiple entries can share
outboxes, archives and private files; each path is captured once. Include download,
enrichment, account-listing and detail-verification profiles. Download profiles
contribute their identity-checked publication root; `lock_roots` can declare
additional roots. All such roots must already be covered by the host backup's
`worker_lock_roots` before inspection.

For containers, `path_mappings` is an array of `{"from": "/container/path",
"to": "/host/path"}` mounts. The longest matching prefix wins. Profile binding
paths resolve relative to the profile directory, while cookie/command paths use
the declared worker home and working directory. The inspector never uses its own
ambient home, working directory or environment values to guess a worker's paths.

The resolver follows private JSON files and layered references, checked helper
assets, cookie-file paths (including yt-dlp arguments), and download archive
templates with a fixed directory. New matching archive files are included on
each new backup. `.bak` files are excluded unless the configured template matches
them. Missing files, changed helper digests, symlinks and unresolved dynamic paths
stop inspection. Browser credential stores require a portable cookie-file policy.
An environment binding requires an `environment` entry mapping its variable name
to the explicit JSON `file`/`pointer` used to provision it. Capture dotenv files,
launcher units, source lists, reviewed n8n workflow exports and other operating
inputs through the host's explicit `components` list.

For changing directories of external state, add `state_directories` to one
worker declaration, for example
`"state_directories": ["/home/node/.n8n/binaryData"]`. The inspector applies that
worker's container mappings, includes every regular file as `operating_state`,
and retains empty directories, membership, identities and streamed checksums.
The combined state-tree inventory is bounded to 16,384 files/directories;
binary payloads do not inherit the source-plan JSON size limit. New backups
enumerate newly created files. A sealed retry uses the original enumeration and
captured bytes even after live files move or disappear. Changed files or tree
membership before the native checkpoint stop the capture.

Declare only the needed state directories, such as n8n's `binaryData`, rather
than the whole application directory containing caches and logs. Live SQLite
databases and their WAL/SHM files must stay outside these opaque trees; declare
the database separately as `operating_database`. Writer coordination and
validation that every database-referenced payload exists remain the enclosing
deployment's responsibility.

For a download worker that runs `stash-n8n-sources`, add
`"source_management": "/private/source-management.json"` to its declaration.
The inspector follows that runtime's current Reddit/Twitter subscription lists,
metadata defaults, shared lock directory and complete operation-state tree,
including unfinished requests, plans, receipts and empty directories. Container
mount mappings apply to these paths as well. The runtime must identify the same
media root as the download profile. Its publication barrier must be included in
the host's `worker_lock_roots`, even when different from the download lock root.

The retained version-2 inventory records tree membership and file checksums;
version-1 inventories remain readable. New captures reject changed membership,
content, symlinks, special files and missing dependencies before requesting the
server snapshot. Sealed retries use the original staged state without consulting
later live operations. Reports contain paths, identities and hashes, never plan
contents or credentials. Explicitly restore the retained source-state files and
recreate their private directories alongside the matching lists, producer
outboxes and native checkpoint before restarting source-management commands.

With `worker_inventory` configured, the host resolves this closure while holding
worker publication barriers. It retains the report with the run and adds every
dependency to the component stage. Parsed profile/private/helper hashes must match
the actual staged bytes before requesting the native checkpoint. A retry uses
the original report and staged files, even if live profiles or archive membership
have changed. The report itself is included in the native archive.

This proves the dependencies of declared workers. It does not discover every
running process, provision outboxes/tokens, validate installed runtime versions,
or prove that the declaration covers all active launchers. Reconcile that scope
against host services, manual entrypoints and published n8n workflow versions
before cutover. Standalone inspection does not hold publication barriers and is
not a coordinated backup.

## Restore and audit

`stash-s3-restore-media` validates v3/v4 native/media references before selecting
a subset. A subset is a media-only plan and cannot inherit a whole-library native
binding. Historical text/v2 backup inputs remain readable. Default planning
does not construct a cold-storage client. For v4, explicit `--check-status`,
`--request-thaw` and `--download` use the manifest's bound bucket/prefix and
verify its object checksums. A download never initiates a thaw itself.

`stash-s3-restore-native --manifest /private/current_manifest.json` audits native
metadata and all object SHA-256 headers. It never requests Glacier restores.
`stash-s3-audit --remote --native-checksums` includes this in the coverage report;
without the extra flag, native coverage is reported as a manifest reference only.
For v4 media, `stash-s3-audit --remote` uses the bound store's paginated inventory
and checks restore paths separately from object keys. Add `--media-checksums`
only for an explicit checksum HEAD audit of the selected cold objects;
this adds one request per distinct object. It never reads cold payloads or
requests a thaw. Historical media manifests lack those checksum descriptors
and cannot use that option. Neither checksum audit runs as part of the normal
scheduled backup.

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
