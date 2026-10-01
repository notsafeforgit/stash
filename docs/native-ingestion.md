# Native producer ingestion

The development server exposes a separate, versioned producer interface at
`/api/v3/ingest`. Protocol 1 currently accepts `source.capture` events for
Reddit and Twitter post identities. It records sanitized source evidence,
publisher choices, collection provenance, and ordered album manifests with a
durable receipt. This interface is not deployed to the compatible production
server.

The v3 server also accepts `file.completed` when its scanner and preview tools
are configured. Admission commits a receipt and durable verification job;
its worker verifies bytes, registers media and source album membership, generates
missing previews, and delivers media/gallery notifications. Poll the receipt's
status to distinguish acceptance, committed registration, and completed intake.
Capability discovery reports `file_ingestion: false` when the worker is unavailable.
Existing acknowledgements remain readable and replayable in that state; new file
events are rejected.
A source capture alone never acknowledges downloaded or playable media.

Native metadata policies now share one evaluator between verified intake and
ordinary scans, with explicit performer defaults and authenticated preview/apply.
Native source-run coordination now provides coalesced windows and fenced leases.
The Python adapter provides durable outboxes, worker lease enforcement,
source-window filtering, download hooks and offline run-request coalescing.
Worker profiles now fingerprint reviewed configuration and execute one claimed
download attempt with concurrent event delivery. Launcher integration, additional
post adapters, host/n8n conversion, catalog import, and native administration/review
UI remain required.
Existing scrapes have not switched to this interface. Root, collection and policy
administration endpoints are described below.

## Authentication and scope

These are access tokens for Stash's ingestion API. Stash does not manage the
producer's Reddit, Twitter, or other website credentials; those remain in
gallery-dl's existing credential configuration. Revoking an ingestion token
only removes that producer token's access to Stash. Already accepted jobs remain
durable. Cancel the job or disable its collection/root to prevent further work;
token rotation does not silently discard an accepted event.

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
library handlers or download acknowledgement. The HTTP worker uses this same
preparation before publishing an accepted file event.
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
These checks do not make filesystem writes atomic with SQLite. The durable worker
rechecks them at publication, before effects, and before its completion commits.

Native schema 1000018 now provides persistent file-generation fences and shared
content identities. `PreparedMedia.RecordContent` records verified bytes against
an existing file UUID and its current generation in the caller's managed write
transaction. It rechecks the root/descriptor before commit, and a generation
guard rejects subsequent replacement/deletion in that transaction. Ordinary file
updates also require the generation the scanner/task read. This storage and
publication boundary is used by the durable file worker. The
[schema guide](native-schema.md) describes identity lifetimes,
unchanged fingerprint handling, and migration behavior.

`CaptureFileTarget` records server-owned intake state for the authorized root
and relative path. It captures an existing file's UUID/generation and the path's
removal counter. Native schema 1000020 retains that counter across deletion and
recreation, including file and folder renames. A concurrent ordinary scan may
create the previously absent file, but delayed intake cannot undo an intervening
removal. A fresh completion for an absent, previously removed path also requires
an explicit restore or ordinary scan to establish a new file lifetime.

`PreparedMedia.PublishFile` reuses that concurrent scan's file record, checks its
type and verified content, and commits prepared metadata and byte proof together.
Case-insensitive database matches must still identify the same held filesystem
object; equal bytes under a different path spelling do not establish that.
Reinspection of unchanged verified bytes preserves generated fingerprints.

`PreparedMedia.PublishMedia` uses the exact file's existing scene/image owner,
or one unambiguous owner of matching current verified bytes. Otherwise it creates
a new scene/image. Legacy MD5/oshash matches and obsolete verification generations
do not authorize associations. Multiple candidates or conflicting media kinds
require review, and existing items retain their metadata. Final checks reject
deletion, detachment, or conflicting ownership before commit. These methods run inside a durable transaction, so file, proof and media
association commit together with the job's publication checkpoint or roll back.

`PreparedMedia.PublishIntake` adds the recorded collection revision, source
attachment evidence and source album membership in that same transaction. It
checks both historical and current collection root/path scope; a label edit may
retain accepted work, while disabling or narrowing scope prevents publication.
The referenced capture must belong to the recorded collection revision and
contain the attachment. These are indexed lookups of the selected records.

