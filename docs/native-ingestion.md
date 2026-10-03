# Native producer ingestion

The development server exposes a separate, versioned producer interface at
`/api/v3/ingest`. Protocol 1 currently accepts `source.capture` events for
Reddit, Twitter, Bluesky, TikTok, Instagram posts/reels, Patreon, Fansly and
Kemono/Coomer post identities. It records sanitized source evidence,
publisher choices, collection provenance, and supported album manifests with a
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
download attempt with concurrent event delivery. Bounded dispatch now discovers
eligible work after restart with persistent producer pagination/backoff. Caller
snapshots now preserve URL lists and time windows before lookup; resolution
commits each selected collection/revision and source ticket atomically in the
producer outbox. Whole-call inspection requires source coverage for every
original ticket. Launcher integration, additional
post adapters, host/n8n activation, catalog import, and native administration/review
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

Each token belongs to one durable producer UUID. It can grant specific collection
UUIDs at specific logical root UUIDs, or all registered collections at explicitly
selected logical roots. A root grant also covers collections registered there
later, so a saved list with hundreds of sources does not need one grant per
source. It grants no collection creation, editing or other administration.
Existing collection grants remain limited to their named collections.

A null root in a collection grant permits metadata access for that unbound
collection; it is not a wildcard. Root grants never include unbound collections.
One token can hold up to 128 combined collection and root grants, with one root
per named collection. A binding change can use separate tokens while an older
outbox drains. Root UUIDs convey no arbitrary host path authority. Tokens have
32 random secret bytes; only their SHA-256 verifiers
are stored. Optional expiry and permanent Stash API-token revocation are checked
in each write transaction. API-token rotation retains the producer UUID and event
receipts.

The application's existing authenticated router exposes administration:

| Method and route under `/api/v3/ingest-admin` | Body / result |
| --- | --- |
| `POST /producers` | `{"label":"Host gallery-dl"}`; returns a producer UUID |
| `GET /producers?after=<uuid>` | Up to 50 producers ordered by UUID |
| `POST /producers/<uuid>/credentials` | `{"scopes":[{"collection_uuid":"…","root_uuid":null}],"root_uuids":[],"expires_at":null}`; returns token metadata and its secret once; either grant list may be empty |
| `GET /producers/<uuid>/credentials?after=<uuid>` | Up to 50 credential records; excludes tokens and verifiers |
| `DELETE /credentials/<uuid>` | Permanently revokes the credential; repeated revocation is safe |

Administration requires application access, independently of the producer token.
JSON writes and origin checks reject cross-site browser administration. Issuing
a named collection grant requires an active collection and a root used by one
of its recorded definitions. An explicitly selected historical binding supports
token rotation while old events remain undelivered. A root grant requires an
active registered root. Retired/disabled roots cannot receive new grants.
Capabilities and token administration responses include both `scopes` and
`root_uuids`. Native migration 1000024 adds no grants to existing tokens.

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
cancellation, bounded recovery, and atomic publication. Supported kinds are
`media.verify`, `album.backfill` (schema 1000040), and `text.translate` (schema
1000045). The HTTP server starts
the file worker when FFmpeg/FFprobe are configured, plus an independent
metadata-only album worker. Both start after plugin routing is initialized and
are cancelled and joined during shutdown before the manager closes SQLite. Transient loop
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

`ArchiveJob.ClaimByID` claims one explicitly selected job revision using indexed
lookups. It preserves readiness, attempt limits and shared-resource exclusion;
it cannot fall through to unrelated work or recover other expired jobs. Trusted
queue maintenance performs recovery separately. A changed or missing selection
returns a conflict; an unchanged but unavailable selection returns no claim.
Both claim paths commit the running job and its attempt together, even when a
caller catches a late write error. This is an internal transaction primitive:
external services must authorize the job's domain scope and current producer
access in that same transaction, with their own final checks before commit.
The `Durable.ClaimByID` convenience wrapper is for trusted internal callers and
does not provide producer authorization or expose a generic HTTP claim route.

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

### Historical source album backfill

