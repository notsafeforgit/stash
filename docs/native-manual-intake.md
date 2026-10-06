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