An attachment's existing media choice directs an unowned incoming file to that
item, including when its UUID has been adopted. This can also select one existing
owner of an intentionally shared file without merging its other owners. Files
already owned elsewhere stay with their owners and conflicting source links
remain for review. A deleted selected item requires review; it is not recreated.
Explicit unlinks and disabled albums are preserved. Otherwise unambiguous verified
attachment evidence can establish the link automatically. Publishers never
implicitly become depicted performers.

As attachments arrive, the native source-gallery service adds their media to the
post's album while preserving manual membership exclusions. Replayed intake
does not duplicate its provenance or evidence. Manual media can record collection
provenance without inventing a source capture, account or gallery. Failures roll
back file, content proof, library media, source links, album changes and job result
together. The HTTP worker commits this registration as a resumable checkpoint
before generating previews and delivering media/gallery notifications. Native
field policies run in that same checkpoint. Admission pins the current policy
revision; changing it before publication leaves metadata for review while still
registering the verified media. The result reports `metadata_state`, applied
`metadata_fields`, and any `metadata:<field>` review requirements.

### Durable archive work

Native schema 1000019 provides `archive_jobs`, immutable submission acknowledgements,
and attempt history. `job.Durable` supplies submission, claim/renew, progress,
cancellation, bounded recovery, and atomic publication. The only accepted kind
is currently `media.verify`. With v3 and FFmpeg/FFprobe configured, the HTTP server
starts one processing loop after plugin routing is initialized. It cancels and
joins that loop during shutdown before the manager closes SQLite. Transient loop
failures are retried; jobs retain their own bounded attempts and retry delay.

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

`Durable.Checkpoint` commits a domain phase and its progress without finishing
the job. File registration uses this boundary; progress survives retries and
lease recovery. A recovered worker reopens and re-verifies the accepted bytes,
validates the original file generation and media association, then resumes its
effects. It retains the original creation/link facts for notification delivery.
No restart invents another item or changes the accepted receipt.

Missing scene covers and image previews use the application's generators;
existing cover selections remain protected. Generation failures stay pending or
failed. Media and gallery hooks receive a stable `hookContext.eventId` and may
repeat after interruption; plugins must deduplicate non-idempotent effects.
These durable notifications supplement existing edit hooks; converting all other
legacy post-commit notifications to persistent delivery remains separate work.

File jobs renew their lease while hashing, probing and running effects. Producer
claims never count as verification. A changed digest, generation, path removal,
inactive collection or lost ownership prevents completion. A later failure does
not undo an earlier committed registration: status reports that distinction.
Source-worker conversion and host/n8n outboxes remain required before switching
the current scheduled scrapes to this interface.

## Wire contract

All producer routes require the bearer token and reject query parameters:

| Method and route under `/api/v3/ingest` | Result |
| --- | --- |
| `GET /capabilities` | Protocol, producer UUID, scopes, supported kinds/namespaces, retention policy, and request limits |
| `POST /batches` | An ordered result for every submitted event |
| `GET /receipts/<event-uuid>` | That producer's original receipt, subject to the token's collection/root scope |
| `GET /receipts/<event-uuid>/status` | Current job state, attempt, bounded error code and safe result; excludes worker arguments and local mount paths |

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

`run_uuid` retains producer run provenance; by itself it does not represent a
claimed server lease or a completed scheduled job. Event collection revisions
pin immutable definitions. Delayed events can reference an earlier active
definition while the collection itself remains active. The requested namespace
and logical root must match that definition and the credential's grant.
Coordinated workers use the native run UUID described below. Older producer run
IDs remain valid provenance while adapters are converted. Neither event kind
changes run ownership or completion state, so delayed outbox delivery cannot
rewind a newer attempt.

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

## Completed file events

A `file.completed` event is at most 16 KiB and has this shape:

```json
{
  "protocol": 1,
  "producer_uuid": "<producer UUID>",
  "event_uuid": "<durable file event UUID>",
  "run_uuid": "<producer run UUID>",
  "collection_uuid": "<collection UUID>",
  "collection_revision": 2,
  "root_uuid": "<logical root UUID>",
  "kind": "file.completed",
  "observed_at": "2026-10-01T12:00:00Z",
  "relative_path": "account/final-image.jpg",
  "size": 12345,
  "sha256": "<lowercase SHA-256 of final file bytes>",
  "media_kind": "image",
  "source": {
    "capture_event_uuid": "<this producer's acknowledged capture event UUID>",
    "attachment": {"namespace": "native:reddit", "value": "<media ID>"}
  }
}
```

