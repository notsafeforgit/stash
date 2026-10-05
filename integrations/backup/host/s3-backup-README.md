# S3 backup and restore

The enabled `s3-backup.timer` runs `s3_log_backup.py --compact` daily at
03:00 in the host timezone (`America/Los_Angeles`). Its persistent setting
catches missed runs after boot, and user lingering keeps it scheduled while
logged out. Best-effort deduplication runs first when scrapers are idle.
The source must be on the mounted `/tank/media` filesystem. A missing or empty source, incomplete
inventory, database failure, or exceeded deletion limit stops publication.

When the source is available, missing files and collections are assumed to have
been intentionally removed by the owner or a dedupe job. They follow normal
automatic tombstone/obsolete handling; no deletion receipts or manual approval
are required. Partial loss on an otherwise healthy source is treated the same
way, by policy.

The availability check uses `/tank/media`, the actual source dataset, because
`/tank` can remain mounted while its child dataset is unavailable. Availability
is rechecked immediately before publishing each current manifest and before
obsolete tag writes, including after reading existing object tags. An outage
stops further publication/tagging. Existing empty-source and deletion-count
circuit breakers remain in place. Objects in the current backup are protected;
untagged objects have no expiration under the verified lifecycle policy.

## Upgrade behavior

- The first run reconciles **all videos** against S3 using the existing
  `--size-only` policy. The old local video manifest did not prove upload success,
  so it is not used to seed the successful-upload ledger. Existing objects with
  matching sizes are skipped; this does not request Glacier retrievals.
- Later runs compare every video's size and nanosecond timestamps with its last
  successful reconciliation. Failed uploads remain pending regardless of age.
  `--init-run` explicitly reconciles all videos again.
- Every metadata unit is checked for additions, edits, and deletions. Existing
  image comparison remains size-based; NFO content is hashed. Same-size video and
  image replacements remain subject to these existing comparison policies.
- Legacy archives with matching saved fingerprints can seed the new file index.
  An unindexed legacy archive whose fingerprint differs, a missing baseline or
  delta, and a reactivated retired unit require a fresh baseline uploaded from
  local files. No existing archive is thawed to rebuild it.
- SQLite gains `video_uploads`, `pending_gc`, and a `unit_state.retired` column.
  This is applied by the next backup run. Existing rows are preserved.
- `current_manifest.json` in the Standard bucket contains the complete directory
  mapping and ordered base/delta chains. Each run also saves a copy under
  `manifests/runs/<run-id>/manifest.json`. `current_manifest.txt` remains available
  as a compatibility allowlist.
- Remote cleanup follows successful publication. Pending cleanup is persisted
  and retried; keys in the current manifest are protected. A cleanup failure can
  report a failed service even though the new manifest was already published.
- Cleanup sets `obsolete=true` on unused archives and deleted videos, preserving
  unrelated tags. It never directly deletes S3 objects. The published manifest
  excludes tombstoned videos while Lifecycle waits to expire their objects.
- A returning video has its obsolete tag cleared before the size-only copy.
  Failed tag removal stops publication; interrupted cleanup remains discoverable
  in the ledger. Clearing a tag reads object metadata only, without retrieval.
- `--dry-run` reads remote inventories and operates on a temporary database copy.
  It does not upload, delete, tag, request thawing, or change the live ledgers.
- `--defer-cleanup` performs and publishes a real backup, but leaves all remote
  cleanup queued. It does not mark objects obsolete. Returning video paths still
  have obsolete tags cleared before reuse to protect the published generation.
  This permits separate verification before lifecycle tagging during maintenance.
- The installed rclone version uses newline-separated lists. Leading spaces and
  `#`/`;` characters are preserved with `--files-from-raw`. Names containing CR or
  LF are rejected before publication, rather than silently skipped.

## Restore planning

```sh
python3 ~/bin/s3_restore_performer.py 'directory name'
```

This reads metadata from the **Standard** bucket and prints a plan. Metadata
archives are selected by default; add `--include-videos` to include video objects.
If the new JSON manifest has not yet been published, the helper can adapt the
existing text manifest plus the SQLite and footprint ledgers in Standard storage.
Unknown or incomplete legacy archive chains are rejected.

For completely offline planning:

```sh
python3 ~/bin/s3_restore_performer.py 'directory name' --manifest /path/manifest.json
```

A legacy text manifest additionally requires `--ledger-db /path/state.sqlite3`
and `--footprints /path/tarball_footprints.jsonl`.

The restore helper requests a potentially billable Glacier **Bulk** thaw only
when explicitly invoked with `--request-thaw`. `--lifetime` controls how many days
the thawed copy stays available. `--check-status` reads status without requesting
a thaw. `--download --destination /new/empty/directory` downloads already-thawed
objects and reconstructs the selected files; it never initiates a thaw itself.

## Offline reconstruction and verification

Local object files must retain their S3 key paths under the object directory,
for example `/path/objects/tarballs/base/...tar`. Destinations must be new or empty.
The output preserves source-relative directory paths.

```sh
python3 ~/bin/s3_restore_performer.py 'directory name' \
  --manifest /path/manifest.json \
  --objects-dir /path/objects \
  --destination /path/reconstructed
```

The helper extracts each base, applies its ordered deltas and per-delta deletions,
and decompresses NFO files. It rejects missing objects, mismatched delta metadata,
unsafe paths, symlinks, and overlapping archive units.

To compare the reconstruction with a known reference tree:

```sh
python3 ~/bin/s3_test_restore.py 'directory name' \
  --manifest /path/manifest.json \
  --objects-dir /path/objects \
  --destination /path/verified-restore \
  --expected-dir /path/reference
```

