# Native physical file deduplication

The development backend implements a preview/apply service for redundant
physical media files in schema 1000095. The `stash-dedupe` host client is available
in the native producer package. The installed host launcher still uses catalogs;
its deployment and associated scan/sidecar-cleanup handoff remain outstanding.
This service is not a scene/image merge operation.

An automatic candidate must contain two distinct, indexed, nonempty regular
video or image locations under one active, reviewed media root. Both must belong
to the same single scene or image. The service keeps that media UUID, selected
metadata, performer links, source-post and album relationships. If the removed
location is the primary file, the surviving location becomes primary through
the ordinary media repository and its after-commit notifications.

Different/ambiguous owners, unindexed files, ZIP members, gallery-file links,
caption-bearing duplicates, hard links and excessive source history require
review or intake first. The service does not infer a media merge, discard
captions, or treat a filename/fclones hash as proof of identical bytes. Unrelated
media entries remain available for a separate explicit merge decision.

## Application API

These routes are under `/api/v3/archive`, behind application authentication and
the same-origin mutation guard. Source producer tokens do not grant access.

| Method and path | Purpose |
| --- | --- |
| `POST /file-deduplication/preview` | Inspect the exact root-relative pair without changing the database or files. |
| `POST /file-deduplication/apply` | Verify both complete files and commit the reviewed removal. |
| `GET /file-deduplication/requests/{request_uuid}` | Recover the original committed result after an uncertain response. |

Preview input:

```json
{
  "root_uuid": "<reviewed media-root UUID>",
  "keep_path": "account/first.mp4",
  "remove_path": "account/duplicate.mp4"
}
```

The result contains `eligible`, `blocked_reason`, a `signature`, the media/file
UUIDs, affected source-match count, duplicate byte size and whether primary
selection changes. `eligible` means the associations permit verification; it
does not assert byte equality. Linux previews use descriptor identity, size,
mtime and ctime instead of hashing whole videos. Other platforms without a
change token require a preview digest.

Apply repeats the input and adds `request_uuid` and the exact preview
`signature`. Persist that body before sending it. Repeating the same committed
request returns its original receipt, even after later file changes. A reused
UUID with different input fails. A stale preview or differing bytes fails
without committing the removal. After a known rejection, prepare a fresh
preview and request; an uncertain response must first recover its saved request.

Neither endpoint reports queued admission as completion. Apply is synchronous
and may take time to read large videos. Cancellation before commit rolls back
the operation; a lost response after commit is recovered through the receipt.
Operational responses omit the internal proof, absolute host paths and file
technical metadata.

## Verification, provenance and recovery

The signature covers both file generations and revisions, the media owner,
primary selection, root revision/binding, original file metadata and up to
1,000 affected source matches. Full server-side SHA-256 verification happens
outside the writer transaction with both descriptors held open. A separate
verification transaction retains their content proofs while both original
paths still exist; rejected deduplication may therefore leave useful hash
evidence without a deletion receipt.

The removal transaction repeats the review checks, retains a signed
`file_deduplications` receipt, and adds explicitly derived source matches to the
surviving file generation. Original observations and matches remain unchanged.
The removed file UUID redirects to the survivor; its old generation proofs and
path-removal fence remain available. Source content claims do not gain a
retroactive checksum. Files associated with different posts can share the same
surviving location without collapsing those posts.

Filesystem removal uses Stash's managed deletion journal and configured trash.
After staging the redundant entry, the service verifies the staged inode and
hashes it again: rename changes ctime, so ignoring that change would conceal a
write during staging. This final read holds the writer transaction. Both the
survivor and staged entry are rechecked before commit. A failure restores staged
files and rolls back associations; a process death is recovered from the
database's commit marker. Unfinished trash transfers retain their journal.

The database snapshot includes deduplication receipts and provenance. Startup
validates their signatures and retained verified-content references.
Anonymisation removes the private receipts before source/content evidence.

## Native host client

`stash-dedupe` discovers candidates with `fclones group`. It retains the oldest
file by mtime, with the relative path breaking ties. It never invokes fclones
removal. The default scraped-folder scope matches the existing host launcher;
`--all-content` includes other supported media under the reviewed root. Empty
files, sidecars and partial downloads are not removal candidates. Symlinks,
escaped paths, different filesystems, changed sizes and repeated report entries
are rejected before submission. The report is bounded to 64 MiB and 100,000
pairs; discovery has a two-hour deadline.

Example after the native cutover, using an application API key supplied through
`STASH_API_KEY`:

```sh
stash-dedupe --endpoint http://localhost:8009 \
  --root /tank/media/porn --root-uuid REVIEWED_ROOT_UUID \
  --state-dir /private/native-dedupe \
  --library-lock /tank/media/backup_ledgers/.backup_run.lock \
  --lock-root /inventoried/host-worker-locks \
  --lock-root /inventoried/n8n-worker-locks \
  --fclones /home/andrew/.cargo/bin/fclones
```