Use `scene` for video scenes or `image` for images/clips. `source` is optional for
manual purchases and other unsourced files. When supplied, it must identify an
attachment in the same producer's acknowledged capture, with matching collection
revision and root. Submit that capture first. Paths must name a final regular
file within the permitted root and directory prefix; `.part` paths are rejected.
The server owns file UUIDs, generations, removal fences, and capture UUIDs.

A file item returns status 202 with an immutable `queued` receipt and job UUID.
Status initially reports `registration_committed: false` and
`media_ingested: false`. After verified file/media/source/album publication,
`registration_committed` becomes true; `media_ingested` becomes true only after
required effects and final validation succeed. The publication projection uses
UUIDs and includes any review reasons. Cancellation or failure can retain a
committed registration while leaving intake incomplete. Receipt replay remains
202 with the original acknowledgement even after the job succeeds.

File event identities are qualified by producer. Replaying an event creates no
new job; distinct file events retain separate provenance jobs and serialize on
their destination. Source-run coalescing uses the separate traversal contract below.

## Source-run coordination

The server advertises `source_runs: true` and `source_run_protocol: 1`.
This coordinates external gallery-dl workers; it does not run shell commands,
store website credentials, or change the currently deployed launch paths.
Producer authentication and collection/root grants are the same as event intake.

| Producer route | Body or result |
| --- | --- |
| `POST /runs` | Submit the typed request below; returns its native run plus the committed `request_uuid` |
| `GET /runs/<uuid>` | Current state, windows, ownership, progress and retry time |
| `POST /runs/list` | `collection_uuid`, optional integer `after`; at most 50 authorized runs in sequence order |
| `POST /runs/<uuid>/attempts` | Optional integer `after` fence; at most 50 attempts |
| `POST /runs/<uuid>/claim` | `owner_uuid`, `policy_sha256`, `lease_seconds` (5–900) |
| `POST /runs/<uuid>/lease` | `owner_uuid`, `fence`, and exactly one of `lease_seconds`, `progress`, or `outcome` |

Paths above are relative to `/api/v3/ingest`. Pagination uses JSON bodies;
query-string tokens and parameters remain rejected. A claim returns 204 and
`Retry-After: 5` while unavailable; inspect status and back off. Repeating a claim
with the same producer and worker UUID returns its still-valid lease. A different
producer or worker cannot borrow it. Rotate the ingestion token while retaining
the producer UUID when the same worker should keep ownership.

Capabilities advertise `source_run_submission_receipts: true` when submission
responses identify the committed request. Multiple requests may coalesce into
one run, so a producer must match `request_uuid` and the collection/policy
definition before releasing a queued submission. The returned run is a current
snapshot, which may already be running, deferred, cancelled or completed when a
lost response is replayed. Admission does not assert scrape completion.

```json
{
  "request_uuid": "<stable request UUID>",
  "collection_uuid": "<configured source collection UUID>",
  "collection_revision": 1,
  "operation": "download",
  "policy_sha256": "<SHA-256 of effective non-secret scan configuration and adapter version>",
  "cooldown_seconds": 30,
  "window": {
    "since": "2026-09-24T00:00:00Z",
    "until": "2026-10-01T00:00:00Z"
  }
}
```

Operations are `download` and `enrich`; download requires a logical media root.
The collection supplies the reviewed target URL, namespace and destination.
Run responses include `target_url` and `path_prefix` from the immutable collection
revision pinned by the run, including historical status responses. They do not
expose the server's absolute filesystem binding. The producer must verify its
local destination against that prefix before writing.
Policy identity covers effective extraction, archive/skip, original-quality,
conversion, metadata-only and pacing behavior plus adapter version; omit secrets
and the requested date window. The Python worker profile supplies this fingerprint
from portable settings, reviewed helper assets and the pinned runtime; local path
bindings and website-access values stay in its environment. Claim requires the matching fingerprint.
No API argument supplies a command line.

