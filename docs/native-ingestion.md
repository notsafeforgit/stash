# Native producer ingestion

The development server exposes a separate, versioned producer interface at
`/api/v3/ingest`. Protocol 1 currently accepts `source.capture` events for
Reddit and Twitter post identities. It records sanitized source evidence,
publisher choices, collection provenance, and ordered album manifests with a
durable receipt. This interface is not deployed to the compatible production
server.

File completion, verified media associations, gallery synchronization after
file intake, durable producer outboxes, run leases, additional post identity
adapters, and host/n8n conversion remain required. Capability discovery reports
`file_ingestion: false`. A capture receipt never acknowledges a download, scan,
or completed gallery. Collection/root administration UI and the catalog importer
also remain pending; current collection definitions come from the core services.

## Authentication and scope

These are access tokens for Stash's ingestion API. Stash does not manage the
producer's Reddit, Twitter, or other website credentials; those remain in
gallery-dl's existing credential configuration. Revoking an ingestion token
only removes that producer token's access to Stash.

Producer requests require `Authorization: Bearer <token>`. Session cookies and
the general Stash API key do not authenticate this interface. Tokens are not
accepted in URLs. Its router has no GraphQL, plugin, filesystem, or application
fallback. Responses use `Cache-Control: no-store`.

Each token belongs to one durable producer UUID and grants specific collection
UUIDs at specific logical root UUIDs. A null root grants metadata access for an
unbound collection; it is not a wildcard. One credential can grant up to 128
collections, with one root per collection. A binding change can use separate
credentials while an older outbox drains. Root UUIDs convey no arbitrary host
path authority. Tokens have 32 random secret bytes; only their SHA-256 verifiers
are stored. Optional expiry and permanent revocation are checked in each write
transaction. Credential rotation retains the producer UUID and event receipts.

The application's existing authenticated router exposes administration:

| Method and route under `/api/v3/ingest-admin` | Body / result |
| --- | --- |
| `POST /producers` | `{"label":"Host gallery-dl"}`; returns a producer UUID |
| `GET /producers?after=<uuid>` | Up to 50 producers ordered by UUID |
| `POST /producers/<uuid>/credentials` | `{"scopes":[{"collection_uuid":"…","root_uuid":null}],"expires_at":null}`; returns credential metadata and its token once |
| `GET /producers/<uuid>/credentials?after=<uuid>` | Up to 50 credential records; excludes tokens and verifiers |
| `DELETE /credentials/<uuid>` | Permanently revokes the credential; repeated revocation is safe |

Administration requires application access, independently of the producer token.
JSON writes and origin checks reject cross-site browser administration. Scope
issuance requires an active collection and a root used by one of its recorded
definitions. A historical root grant supports rotating credentials while old
events remain undelivered. Retired/disabled roots cannot receive new grants.

## File preparation in core

The backend can prepare a root-relative image or video through
`ingest.PrepareMedia`. This is an uncommitted inspection, with no database writes,
library handlers, download acknowledgement, or HTTP file-completion support yet.
It verifies the reviewed root, opens a confined regular file, calculates SHA-256,
and checks optional producer size/digest claims. Partial downloads are rejected.
The shared scanner calculates its normal matching fingerprints and image/video
metadata using independent, cancellable readers of that same descriptor.

FFprobe receives a seekable descriptor on Linux, including for MP4 files with
trailing metadata. Other platforms use a private bounded copy of the descriptor.
It accepts self-contained media containers, blocks indirect playlists/network
input, limits output to 1 MiB, and has a 30-second timeout. Audio-only files and
media without visual dimensions are rejected. Animated images keep the shared
scanner's clip classification.

Preparation rechecks file identity, size, modification time, root binding, and
the named path after probing. Linux also checks ctime to detect same-size writes
followed by restoring mtime; platforms without that adapter rehash. The caller
keeps the descriptor open and must revalidate the current root immediately before
commit, alongside authorization, collection policy, and file-generation fences.
These checks do not make filesystem writes atomic with SQLite. Durable workers
that enforce generation fences, transactional file/provenance publication, and
receipt integration remain required before advertising `file_ingestion: true`.

