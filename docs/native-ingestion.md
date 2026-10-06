# Native producer ingestion

The development server exposes a separate, versioned producer interface at
`/api/v3/ingest`. Protocol 1 currently accepts `source.capture` events for
Reddit, Twitter, Bluesky, TikTok, Instagram posts/reels and individual stories, Patreon, Fansly and
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
| Instagram stories/highlights | `native:instagram`, original story `media_id` with validated `instagram_media` evidence; container ID/type remains provenance |
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
worker: file selection and source-window adapters currently cover Reddit, Twitter,
Instagram, Coomer, Kemono, Bluesky and TikTok. Instagram requires versioned original attachment evidence for
file linking, preserves carousel slots before filtering/reordering, and treats
each story as its own post. Only evidenced carousel posts create source galleries.
Historical metadata without this evidence can retain a regular post identity,
but cannot establish attachment order from download numbering. See the
[producer's Instagram contract](../integrations/gallery-dl/README.md#instagram-downloads-and-source-albums).

Coomer/Kemono user, post and posts-listing downloads require `original=true` and
`mirror_media` version 1. The producer and backend validate the same qualified
post reference and original primary/attachment/inline membership. Primary aliases
do not invent another slot; missing attachments and repeated source positions
remain visible. File selection and download order cannot change album membership.
Unsupported audio/archive outputs retain excluded captures without file receipts
or download-archive acknowledgements. Windows use the source `published` timestamp,
never the mirror's `added` time. New manifests enable shared post-body storage
without changing the partition of historical captures. See the
[mirror contract](../integrations/gallery-dl/README.md#kemonocoomer-downloads-and-source-albums)
for coverage and container limitations.

Bluesky/TikTok use `bluesky_media`/`tiktok_media` version 1 with source membership
validated against original embeds or photo/video fields. Bluesky attachment IDs
are blob CIDs, within DID/record post scope. TikTok photo IDs are original image
keys independent of signed URLs/renditions; videos have a post-scoped key.
Missing slots and repeated positions survive file selection. Windows use original
`createdAt`/`createTime`, and undated profile/shortlink routes only admit verified
post-producing child extractors. TikTok extraction failures cannot acknowledge a
complete source run. See the
[Bluesky/TikTok contract](../integrations/gallery-dl/README.md#blueskytiktok-downloads-and-source-albums)
for supported collections, auxiliary-output choices and preserved historical
capture partitions.

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

Legacy-only posts with a retained discovery lookup can gain their Reddit/Twitter
identifier during this same publication transaction. The candidate URL must
identify the fetched post and agree with independently retained account,
filename, or original text/date evidence. The existing post UUID remains intact;
an identifier already owned by another post requires review. Missing or
ambiguous proof returns HTTP 422 `post_identity_requires_review`, records a
terminal unsuccessful attempt and keeps the checkpoint available. It does not
complete the target or refetch automatically. The scoped job description includes
`discovery_resolution` after success; application users can inspect the same
proof through `/api/v3/archive/enrichment-jobs/{job}/discovery-resolution`.

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
Capabilities advertise `enrichment_protocol: 2`, `enrichment_dispatch_protocol: 1`,
`enrichment_collections_protocol: 1`, `enrichment_source_pacing_protocol: 1` and
`max_enrichment_checkpoint_bytes: 33554432`.

| Method and path | Request / result |
| --- | --- |
| `POST /collections/ready` | `{policy_sha256, extractor_version, after, limit}` discovers up to 100 permitted active collections with eligible work; returns `{uuid}` candidates ordered after the UUID cursor |
| `POST /collections/{uuid}/ready` | `{limit, after?}` selects up to 100 eligible **unadmitted** targets; `after` contains the previous priority, `not_before` and UUID |
| `POST /collections/{uuid}/jobs/ready` | `{policy_sha256, extractor_version, after, limit}` discovers eligible admitted jobs, ordered after their integer sequence; returns `{sequence, uuid}` candidates |
| `POST /targets/{uuid}/jobs` | `{expected_revision, policy_sha256, extractor_version}` admits or replays the job bound to that target revision |
| `POST /handoffs/{uuid}/jobs` | `{expected_plan_sha256, policy_sha256, extractor_version}` atomically consumes an application-reviewed checkpoint handoff or recovers its original job |
| `GET /jobs/{uuid}` | Returns `{job, target, discovery_resolution?}`, including immutable source URL/input, current scheduling state and any verified legacy identity proof |
| `GET /jobs/{uuid}/seed` | Verified `{handoff_uuid, plan_sha256, sha256, body}` for the job's retained handoff, or null for an ordinary job; requires the job's producer scope |
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

Argument version 2 pins capture-context semantics; an optional `handoff` pins
the reviewed seed. A new ordinary job uses a `stash-metadata-fetch-v1` transcript;
a handoff job uses `stash-metadata-fetch-v2`, preserving original captures in its
immutable prefix. The worker reads a seed only when it has no native checkpoint,
then creates its first real checkpoint at expected revision zero. Seed reads
carry no lease/attempt or checkpoint revision. A saved local delivery is recovered
before another seed read or fetch. Original native version-1 jobs remain readable
and executable under their frozen identity/proof contract. See
[reviewed execution](native-schema.md#executing-a-reviewed-checkpoint-handoff).

Retryable failure codes are `rate_limited`, `extraction_failed`, `timeout`,
`worker_failed` and `source_busy`. Server backoff starts at five minutes and increases by attempt;
the eighth attempt becomes terminal. Authentication, access, challenge,
not-found, unsupported-extractor, malformed-checkpoint, size, runtime and
post-identity failures require review. Clients cannot report success through
the failure route or supply their own retry deadline. Failed attempts retain
checkpoints and do not complete the target. A terminal job requires an explicit
owner retry that creates a new target revision.

Claims coordinate with source downloads in the same write transaction. Enrichment
excludes other claims for its services and collection. Download attempts reserve
their root and supported linked services; independent downloads retain their
existing destination/target exclusions. Eligible queued downloads initially take
precedence. Actual enrichment claim requests keep a 90-second interest deadline,
refreshed by polling. After four download starts or two minutes of live waiting,
the oldest eligible requester receives the next turn once existing downloads
drain. An enrichment start resets download preference. Stale/held work and
cooling dependencies cannot reserve unrelated services. A retry reserves its
retained pending child services at claim time. See the
[scheduling contract](native-schema.md#bounded-source-preference).

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

Collection discovery applies those current grants before pagination. It includes
collections with eligible unadmitted targets or due queued jobs for the requested
runtime/policy; held, stale, running and terminal jobs alone do not make a
collection ready. A root grant covers newly registered collections at that root;
an exact collection grant with no root covers only that unbound collection.
Moving a collection cannot reuse its old root grant. The response contains only
UUIDs, and the caller cannot submit replacement grants. Discovery neither admits
work nor changes leases or scheduling state.

The native server runs enrichment maintenance every 30 seconds independently of
media and translation workers. It cancels stale source/target work, recovers
expired attempts with backoff and preserves checkpoint/receipt evidence. Claims
and source reservations also recover conflicting expired work; discovery remains
read-only. Stash never contacts websites for these operations. Legacy queue import
and host/n8n activation remain required before switching production schedules.

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
Producer schema 10 adds persistent rotation across local download/metadata
profiles and automatically discovered collections. Global pending-delivery
recovery runs before loading website profiles. See
[dispatch across local profiles](../integrations/gallery-dl/README.md#dispatch-across-local-profiles).

### Legacy enrichment preparation

The native migration preparation policy `automation-enrichment-v1` interprets
frozen enrichment jobs, cooldowns, seed/source progress and catalog completion
receipts. It preserves original attempt counts, priorities, times and status
meanings. Decimal retry deadlines round upward to milliseconds without a floating
point conversion. Matching platform and account cooldowns can extend a job's
deadline; neither can shorten an existing native deadline. Platform labels remain
historical labels: mirror OnlyFans/Fansly traffic belongs to Coomer and mirror
Patreon traffic to Kemono according to the actual source URL. Account denials do
not become service-wide outages.

Preparation returns migration candidates. Pending/retry jobs are candidates for
held work. `done` requires the original catalog receipt, `already_native` requires
a retained gallery-dl capture, and `coalesced` requires the imported post alias.
Source enumeration completion and the last attempted platform are historical
progress, not evidence of downloaded or enriched media. Explicit exclusions stay
excluded; missing URLs and identity conflicts remain reviewable. Original staged
metadata retains its exact digest and requires conversion/review before it can
become a native checkpoint, even if its queue row says `done`.

The preparation functions alone create no native targets or execution history.
The domain importer still needs to bind native posts/collections, preserve queued
URL evidence, resolve historical proofs and map scheduling state before reviewed
activation. Missing receipts in an older catalog snapshot cannot be inferred from
a later automation row's success label. Production cutover uses coordinated fresh
inputs; a rehearsal made from snapshots taken at different times cannot certify
completion of that later work.

## Account listing discovery

Here, a **page** is one batch of post metadata returned during an account search.
It can contain several posts, and each post can have several media records. A
**cursor** is the source's bookmark for requesting the next batch. An imported
cursor or page count records earlier progress; it does not recreate those
batches' metadata. Missing batch records limit what the archive can verify about
that search and do not establish that media downloads were missed.

The shared page parser, selected-job producer executor and scoped discovery
worker API are implemented. Schema 1000071 retains pinned listing definitions,
producer-owned attempts and immutable compact page receipts. Each leased
`account.list_page` job finishes after one retained page, releasing the shared
service reservation before the next page can run. Earlier pages and imported
cursors survive retries; final enumeration and candidate matching are separate
outcomes. See the [storage and scheduling contract](native-schema.md#durable-account-listing-pages).

Capabilities advertise `discovery_protocol: 1`, `discovery_readiness_protocol: 1`,
`discovery_dispatch_protocol: 1`, `discovery_collections_protocol: 1`,
`discovery_source_pacing_protocol: 1` and `max_discovery_page_bytes: 33554432`.
The following routes live under `/api/v3/ingest/discovery` and use the existing
producer bearer credentials and collection/root grants. Authentication runs
before reading page bodies; session cookies and application API keys cannot
authorize these operations. Existing enrichment endpoints remain post-only.

| Route | Request and result |
| --- | --- |
| `POST /collections/ready` | Optional UUID `after` and `limit` (1–100, default 50). Returns ordered `{uuid}` containers with listing definitions, under current collection/root grants. This does not assert that a listing is ready for a particular policy/runtime. |
| `POST /collections/{collection}/jobs/ready` | Requires `policy_sha256` and `extractor_version`, with integer `after` and `limit` (1–100, default 50). Returns due queued `{uuid, sequence}` candidates from the bounded active queue. |
| `POST /collections/{collection}/listings/ready` | Requires policy/runtime, optional UUID `after` and `limit`. Returns `{listings: [{uuid, definition_sha256}], after, has_more}` for definitions eligible to admit a page. The cursor tracks inspected definitions, including ineligible ones. |
| `POST /listings/{listing}/jobs` | Requires `expected_definition_sha256`, `policy_sha256` and `extractor_version`. Admits or replays the current page job for an existing definition. After a successful nonfinal page, it admits the next page; after final success it returns that completed job. |
| `GET /jobs/{job}` | Returns `{job, listing, cursor, receipt}`. The input cursor belongs to this particular page job, even after later jobs advance the listing. The optional receipt belongs only to this job. |
| `POST /jobs/{job}/claim` | Requires `expected_revision`, stable `owner_uuid`, policy/runtime and `lease_seconds` between 5 and 900. Returns the running job or HTTP 204 while unavailable. Reusing the original owner recovers a lost claim response. |
| `POST /jobs/{job}/renew` | Requires `owner_uuid`, `fence` and `lease_seconds`. Renews only the authenticated producer's current attempt. |
| `POST /jobs/{job}/source` | Requires `owner_uuid`, `fence` and the exact listing `url`. Returns `{job_uuid, fence, ready}` for the held service reservation. Unrelated profiles and child services are rejected. |
| `POST /jobs/{job}/page` | Requires `owner_uuid`, `fence`, the requested `ordinal` and an object-valued `body`. Commits the page receipt and job success together. |
| `POST /jobs/{job}/failure` | Requires `owner_uuid`, `fence` and a controlled `error_code`. Returns the original immutable attempt outcome, including on replay after a successor succeeds. |

The route supplies job identity and the credential supplies producer identity;
requests cannot substitute either in their bodies. Unknown or duplicate JSON
keys, compressed requests and oversized envelopes are rejected. The page body limit
is 32 MiB, with 4,096 extra bytes allowed for its request envelope. Send that body
as JSON, preserving source number tokens; do not encode it as an escaped string.
The receipt's SHA-256 covers its native canonical page body, not the envelope.
The wire and persisted forms retain compact shared metadata and original observation
times. Descriptions return references and a small receipt rather than repeating
the page body.

Controlled retry codes are `rate_limited`, `timeout`, `extraction_failed`,
`worker_failed` and `source_busy`; exhausting the attempt budget becomes a
failure. Terminal codes are `authentication`, `access_denied`, `challenge`,
`not_found`, `unsupported_extractor`, `result_too_large`, `invalid_checkpoint`,
`not_a_post_url`, `runtime_changed`, `unsupported_profile` and
`pagination_stalled`. Shared source cooldowns still apply. Failure replay cannot
extend the deadline or finish another attempt.

Changed definitions/cursors return HTTP 409 `discovery_work_changed`; stale or
foreign attempt ownership returns HTTP 409 `lease_lost`. Invalid discovery data
returns HTTP 400 `invalid_discovery_work`; malformed envelopes use the common
HTTP 400 response. Missing records return 404, scope violations 403, and capacity
exhaustion 429. Responses retain `Cache-Control: no-store`.

The supported Python `DiscoveryClient` validates definition digests, original
cursor bindings and receipt ownership before accepting responses. Its shared
`JobLease` uses the server clock and stable owner identity for renewal and lost
claim recovery. The producer retains claim intents, exact page bytes and
acknowledgements in a durable local discovery journal. Its capacity reservations
share the existing download/enrichment budget. Body removal requires a matching
receipt; a terminal job description cannot discard pending local evidence.
Source number tokens and Unicode survive native canonical page encoding.
See [producer discovery](../integrations/gallery-dl/README.md#account-listing-page-collector)
for the client contract and dispatch commands.

Collection discovery uses current active source/root definitions and indexed
collection/root grants. Each grant contributes at most `limit` containers before
deduplication and the final limit; no listing or page bodies are loaded. A root
grant includes later registered collections, and no longer includes a collection
moved to another root. Disabled sources and containers without listings are
excluded. Follow each container with policy/runtime-specific readiness; a
container can contain only stale or completed definitions.

Job and listing readiness require the collection's current grant and exact
policy/runtime; they never admit or claim work. Listing scans inspect at most `limit` definitions
plus one lookahead using the collection/UUID index. An empty `listings` array
with `has_more: true` must advance using the returned `after`. Candidates skip
future deadlines, stale sources, active/failed/cancelled jobs and completed
listings. A succeeded nonfinal page permits admission of the next page. These
reads use small receipt summaries without loading retained page bodies; admission
and claim revalidate all selected work. Multiple calls do not form a snapshot.

Application-owned maintenance runs with the HTTP server every 30 seconds,
independently of connected producers. It inspects only the bounded active queue,
cancels jobs after collection revision/state/root changes, disabled roots or
account consolidation, and expires abandoned claims with the existing attempt
budget and retry backoff. A future deadline is not a source change. Maintenance
preserves fetched pages, original producer attempts and receipt replay. It
neither creates new listing definitions nor contacts a source website.

A separate application worker compares retained batches with explicitly bound
historical targets. Native schema 1000072 saves each target's comparison cursor,
original page references and candidates grouped by qualified post ID. Comparison
runs under a read transaction, followed by a short revision-checked commit.
Restart continues from the saved cursor; later native source/post changes stop
new comparison while preserving earlier receipts. This processing does not
change the producer's page acknowledgement, publish a post identity or certify
historical listing coverage. See [candidate storage](native-schema.md#account-listing-candidate-evidence).

These endpoints do not create listing definitions, activate imported work,
explicitly retry a terminal job or accept a candidate match. The selected-job producer worker delivers
saved evidence before claiming or fetching; its delivery-only CLI needs no
website profile and cannot claim another attempt. A listing profile explicitly
binds `account.list_page`, separately from enrichment policies. Producer schema
12 adds durable per-collection/policy cursors and backoff for `dispatch-discovery`.
Each invocation replays saved deliveries before resuming owned work, visiting
due jobs and admitting an eligible existing definition. Omitting the profile
permits delivery only. A lost admission is recovered through the ready-job index;
filtered empty pages advance using their inspection cursor. An idle dispatch
does not establish enumeration completion. Schema 13 integrates `account.list_page`
profiles into `dispatch-all`, with durable collection/profile rotation and separate
saved-delivery cursors for discovery and enrichment. Both saved deliveries run
before website profiles load, preserving recovery when access bindings are
unavailable. Selection commits before execution, so continuously busy profiles
cannot reset the traversal after restart. Reviewed activation and publication of
complete, unique corroborated matches are implemented, including background publication
from listing evidence or authenticated detail results. Actual coverage and automatic
candidate admission remain necessary before
host/n8n callers switch to native discovery. In particular, a
retained nonfinal page is successful page delivery, not successful enumeration
or a completed catalog import.

### Reviewed discovery activation

These application-authenticated routes live under `/api/v3/archive`; scoped
producer credentials do not grant review or source administration.

| Route | Behavior |
| --- | --- |
| `POST /discovery-activations/preview` | Preview one legacy-backed listing and 1–1,000 exact original target references. |
| `POST /discovery-activations` | Apply `{input, expected_plan_sha256}` atomically, or replay the same saved operation. |
| `GET /discovery-activations/{activation}` | Return the immutable activation receipt. |
| `GET /discovery-listings/{listing}` | Inspect the original definition, including saved cursor and historical page count. |
| `GET /discovery-listings/{listing}/pages` | Page through compact received-page receipts without loading source bodies. |
| `GET /discovery-match-targets/{target}` | Inspect the original target binding and durable comparison progress. |
| `GET /discovery-match-targets/{target}/review` | Inspect current coverage, candidate counts and blockers without fetching or changing metadata. |
| `POST /discovery-match-targets/{target}/detail-preview` | Compare a supplied metadata-fetch transcript against one original weak candidate, without retaining it or changing the review. |
| `POST /discovery-match-targets/{target}/publication` | Publish a complete unique match with `{expected_target_revision}` and, for detail corroboration, the review's `detail_job_uuid`; replay requires the same proof. |
| `GET /discovery-match-targets/{target}/publication` | Retrieve the accepted identity and native publication receipt. |
| `GET /discovery-match-targets/{target}/publication/records` | Inspect original page or detail-transcript record ordinals and their native capture UUIDs. |
| `GET /discovery-match-targets/{target}/candidates` | Inspect distinct candidate post IDs and their strongest retained evidence. |
| `GET /discovery-match-candidates/{candidate}/evidence` | Inspect each original page's basis and record ordinals. |

The preview input contains an operation `uuid`, `manifest_sha256`, a complete
`listing` definition and `targets: [{source_ordinal, source_sha256}]`. The listing
requires its original `legacy` snapshot/account reference, saved cursor/history
and preserved retry deadline, with the explicitly selected current collection,
root, worker policy and extractor. Preview derives current post UUIDs/revisions
and includes them in its plan hash. Save the complete returned plan before Apply;
after a lost response, inspect or retry that same operation and expected hash.
An exact replay remains valid after later native edits, while changed input or
new activation against stale source state returns `409 discovery_work_changed`.

`GET /collections/{collection}` retrieves the selected collection's current
definition directly for review, including its revision, state and root binding.
It uses application authentication and returns 404 for an unknown UUID.

List routes accept nonnegative `after` and `limit` from 1 to 100. Candidate
pagination uses `sequence`; page/evidence pagination uses the original page
ordinal. All responses disable caching. A receipt proves only that reviewed
bindings were retained. Producer admission, fetched-page receipts, compared
listing completion and accepted post identity remain distinct states. Historical
page counts do not become native receipt counts, and activation never certifies
historical coverage or completed catalog import. The
[`stash-activate-automation-discovery` operator command](../integrations/gallery-dl/README.md#reviewed-discovery-activation)
saves a private, digest-bound review file and recovers the original operation
after a lost Apply response. Reviewed recovery of missing-history searches is
described below. The review UI, detail execution and staging release remain open.

The detail-preview input is `{expected_target_revision, candidate_sequence,
extractor_version, body}`. `body` is the compact metadata-fetch JSON object,
limited to 32 MiB; it is not an escaped string. The server loads the selected
candidate's original page and frozen catalog evidence through indexed reads in
one read transaction. The response has `preview_only: true`, compact `evidence`
and the unchanged current `blockers`. Evidence includes page/transcript hashes,
the selected post ID, record ordinals and, when corroborated, the witness ordinal
and basis. It does not repeat the source payload or include worker settings.

`evidence.status` is `corroborated`, `uncorroborated` or `pending`. The requested
URL/runtime and every returned post must match the selection. Original caption,
date or source-URL evidence must independently corroborate it; confirming the
inferred URL, account or filename alone is insufficient. A contradictory
publisher or another post in the response is rejected. Pending child requests
remain pending; unresolved references remain explicit. A failed or changed
detail response cannot remove a competing candidate.

Preview bytes have no authenticated producer receipt. Even a corroborated
preview leaves `detail_required` and other blockers intact. This endpoint does
not fetch a website, create a job, accept an identity or publish captures. Only a
saved, authenticated detail result can supply publication proof.

The target review uses one database snapshot. `coverage.retained_pages` counts
received batches of source posts, while `target.last_page` counts batches already
compared against that target. `coverage.retained_complete` means the saved search
reached its final cursor. `coverage.complete` additionally requires comparison
through that cursor, no initial saved cursor and no missing historical batches.
A resumed search can finish and still report `history_not_retained`; its original
page count supplies no metadata with which to check competing matches. Complete
coverage describes that retained search, not all posts a source ever hosted.

`candidate_count` groups distinct source post IDs, and `detail_candidate_count`
counts candidates whose original listing evidence is weak; corroboration does
not rewrite that evidence. A single candidate is included with its existing
native post, if any, and its latest completed `detail` result when available.
Only a corroborated result clears `detail_required`; all other blockers remain.
A newer negative result cannot be bypassed by selecting an older positive one.
Multiple candidates
remain available through the paginated candidates route. The `blockers` array
can contain:

| Blocker | Meaning |
| --- | --- |
| `history_not_retained` | The search resumes after batches whose source bodies are unavailable. |
| `listing_incomplete` | The retained search has not reached its final cursor. |
| `comparison_pending` | Received batches remain to be compared with the target. |
| `source_changed` | The original account, collection revision or root is no longer eligible. |
| `post_changed` | The reviewed post revision or active state changed. |
| `post_already_identified` | The target already has an identifier outside the imported catalog namespace. |
| `no_candidate` | The complete retained search produced no candidate. |
| `competing_candidates` | More than one distinct post remains a candidate. |
| `detail_required` | Weak listing evidence lacks usable authenticated corroboration for a sole candidate. |
| `identifier_in_use` | The sole candidate's identifier belongs to another native post, including a forgotten post. |
| `search_replaced` | A reviewed recovery search replaced this listing; its original evidence is retained. |
| `earlier_comparison_pending` | The predecessor search still has retained batches to compare with this target. |
| `earlier_candidates_differ` | The predecessor contains a candidate other than the new search's sole candidate. |

Before publication, an empty blocker list identifies a unique listing candidate
for further publication validation. This response accepts no identity, publishes
no capture and supplies no durable approval token. Later changes require another
review. After publication, the review includes its immutable `publication`
receipt and clears blockers; later native edits do not undo committed work.
Review reads compact receipts and references without loading source page bodies.

Publication is application-authorized and makes no source request. It requires
the exact completed target revision, a search retained from its beginning, one
corroborated candidate, unchanged native source/post choices and an unclaimed source
post identifier. It reparses the original evidence and selects the observation
that actually supplied the matching basis. An earlier title-only observation
cannot acquire the corroborating observation's time.

Identity and canonical URL evidence, all selected-post observations from the
candidate's strongest page (or the selected detail transcript), publisher/album/translation effects and the receipt
commit together. The original producer, observation times, shared payload/profile
data and parent context remain intact. Other posts on that page remain staged.
The operation preserves the existing legacy post UUID and keys; a source ID owned
by another post returns a conflict for explicit consolidation review. It neither
downloads media nor chooses depicted performers.

For a weak candidate, send `detail_job_uuid` from the review's `detail.job_uuid`.
Preparation reconstructs its comparison from the frozen source, original listing
page and retained checkpoint. A detail completed before later listing comparisons
can remain valid for the same candidate and native post revision. Its original
`needs_detail` flag remains true. The publication uses policy
`retained-discovery-detail-publication-v1` and retains `detail_job_uuid`; its record
ordinals and corroborating timestamp refer to that transcript, not the weak listing
record. Pending children cannot supply completion; unresolved child references
stay in the retained evidence. Neither detail nor listing bodies are released.

Recover a lost response with GET or repeat the same POST. Replay returns the
original receipt even after a later source edit or post tombstone; a different
target revision or detail job cannot reuse it. GET returns 404 before publication. Record
pagination uses zero-based `record_ordinal`, with `after=-1` by default and
`limit` from 1 to 100. These associations prove native capture publication only;
they do not release listing staging or declare the whole catalog import complete.

The server also publishes eligible matches automatically after comparison. Its
worker inspects at most 32 target rows at a time, skips published and blocked
targets, and calls the same publication service with the target's current
revision and selected detail result where needed. It rereads and validates the evidence before committing, so readiness
cannot authorize a changed match. A competing worker shares the original receipt.
Targets requiring review retain their evidence without holding up ready targets.
Comparison and publication pause after a traversal finds no work, and resume
from durable progress after restart. Neither worker admits a source job or
contacts a website.

### Recovering a search with missing historical batches

Use the same discovery activation routes and saved-plan command with a new
listing UUID and `recovery_of: {listing_uuid, sha256}` pointing to the original
listing definition. Retain its `legacy` reference, account, collection and
profile URL; select current collection/root and worker policy values explicitly.
Set `initial_cursor` to null and `historical_pages` to zero for the new search.
Its deadline must preserve both the original listing deadline and the latest
attempt's available time. Each selected target must already be bound to the
original search. Further target batches can reuse this recovery's exact listing
definition; a second replacement of the original is rejected.

Preview and Apply reject an active producer attempt. Once it finishes or queue
maintenance recovers its expired lease, Apply can cancel queued predecessor work
and commit the replacement, original-target links and activation receipt together.
The original search stops admitting work. Its cursor, historical count, attempts,
pages, comparisons and candidate references remain intact. Retry a failed page
of the recovery through the ordinary job retry path.

The target review returns `recovery_from` with the original listing/target UUIDs,
uncompared batch count and earlier candidate counts. The original target review
exposes `replacement_listing_uuid`. Retained earlier batches must finish
comparison, and differing candidates require review even if the fresh search
finds only one match. The same checks protect automatic/manual publication and
startup verification. A recovery with no earlier conflict can publish only after
the fresh search itself finishes and all its batches have been compared.

This obtains new complete search evidence without inventing lost historical
bodies. It cannot prove that a service still exposes posts deleted or hidden
since the earlier scrape. Ordinary database backups retain both searches.

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

The download executor also requires `source_run_fairness_protocol: 1`. Running
attempts expose `turn_until`, five minutes after their recorded start, independent
of lease renewals. On reaching it, the worker finishes its current file/checkpoint
and yields before further source work. A resumed traversal may finish replay and
one new checkpoint first, preventing a long replay from repeatedly consuming the
whole turn without progress. It reports `state: "retry"` and
`error_code: "source_turn_complete"`, with no `error_scope` or retry override.
This preserves pending windows and progress, applies the normal target cooldown,
and leaves the failure count unchanged. The CLI reports an acknowledged yield
as `yielded`, an incomplete result; it does not assert source completion.

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

Historical plugin settings and folder rules enter through the separate
[metadata policy migration API](native-metadata-policy-migration.md). It retains
every source value and its conversion disposition, guards reviewed native
references and publishes a policy with an immutable receipt. Unresolved
conversions require a disabled policy. Producer intake does not migrate settings
or obtain access to these application-authenticated routes.

Policies belong to a source collection, including an unsourced directory or
manual batch. Definitions and their history live in the native database; there
is no plugin settings JSON or parallel catalog writer. Schema promotion creates no
policies and changes no selected metadata. Each field decision made by a policy
references the exact policy revision, with capture provenance for source-backed
mappings. Collection scope changes require reviewing a new policy revision.

The application-authenticated API lives under `/api/v3/archive`. Producer tokens
do not authorize these routes. Browser writes use the same origin checks as
other native administration. JSON uses snake_case keys.

| Method and path | Contract |
| --- | --- |
| `GET /media-roots?after=<uuid>` | Up to 50 logical roots and their reviewed local bindings |
| `POST /media-roots/probe` | Read-only check of `server_path`; returns canonical `path` and `directory_identity` without creating a root |
| `POST /media-roots`, `PUT /media-roots/<uuid>` | Complete definition: `expected_revision` (zero on create), `label`, `state`, checked `binding: {path, directory_identity}` (null unbinds), `reason`; new bindings/reactivation reverify the directory. Optional body `uuid` must match the PUT path |
| `GET /media-roots/<uuid>/history?after=<revision>` | Immutable definitions and bindings, default 25, bounded `limit` from 1 to 100 |
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
returns one value per field; `empty` selects a configured `fallback` or omits the
field, while null is an actual value and
must satisfy that field's type. Expressions share a 250 ms deadline and retain
1 MiB output, 16 KiB expression and one-result limits. Native input allows 12 MiB
for retained source payload plus entity data; plugin limits remain unchanged.

A jq mapping can add a destination-typed `fallback`. For example, a reviewed
folder performer can be used when no performer names are present in source data:

```json
{
  "performers": {
    "jq": ".source.payload.actors | select(type == \"array\" and length > 0)",
    "reference_names": true,
    "fallback": ["56c9895d-03c1-4a93-8c0d-fbd99d27de22"]
  }
}
```

Only an empty jq result selects the default. Explicit null, false, zero, blank
strings and empty arrays remain actual results. Expression errors and missing or
ambiguous name matches require review; they never silently select the default.
The expression above deliberately omits absent or empty actor lists. Use a
different expression if an empty list should clear performers instead.

Defaults always use the target field's ordinary typed shape, including explicit
native UUIDs for relationships, regardless of the expression's name-matching
option. Draft previews, saves and migration previews validate them even when the
expression currently supplies a value. Migration previews bind their target
revisions; application previews bind the references actually selected. Redirects
resolve at application, while deleted
targets require review. Defaults have policy provenance, with `used_fallback: true`
in the preview, and do not claim a source capture. Existing field protection and
source precedence still apply. The editor provides the same typed controls and
library pickers used for fixed values. Removing the default restores omission.

Mapping data contains `source` (the selected post/capture, normalized `metadata`,
reconstructed retained `payload`, shared post URLs and compact translation choices,
or null), `entity` (UUID,
kind and permitted native field values), and `context` (creation flag, filename
and relative path).
It includes no plugin configuration, mapping definitions, settings, duplicate edit
`input`, or field-name list. Expanded data is returned only with `include_data`;
normal previews contain proposed changes and their status. Protected values and
disabled rules can be inspected without applying them. HTTP previews select an
existing entity; requests cannot impersonate a creation event.

Editing a collection keeps its earlier captures available in the policy sample
picker. A newly reviewed rule can use that collection's recorded capture history
when a reviewed post association or current attachment links to the selected
scene/image and the selected file remains inside the current root and folder scope. The collection and policy
revisions must still be current; changing a definition invalidates old previews.
Repeated membership in several revisions produces one selectable capture, while
distinct source observations remain separate. Reading or applying a rule never
rewrites the original capture's collection revision. Producer intake continues
to require membership under the exact revision recorded by its event.

A source choice supplies `capture_uuid` and exactly one of `attachment_uuid` or
`post_media_decision_uuid`. A direct post association allows retained NFO captures
without inventing an attachment slot. The sample picker returns one direct choice
per capture when a reviewed post link exists; it does not also repeat that capture
for each attachment. Pagination uses `after_capture` with either
`after_attachment` or `after_post_media_decision`. Source-derived field decisions
retain the selected direct association UUID alongside capture and policy
provenance. A changed or rejected association invalidates its old choice.

`source.urls` contains the selected post's distinct known URLs in lexical order.
They come from shared post evidence, which may include observations recorded
after the selected capture; they need not appear in its original raw payload.
Only that post is read, through indexed pages. Repeated observations of one URL
do not repeat it here. A later URL change is rechecked when applying a preview.

The set is limited to 4,096 URLs and 1 MiB of encoded text. `urls_complete` is
true for a complete set, including an empty array. If either limit is exceeded,
`urls` is null and `urls_complete` is false, so a partial list cannot silently
replace an entity's existing URLs. This mapping adds known post URLs while
preserving links already selected from other posts:

```jq
.entity.urls as $existing
| .source
| select(.urls_complete)
| ($existing + .urls | unique)
| select(length > 0)
```


`source.translations` contains distinct retained results for the selected post
whose exact original text matches this capture's `title` or `original_text`.
Each entry has its result `uuid`, applicable `source_fields`, `translated_text`,
nullable `source_language`, `target_language` and `provider`, plus an
`evidence_uuid` and `captured_at`. The latter identify the latest known observation
of that result. Times are normalized to UTC with fixed nanosecond precision for
chronological jq sorting; an unknown observation time stays null. Original
timestamps and every assertion remain available through the
[translation evidence API](native-schema.md#retained-source-translations).

A result that serves both title and caption appears once with both source field
names. Repeated assertions reuse that body. Original text and raw source payload
are not copied into each choice. A different post, a changed original caption
or an unknown original cannot supply a match. Language is never inferred:
unknown-language evidence does not match an English selection.

Indexed 100-row pages inspect only this post and at most two exact originals.
The complete set is limited to 128 distinct results, 4,096 evidence assertions
and 1 MiB of encoded choices. Exceeding any limit returns `translations: null`
and `translations_complete: false`, never a partial candidate list. An empty
complete list means no matching results are retained. Constructing these choices
fetches and encodes shared text once per result, even when many observations
reference it.

This title mapping prefers the most recently observed English result and falls
back to the original title when no nonempty translation is selected. An
incomplete candidate set becomes a review error. A manual file without a source
omits the mapping, allowing its ordinary filename fallback:

```jq
.source as $source
| if $source == null then empty
  elif $source.translations_complete != true then
    error("Translation choices are incomplete")
  else
    ([ $source.translations[]
       | select(.target_language == "en" and (.source_fields | index("title"))) ]
     | sort_by(.captured_at, .evidence_uuid)
     | last
     | .translated_text
     | select(type == "string" and length > 0))
    // $source.metadata.title // empty
  end
```

Use `original_text` and `source.metadata.original_text` for a details mapping.
Reading these choices creates no translation jobs or metadata decisions. A new
translation is considered when a policy next evaluates; a changed selected
value invalidates an earlier Apply digest. Explicit and preserved entity values
keep their precedence. Completing translation work alone does not apply this
mapping to existing scenes/images.

The shared [`readable_text` filter](plugin-settings.md#jq-api) preserves readable
HTML captions when selecting a display field. For an untranslated source caption:

```jq
.source.metadata.original_text
| readable_text
| select(type == "string" and length > 0)
```

To display a selected translation, apply `readable_text` after choosing its result
or the original text. Paragraph and line breaks are preserved, while plain input
keeps its whitespace and literal entities. This conversion changes the proposed
field value; the capture's original text and stored translation result keep their
original bytes. Existing explicit clears and protected fields retain precedence.

Use this date mapping to turn a known source timestamp into Stash's calendar
date without losing timezone information before selecting its day:

```jq
.source.metadata.published_at | utc_date | select(. != null)
```

The [strict `utc_date` filter](plugin-settings.md#jq-api) accepts date-only values
and RFC3339 timestamps, including fractional seconds and offsets. Missing values
omit the mapping. Invalid calendar dates and timezone-less timestamps require
review; they are not silently corrected or assigned the server's timezone.
The capture's original `published_at` and `date_basis` remain unchanged. A
historical producer's known UTC convention needs an explicit conversion rule
if its retained timestamps lack offsets.

Only typed curated fields from `MetadataFields` are accepted. Identity, file
fingerprints, jobs and raw source evidence are not mapping targets. Relationships
use native UUIDs; redirects resolve to the surviving identity and deleted targets
require review. A relationship mapping can opt into name resolution with
`reference_names: true`. Its constant value or jq output uses the target's shape:

| Target | Name value | Matching |
| --- | --- | --- |
| `studio` | `"Studio name"` or `null` to clear | Canonical name and individual aliases |
| `performers` | `["Performer name", "Known alias"]` | Canonical names and individual aliases |
| `tags` | `["Tag name"]` | Canonical names and individual aliases |
| `groups` | `[{"name":"Album","scene_index":2}]` | Canonical names; `scene_index` is optional |

The editor offers the name option only for relationship fields, a plain text
studio input and one-name-per-line performer/tag inputs. Group constants use one
structured JSON value. Empty arrays clear list relationships. Values are limited
to 128 names, each nonblank, without surrounding whitespace or control characters,
and at most 1,024 UTF-8 bytes. Group free-form alias text is not split into names.

Schema 1000079 adds case-insensitive name/alias indexes for studios, tags and
groups. These and the existing performer-name index use SQLite `NOCASE` (ASCII
case folding); non-ASCII spelling remains exact. Shared lookups report up to 100
candidates with an explicit overflow flag, deduplicating repeated spellings of
one entity before applying the limit. Retained catalog-edit review uses the same
resolver. Neither path chooses between a canonical name and a colliding alias,
creates missing entities, or uses approximate matches.

Unambiguous names can be added while collisions remain for review. Unresolved
names cannot erase existing inherited relationships; known groups retain their
selected scene index while unrelated existing groups remain. Any unresolved name
prevents automatic organization. An explicit native UUID resolves ambiguity.
Previously saved native `performer_names` definitions remain readable without
rewriting immutable history; that flag is restricted to performers and cannot
be combined with `reference_names`. Publisher/account ownership alone never
supplies depicted performers.

`on_create` and `on_existing` control automatic application independently.
`skip_organized_on_create` reproduces the old creation-only condition, including
creation requests that already supplied organized=true. `mark_organized` sets an
unprotected organized field only when a non-filename mapping selected a value
and no mapping/name conflict remains. It never clears an existing organized
choice and never treats organization as identity or download completeness.

An optional `organized_requires` list adds completeness requirements from that
media kind's metadata field schema, excluding `organized` itself. Duplicate or
unknown names are rejected. Requirements inspect the effective values after
applying permitted changes: protected current selections count; proposed clears
do not, and rejected candidates cannot fill a missing field. Blank strings,
empty lists/objects and null are missing; zero is a valid selected rating.
Unsatisfied requirements produce an omitted organized change with a diagnostic.
An omitted or empty list retains the ordinary behavior, and repeated empty-list
saves are no-ops. These requirements remain separate from the rule's successful-
mapping and unresolved-name checks.

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

### Reviewed post-to-media associations

The application can browse posts independently of scene/image selection:

| Read-only route | Behavior |
| --- | --- |
| `GET /posts?after=<post-uuid>&limit=N` | Compact post summaries in UUID order; limit 1–100 |
| `GET /posts?uuid=<post-uuid>` | Exact archive identity lookup |
| `GET /posts?namespace=<namespace>&value=<source-id>` | Exact qualified source identity lookup |
| `GET /posts?url=<encoded-url>` | Exact retained URL lookup; returns every matching post without treating a shared URL as proof of identity |
| `GET /posts/<post>` | One compact summary, including active/forgotten state and current revision |
| `GET /posts/<post>/identifiers?limit=N` | Qualified identifiers; continue with both `after_namespace` and `after_value` |
| `GET /posts/<post>/publishers?after=<account-uuid>&limit=N` | Canonical accounts selected by current capture-publisher decisions; retained claims alone do not select a publisher or depicted performer |
| `GET /posts/<post>/media?after=<media-uuid>&limit=N` | Canonical scene/image identities with their explicit choices, retained-evidence flag and independent attachment-link count |
| `GET /posts/<post>/album` | Current gallery choice, including disabled state and deleted/redirected gallery resolution; `null` when no choice exists |

These routes use the `/api/v3/archive` application-authenticated boundary.
The three exact lookup selectors are mutually exclusive. Source IDs require
their namespace, and URL lookup does not normalize or guess alternate URLs.
Summary pages include at most three identifiers and URLs with continuation flags,
plus the latest stored capture excerpt; payloads and profile bodies are not loaded.
Use the existing URL and shared capture-summary routes below for expansion.

Media pages resolve merged identities before applying the cursor, so repeated
evidence and aliases produce one row. Explicit unlinks and conflicting merged
choices remain visible and suppress attachment-based association; a positive
attachment count does not override them. Deleted entities retain their UUID
and state with no current local ID. Post captions and captures are fetched
separately, rather than repeated in each media row. Inspection does not select
sources, create galleries, change membership or assign performers.
The selected post is bounded to 8,192 retained media identities before redirect
resolution; larger sets return `422 source_review_limit` rather than silently
truncating evidence. These APIs provide the standalone browser foundation;
its application page and navigation are separate implementation work.

Application-authenticated routes under `/api/v3/archive` expose direct links:

| Route | Behavior |
| --- | --- |
| `GET /posts/<post>/media/<entity>` | Current explicit decisions, canonical media UUID, post/media revisions and merge conflicts |
| `PUT /posts/<post>/media/<entity>` | Record a guarded review decision with a durable request UUID |
| `GET /posts/<post>/media/<entity>/history?after=<post-revision>&limit=N` | Original decisions across merged media identities, in revision order; limit 1–100 |
| `GET /post-media-decisions/<request-uuid>` | Original committed decision for response-loss recovery |
| `GET /entities/<entity>/source-posts?after=<post-uuid>&limit=N` | Targeted, unique post summaries from retained evidence, explicit choices and current attachment links across merged media identities; limit 1–100 |
| `GET /posts/<post>/media/<entity>/review` | Refresh one post card with current guards, latest capture excerpt and independent evidence/link status |
| `GET /posts/<post>/urls?after=<url-uuid>&limit=N` | Shared source URLs in UUID order; limit 1–100 |
| `GET /posts/<post>/capture-summaries?limit=N` | Capture references and shared revision metadata, without raw payloads or profiles; limit 1–100 |

PUT takes `uuid`, `post_uuid`, `media_uuid`, `expected_post_revision`,
`expected_media_revision`, `expected_decisions` (all current decision UUIDs),
`state`, `origin:"review"` and `reason`. Obtain the current guards from GET and
save the exact request before sending it. A retry returns its original decision;
reusing its UUID for a different choice conflicts. The decision list guard detects
choices brought together by a merge even if the surviving media fields did not
change. Review resolves the current merged group and records which earlier
decisions it replaces, preserving original history.

The request body limit is 64 KiB, accommodating the bounded merged decision
set and reason. A current-state rejection returns `409 post_media_conflict`.
Reusing a request UUID with different contents returns
`409 post_media_request_conflict`; clients must retain that unresolved request
rather than treating it as a safe rejection of the original intent.

`linked` explicitly associates the whole post with the media. `unlinked` rejects
that pair, suppressing its attachment-based source metadata and automatic album
membership too. `undecided` returns the pair to attachment evidence. Conflicting
states after a media merge require review. Rejecting one attachment remains a
slot-specific decision and does not erase a separately reviewed direct post link.

A direct link creates no album or attachment order. Decisions update existing
source-gallery membership through the usual service, preserving manual members,
cover and ordering. A conflicting merged choice holds synchronization for review.
Previously selected field values and their provenance remain when a source link
is rejected; this is not a request to clear the user's metadata. Source publishers
never become depicted performers through these operations. Observational evidence
alone does not select a link. The bounded matching service below can propose and
apply historical file proofs.

Scene and image **Sources** sections expose these operations on desktop and
mobile. They distinguish retained candidate evidence, explicit post choices and
attachment links. Changing a link saves its exact request in deployment-scoped
IndexedDB before sending, and refreshes the selected card after a verified
receipt. A reload does not automatically send a mutation; explicit recovery
checks the original receipt first. Definitively rejected guards require a new
review, while uncertain transport failures and mismatched receipts remain saved.

Post lists return a latest stored capture with a title excerpt of at most 512
characters, at most three shared URLs and an explicit `more_urls` flag. They
never fetch raw post/profile bodies. Expandable post text groups captures under
their shared revision; link history loads separately. Capture pages sort by
observation time, or by recorded time when the historical observation time is
unknown, then capture UUID. Continue with `after_uuid`, `after_time` (RFC 3339
with optional nanoseconds) and `after_clock` (`observed` or `recorded`). All
three cursor fields are required together. Unknown observation time stays null
and is labelled separately from the time retained evidence entered the archive.
Capture summaries carry each revision's metadata once per page; the UI also
deduplicates revisions across loaded pages. This does not select a caption,
copy source account ownership into performers, or activate metadata policies.

### Historical post-to-media matching

The `catalog-files-v1` policy follows retained catalog appearance, file observation
and file-match references. It reuses the native checks for current file/archive
generations, exact or surviving paths, verified content where recorded, and unique
current media ownership. A historical match does not perform new byte hashing.
The service uses no filename inference, publisher attribution or attachment-order
guessing. Several independently proven media can share a post; repeated evidence
for the same resolved media produces one choice with retained proof references.
Competing media for one observation, unavailable files and unproven claims stay in
review. Every explicit post choice is preserved, including `undecided`. Unscoped
attachment rejections also hold matching for review rather than being overridden.

| Application route | Behavior |
| --- | --- |
| `GET /post-media-backfill-posts?after=<uuid>&limit=N` | Indexed discovery of posts with retained post-level media evidence; limit 1–100 |
| `GET /posts/<post>/media-backfill-preview` | Read-only candidates, current guards, proof status and preview signature |
| `POST /posts/<post>/media-backfills` | Apply eligible candidates from that exact preview with `uuid`, `post_uuid` and `signature` |
| `GET /post-media-backfills/<request-uuid>` | Original durable outcome for exact retry and response-loss recovery |
| `GET /post-media-decisions/<decision-uuid>/evidence` | Foreign-key-linked appearance and file-match proof references |

These routes use application authentication, never producer-token authority.
Apply revalidates the preview and current file/ownership proofs in a bounded
transaction. Changed previews return 409; oversized per-post evidence returns
422 without truncation. The service creates only reviewed post/media decisions
and matching receipts, using the existing gallery service once per post. It does
not create attachments or galleries, replace selected metadata, or enable rules.
Request UUID reuse with different inputs conflicts. A committed request remains
recoverable after restart or a later explicit unlink; replay does not relink it.

The supported `stash-backfill-post-media` client first writes a private immutable
plan. Bounded parts retain candidate/proof previews and request UUIDs without one
filesystem object per post. It validates every part before any submission, checks
part hashes again before use, and binds the plan to its original endpoint. Prepare
supports up to one million discovered posts or an explicit `--posts-file` JSON
array. Oversized posts remain visible as review items while other posts proceed.
Use a new plan for stale previews, keeping prior requests and receipts intact.

```sh
stash-backfill-post-media prepare --endpoint STASH_ORIGIN --output /migration/post-media
stash-backfill-post-media show --plan /migration/post-media --expected-sha256 PLAN_SHA256 --post POST_UUID
stash-backfill-post-media apply --endpoint STASH_ORIGIN --plan /migration/post-media --expected-sha256 PLAN_SHA256
stash-backfill-post-media status --endpoint STASH_ORIGIN --plan /migration/post-media --expected-sha256 PLAN_SHA256
```

Use the printed plan digest after inspecting its proposals. `STASH_API_KEY` is
read at request time and never saved in the plan. Apply/status report original
receipt outcomes separately from pending requests and review items. Their exit
codes are 0 for processed work without review, 1 for an input/transport failure,
2 when review is required, and 3 while requests remain unsubmitted. A saved receipt
describes that matching operation, not a claim that later user edits were undone.
Rehearse matching and independently reconcile selected metadata before production
use; populated source coverage and the broader review UI are release gates.

### Historical metadata review API

These application-authenticated routes live under `/api/v3/archive`; producer
tokens do not grant access. The backend supports explicit review of imported
catalog edits. Scene and image pages expose these choices in their **Metadata
review** section, available through desktop tabs and the mobile section menu.

| Route | Result |
| --- | --- |
| `GET /entity-identities/<kind>/<local-id>` | Current native UUID/revision for an existing library link or picker selection |
| `GET /entities/<uuid>/metadata-fields` | Typed curated fields, selected values, protection, provenance and relationship revisions |
| `GET /entities/<uuid>/metadata-fields/<field>/history?after=<sequence>&limit=N` | Immutable decisions in ascending sequence order |
| `GET /entities/<uuid>/file-edits?after_history=<uuid>&after_match=<uuid>&limit=N` | Historical alternatives linked to that scene/image's files; both cursor parts are required together |
| `POST /metadata-file-edits/preview` | Current and proposed values, name candidates, file generations, status and digest; no writes |
| `POST /metadata-file-edits/apply` | Revision-checked application and durable receipt |
| `GET /metadata-file-edits/requests/<request-uuid>` | The original committed receipt, or 404 |

List limits are 1–100, default 50. Existing integer library IDs remain valid
navigation addresses; edits use the resolved native UUID. Historical alternatives
identify their complete immutable source entry at `GET /file-history/<uuid>`.

A preview request selects one source field, for example:

```json
{
  "entity_uuid": "<scene-or-image-uuid>",
  "history_uuid": "<history-uuid>",
  "source_field": "actors",
  "match_uuid": "<file-match-uuid>",
  "selections": {
    "An ambiguous source name": {"uuid": "<performer-uuid>", "revision": 3}
  }
}
```

`selections` is optional and only accepts names actually present in that edit.
Unique canonical/alias matches can be proposed automatically; missing or
ambiguous names return `unresolved_names`, never a partial replacement that drops
the unresolved performers. Up to 100 candidates per name are returned with
`more:true` when truncated. Requests allow up to 128 names; larger or unsupported
retained values return `unsupported`. No new performer, account or relationship
target is created by preview or apply. Studio, tag and group choices also use
native UUIDs and reviewed target revisions.

Name candidates also include their local library IDs. The UI shows names,
disambiguation and those IDs, and can search existing performers, studios, tags
and groups when a retained name has no suitable candidate. Selecting depicted
metadata does not associate a source account with a performer.

Only `ready` previews can be applied. Save the preview request plus its `digest`
and a new `request_uuid` before posting to `/metadata-file-edits/apply`. Retry the
exact saved body after a lost response, or inspect its receipt; a committed
retry returns `replayed:true` without editing again. A changed preview or reused
request UUID with different contents returns 409. Requests are limited to
256 KiB and cannot specify arbitrary target columns or source payload changes.

Legacy null removes field protection while keeping the displayed value until a
permitted native policy updates it. The mode change is explicit in preview.
Historical edits remain separate choices regardless of timestamps, duplicate
content claims or file survivors. File/ZIP generations, ownership, selected
entity revision and relationship candidates are checked again before commit.

The browser journals each Apply body in IndexedDB before transmission. One
pending choice per scene/image is shared across tabs, and separate public
deployment prefixes have separate journals. Reopening the panel only reads
state. **Check and retry saved change** inspects the original receipt before
retrying the identical body. Uncertain responses and storage errors preserve the
pending choice; a definitive stale-preview refusal permits **Review again**.
Successful receipts remove the pending browser entry and refresh active library
queries. A subsequent display-refresh failure does not turn a saved change into
a failed Apply. Browser storage is temporary request recovery; committed choices
and their provenance live in the native database and its backups.

Application requests use same-origin session authentication and the public
mount prefix. The Vite development proxy forwards `/api/v3/` while retaining the
browser Host/Origin pair. These clients do not receive producer tokens, website
credentials or plugin settings.

### Account ownership review API

The application-authenticated `/api/v3/archive` routes also support account
owners independently of depicted scene/image performers. A source account can
remain undecided or explicitly unlinked, including an aggregator. Accounts on
different services remain separate while sharing a chosen performer UUID.

| Route | Result |
| --- | --- |
| `GET /source-accounts?q=...&namespace=...&ownership=...&after=<uuid>&limit=N` | Canonical account cards with current ownership and at most eight identifiers |
| `GET /source-accounts/lookup?namespace=...&kind=...&value=...&after=<uuid>&limit=N` | Exact qualified identifier candidates; matching a handle does not prove uniqueness |
| `GET /source-accounts/<uuid>` | Current account revision, redirect if consolidated, identifiers and resolved owner |
| `GET /source-accounts/<uuid>/identifiers?after=<uuid>&limit=N` | All identifiers, including those retained from consolidated accounts |
| `GET /source-account-identifiers/<uuid>/evidence?after=<key>&limit=N` | Retained evidence and observation interval for the selected claim |
| `GET /source-accounts/<uuid>/ownership-history?after=<revision>&limit=N` | Previous explicit decisions, in ascending revision order |
| `POST /account-ownership/preview` | Reviewed current/proposed owner and digest; no writes |
| `POST /account-ownership/apply` | Atomic ownership decision and retry receipt |
| `GET /account-ownership/requests/<request-uuid>` | Original committed receipt, or 404 |

These lists default to 25 rows and accept limits from 1 to 100. The last row's
UUID, evidence key or ownership revision is the next cursor. Account card
`more_identifiers` explicitly marks a truncated summary. `q` is a literal
substring of labels or identifier values; namespace scopes remain distinct.
Performer names and local IDs are display/navigation values. Deleted owners
retain their UUID and state without a reusable local ID.

A linking preview requires an explicit native performer identity:

```json
{
  "account_uuid": "<account-uuid>",
  "account_revision": 4,
  "state": "linked",
  "performer_uuid": "<performer-uuid>",
  "performer_revision": 9,
  "reason": "Confirmed from the account profile"
}
```

Use `GET /entity-identities/performer/<local-id>` to resolve a picker selection.
Neither a matching name nor alias selects an owner automatically. `unlinked`
and `undecided` requests omit both performer fields. They respectively record
an explicit no-owner choice or return the account to review.

Save the request plus the preview's `digest` and a new `request_uuid` before
Apply. Retry that identical body after response loss, or read its receipt.
Successful Apply returns `{ "review": ..., "replayed": false }`; a committed
retry returns the original receipt with `replayed:true` without restoring a
superseded link. A stale account/performer preview returns 409 `preview_changed`;
changed contents under the same request UUID return 409 `request_conflict`.
Requests are bounded to 16 KiB and reject unknown fields. These routes require
the application session and same-origin checks; producer grants cannot manage
ownership.

The v3 **Account review** screen is available from the desktop More options menu
and the mobile navigation drawer. It starts with accounts needing review, with
separate linked, unlinked and all-account filters. Search and paging fetch 25
canonical accounts at a time. Handles and IDs already associated with the same
account appear on one card; shared identifiers do not establish ownership or
automatically consolidate accounts.

Open **Review account**, or **Change link** for an existing owner, and search for
an existing performer. Names, disambiguation and local IDs distinguish picker
choices. **Preview ownership** resolves that exact selection to its native UUID
and revision and shows the current and proposed owner. **Apply ownership change**
records the decision and refreshes only the selected account/card. **Unlink**
records no performer association; **Review later** returns the account to the
undecided queue. Neither operation changes depicted performers. Identifiers,
their retained evidence and ownership history load separately when expanded.

Before applying, the browser saves the original request in IndexedDB, scoped to
the public application endpoint and account UUID. After an interrupted request,
reopen that account and choose **Check and retry saved change**. Recovery checks
the original receipt before sending the same body again. Tabs share the saved
choice, and an unconfirmed write cannot be replaced with a different one. Only
a definitive stale-preview rejection enables **Review again**. Browser storage
holds pending delivery; committed ownership and history are authoritative in the
native database. The existing scene/image metadata journal retains its original
storage name and format.

### Account consolidation review API

The same application session can explicitly join duplicate records for **one
service account**. Native services and mirrors have distinct namespaces. Separate
accounts belonging to one performer should retain separate account records and
share their owner; a matching handle alone does not establish equivalence.

| Route | Result |
| --- | --- |
| `POST /account-consolidation/preview` | The two account components, resulting owner, identifier conflicts and apply blockers; read-only |
| `POST /account-consolidation/apply` | Atomic consolidation and original event receipt |
| `POST /account-consolidation/requests/<request-uuid>/check` | Read-only match against the exact saved request, or 404 when uncommitted |
| `GET /source-accounts/<uuid>/consolidation-history?after=<sequence>&limit=N` | Events involving that original account record, in sequence order |

A preview identifies two distinct canonical accounts in the same qualified
namespace:

```json
{
  "source_uuid": "<record-to-redirect>",
  "destination_uuid": "<record-to-keep>",
  "ownership_mode": "preserve",
  "accept_identifier_conflicts": false,
  "reason": "Confirmed the captured ID and profile handle belong together"
}
```

`preserve` keeps compatible existing ownership, including an explicit unlink.
When one account is undecided, the other's choice is preserved. When choices
conflict, use `ownership_mode:"choose"` with an `ownership` object containing
`state` (`linked`, `unlinked` or `undecided`). A linked choice also requires
`performer_uuid` and `performer_revision`; the other states omit those fields.
Preserve mode omits the entire ownership object. Stable-ID disagreements require
explicit acknowledgement with `accept_identifier_conflicts:true`. Preview
returns `blockers` (`ownership` and/or `identifiers`) and `ready:false` until
these choices are resolved. It does not auto-apply or discard conflicting claims.

Preview's `digest` binds both account components, all retained identifier claims
and their current owners. An explicit performer choice carries an independently
checked revision. Apply sends the preview input, digest and a new `request_uuid`.
The event's immutable request digest binds that entire request, including the
ownership choice, reason and acknowledgement, for exact replay. These routes use
the existing consolidation schema; they add no table or duplicate request body.

Save the exact Apply body before transmission. Receipt checking POSTs that same
body to the request's `/check` route, which performs only a primary-key event read
and digest comparison. It never applies a change or reconstructs an outcome from
current ownership. A matching check returns `{request,consolidation}`. Apply
returns `{review:{request,consolidation},replayed:false}`; a committed retry returns
the original event with `replayed:true`, even after further consolidation or
performer changes. Changed input under the same UUID returns `request_conflict`.
A new stale request returns `preview_changed`; unresolved choices return
`ownership_resolution_required` or `identifier_resolution_required`. Requests
remain bounded to 16 KiB, with strict JSON and same-origin checks.

In **Account review**, expand **Consolidate duplicate account records**, select
the account to keep, and preview the resulting identifiers and ownership. Search
is limited to the same service and 25 candidates; refine it or enter an exact
archive account UUID when necessary. Inspect each record's evidence before
acknowledging conflicting IDs. After an interrupted Apply, **Check and retry saved
consolidation** recovers its original request, including when the source already
redirects. Successful confirmation refreshes the two affected cards and removes
the redirected card from the retained queue. History is loaded on expansion.
Consolidation does not merge performers, move files or change media attribution.

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

### Candidate detail worker API

The scoped producer API advertises `discovery_detail_protocol: 1` and
`max_discovery_detail_bytes`. Routes below use the prefix
`/api/v3/ingest/discovery-details` and the same bearer-only producer authentication,
collection/root grants, strict JSON envelopes and no-store responses as listing
workers. Website credentials remain in the external worker environment.

| Operation | Route | Input |
| --- | --- | --- |
| Admit selected candidate | `POST /targets/{target}/jobs` | `expected_target_revision`, `candidate_sequence`, `policy_sha256`, `extractor_version`, optional `automatic` |
| Find collections to inspect | `POST /collections/inspect` | `policy_sha256`, `extractor_version`, `after`, `limit` |
| Inspect automatic candidates | `POST /collections/{collection}/candidates` | `after`, `limit` |
| Find queued jobs | `POST /collections/{collection}/jobs/ready` | `policy_sha256`, `extractor_version`, `after`, `limit` |
| Inspect original job | `GET /jobs/{job}` | None |
| Claim | `POST /jobs/{job}/claim` | `expected_revision`, `owner_uuid`, `policy_sha256`, `extractor_version`, `lease_seconds` |
| Renew | `POST /jobs/{job}/renew` | `owner_uuid`, `fence`, `lease_seconds` |
| Reserve website service | `POST /jobs/{job}/source` | `owner_uuid`, `fence`, `url` |
| Save checkpoint | `POST /jobs/{job}/checkpoint` | `owner_uuid`, `fence`, `expected_revision`, `body` |
| Resume saved checkpoint | `GET /jobs/{job}/checkpoint` | None |
| Complete comparison | `POST /jobs/{job}/complete` | `owner_uuid`, `fence`, `checkpoint_revision`, `checkpoint_sha256` |
| Read completed result | `GET /jobs/{job}/result` | None |
| Record controlled failure | `POST /jobs/{job}/failure` | `owner_uuid`, `fence`, `error_code` |
| Explicitly retry failed/cancelled job | `POST /jobs/{job}/retry` | `{}` |

Admission binds an existing weak candidate and the original listing evidence.
The detail runtime/policy is explicitly selected; it need not equal the listing
worker's policy. Exact admission replay returns its original job. Claims use
5–900 second leases, check the authenticated producer and runtime, and return
204 when scheduling prevents ownership. Readiness is a bounded active-job lookup;
it does not claim work or load source bodies.

`body` is a compact `stash-metadata-fetch-v1` JSON object for the exact canonical
Reddit/Twitter post, at most 32 MiB. Checkpoints retain original record producers
and times across attempt failover. A changed post/runtime, rewritten earlier
record or unresolved pending request cannot become completed evidence. Empty
responses can complete with `uncorroborated`. An unchanged original checkpoint
acknowledgement can be recovered after its lease expires without granting a new
lease. The server chooses retry delays and shared website cooldowns; failure
requests contain controlled codes, not raw extractor output.

Completion compares the retained transcript with the original frozen target and
listing. Its immutable response includes the job, checkpoint revision, completing
fence and compact `evidence` (`corroborated` or `uncorroborated`). Replay and
`GET .../result` return that original result. A successful comparison does not
publish metadata, accept a post identity or clear the current review blockers.
The application `detail-preview` endpoint remains a separate read-only operation;
supplied preview bytes cannot acknowledge a producer job.

The backend and Python detail worker are implemented. Producer schema 14 retains
claims, exact checkpoint bytes, completion/failure intents and comparison receipts
separately from enrichment publications. `deliver-detail` recovers persisted
operations without loading website settings. `post.verify_candidate` profiles
participate in `dispatch-all`, with independent delivery and collection cursors.
See the [producer commands](../integrations/gallery-dl/README.md#candidate-detail-worker).

`POST /api/v3/ingest/discovery-details/collections/ready` accepts
`policy_sha256`, `extractor_version`, `after` (a collection UUID) and `limit`.
Its sorted `{uuid}` entries come from the bounded active detail jobs and current
collection/root grants; it reads no transcript bodies and cannot admit work.
The capability is `discovery_detail_collections_protocol: 1`.

Automatic admission requires `discovery_detail_admission_protocol: 1` and a
selected `post.verify_candidate` worker profile. `/collections/inspect` returns
permitted active containers with retained listings, including containers that
still have blocked or empty work. It does not promise a runnable job.
`/collections/{collection}/candidates` returns `{candidates, after, has_more}`.
Its cursor is `{listing_uuid, source_ordinal}`; each request inspects at most 32
listing definitions and 32 targets through the existing indexes. `after`
advances over inspected rows even when no candidates are eligible. Resume while
`has_more` is true; a shorter candidate array does not mean traversal finished.

Each candidate supplies its cursor, target UUID/revision, candidate sequence,
post namespace/value and canonical fetch URL. Eligibility requires a completed
retained comparison, exactly one weak candidate, and no review blocker other
than missing detail. Existing detail-job history for that candidate, including
an earlier target revision, prevents automatic readmission. Failed/cancelled
work needs explicit retry; completed comparisons remain available for review.
Missing history, competing candidates, earlier recovery evidence, changed
source/post choices and identifier ownership remain blockers.

The producer checks the selected profile's URL support and sends
`automatic:true` with admission. The server repeats eligibility inside its
transaction and before commit. Exact original admission still replays after
later changes. Producer schema 15 adds a durable candidate cursor, preserving
all schema-14 journals and retry delays. Saved delivery and existing jobs run
first; an uncertain admission is recovered through the ready-job pass. Complete
container traversals pause before polling again.

Authenticated results feed the guarded native publication service above.
Verified staging release remains transition work. Production scrapers have not
switched to these routes.