The reference tree must contain exactly the selected source-relative files.
Add `--include-videos` to both selection and reference when comparing videos.
Verification compares file lists and SHA-256 content hashes, including the
absence of deleted files. The test helper has no thaw or network action.

## Regression suite

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s ~/bin/s3-backup-tests -v
```

The backup tests use temporary local object stores, block real subprocess/AWS
client calls, and cover upload retries, full reconciliation, deletion-only changes,
publication failures, state retirement/reactivation, compaction, legacy format
adaptation, and complete local reconstruction.


## Dedupe before backup

`dedupe-scraped-content` retains the oldest local regular copy by filesystem
creation time. `fclones remove --priority newest` selects newer copies for
removal; the old `oldest` setting did the opposite. Website-provided modification
times do not decide which copy survives. Symlinks are excluded from duplicate
groups so they cannot displace the last regular file. Existing scraped-directory
scope and NFO/partial-file exclusions are preserved.

The pre-backup check bypasses the ordinary 24-hour dedupe cooldown but still
requires idle scrapers. Dedupe and backup also share an exclusive lock, including
manual invocations. The existing best-effort service behavior remains: a busy
scraper can cause dedupe to be skipped while backup proceeds.

Keeping the old path helps the backup avoid sending the same image again under a
new scraped path. This only compares copies still present locally and inside the
dedupe scope. It cannot recognize an image retained only in Glacier, and archive
compaction can still upload existing images as part of a replacement baseline.

The dedupe regression tests execute native fclones only against temporary test
files. Audit tests use a fake S3 client and never retrieve archived data.

## Read-only coverage audit

The audit compares the source tree with a locked snapshot of the local ledgers.
Use `--remote` to also LIST archive object metadata and read the published
manifest from the Standard bucket. It never requests a thaw, downloads archive
payloads, writes S3 data, or removes source files. Reading S3 metadata still uses
ordinary S3 API requests.

```sh
AWS_SHARED_CREDENTIALS_FILE=~/.config/s3-backup/aws-credentials \
AWS_CONFIG_FILE=~/.config/s3-backup/aws-config \
AWS_PROFILE=s3-backup-daily \
python3 ~/bin/s3_backup_audit.py --remote \
  --output-dir ~/s3-backup-audits/$(date +%Y%m%d-%H%M%S)
```

Each explicitly requested audit writes `report.md`, `summary.json`, and
`issues.jsonl`. Its working ledger snapshots and fetched manifests use temporary
storage and are removed on success or failure; the full object listing is kept
only in memory. Use a fresh output directory for each run.
Without `--remote`, the audit checks only local state and cannot establish what
is currently published or present in S3.

Every image/NFO path is checked for additions and deletions. Indexed NFO files
are compared by size by default; add `--hash-nfo` to also read and hash their
contents, which can be slow across the whole library. Unindexed legacy units
still require NFO reads to verify their saved fingerprints. Backup itself
continues hashing NFO content regardless of this audit option.

Pending image/NFO deletions are paths in the published generation's file index
that no longer exist locally. The next successful backup records them in a delta,
replaces the baseline with the current file set, or retires an empty unit. The
audit itself does not perform that backup. Legacy archives without a member index
can only be compared by their saved fingerprint; changed units need a new baseline
from local files. Matching fingerprints and S3 object presence are metadata checks,
not verification of image bytes inside cold archives.

Deletion records prevent deleted images from reappearing in a reconstruction.
Their bytes remain in older archive objects until compaction and lifecycle cleanup.
Archives outside the current manifest can be retained history, so the audit does
not classify them automatically as safe to delete.

## Lifecycle policy (verified 2026-09-28)

The live policy expires all objects tagged `obsolete=true`, including loose
videos, at 180 days after object creation. It removes matching noncurrent versions after
one day, and later removes empty delete markers. The 180-day clock does not
start when the obsolete tag is added: an older object can become eligible as
soon as it is tagged. Maintenance marks obsolete objects with tags while
leaving deletion to S3 Lifecycle, without direct `DeleteObject` requests.

The approved expansion was applied and read back successfully. Bucket
versioning is enabled. The 50 obsolete loose videos from the baseline rebuild
are now covered by the same lifecycle rules as obsolete archives. Empty
delete-marker cleanup covers the whole bucket. The daily profile still cannot
change lifecycle rules; administrative access was used for this update.

## Rich scrape metadata catalogs

The scheduled backup also snapshots `/tank/media/scrape_metadata` and stores its databases in S3 Standard under `scrape-catalogs/`. The media manifest includes the catalog snapshot reference and checksum. Missing catalogs or failed catalog uploads stop publication and obsolete tagging. Dedupe preserves post/NFO provenance before removing duplicate media. See `/home/andrew/src/private/scrape-catalog/README.md` for the schema, creator linking, and metadata recovery. NFO generation is retired; Stash reads the catalogs through the Catalog Metadata plugin.

## Local temporary files

Each backup creates a disposable workspace under `/home/andrew/s3_backup_tmp/`
while holding the backup lock. Catalog snapshots, upload object links, copied
ledgers, and manifests are removed on normal exit, including handled failures.
The next real backup removes abandoned workspaces left by power loss or a forced
kill; dry runs leave other workspaces alone. No local snapshot history accumulates.

Live catalogs and `/tank/media/backup_ledgers/` remain authoritative operational
state. In particular, `pending-archives/` holds uploads awaiting a durable commit
and must survive failures. This cleanup does not touch those retry payloads or
change retention of backups already uploaded to S3.
