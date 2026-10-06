# Native local-file intake

Application-authenticated intake can register a purchased video or image under a
bound native media root. Choose an existing `directory` or `manual_batch`
collection; its metadata policy supplies any fixed performer UUIDs and filename
title fallback. The path needs no producer credential, account, post or capture.

The application file-picker and batch workflow are still being implemented.
These routes provide their reviewed admission and durable execution boundary.
Existing ordinary scans continue to use configured folder policies.

## API

All routes below use `/api/v3/archive` and application authentication. Producer
bearer tokens cannot authorize them. Responses use `Cache-Control: no-store`.

| Method and path | Purpose |
| --- | --- |
| `GET /manual-intake/capabilities` | Whether the configured server can process file imports |
| `POST /manual-intake/preview` | Inspect a scoped file without registering media or queuing work |
| `POST /manual-intake/apply` | Verify the preview and accept a durable import request |
| `GET /manual-intake/requests/{request}` | Recover the original request and current execution status |
| `POST /manual-intake/requests/{request}/cancel` | Cancel the selected `expected_revision` |

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
processing failures use the worker's bounded automatic retries. Explicit retry
controls and batch review remain part of the unfinished application workflow.

Portable export stores these requests, attempts, collection memberships, policy
decisions and media identities in the native database. They need no producer
outbox. Bulk media still belongs to the media backup inventory. A relocated
installation must review its root bindings before executing retained work.