Windows are half-open published-time ranges `[since, until)`, with millisecond
precision. `since: null` requests all history before the explicit `until`.
Persist absolute cutoffs with the local request before transmission; a network
retry must not quietly become a newer request. A producer/request UUID is an
immutable acknowledgement: replay returns its original run, and changed request
contents return 409. A fresh scheduled request may create a run after the prior
one succeeds.

Equivalent work shares one active run across producers. Its identity includes
collection and root revisions, operation, policy fingerprint and cooldown.
Every launch path must reference the same configured collection UUID for the
same target. Distinct collections remain distinct provenance, even when a shared
URL forces them to serialize. Queued windows merge without filling unrequested
gaps. While running, the claimed window and already completed ranges are
subtracted from new requests. Widening a seven-day scan to all history retains
the missing older interval, rather than another full copy of the seven-day scan.
There are at most 64 pending/completed/claimed intervals per active run and
10,000 active or deferred runs. Capacity errors do not acknowledge lost work.

Claims prefer the newest uncovered range. Pending download work takes precedence
over enrichment for the same collection. A collection, the same normalized
target URL, and overlapping destination paths cannot have concurrent owners.
Server mount identities and resolved paths handle host/container mappings and
existing symlinks; a missing destination may be created by the worker afterwards.
The worker must still acquire the existing shared filesystem download lock
before source work or filesystem writes. The coordinator does not make a
distributed filesystem operation atomic or physically stop an expired process.

Each claim increments its fence and records an immutable traversal window in an
attempt. Progress contains `items_seen`, `files_completed` and a bounded source
`cursor`. Items count traversal entries, including individual media in an album,
matching gallery-dl's existing resume checkpoint. Counts cannot decrease or move a cursor backwards under equal counts.
The same-window retry receives its checkpoint. A changed traversal window starts
afresh; a cursor from a different range is not reused. Cursor meaning remains an
adapter contract, so it must be a recoverable source key rather than a raw log,
secret, or arbitrary command.

An `outcome` contains `state` (`succeeded`, `retry`, or `deferred`), `error_code`,
and optional `retry_after_seconds` (at most one week). Success has no error code
or retry delay and certifies traversal of the claimed window under that policy.
It does not certify that every queued file event has finished ingestion.
Remaining windows keep the run queued; otherwise it succeeds. A failure restores
the claimed window, retains its checkpoint, and uses exponential retry delay
starting at five minutes. Eight consecutive failed/expired attempts cause a
durable deferral. Timers and manual submissions cannot clear it or shorten an
existing delay. Target cooldown also survives changes of scan policy.

Lease renewal, progress and completion require producer UUID, worker UUID,
current fence and an unexpired server deadline. Publication checks the deadline
again before commit. Expired ownership is recovered during subsequent claims;
late responses cannot finish a later attempt. On a lost completion response,
inspect that fence in attempt history before retrying. A collection/root change
prevents renewal or success under the obsolete definition and defers queued
work. Already captured evidence can still drain through scoped ingestion.

Application-authenticated `POST /api/v3/ingest-admin/runs/<uuid>/review` accepts
`expected_revision` and `action` (`retry` or `cancel`). Retry reopens a deferred
run only while its definition is still current, preserving target cooldown.
A changed definition needs a newly reviewed request. Cancellation invalidates
ownership; it does not undo source evidence or completed-file jobs. Review
actions and attempt outcomes remain in the database. Producer tokens cannot
access this administrative route.

The producer SDK implements durable local request coalescing during outages,
outbox delivery, shared filesystem locking, lease renewal and pausing before the
next source request after expiry. It applies the claimed Reddit/Twitter post
window before file processing. Reviewed worker profiles, one-attempt execution
and finish-response recovery are implemented. Conversion of the actual
host/n8n/recovery configuration and launch paths remains required.
No legacy PID, lease or journal is promoted automatically by this migration.
The separate importer must preserve permanent completions, checkpoints,
deferrals and intentionally ignored unavailable originals before cutover.

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
capture item contains a receipt with producer/event identity, digest, source post
and capture UUIDs, recorded collection definition, committing token identity,
timestamp, and a small result projection. File receipts instead require a job
UUID and have source identifiers only when a source capture was referenced. The raw source payload is not duplicated there.