These routes live under `/api/v3/archive` with normal application authentication
and same-origin checks. Scoped producer tokens do not authorize these operations.
The [matching policies](native-source-identity.md#matching-imported-media-to-source-albums)
use retained evidence and current library records; they perform no downloads or
filesystem inspection and do not require FFmpeg.

| Method and route | Request or result |
|---|---|
| `GET /album-backfill-posts` | Selected post UUID, state, selection UUID and mode; `after` post UUID and `limit` (default 50, maximum 100) |
| `POST /posts/{post}/album-backfill/preview` | `{ "policy": "legacy-reddit-filename-v1" }`; read-only action, signature, proposed choices, ordered slots and membership changes |
| `POST /posts/{post}/album-backfills` | `{ "request_uuid": "…", "policy": "…", "signature": "…" }`; queues the reviewed preview |
| `GET /album-backfill-requests/{request}` | Looks up the original submission, including after a lost response |
| `GET /album-backfills/{job}` | Current work and publication status |
| `GET /posts/{post}/album-backfills` | Targeted job history, `after` sequence and `limit` (default 50, maximum 100) |
| `GET /album-backfills/{job}/attempts` | Attempt history, `after` fence and the same bounded limit |
| `POST /album-backfills/{job}/cancel` | `{ "expected_revision": 3 }`; cancels queued/running work |
| `POST /album-backfills/{job}/retry` | `{ "request_uuid": "…", "expected_revision": 3 }`; new work resuming a failed/cancelled job |

Clients must retain the exact request UUID, policy and signature before submitting.
An uncertain response is resolved by looking up or resending that same request.
Admission returns 202 for queued/running work; terminal replay returns 200.
Status inspection returns 200 even while work remains pending. Queue saturation
returns 429. Changed previews or job revisions return 409, invalid requests 400,
missing identities 404, and a matching review limit 422. A complete bounded
preview is required; the server never treats truncated candidates as unique.

The worker revalidates the signature immediately before applying choices. Gallery
membership and its publication checkpoint commit in one managed transaction.
`publication_committed` reports that boundary; `hooks_finished` becomes true only
when the worker has delivered the applicable hooks and completed the attempt.
`publication` contains the original event/post/gallery UUIDs, action and counts,
without repeating large previews in job results. Disabled or ineligible posts
can complete as no-ops. Only created or changed galleries notify plugins.
Every status includes the original preview `signature`, allowing a resumed
client to verify its post, policy and reviewed plan together.

Automatic retry uses at most ten attempts with a 30-second delay and renewed
worker leases. Notification delivery is at least once; plugins can deduplicate
the stable `hookContext.eventId`. Explicit retry retains terminal history in the
old job and creates a new submission. If publication already committed, it resumes
only notification delivery, preserving the original event identity and any later
library edits. This remains true after cancelling a queued notification retry.
Unpublished retries must still match their original preview; changed evidence
requires a fresh preview and request. Cancellation cannot undo committed changes
or retract a notification already delivered.

HTTP responses use snake_case names. Initial title/details/date appear only for
gallery creation, and dates retain their calendar precision. The API is available
on the development branch. The supported
[`stash-backfill-source-albums` command](../integrations/gallery-dl/README.md#historical-source-albums)
prepares private immutable plans, applies them, inspects saved submissions and
prepares explicit retries. Native review UI and production activation remain pending.

Discovery uses indexed UUID pagination over selected attachment lists. It includes
disabled and forgotten posts for exclusion accounting, and returns no materialized
manifests. The command records forgotten posts without submitting work for them;
disabled and ineligible previews can be reviewed and submitted as no-ops. Discovery
across pages is not a global snapshot under concurrent writes. Migration must use
its quiesced database boundary or a reviewed explicit list of post UUIDs; per-post
preview signatures still protect against changed evidence before application.

## Wire contract

All producer routes require the bearer token and reject query parameters:

| Method and route under `/api/v3/ingest` | Result |
| --- | --- |
| `GET /capabilities` | Protocol, producer UUID, scopes, supported kinds/namespaces, retention policy, and request limits |
| `POST /collections/lookup` | Current permitted collection candidates for exact source URLs at one logical root |
| `POST /batches` | An ordered result for every submitted event |
| `GET /receipts/<event-uuid>` | That producer's original receipt, subject to the token's collection/root scope |
| `GET /receipts/<event-uuid>/status` | Current job state, attempt, bounded error code and safe result; excludes worker arguments and local mount paths |

A batch contains 1–8 entries and is at most 16 MiB. Each event is at most 5 MiB;
its source payload remains limited to 4 MiB and projected metadata to 256 KiB.
Send `Content-Type: application/json`; compressed request bodies are not accepted.
Unknown envelope fields, duplicate JSON keys, invalid Unicode, excessive nesting,
and unsupported protocol/kind/retention versions are rejected.

Collection lookup accepts `{"root_uuid":"…","targets":["https://…"]}` with
1–50 distinct URLs; a null root selects unbound metadata collections. Its
`collection_lookup` capability is advertised separately. The response echoes
the root and requested URLs in order, each with candidate `collection_uuid`,
`collection_revision` and `state` values. Each URL returns at most 128 candidates
and a `has_more` boolean; `true` means additional matches exist and requires
review, never automatic selection. Empty candidate lists are explicit.
The lookup applies the token's collection
and root grants before returning candidates and reads only current definitions.
Historical target URLs and moved roots cannot redirect a caller's source work.
Labels, account IDs and path information are excluded from this projection.
Callers must preserve ambiguity and inactive states for review; this read grants
no collection creation or modification authority. Native submission still
validates the chosen definition when admitting the request.
The Python lookup client accepts at most 4 MiB for this bounded response; other
producer responses retain their 1 MiB limit.

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

Post identity is verified against captured evidence, preserving exact large
integers and rejecting disagreeing identifiers:

| Extractor | Post identity |
| --- | --- |
| Reddit | `native:reddit`, captured `id` |
| Twitter | `native:twitter`, agreeing `tweet_id`/`rest_id`/`id_str` |
| Bluesky | `native:bluesky`, `author.did/post_id`, verified against the feed-post AT URI when present |
| TikTok, Patreon, Fansly | `native:<service>`, numeric post `id` |
| Instagram posts/reels | `native:instagram`, numeric `post_id`, agreeing with `sidecar_media_id` when present |
| Kemono/Coomer | `mirror:<extractor>:<service>`, captured `user/id` |

A Bluesky handle cannot substitute for its DID; neither an Instagram file's
`media_id` nor a story/highlight container ID is treated as a regular post ID.
Mirror account and service scopes remain distinct from native service accounts.
Capture provenance records the actual `coomer`/`kemono` extractor separately.
Capabilities enumerate native services in `post_namespaces` and advertise
`mirror:coomer:`/`mirror:kemono:` in `post_namespace_prefixes`; the service suffix
must still satisfy the qualified-namespace rules.

Reddit crossposts and independent social posts keep their own identity. Supported
Imgur/Redgifs child hosts retain their enclosing post scope, caption and publisher.
A social extractor's parent feed/profile does not replace its actual post.
Unknown post adapters are rejected even when their account parser is supported.
Post support alone does not establish attachment membership or enable a download
worker: file selection and source-window adapters still cover Reddit/Twitter.

## Metadata enrichment checkpoints

The isolated gallery-dl metadata collector returns a compact
`stash-metadata-fetch-v1` transcript. This is an internal producer checkpoint,
separate from the `source.capture` event format. The enrichment
coordinator now binds jobs to target/source revisions and persists checkpoints
under producer-owned leases. It can publish verified checkpoints through the
native capture transaction. Scoped producer routes and a Python transport/lease
client are available. Durable local execution, dispatch and scheduled-service
conversion remain unfinished.

`archive.ParseEnrichmentTranscript` validates the exact envelope, supported
direct-post URL, extractor/retention versions and each record. Backward base
references share unchanged metadata; patches replace top-level fields and a
sorted removal list preserves missing-versus-null semantics. Parent references
reconstruct child attribution to at most two levels. Source retention is
checked on reconstructed metadata. Observation times retain their original
precision, and large source identifiers never pass through floating point.
Checkpoint reads and extraction subprocess handoff also preserve original JSON
number tokens, including decimal precision, exponent spelling and negative zero.
Ordinary event encoding and existing receipt digests are unchanged.

Limits are 32 MiB for the compact body, 1,024 records, 256 references per pending
or unresolved list, 4 MiB per reconstructed record including parents, and
128 MiB for the sum of reconstructed record sizes. Compact sharing cannot bypass
those expansion limits. Malformed or duplicate records/references are rejected.

A resumed transcript must keep the earlier record and unresolved-reference
prefixes exactly, including original observation times. Each earlier pending
child must remain pending, become explicitly unresolved, or have new records
for its URL and parent. Validation alone neither proves the intended existing
post's identity nor certifies publication or completion.

The coordinator authenticates current Stash producer credentials against the
job's recorded collection/root scope. Moving a collection cannot expose old
checkpoints to a new root's credentials. Each attempt is bound to its producer;
resuming on a new attempt preserves the original producer for already retained
observations. Renewals and new checkpoint writes recheck eligibility, credentials
and lease deadlines before commit. Exact acknowledgement replay can confirm an
old write after its attempt expires without restoring that attempt's ownership.

Only the current compact body is retained per job; small immutable receipts and
record hashes preserve prior acknowledgements and provenance. Storage is bounded
across jobs, including failed/cancelled work. Saving a checkpoint leaves the
target pending and selected media metadata unchanged. The separate `Publish`
operation requires a saved revision/digest with no pending children. It verifies
every record against the existing target post, preserves original producers,
reuses capture/publisher/album/translation services and commits captures, job
result and target completion together. Ordinary job success cannot bypass that
proof. Exact completion replay survives expiry and later source edits without
applying a new policy. Unresolved external references remain explicit limitations.

Successful publication verifies and releases the compact staging body atomically,
retaining native captures, acknowledgement/provenance rows, unresolved references
and a versioned integrity proof. Failed/cancelled work retains staging. Older
publications keep their bodies through migration until the scoped internal
`ReleaseCheckpoint` operation verifies them. A null `CheckpointHead` with a
publication/release receipt means completed staging was released, not that the
observations disappeared. Exact acknowledgement and completion replays still work.
Worker dispatch, source scheduling and the remaining download
adapters remain transition work. See [staging release](native-schema.md#completed-enrichment-staging-release),
[publication](native-schema.md#verified-enrichment-publication),
[checkpoint storage and lifecycle](native-schema.md#enrichment-jobs-and-checkpoint-ownership)
and the [producer contract](../integrations/gallery-dl/README.md#metadata-only-extraction-for-enrichment).

### Producer enrichment API

All paths below are relative to `/api/v3/ingest/enrichment`. They require a
current Stash producer bearer token. Authentication precedes checkpoint body
decoding; each operation rechecks collection/root grants and attempt ownership
in its domain transaction. Ordinary Stash API keys and session cookies do not
grant producer access. Website credentials stay in the external worker.
Capabilities advertise `enrichment_protocol: 1`, `enrichment_dispatch_protocol: 1`,
`enrichment_source_pacing_protocol: 1` and
`max_enrichment_checkpoint_bytes: 33554432`.

| Method and path | Request / result |
| --- | --- |
| `POST /collections/{uuid}/ready` | `{limit, after?}` selects up to 100 eligible **unadmitted** targets; `after` contains the previous priority, `not_before` and UUID |
| `POST /collections/{uuid}/jobs/ready` | `{policy_sha256, extractor_version, after, limit}` discovers eligible admitted jobs, ordered after their integer sequence; returns `{sequence, uuid}` candidates |
| `POST /targets/{uuid}/jobs` | `{expected_revision, policy_sha256, extractor_version}` admits or replays the job bound to that target revision |
| `GET /jobs/{uuid}` | Returns `{job, target}`, including immutable source URL/input and the target's current scheduling state |
| `POST /jobs/{uuid}/claim` | `{expected_revision, owner_uuid, policy_sha256, extractor_version, lease_seconds}` claims the selected job; unchanged but unavailable work returns 204 |
| `POST /jobs/{uuid}/renew` | `{owner_uuid, fence, lease_seconds}` renews current ownership |
| `POST /jobs/{uuid}/source` | `{owner_uuid, fence, url}` reserves the main service or a supported linked service before extractor initialization; returns `{job_uuid, fence, ready}` |
| `GET /jobs/{uuid}/checkpoint` | Current receipt plus compact `body`, or null when absent/released |
| `POST /jobs/{uuid}/checkpoint` | `{owner_uuid, fence, expected_revision, body}` saves or replays retained evidence; `body` is a JSON object |
| `POST /jobs/{uuid}/publish` | `{owner_uuid, fence, checkpoint_revision, checkpoint_sha256}` verifies and publishes the saved checkpoint atomically |
| `POST /jobs/{uuid}/failure` | `{owner_uuid, fence, error_code}` records a controlled unsuccessful attempt and returns its immutable receipt |
| `GET /jobs/{uuid}/publication` | The publication receipt, or null |
| `GET /jobs/{uuid}/release` | The verified staging-release receipt and unresolved references, or null |

Job identity comes from the route and producer identity from the credential.
Unknown request fields are rejected. Leases last 5–900 seconds. Checkpoint
envelopes allow 32 MiB plus 4 KiB; all reconstructed-record limits still apply.
Source responses use the native JSON representation without expanding HTML
characters. Clients bound response reads and validate checkpoint hashes,
counts, source URL and runtime before resuming. The Python lease helper uses
the server's HTTP date and monotonic request start, stops further extraction
when renewal fails, and does not extend ownership based on its local wall clock.

Retryable failure codes are `rate_limited`, `extraction_failed`, `timeout`,
`worker_failed` and `source_busy`. Server backoff starts at five minutes and increases by attempt;
the eighth attempt becomes terminal. Authentication, access, challenge,
not-found, unsupported-extractor, malformed-checkpoint, size, runtime and
post-identity failures require review. Clients cannot report success through
the failure route or supply their own retry deadline. Failed attempts retain
checkpoints and do not complete the target. A terminal job requires an explicit
owner retry that creates a new target revision.

Claims coordinate with source downloads in the same write transaction. Claims for a source-run target service cannot overlap an enrichment reservation
for that service. Enrichment also excludes other work in its collection. Independent downloads retain their existing
destination/target exclusions. A download run's current binding covers its target
service; reserving its own linked extractors remains download-adapter work. Eligible queued downloads take precedence over
new enrichment; stale definitions and deferred/backoff work do not reserve the
service. A retry reserves its retained pending child services at claim time.

Before initializing a newly discovered Redgifs or Imgur child extractor, the
worker calls `/source` using its current lease. The route accepts the job's main
service and those two supported child services, with producer/lease/source checks
through commit. A reservation lasts only for that attempt; replay cannot prolong
it or transfer it to a successor. A busy response leaves the child's URL in the
checkpoint with `source_busy`, preserving parent observations for child-only retry.
Scheduling reservations store service identities, not website credentials or URLs.

Native cooldowns are shared across producers and both work families. Typed
rate-limit, timeout and extraction failures pause the affected service for at
least one hour; authentication/challenge failures pause it for at least one day.
A longer native retry deadline wins. A retained child failure pauses that child's
service, independently of the post's main service. Missing posts, account access
denials, local worker failures and busy reservations do not declare a service
outage. Replaying an old failure receipt does not extend its cooldown. Mirror
sites use their contacted mirror service, independently of creator-account
namespaces such as OnlyFans or Patreon.

Lost failure responses replay against the original producer/owner/fence and
error code, even after a successor attempt starts. They cannot stop the successor.
A revoked credential cannot replay; a replacement credential for the same
producer with the original grants can. Reposting an unchanged checkpoint can
return a predecessor's receipt. Preserve that acknowledgement's original fence;
only publishing the currently selected checkpoint proves completion.

Target pages preserve priority descending, deadline ascending and UUID ascending
order. Job discovery reads the bounded active index and filters the selected
collection, runtime/policy, native backoff and current source eligibility. It
does not traverse historical job rows. Authentication requires the collection's
current root scope; changed historical jobs are ineligible and are never exposed
through the current root's discovery grant. Claim independently checks authority.

The native server runs enrichment maintenance every 30 seconds independently of
media and translation workers. It cancels stale source/target work, recovers
expired attempts with backoff and preserves checkpoint/receipt evidence. Claims
and source reservations also recover conflicting expired work; discovery remains
read-only. Stash never contacts websites for these operations. Multi-collection
fairness, legacy queue import and
host/n8n activation remain required before switching production schedules.

The Python selected-job executor now journals stable claim requests, returned
checkpoint bytes and pending publication/failure operations in producer outbox
schema 8. It reserves checkpoint capacity before extraction and preserves fresh
results before checking for late lease loss. An accepted checkpoint and its next
delivery intent are committed locally together. Exact acknowledgements release
local bodies; conflicting successor checkpoints or rejected bodies remain in
review. Delivery-only recovery needs the Stash producer token, with no website
profile or credentials. See the [profile and CLI contract](../integrations/gallery-dl/README.md#durable-selected-job-execution).
The selected-job executor accepts an already admitted UUID. The collection
dispatcher discovers those jobs before admitting fresh targets; producer schema
9 persists its cursors and backoff across restarts. It can also deliver saved
results without loading a profile. See [dispatch and maintenance](../integrations/gallery-dl/README.md#enrichment-dispatch-and-native-maintenance).

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
| `POST /runs/ready` | `root_uuid`, `policy_sha256`, optional integer `after`; up to 50 permitted download candidates as `sequence`/`uuid` pairs |
| `POST /runs/<uuid>/attempts` | Optional integer `after` fence; at most 50 attempts |
| `POST /runs/<uuid>/claim` | `owner_uuid`, `policy_sha256`, `lease_seconds` (5–900) |
| `POST /runs/<uuid>/lease` | `owner_uuid`, `fence`, and exactly one of `lease_seconds`, `progress`, or `outcome` |
| `POST /runs/<uuid>/source` | `owner_uuid`, `fence`, `url`; returns `run_uuid`, `fence`, `source_scope` and boolean `ready` |

Paths above are relative to `/api/v3/ingest`. Pagination uses JSON bodies;
query-string tokens and parameters remain rejected. A claim returns 204 and
`Retry-After: 5` while unavailable; inspect status and back off. Repeating a claim
with the same producer and worker UUID returns its still-valid lease. A different
producer or worker cannot borrow it. Rotate the ingestion token while retaining
the producer UUID when the same worker should keep ownership.

The download executor requires `source_run_pacing_protocol: 1`. Before each root
or linked extractor initializes, it reserves the contacted service through
`/source`. The current attempt may reserve its root service, Redgifs or Imgur.
Authority, definition and lease validity are checked through transaction commit.
A busy service returns `ready: false` and remains a recorded dependency without
holding it. Finish that attempt with `state: "retry"`, `error_code: "source_busy"`
and `error_scope` equal to the returned `source_scope`. Exact-window retries wait
for recorded dependencies before repeating parent extraction; a widened traversal
starts its own dependency set. Repeated reservations under the same fence do not
extend ownership or rewrite service start times.

An unsuccessful `outcome` may include `error_scope` with a controlled source code:
`source_busy`, `rate_limited`, `timeout`, `extraction_failed`, `authentication`,
`access_denied`, `challenge` or `not_found`. Except for `source_busy`, the attempt
must have held that service. Attempts expose the retained `error_scope`; lost
completion responses must match outcome, code and scope as well as producer,
owner and fence. Error scopes contain service identities, never source URLs.
Only the failing service receives an applicable shared cooldown. Busy sources,
missing posts, access-denied accounts and local/media failures do not create a
service-wide outage. Source completion still does not certify media intake.

Capabilities advertise `source_run_dispatch: true` for scoped work discovery.
It uses the bounded active-run index and the server clock, excluding future
retry times, live leases and explicitly deferred work. Expired leases can be
discovered, but only a claim can recover them, applying normal retry delay and
deferral limits. Pagination never grants ownership or certifies completion.
The producer's `dispatch` command retains its cursor/backoff across restarts,
continues delivery/admission during discovery outages, and still uses the
existing claim before downloading. See the producer guide for cycle outcomes.

An explicit root grant includes eligible runs from all registered collections at
that root. Collection grants still filter discovery by their named IDs. Run
history and admission replay check the run's recorded root, including after a
collection moves: a token for only the new root cannot read or replay old-root
work. New submissions against another root are rolled back before admission.

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

## Automatic source translations

Translation has a separate collection policy from scene/image metadata mapping.
Configure it through `PUT /api/v3/archive/collections/{uuid}/translation-policy`:

```json
{
  "expected_revision": 0,
  "expected_collection_revision": 1,
  "reason": "Translate new source titles and captions",
  "definition": {
    "enabled": true,
    "provider_policy": "translate-shell-bing-text-v1",
    "target_language": "en",
    "title": true,
    "caption": true,
    "priority": 100
  }
}
```

Zero policy revision creates the first policy; later writes require its current
revision. Title reads normalized source `metadata.title`; caption reads
`metadata.original_text`. Empty or whitespace-only values produce `no_text`
entries. Nonempty original strings are preserved exactly. Equal strings share
one request/cache even across posts and fields. Target identity retains the post,
collection revision and field; repeated media captures of one post reuse its
existing work without changing a hold, retry deadline, priority or completion.

First accepted capture delivery commits a `translation` decision with the capture
and receipt. Its status is `no_policy`, `disabled`, `collection_changed`, or
`recorded`; individual recorded entries say `created`, `retained`, or `no_text`.
Target references identify the revision observed by that decision, not a claim
that provider execution finished. The separate translation worker must be enabled
and configured to process due targets. Scheduling and translation evidence do not
select scene/image titles or other curated fields.

A missing policy disables scheduling. Disabling a policy affects future captures;
already queued targets retain their own scheduling controls. Policy changes do
not revisit previous captures, including accepted offline deliveries. Their exact
receipts remain replayable. Existing text can be queued explicitly through the
[translation target APIs](native-schema.md#translation-requests-cache-and-targets).

Policies bind the reviewed collection revision. If a configured policy no longer
matches the event's collection revision or current collection, source evidence
still commits and `translation_policy` is reported for review. A new reviewed
policy applies to subsequent captures under that collection revision. Replaying
an old event cannot overwrite its saved decision. Ordinary file scans have no
source text and do not fabricate a capture for this policy.

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
again. Producer schema 4 retains each ticket's assignment to its original frozen
submissions, including ranges spread across several requests. `ticket-status`
validates their native runs and completed windows; a later unrelated rescan cannot
complete an earlier cancelled request. Completed source traversal does not certify
file intake; inspect file receipts separately. Upgrading earlier producer schemas
preserves events, dependencies, receipts, delivery leases and dispatch backoff in
one transaction; old ticket assignments are reconstructed from their first
covering submissions.

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

The local configuration converter now stages profiles from ordered gallery-dl
JSON layers, preserving first-match rule order, archive formats and website
access references while removing recognized catalog writers. Optional source
categories retain relevant child settings and reject a different root extractor.
See the package guide for conversion and publication semantics.

Staged Twitter/Reddit host launchers now expand the existing account/list, mode
and date options into durable caller snapshots. Full-history and Reddit top mode
select separately reviewed global `skip=true` profiles. Local recording returns
the execution UUID; strict inspection cannot report success until every original
source ticket completes. The staged n8n adapter now gates source admission on
native permanent history, freezes completion proof and exposes pending results
for a durable workflow wait. An explicit maintenance importer now retains old
n8n result tokens in the producer outbox for local inspection, preserving exact
bytes and import manifests without creating source-completion proof. The other
operational journal families and saved executions still require migration before
activation.

This package is not yet installed into the host/n8n launch paths. Runtime alignment,
activation of converted profiles, additional source adapters and conversion of
recovery and scheduled callers remain required before cutover.

The producer's n8n Containerfile now builds an isolated pinned worker runtime
on an explicitly selected existing custom n8n image. A separate rehearsal image
has passed the producer suite and actual host/n8n profile comparison; selecting
that image and updating the live workflow commands still belong to cutover.

## Permanent account backfill decisions

Native backfill history records an account's accepted one-time scrape separately
from ongoing incremental runs. A subject contains `root_uuid`, `platform` and
`account`; current definitions cover the existing Reddit/Twitter n8n modes.
Reddit lookup folds name case and Twitter lookup normalizes numeric IDs. Exact
source URLs retain the requested spelling/query shape for coverage validation.
These are source-account keys, with no implied performer ownership or attribution.

| Route | Authority and behavior |
|---|---|
| `POST /api/v3/ingest/backfills/status` | Explicit producer root grant; body adds `component` to the subject |
| `POST /api/v3/ingest/backfills/complete` | Same root grant; proves native completion using original request definitions |
| `POST /api/v3/archive/backfills/status` | Application authentication; same compact status |
| `POST /api/v3/archive/backfills/import` | Application authentication; atomic batch of 1–50 historical records, at most 4 MiB |
| `GET /api/v3/archive/backfills/{decision}` | Application authentication; retained evidence for one decision |

Status is `needed`, `completed` or `skipped`, with `account_complete` and compact
decision summaries. A completed requested component suppresses that component;
the completed required set (Twitter, or both Reddit new and top) suppresses all
account backfill modes. An explicit legacy skip suppresses outstanding work
without claiming account completion. Summaries report the basis and decision
time; private logs/provenance remain in the application-only evidence endpoint.
`account_complete` describes an accepted workflow outcome, not proof that every
historical post or original media file was obtainable.

Completion accepts `uuid`, the subject, `component`, an explicit full-history
`window` (`since: null`), `policy_sha256` and 1–512 original `SourceRunRequest`
objects. Their producer-scoped stored hashes must match. Actual completed ranges
must cover the whole window for every expected component URL, using the same
root and policy. Queue admission, another account's run, fabricated ranges and
missing targets cannot produce a completion decision. The caller must select
its reviewed full-history profile; the server verifies committed run coverage,
not the source website's exhaustiveness. File intake retains its own receipts.

Use the same completion UUID and proof after a lost response. A changed proof
under that UUID conflicts. Root authority is checked again before commit;
collection-only grants cannot expose account-wide history. Capability responses
advertise `source_backfill_protocol: 1`.

Historical import accepts `root_uuid`, a stable input-database `source_uuid`,
`table` and the full source `record`. Only `backfill_completion` and
`legacy_backfill_skip` are handled by this importer. Other catalog/journal
families retain their separate migration requirements. Imported decisions never
manufacture source windows, and producer tokens cannot submit legacy assertions.
See the [maintenance importer](../integrations/gallery-dl/README.md#backfill-journal-import)
for read-only preflight and resumable batches. The [native n8n adapter](../integrations/gallery-dl/README.md#native-n8n-backfills)
uses this status/proof API with durable caller snapshots. Its producer schema 7
commits the first history decision and local source snapshot together, then retains
the original ticket bindings and submitted proof across retries. Converted graphs
wait on pending results and inspect the same token. Installed n8n workflows still
use the old runner until the remaining operational-history and deployment gates pass.
The [receipt importer](../integrations/gallery-dl/README.md#retaining-old-n8n-result-tokens)
preserves historical inspection results separately from this proof API. Importing
one never assigns an account, launches work or marks native source windows complete.

## Legacy scan journal inspection

The application-authorized `/api/v3/archive` router now accepts frozen journal
snapshots through `POST /scan-journals/import`. Its input is `uuid`, `root_uuid`,
`source_uuid` and `document`; the document contains `captured_at`, `tables` and
`external_tables`. It is limited to 10,000 rows and 8 MiB, and commits atomically.
Only the seven documented scan-history families are accepted. Unknown input
tables/fields and unsafe or unsupported command forms block import. The original
account-backfill table counts are inventoried under `external_tables`; their rows
use the separate backfill importer.

`GET /scan-journals/<uuid>` returns the retained receipt and counts.
`GET /scan-journals/<uuid>/records?after=<sequence>&table=<family>` returns up to
100 summaries in sequence order; omit `table` to inspect every family. Advance
with the last sequence and stop on an empty page. Original evidence is available
separately through `GET /scan-journal-records/<uuid>`. These routes require
application access, reject cross-origin mutations, and return `no-store` results.
Producer ingestion tokens cannot import or inspect this maintenance evidence.

The [journal command](../integrations/gallery-dl/README.md#retaining-the-scan-journal)
provides read-only preparation, an explicit reviewed-digest apply and replay after
a lost response. Retention does not activate jobs; reviewed activation uses the
separate operation below. Old runtime ownership and historical completion hashes are never substituted
for native source leases or coverage.

## Activating retained scan requests

`POST /scan-journal-activations/preview` takes `uuid`, `scan_record_uuid`,
`collection_uuid`, `collection_revision`, `root_revision`, `policy_sha256`,
`cooldown_seconds`, an explicit millisecond-precision `cutoff`, and optionally
`checkpoint_record_uuid`. It requires application access. The selected scan's
exact target URL must match the active native collection, and its snapshot root
must match the active root revision. The cutoff must be at or after the frozen
snapshot time and cannot be in the future beyond the normal one-minute allowance.
This binding records the reviewed worker profile digest; it never executes the
old command or imports its private configuration.

Preview consolidates every scan in the same snapshot/context/exact URL group.
Old commands must agree after removing their date-min option. The oldest lower
bound wins; an unbounded request includes all history. Unzoned legacy Reddit dates
keep gallery-dl's UTC interpretation. The largest retry count (capped at the native eight-failure limit) and latest
delay are retained. Fractional delays round up to the next millisecond. Any
existing deferral, or eight prior failures, leaves the run
deferred until an explicit normal source-run review. Preview returns every
included scan and deferral UUID, the effective window, recovery policy and a
`plan_sha256`; it creates no work.

`POST /scan-journal-activations` takes `{binding, expected_plan_sha256}` and commits
one new native download run with its immutable activation receipt and all original
job bindings. An existing active native run with the same work identity conflicts;
activation cannot replace a native lease, progress or pending ranges. A later
snapshot of an already activated original job also conflicts. Exact retries after
a lost response return the original activation, even after its run has started
or finished. `GET /scan-journal-activations/<uuid>` inspects the receipt.
The receipt's state describes the initial activation; inspect its `run_uuid`
through the source-run API for current execution state. Scan record responses add
`activation_uuid` when bound; original summaries and evidence
remain unchanged. Producer tokens have no access to these maintenance routes.

Without an explicit checkpoint, the worker replays the retained date window
without stopping on archived files. With a checkpoint, its scope must belong to
one of the selected scans and have the same consolidated lower bound. The first
claim seeds its old item position and `gallery-dl-archive-v1:<hash>` cursor, with
zero claimed completed files. The worker reproduces the old archive-key hash
from actual gallery-dl metadata, replays through that position and then writes
native cursors. A missing position cannot report completion. As in the old worker,
a full-history/no-skip profile without an archive stop rule ignores that legacy
checkpoint. Expanding the traversal window discards the incompatible cursor and
disables archive stopping for that window, including subsequent retries.

Only a real claim creates an attempt. Activation creates no producer identity,
access token, historical lease, source-window completion or media-intake proof.
Manual/unbound checkpoints and historical service handoffs remain separate review
evidence. The [activation CLI](../integrations/gallery-dl/README.md#activating-retained-scans)
uses the same preview/apply contract. Rehearsal bindings are not production
configuration; final activation follows the common quiesced cutover boundary.

The ingestion capability response advertises `source_run_recovery_protocol: 1`.
Current workers require it and send `recovery_protocol: 1` when claiming a run.
An older worker cannot claim recovered work: otherwise it could ignore the replay
policy and incorrectly stop at archived items. Recovery policy remains fixed
throughout the lease. Ordinary non-recovery requests keep the source-run protocol.