Supply **every** worker lock root from the deployment inventory. The client
acquires its state lock, the existing backup/dedupe lock, and all native-worker
publication barriers before discovery or API calls. It retains those locks
through receipt recovery and removal, rechecking directory/mount and lock-file
identities before operations. These cooperative barriers do not stop legacy
downloaders; activate this caller with the native worker handoff.

The private `dedupe.sqlite3` journal retains each run's manifest and immutable
per-pair intents/results without creating a file for every request or receipt.
SQLite transactions persist requests before submission and atomically record
completion; a partial journal write cannot publish a new active run.
The manifest fixes the endpoint, root identity, scope and lock roots. Every
request is durably saved before Apply. On restart, the same invocation resumes
the saved run before starting any new discovery, including when a successful
removal's response was lost. It checks the original receipt before replaying an
unchanged request. An invalid response or request-UUID conflict keeps that intent
pending. It never replaces an uncertain request with a new UUID.

Pairs are previewed sequentially: a preceding primary-file change can advance
the media owner's revision. Known stale/unequal-byte rejections and ineligible
owners become retained review outcomes; they are not counted as removals.
An active run is retired only after every pair has either a validated committed
receipt or a known review outcome. Completed runs remain in the same journal
for inspection and backup; do not discard pending state or change its configured
endpoint/root to bypass a failed recovery. A later completed invocation can
discover and review current candidates again.

The JSON summary distinguishes `committed`, `review`, `pending`, `finished` and
`all_removed`. Exit codes are 0 for a run without review cases, 3 for completed
processing with review cases, 2 for a busy state/library lock, and 1 for failure
or uncertainty. `--request-timeout` defaults to 900 seconds; a timeout retains
the original intent. `--report` accepts a bounded saved fclones JSON report for
fixture/reviewed operation, with the same path checks and server byte proof.
`--key-file` can supply a private application key file instead of `STASH_API_KEY`.
The file must be owned by the invoking user with no group/other access; the key
is read directly for requests and is not exported into fclones' environment.

## Scheduled and pre-backup launcher

`stash-dedupe-host --config /private/dedupe.json` uses this explicit configuration:

```json
{
  "format": "stash-host-dedupe-v1",
  "endpoint": "http://localhost:8009",
  "root_uuid": "REVIEWED_ROOT_UUID",
  "root": "/tank/media/porn",
  "state_dir": "/private/native-dedupe",
  "library_lock": "/tank/media/backup_ledgers/.backup_run.lock",
  "lock_roots": ["/inventoried/worker-locks"],
  "api_key_file": "/private/stash-application-key",
  "fclones": "/home/andrew/.cargo/bin/fclones",
  "stamp_file": "/run/user/1000/dedupe-last-run"
}
```

The scheduled launcher applies the existing 24-hour completion cooldown. A
pending run always resumes despite a recent timestamp. `--before-backup` skips
the cooldown to discover files added since the preceding maintenance pass.
Both modes retain state/library/publication exclusion; busy state or library
locks skip without updating the timestamp. Completed review cases remain
explicit in JSON and permit the backup to continue. Failed/uncertain work exits
1 and does not advance the timestamp. This host wrapper returns 0 for a
completed maintenance pass or a documented skip; the underlying diagnostic CLI
retains its separate review/busy exit codes.

The packaged `systemd/stash-native-dedupe.service` and `dedupe.env.example` use
the installed producer runtime. The staged host replacement keeps the existing
half-hour timer and daily cooldown, and the pre-backup wrapper invokes
`stash-dedupe-host --before-backup` before backup takes the shared library lock.
It has no catalog writer or global cleanup operation. Complete direct-file
scan scheduling before dropping the old post-dedupe scan trigger.

Declare this runtime under the worker inventory's top-level `maintenance` list:

```json
{"kind": "file_deduplication", "config": "/private/dedupe.json"}
```

The resolver requires the same root as an inventoried download worker and the
complete set of worker publication locks. It includes the runtime configuration
and private application-key file, and captures `dedupe.sqlite3` as an
`operating_database` through SQLite backup, including committed WAL state.
Initialize the private journal before enabling the first native backup. The
resolved version-3 inventory records the library lock; capture verifies the
inherited descriptor owns that exact exclusive flock. A different descriptor
or a lock held by another process cannot establish the backup boundary.
Restore this journal alongside the matching native database/media snapshot
before restarting maintenance; do not copy a live database without its WAL.

The client does not perform the old orphan-NFO/text cleanup. Retained sidecars
need their verified native import and cleanup boundary, and direct/manual scan
intake must remain available. Installed production launchers have not switched;
the final cutover must install the staged service/wrappers and maintenance
inventory together with the native server and workers.

Tests cover scenes and images, primary replacement, unchanged selected values,
retained original/derived source matches, restart replay, changed bytes with
restored mtime, stale ownership/root/metadata/evidence, refusal of ambiguous
owners and captions, transaction rollback, and real child-process death before
and after commit. Python tests cover candidate bounds, locks, private saved
intents, changed responses, known rejections and uncertain replay. A real
Go/Python HTTP test drops a committed deletion response, reopens the database,
restarts the client and verifies recovery plus the next pair's new preview.
Host activation and the populated schema-95 rehearsal remain release work.
