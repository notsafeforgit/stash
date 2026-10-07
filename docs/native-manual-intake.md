# Native local-file intake

Application-authenticated intake can register a purchased video or image under a
bound native media root. Choose an existing `directory` or `manual_batch`
collection; its metadata policy supplies any fixed performer UUIDs and filename
title fallback. The path needs no producer credential, account, post or capture.

Open a folder or batch collection from **Collections**, set its performer and
filename defaults in **Metadata rules**, then open **Import local files**.
Browse one folder at a time and select up to 25 images/videos, including files
from multiple subfolders. Review the selection before starting it. The browser
saves every request before sending any of them; each file has its own outcome.
The same collection route is available on desktop and mobile.

Opening the screen or refreshing status never submits work. **Continue saved
imports** first checks each original receipt, then submits only unaccepted
requests or resumes an unconfirmed cancellation/retry. The current review is
stored in this browser, scoped to the application endpoint and collection; the
actual requests and job history live in the native database. **Start another
batch** dismisses only a fully resolved browser review and does not remove its
server history. Ordinary scans continue to use configured folder policies.

## API

All routes below use `/api/v3/archive` and application authentication. Producer
bearer tokens cannot authorize them. Responses use `Cache-Control: no-store`.

| Method and path | Purpose |
| --- | --- |
| `GET /manual-intake/capabilities` | Whether the configured server can process file imports |
| `GET /collections/{collection}/intake-files` | Browse a bounded page of folders and supported visual files |
| `POST /manual-intake/preview` | Inspect a scoped file without registering media or queuing work |
| `POST /manual-intake/apply` | Verify the preview and accept a durable import request |
| `GET /manual-intake/requests/{request}` | Recover the original request and current execution status |
| `POST /manual-intake/requests/{request}/cancel` | Cancel the selected `expected_revision` |
| `POST /manual-intake/requests/{request}/retry` | Resume a terminal attempt using `expected_revision` and a new `request_uuid` |

Directory listing accepts `directory` (defaults to the collection prefix),
`q` (literal filename search), `limit` (1–100; the UI uses 50), and the previous
page's `after`/`signature`. It reads only that folder, sorts directories before
files, and retains only a page plus one candidate. Changed folder/root/collection
identities invalidate pagination. A folder with over 100,000 entries is rejected
rather than presented as complete. Audio, partial downloads, symlinks and other
unsupported entries are omitted. Individual file previews still recheck the
selected bytes and scope; listing does not reserve a file against changes.

Preview accepts `collection_uuid`, `relative_path` and `media_kind` (`scene` or
`image`). The file must be a complete, nonempty regular file beneath the current
collection folder and reviewed root binding. Traversal, escaping symlinks, ZIP
members, partial downloads and removed archive paths are rejected. Restoring a
removed path requires a separate explicit restoration.

Preview returns the filename, size, modified time, root/collection/policy
revisions, optional existing file UUID, and a `signature`. It omits the server's
absolute mount path and filesystem identity tokens. An existing file UUID means
that path is registered; its absence does not promise a new scene or image.
Verified content matching runs later in the worker.

The additional `file_signature` identifies the confined filesystem inspection
and root binding. It stays stable when registration changes database identities,
but includes inode/change-time evidence (or a full digest on platforms without
that evidence). It detects an overwrite with restored mtime and unchanged size.
It never authorizes an import: Apply still requires the complete preview
`signature`. This discovery hint does not change the admission-signature format
of already saved native requests.

Apply accepts the original preview inputs plus `signature` and a new
`request_uuid`. Persist that exact request before sending it. HTTP 202 means the
request was accepted into the queue; it does not claim the file was verified or
imported. A lost response is recovered by GET or by repeating the exact request.
Reusing a request UUID with different input returns a conflict. Recovery still
works after the file disappears, the policy changes, or the file worker becomes
unavailable. An unavailable worker cannot accept new requests.

## Execution and preservation

The existing `media.verify` worker handles both producer completions and manual
imports. Application admission uses a separate server-owned work variant and the
existing immutable job-submission receipt; no new database schema is needed.
The reviewed file identity, path lifetime and definition revisions are checked
again before registration. Platforms with a filesystem change token need no
full-file hash for preview; other platforms bind the review to a digest.

The worker hashes and probes the file through a confined descriptor, resolves
existing verified media, applies the collection policy, and records collection
membership. It does not infer a performer from a vendor filename. Explicit title
clears and curated relationships remain protected. New source appearances can
subsequently link to the same native media identity through source review.

Manual imports require the reviewed policy/root/collection revisions before
first publication. A changed review fails before registration. After registration
commits, retries resume preview generation and stable after-success notifications
without applying metadata again. Later policy edits do not erase that committed
result. Notifications are delivered at least once using their original event IDs.

