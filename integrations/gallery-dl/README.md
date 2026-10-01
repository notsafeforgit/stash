# Native Stash producer

This package implements the durable delivery portion of the gallery-dl adapter.
It is development code on `v3-rewrite`; the installed host and n8n download
helpers still use their existing catalogs. Download hooks, native run lease
integration, launcher conversion and production cutover remain separate work.

Python 3.12 or newer is required. Runtime delivery uses only the standard
library. Install the local package with `pip install ./integrations/gallery-dl`
in the runner's environment when integrating it; do not replace a live wrapper
with this CLI. The CLI drains events that a producer has already durably queued.

## Authentication boundary

`STASH_INGEST_TOKEN` is a token for Stash's ingestion API. The queue binds to a
Stash HTTP origin and stable producer UUID, independently of that token. Rotating
the token preserves queued event identity and receipts. The client reads the
environment reference for each request and sends the token only in the
Authorization header; it does not store the token, follow redirects, inherit
website proxies or load cookies. Website credentials remain with gallery-dl.

The endpoint must be an HTTP(S) origin without user information, path, query or
fragment. HTTPS uses normal certificate verification. A request timeout is at
most 30 seconds. Response bodies are bounded and do not become persisted error
messages. Queue errors contain controlled codes.

## Queue contract

Provision a private directory on a durable local filesystem that supports SQLite
WAL and filesystem flushes. Keep it on the worker's persistent volume and include
it in backups; an ephemeral container layer is unsuitable. The constructor does
not create missing parent directories, so a missing mount does not silently
become a replacement queue. It rejects a different producer/origin, foreign
database lineage and a symlink in place of the database.

Use `stash_ingest.retention.retain(metadata)` on a copy, construct a supported
event envelope, serialize it with `encoding.encode`, and call `Outbox.enqueue`
before reporting the evidence captured. Event fields are documented in
[native ingestion](../../docs/native-ingestion.md). The outbox validates the
envelope and requires source data to already satisfy `gallery-dl-retained-v1`.
The envelope has no plugin-settings field. Retention removes known secret-bearing
fields and private runtime handles; unsupported public runtime objects are
rejected. Server source-identity and domain checks remain authoritative.

A `file.completed` event requires the actual final relative path, positive size,
SHA-256, and image/scene kind after transformations and filesystem flushes. Its
source capture must already be queued for the same collection revision and root.
Delivery waits for that capture's receipt. The queue never creates a filename or
infers an attachment from a directory name; those are producer integration tasks.

SQLite transactions use WAL and `synchronous=FULL`. Independent drainers claim
bounded batches with expiring, fenced delivery leases. Interrupted work replays
the exact original bytes and event UUID. A mismatched receipt cannot discard the
payload. Valid receipt storage and payload removal commit together, retaining a
small acknowledgement row for replay and dependent file events. The download
archive is independent and is never modified by this package.

Default capacity is 10,000 unacknowledged events or 512 MiB of queued payloads.
Pending, in-flight and review events all consume capacity. Exhaustion raises an
error and must pause the producer; no event is evicted. Acknowledgement rows are
retained, so capacity bounds queued payloads rather than total disk usage. A disk
write failure propagates to the caller and must stop new download work.

Transient failures use persistent exponential backoff, from five seconds to one
day. A rejected/expired Stash token leaves the evidence pending for rotation.
Scope, schema and conflict errors require explicit review. Retrying a reviewed
event preserves its bytes; correcting its contents requires a new event UUID.

## Inspection and delivery

The CLI takes `--outbox PATH --endpoint ORIGIN --producer UUID`, followed by one
of these commands. `--token-env NAME` changes the environment reference; there
is no command-line token argument.

| Command | Result |
|---|---|
| `status` | Counts for pending, sending, acknowledged and review events, queued bytes and oldest age |
| `drain` | Attempts one ready batch of at most eight events; exits 2 while any local work remains |
| `retry EVENT_UUID` | Requeues one explicitly reviewed event with its original contents |
| `receipt-status EVENT_UUID` | Reads actual server ingestion/worker status |

`acknowledged` means Stash accepted the event transaction. For a file, that means
verification was queued; it does not assert a successful media import. A disabled
file processor leaves new file events pending while previously committed
receipts remain recoverable. The CLI does not schedule new source runs.

## Validation

`make validate-producer` runs the standard-library tests, including the same
retention corpus used by Go. Tests cover concurrent delivery, subprocess death,
receipt replay, token rotation, dependency ordering, capacity, partial batches,
redirect rejection and disabled file processing. The backend test
`TestPythonProducerDurableDeliveryAgainstNativeHTTP` runs this actual client
against the native HTTP router and SQLite, loses a committed response, and checks
replay and independent rejection. Both checks are included in `make validate-fork`
and the build workflow. No production endpoint or source website is contacted.