Native schema 1000018 now provides persistent file-generation fences and shared
content identities. `PreparedMedia.RecordContent` records verified bytes against
an existing file UUID and its current generation in the caller's managed write
transaction. It rechecks the root/descriptor before commit, and a generation
guard rejects subsequent replacement/deletion in that transaction. Ordinary file
updates also require the generation the scanner/task read. This storage and
publication boundary does not yet implement the durable file worker or completion
HTTP event. The [schema guide](native-schema.md) describes identity lifetimes,
unchanged fingerprint handling, and migration behavior.

### Durable archive work

Native schema 1000019 provides `archive_jobs`, immutable submission acknowledgements,
and attempt history. `job.Durable` supplies submission, claim/renew, progress,
cancellation, bounded recovery, and atomic publication. The only accepted kind
is currently `media.verify`; its file-completion admission and worker loop are
still being connected. No new worker runs automatically, and the public API
continues to advertise `file_ingestion: false`.

Each server-created submission has a stable request UUID. Replaying it returns
the original job even after completion. Distinct submissions with the same kind
and work key share queued/running work, provided their arguments, resource key,
and attempt limit agree. A new submission after completion can create a new job.
These internal submission digests use canonical arguments; the public capture
protocol retains its separate exact-request-byte digest contract.

Work/resource keys are hashes calculated by the admitting domain service. A
unique running-resource index prevents two jobs owning the same destination at
once. Keys must represent the effective target and policy; widening a scrape
window requires an explicit policy decision, not silently reusing another job's
key. Shared filesystem locks remain necessary for external downloaders.

Claims increment a persistent fence and record an owner and deadline. Renewal,
progress, and publication require that exact unexpired lease. Recovery examines
at most 100 expired attempts per transaction and either requeues them or marks
them failed at their attempt limit. Retry times survive repeat submissions;
duplicates cannot bypass backoff. The default service capacity is 10,000 active
jobs, checked before new work is created; coalescing and receipt lookup still
work at capacity. Queue/history reads use bounded indexed pagination.

`Durable.Publish` checks the lease, runs domain writes, records the attempt result,
and checks ownership/deadline again before commit. Failure rolls everything back.
Expensive inspection runs outside that transaction; prepared files retain their
descriptors and add their own final checks. Cancellation requires the reviewed
job revision and immediately invalidates its lease. Result/progress objects are
bounded; failures use machine-readable codes rather than persisting raw stderr.

This is the durable storage/publication boundary, not a replacement for the
current scheduled scrapes. Producer authentication and scope checks, path/file
reservations, file-completion receipts, actual workers, source-run coordination,
and host/n8n outbox delivery remain required before switching those callers.

## Wire contract

All producer routes require the bearer token and reject query parameters:

| Method and route under `/api/v3/ingest` | Result |
| --- | --- |
| `GET /capabilities` | Protocol, producer UUID, scopes, supported kinds/namespaces, retention policy, and request limits |
| `POST /batches` | An ordered result for every submitted event |
| `GET /receipts/<event-uuid>` | That producer's original receipt, subject to the credential's collection/root scope |

A batch contains 1–8 entries and is at most 16 MiB. Each event is at most 5 MiB;
its source payload remains limited to 4 MiB and projected metadata to 256 KiB.
Send `Content-Type: application/json`; compressed request bodies are not accepted.
Unknown envelope fields, duplicate JSON keys, invalid Unicode, excessive nesting,
and unsupported protocol/kind/retention versions are rejected.

An event has this shape; UUID placeholders must be replaced with canonical,
lowercase, nonzero UUIDs:

```json
{
  "protocol": 1,
  "producer_uuid": "<provisioned producer UUID>",
  "event_uuid": "<durable event UUID>",
  "run_uuid": "<worker run UUID>",
  "collection_uuid": "<authorized collection UUID>",
  "collection_revision": 1,
  "root_uuid": null,
  "kind": "source.capture",
  "observed_at": "2026-09-30T12:00:00Z",
  "extractor_version": "1.32.15-dev",
  "retention_policy": "gallery-dl-retained-v1",
  "post": {"namespace": "native:reddit", "value": "example-post"},
  "metadata": {"title": "Album title", "date_basis": "source"},
  "source": {
    "category": "reddit",
    "id": "example-post",
    "author": "example-account",
    "author_fullname": "t2_example-account-id",
    "is_gallery": true,
    "gallery_data": {"items": [{"media_id": "first"}, {"media_id": "second"}]}
  }
}
```

`run_uuid` currently retains producer run provenance; it does not represent a
claimed server lease or a completed scheduled job. Event collection revisions
pin immutable definitions. Delayed events can reference an earlier active
definition while the collection itself remains active. The requested namespace
and logical root must match that definition and the credential's grant.

The producer must apply the declared retention policy before durable outbox
storage. The server validates it again: secrets, redundant media renditions,
and irrelevant profile noise are not silently accepted. The source is structured
JSON, not an escaped JSON string. The server partitions and deduplicates post,
profile, and per-file data itself. Producers cannot supply arbitrary storage
patches, precomputed profile bodies, or the trusted legacy-import policy.

Post identity is verified against captured Reddit `id` or Twitter
`tweet_id`/`rest_id`/`id_str` evidence, with exact large-number handling. Reddit
crossposts keep their own identity; embedded Reddit parents from another media
extractor retain their Reddit post scope. Unknown post identity adapters are
rejected until supported, even though the account parser knows more services.

## Byte identity and receipts

Each batch entry is `{"sha256":"<digest>","event":<event object>}`. The digest
covers the exact UTF-8 bytes from the event's opening `{` through its closing `}`,
including internal whitespace. It excludes surrounding batch whitespace.
Serialize the event once, persist those bytes and its UUID in the outbox, and
reuse them on every retry. Do not regenerate timestamps, change formatting, or
re-serialize a previously saved event when assembling a batch. For example:

```python
event_bytes = json.dumps(event, ensure_ascii=False, separators=(",", ":"),
                        allow_nan=False).encode("utf-8")
digest = hashlib.sha256(event_bytes).hexdigest().encode("ascii")
# Persist event_bytes and the event UUID before delivery.
batch = b'{"events":[{"sha256":"' + digest + b'","event":' + event_bytes + b'}]}'
```

Events commit independently. HTTP 200 for a parsed batch means the results are
available; inspect each item's `status`, `error`, and `receipt`. A successful
item contains a receipt with producer/event identity, digest, source post and
capture UUIDs, recorded collection definition, committing credential, timestamp,
and a small result projection. The raw source payload is not duplicated there.

The result identifies publisher resolution (`linked`, `review`, `unavailable`,
or a preserved choice), album selection (`selected`, `protected`, `review`, or
`unavailable`), review reasons, and `media_ingested: false`. Ambiguous publishers
or incompatible album lists retain their evidence and require review. Pinned or
disabled album choices survive automatic delivery. Publishers never become
depicted performers merely because they published or aggregated a post.

Receipt and domain changes commit together. Failure while saving a receipt
rolls everything back. The same producer/event UUID and bytes return the original
receipt, including after restart, credential rotation, or later source changes.
A different digest for an existing identity returns a conflict. Receipts are
immutable acknowledgements; they do not change when later review resolves a
conflict. Revoked/expired credentials cannot read or replay them.

Per-item errors use HTTP-style statuses: 400 `invalid_event`, 401 `unauthorized`,
403 `outside_scope`, 404 `not_found`, 409 `conflict`, 422 `unsupported`, or 503
`temporarily_unavailable`. Network loss or a retryable server error requires
redelivery of the same event bytes. A rejected event has no success receipt and
must not be marked imported or removed from an operational outbox.

Native schema 1000017 owns producer identities, credentials, grants, and receipts.
Foreign keys bind receipts to their actual capture, collection revision, and
credential scope. Anonymised exports remove them with private source evidence.