Status distinguishes `registration_committed` from `media_ingested`, which is
true only after the worker's remaining effects succeed. Cancellation preserves
committed registration, if any, and stops remaining work. Recover an uncertain
cancellation through GET before retrying with the latest revision. Transient
processing failures use the worker's bounded automatic retries.

Explicit retry retains the failed/cancelled job and creates a new submission.
Equivalent concurrent retries share one active job. A published registration
carries its original publication and notification identities into the new job;
its metadata is not applied again. An unpublished retry must still match its
original file and policy preview, including a final admission check at commit.
A stale file needs a fresh review. Status exposes the previous job and reviewed
revision, and still reports committed media if a resumed job is cancelled before
it starts. Recover a lost retry response through its new request UUID; replaying
the saved retry body cannot create another job.

Portable export stores these requests, attempts, collection memberships, policy
decisions and media identities in the native database. They need no producer
outbox. Bulk media still belongs to the media backup inventory. A relocated
installation must review its root bindings before executing retained work.

## Scheduled folder discovery

`stash-intake-folders --config /private/intake.json` connects explicit reviewed
directory/manual-batch collections to this same API. It is packaged in the
native producer runtime; it does not launch gallery-dl. The host's compatible
scan helper has not yet been replaced. Example configuration:

```json
{
  "format": "stash-folder-intake-v1",
  "endpoint": "http://localhost:8009",
  "root_uuid": "REVIEWED_ROOT_UUID",
  "collections": [
    {"uuid": "REVIEWED_DIRECTORY_COLLECTION_UUID", "path_prefix": "."}
  ],
  "exclude": ["scrapes"],
  "state_dir": "/private/native-intake",
  "library_lock": "/tank/media/backup_ledgers/.backup_run.lock",
  "api_key_file": "/private/stash-application-key",
  "max_pending": 25,
  "entries_per_run": 1000,
  "settle_seconds": 60,
  "scan_interval_seconds": 3600
}
```

Collection UUIDs and prefixes must match the active server definitions. Add
explicit child collections for their folder policies; the most specific
configured prefix wins. Exclusions are root-relative directory prefixes.
Deployment must cover all intended manual folders and exclude producer-owned
scopes using the current source inventory. Adding sources or changing collection
coverage requires updating that inventory. This client does not infer performers,
create collections, enable migrated policies, or discover every source scope
on its own. Finish that deployment coverage before retiring the old scan trigger.

Each invocation recovers saved admissions before discovering more files. It
retains at most `max_pending` uncertain/queued/running requests across invocations.
Admission is not completion: only a validated `succeeded` receipt counts as an
import. Failed/cancelled outcomes and rejected previews stay explicit; unchanged
terminal attempts are not automatically resubmitted. Native workers retain their
bounded transient retries, and the existing manual-intake retry API remains
available for explicit recovery.

Discovery walks bounded directory pages, including nested folders and files
with old modification dates. A changed continuation restarts that directory;
retained file versions prevent duplicate submissions. A complete traversal waits
the configured interval before starting again; pending receipts are checked on
every invocation. Files younger than `settle_seconds`, zero-byte files, audio,
partials, symlinks and unsupported entries are not admitted. The API checks
reviewed file identities again before publication. ZIP-member intake still uses
the ordinary scanner; it is not supported by this manual-file API.

The first encounter with an already indexed file records an `already_indexed`
baseline, without claiming its previews or notifications completed. Subsequent
file, collection or policy version changes can create new admissions. Completing
registration alone does not change the discovery fingerprint. Existing field
clears and curated relationships remain protected by the native policy service.

One private `intake.sqlite3` stores directory cursors, observed versions and exact
request bodies before transmission. A state lock prevents concurrent invocations;
the shared backup lock excludes admissions during coordinated capture. Endpoint,
root and library-lock changes are refused for an existing journal. Collection
coverage/exclusion changes are allowed only after its pending requests resolve;
completed receipts remain retained and discovery restarts. Do not delete a journal
to bypass uncertain work.

The packaged `stash-native-intake.timer` invokes the oneshot service two minutes
after boot and two minutes after each completion. A busy lock skips that attempt;
a failure retains pending state. Its fifteen-minute runtime limit is recoverable
on the next invocation. It is independent of dedupe scheduling and performs no
global `metadataClean` or wildcard autotag operation.

Declare `{"kind":"folder_intake","config":"/private/intake.json"}` in the
worker inventory's `maintenance` list. The root must be inventoried, and the
configuration, private key and journal join the same verified backup boundary.
Initialize the journal before enabling backup; capture uses SQLite backup, not
raw database/WAL copies. Restore it with the corresponding native checkpoint
before resuming admissions.