For `source.capture`, the result identifies publisher resolution (`linked`, `review`, `unavailable`,
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
403 `outside_scope`, 404 `not_found`, 409 `conflict`, 422 `unsupported`,
429 `queue_full`, or 503 `temporarily_unavailable`. Network loss or a retryable
server error requires redelivery of the same event bytes. A rejected event has no success receipt and
must not be marked imported or removed from an operational outbox.

Native schemas 1000017 and 1000021 own producer identities, Stash API token
grants, and receipts. Foreign keys bind receipts to their actual capture or job,
recorded collection revision, and token scope. Anonymised exports remove them with private source evidence.


## Native metadata policies

Policies belong to a source collection, including an unsourced directory or
manual batch. Definitions and their history live in the native database; there
is no plugin settings JSON or parallel catalog writer. Migration creates no
policies and changes no selected metadata. Each field decision made by a policy
references the exact policy revision, with capture provenance for source-backed
mappings. Collection scope changes require reviewing a new policy revision.

The application-authenticated API lives under `/api/v3/archive`. Producer tokens
do not authorize these routes. Browser writes use the same origin checks as
other native administration. JSON uses snake_case keys.

| Method and path | Contract |
| --- | --- |
| `GET /media-roots?after=<uuid>` | Up to 50 logical roots and their reviewed local bindings |
| `POST /media-roots`, `PUT /media-roots/<uuid>` | Complete definition: `expected_revision` (zero on create), `label`, `state`, `server_path` (null unbinds), `reason`; the server probes and records directory identity |
| `GET /collections?after=<uuid>` | Up to 50 current source collections |
| `POST /collections`, `PUT /collections/<uuid>` | Complete definition: `expected_revision`, `label`, `kind`, `namespace`, `state`, `target_url`, nullable `account_uuid` and `root_uuid`, `path_prefix`, `reason`; path prefix is `.` for the whole root |
| `GET /metadata-fields/scene`, `/image`, `/gallery` | Native field names, value types, clear values and relationship kinds; intake policies currently target scenes/images |
| `GET /collections/<uuid>/metadata-policy` | Current policy, or null |
| `PUT /collections/<uuid>/metadata-policy` | `expected_revision`, `expected_collection_revision`, `definition`, `reason`; only owner-reviewed edits are accepted here |
| `GET /collections/<uuid>/metadata-policy/history?after=<revision>` | Up to 50 immutable revisions in ascending order |
| `POST /metadata-policy/preview` | Existing `collection_uuid`, `entity_uuid`, and one of its `file_uuid` values; optional `source: {capture_uuid, attachment_uuid}` and `include_data` |
| `POST /metadata-policy/apply` | The same selection plus the preview `digest`; recomputes it in the write transaction and rejects changed selections/candidates with 409 |

A definition can contain both `scene` and `image` rules. This directory example
initializes filenames and assigns an explicitly selected performer UUID:

```json
{
  "enabled": true,
  "apply_to_scans": true,
  "rules": {
    "scene": {
      "on_create": true,
      "on_existing": false,
      "skip_organized_on_create": false,
      "mark_organized": false,
      "filename_title_fallback": true,
      "mappings": {
        "performers": {"value": ["56c9895d-03c1-4a93-8c0d-fbd99d27de22"]},
        "title": {"jq": ".source.metadata.title // empty | select(type == \"string\" and length > 0)"}
      }
    }
  }
}
```

Each mapping has exactly one `value` or `jq`. The outer configuration is normal
JSON; constant values are JSON values, not JSON serialized inside strings. Jq
returns one value per field; `empty` omits it, while null is an actual value and
must satisfy that field's type. Expressions share a 250 ms deadline and retain
1 MiB output, 16 KiB expression and one-result limits. Native input allows 12 MiB
for retained source payload plus entity data; plugin limits remain unchanged.

Mapping data contains `source` (the selected post/capture, normalized `metadata`,
and reconstructed retained `payload`, or null), `entity` (UUID, kind and permitted
native field values), and `context` (creation flag, filename and relative path).
It includes no plugin configuration, mapping definitions, settings, duplicate edit
`input`, or field-name list. Expanded data is returned only with `include_data`;
normal previews contain proposed changes and their status. Protected values and
disabled rules can be inspected without applying them. HTTP previews select an
existing entity; requests cannot impersonate a creation event.

Only typed curated fields from `MetadataFields` are accepted. Identity, file
fingerprints, jobs and raw source evidence are not mapping targets. Relationships
use native UUIDs; redirects resolve to the surviving identity and deleted targets
require review. To opt into canonical/alias matching, a `performers` mapping may
set `performer_names: true` and return an array of names. The indexed lookup
reports all candidates (up to 100, with an explicit overflow flag). It never
chooses between a canonical name and a colliding alias, creates a performer, or
uses approximate matches. Unambiguous names can be added while collisions remain
for review; an unresolved name cannot erase existing inherited attribution.
Use an explicit UUID to resolve ambiguity. Publisher/account ownership alone
never supplies depicted performers.

`on_create` and `on_existing` control automatic application independently.
`skip_organized_on_create` reproduces the old creation-only condition, including
creation requests that already supplied organized=true. `mark_organized` sets an
unprotected organized field only when a non-filename mapping selected a value
and no mapping/name conflict remains. It never clears an existing organized
choice and never treats organization as identity or download completeness.

Preserved legacy values and explicit set/clear decisions always win. Filename
fallback initializes an empty inherited title; another file attached to the same
item cannot oscillate it between filenames. A permitted source mapping can later
replace that fallback. Newer captures of the same post can update inherited
source fields. Older captures cannot roll them back; another post or collection
with a different selection requires review instead of winning by arrival order.

For ordinary scans, `apply_to_scans` opts a collection into directory matching.
Lookups use bound root paths and the file's ancestor prefixes, with dedicated
indexes. The most specific scope wins; equal-depth matches require review, and
a disabled child policy can mask an enabled parent. The scanner records manual
intake provenance without a fictional account/post and re-evaluates eligible
unchanged files on rescan. ZIP members do not yet support folder policies.

Remaining work includes the native forms/review queue, migration and comparison
of installed plugin mappings, per-name reviewed resolution, and catalog import.
Scan conflicts currently appear in scan logs and repeatable previews; file jobs
also retain their review summary in durable status. General API/scan edit hooks
still use the existing after-commit delivery path; the ingestion worker retains
its retryable notification checkpoint.

## Producer delivery queue

The Python package in [integrations/gallery-dl](../integrations/gallery-dl/README.md)
implements a durable producer outbox and the HTTP delivery client. It binds a
queue to a stable producer UUID and Stash origin, stores exact event bytes before
delivery, and retains receipts atomically with removing acknowledged payloads.
Source capture dependencies, capacity bounds, concurrent drainer fences,
persistent backoff and explicit review states survive worker restart. A file
receipt acknowledges admission to verification, not media completion.

The same producer database now coalesces source-run requests while Stash is
unavailable. Overlapping windows merge and disjoint ranges stay separate. Once
a submission is claimed, its UUID and bytes stay fixed across backoff, lost
responses and restart. Native admission receipts echo the committed request
UUID; admission does not assert that a scrape has finished. Optional caller
tickets prevent command-response retries from scheduling the same execution
again. Producer schema 2 adds this queue transactionally to schema 1 without
rewriting pending events, dependencies, receipts or delivery leases.

The package uses `STASH_INGEST_TOKEN` (or another named environment reference)
only for Stash's API. Website logins, cookies and downloader proxy settings
remain in the gallery-dl environment and are not managed by this client.

The adapter queues retained captures before download, checks source leases and
the pinned destination, holds shared filesystem locks, and queues flushed final
files before archive acknowledgement. It preserves original Twitter attachment
membership through gallery-dl's transformation and supports single-media Reddit
evidence without inventing albums. The claimed date window is enforced with
source timestamp precision, including out-of-order posts and parent dates for
linked child work. The
[package guide](../integrations/gallery-dl/README.md) describes supported runtime
paths and remaining caller responsibilities.

The shared Python/Go policy corpus verifies retention before queued persistence.
`make pre-producer` installs the pinned gallery-dl/yt-dlp test runtime into an
isolated environment; `make validate-producer` runs delivery and real downloader
tests. The backend's HTTP tests execute delivery, source leases and the download
worker against isolated native databases, including a lost completion response
and file admission after outbox restart. Python 3.12 or newer is required.

This package is not yet installed into the host/n8n launch paths. Deployment
configuration conversion, additional source adapters and conversion of recovery
and scheduled callers remain required before cutover.
