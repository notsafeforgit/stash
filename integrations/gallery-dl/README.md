# Native Stash producer

This package implements durable delivery and a gallery-dl download adapter.
It is development code on `v3-rewrite`; the installed host and n8n download
helpers still use their existing catalogs. Launcher conversion, activation of
reviewed worker profiles, additional source adapters and production cutover remain unfinished.

Python 3.12 or newer is required. Runtime delivery uses only the standard
library. Install the local package with `pip install ./integrations/gallery-dl`
in the runner's environment when integrating it; do not replace a live wrapper
with this CLI. The CLI can queue source requests, drain captured events and
execute one claimed download attempt using an explicitly reviewed local profile.
The optional `[gallery]` extra pins the gallery-dl source revision and yt-dlp
version used to verify downloader integration. Run `make pre-producer` to install
that test runtime into `.local/native-producer`, independently of live workers.

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
small acknowledgement row for replay and dependent file events. Outbox delivery
never modifies the download archive; the download adapter handles it as below.

Default capacity is 10,000 unacknowledged events or 512 MiB of queued payloads.
Pending, in-flight and review events all consume capacity. Exhaustion raises an
error and must pause the producer; no event is evicted. Acknowledgement rows are
retained, so capacity bounds queued payloads rather than total disk usage. A disk
write failure propagates to the caller and must stop new download work.

Transient failures use persistent exponential backoff, from five seconds to one
day. A rejected/expired Stash token leaves the evidence pending for rotation.
Scope, schema and conflict errors require explicit review. Retrying a reviewed
event preserves its bytes; correcting its contents requires a new event UUID.

## Download lifecycle

`RunQueue` durably records and submits typed source-run requests. `RunLease`
claims the work, renews ownership and sends checkpoints or a terminal outcome.
Lease deadlines use the response's server time and a conservative monotonic
budget, so a worker's wall-clock skew cannot extend ownership. Start the lease's
heartbeat before extraction and close it when finished. Failed renewal or
progress pauses new source work; it does not discard a current file's evidence.

Construct `Producer` with the matching outbox, lease and a `filesystem.Root`
whose device/inode identity was provisioned explicitly. `NativeDownloadJob`
requires that producer and an existing persistent lock directory shared by all
host/container writers. It checks the pinned collection URL and path prefix,
mount identity and live lease before extraction/download boundaries. Child jobs
inherit those objects, and asynchronous extraction is disabled. Destination
locks cover a filename stem and its transformed encodings.

The adapter enforces the claimed half-open source-post window (`since` inclusive,
`until` exclusive) before directory, file or postprocessor handling. Reddit's
original `created_utc` and Twitter's Snowflake timestamp retain milliseconds
that gallery-dl's formatted dates discard. Raw and transformed Twitter payloads
use the same boundaries. Missing or invalid source dates stop file processing.
An older pinned post is skipped without ending traversal, so later in-range
posts are still visited. Linked child work uses its accepted parent post's date.
Postprocessor initialization waits for an accepted post, and its proposed
destination is checked before initialization or post callbacks can run.

Date limits apply to each extractor instance. They replace inherited
`date-min`, `date-max`, `date-after` and `date-before` without changing the shared
configuration file. Child extractors do not apply unrelated upload-date limits
to an accepted parent's content. Other configured predicates and skip policies
remain in effect and must be included in the worker's reviewed configuration.
Extractor keywords cannot replace the source identity or publication date.

Before downloading, the adapter queues a retained source capture and derives the
attachment from source evidence. Reddit galleries use their media IDs; ordinary
Reddit images and videos use direct service URLs. Twitter captures preserve the
original media list before gallery-dl's transformation and keep each output's
attachment ID. Output numbers and filenames do not establish attachment identity.
Unsupported or ambiguous attachments stop before download. A single attachment
can be associated without creating a gallery.

After synchronous postprocessing, the adapter flushes and hashes the actual
final file, then queues its dependent file event before updating gallery-dl's
archive. Metadata writes use atomic replacement. Failed processors, queue
capacity or persistence failures leave the archive unacknowledged. Existing
unarchived files retry metadata/exec processing; archived files can repair queued
delivery without downloading again. Unresolved archive skips queue source
evidence only. The existing GIF-to-MKV converter is recognized explicitly.
Filename budgeting preserves source IDs and handles UTF-8 and downloader
temporary suffixes without truncating the source metadata.

Checkpoints retain the last completed source cursor during bounded replay. A
missing saved cursor cannot report successful traversal. The worker entry point
below supplies configuration, heartbeat, event delivery and attempt outcomes;
production launcher conversion remains separate work.
Only Reddit/Twitter attachment adapters are implemented so far; external linked
sources and multi-entry yt-dlp output association still need integration. These
SDK classes are not a production launcher and do not change installed hooks.

## Worker profiles and execution

A `stash-gallery-worker-v1` JSON profile separates portable gallery-dl settings
from deployment paths and local website-access references. Its five required
keys are `schema`, `root`, `locks`, `gallery` and `bindings`. Optional
`source_category` restricts the root extractor; a mismatch stops before extraction:

```json
{
  "schema": "stash-gallery-worker-v1",
  "source_category": "reddit",
  "root": {"uuid": "REVIEWED_ROOT_UUID", "path": "/media/porn", "identity": [0, 0]},
  "locks": {"path": "/persistent/download-locks", "identity": [0, 0]},
  "gallery": {
    "extractor": {
      "base-directory": "${stash:media_root}",
      "directory": ["{author}, reddit"],
      "filename": "{id}_{filename}.{extension}",
      "archive": "${stash:archive}",
      "skip": true,
      "reddit": {"cookies": "${stash:reddit_login}"}
    }
  },
  "bindings": {
    "archive": {"kind": "path", "path": "/persistent/archives/reddit.sqlite"},
    "reddit_login": {
      "kind": "private",
      "file": "/private/gallery-dl/config.json",
      "pointer": "/extractor/reddit/cookies"
    }
  }
}
```

This is a template, not an installable configuration. Provision the native root
UUID and review each existing directory's device/inode pair using
`filesystem.Root.probe` before filling `identity`; `[0, 0]` is a placeholder.
Use the actual shared lock directory and existing archive/path rules when
converting a deployment. The directory produced by gallery-dl must fit the
claimed collection's path prefix. Root and lock identities are checked again
at source boundaries; a replacement mount stops further work.

`path` bindings relocate archives and other local files without changing policy
identity. `asset` bindings add `path` and a reviewed `sha256` for local helper
code. Python postprocessor `function` settings must refer to such an asset,
for example `${stash:converter}:prepare`. Asset digests are checked when loading
the profile and their file identities are checked during execution. Put local
conversion helpers used by exec processors in asset bindings too. This does not
sandbox trusted postprocessors or fingerprint arbitrary system executables.
Legacy `gallery_catalog_hook` writers are rejected, including named definitions.

`private` bindings read a JSON Pointer from an existing local JSON file, or use
`{"kind":"private","env":"WEBSITE_ACCESS"}`. A complete reference can supply
structured cookies or headers. Layered access settings use
`{"kind":"private","sources":[{"file":"…","pointer":"…"},…]}` and merge
objects in order, replacing scalar/list values as gallery-dl does. Private
references are accepted only in website access settings and the values of known
yt-dlp access arguments such as `--cookies` or `--password`. They cannot be
interpolated into filenames or shell commands. These values
stay in worker memory and gallery-dl; they are not sent to Stash or copied into
the outbox. The Stash API token still uses the separate `--token-env` option.

The policy SHA-256 includes the portable gallery configuration, reviewed asset
digests, pinned gallery-dl/yt-dlp runtime identity and adapter source digest. It
excludes private values, local binding paths, directory identities and the
requested run window. Equivalent host/container profiles therefore share a
policy; rotating cookies does not create a new policy. Changing extraction or
conversion settings/code does. Conditional map order is retained in the digest
and saved profile: rearranging first-match filename rules changes the policy.
Relative binding paths resolve against the
profile file. Keep one worker in each process: gallery-dl configuration is
process-global, and simultaneous activation is rejected.

`worker-policy --profile FILE` validates the local profile and prints its policy
digest and root UUID. `queue-run --profile FILE` uses that digest in place of
`--policy`. `execute-run RUN_UUID --profile FILE` first checks server download
capability, root/operation/policy agreement and live ownership. It starts the
heartbeat and a drainer with its own SQLite connection, then runs gallery-dl.
Temporary failures request retry, source/access/configuration failures defer,
and loss of ownership pauses further source work. Current-file events remain
durable. A lost finish response is accepted only after finding the exact
producer, owner, fence and outcome in the server's attempt history.

The execution command prints JSON on stdout; downloader and child-process logs
go to stderr. Exit 0 and `source_succeeded` mean this source **attempt** finished.
Other source windows and media jobs can remain pending. `run_state` reports the
acknowledged run state when available; `intake_completion` explicitly requires
checking native receipts. Waiting, retry, deferred, paused and unconfirmed
completion exit 2. Invalid configuration or unavailable admission exits 1.
Continue draining the durable outbox after execution; stopping the download
process does not certify that its file events were delivered.

## Automatic source dispatch

`dispatch --profile FILE` performs one bounded scheduling cycle: deliver one
ready event batch, submit one queued source request, then discover up to 50
eligible runs and execute at most one claimed attempt. It uses the scoped
`POST /runs/ready` API, filtered by the reviewed root and policy. The server
returns only run UUIDs and pagination sequences. A discovery result grants no
ownership; the existing claim still checks collection definitions, destination
overlap, cooldowns and fencing before any source work starts.

```sh
stash-ingest --outbox /persistent/producer.sqlite --endpoint STASH_ORIGIN \
  --producer PRODUCER_UUID dispatch --profile /persistent/profiles/reddit.json
```

The producer retains its pagination cursor and discovery backoff in the outbox.
A page of busy candidates cannot permanently hide later work, including after
process restart. Cursor changes use revisions so competing dispatchers cannot
both advance the same page state. End-of-list wraps the cursor for a later pass;
this does not acknowledge any scrape as completed. Expired attempts go through
the server's recovery/backoff path. Deferred work requires explicit review.

Delivery and request admission continue while discovery is backing off. A cycle
can report `idle`, `waiting`, `backoff`, `contended`, `unavailable`, or a worker
attempt outcome. Exit 2 denotes unfinished local delivery/admission or an
unsuccessful attempt; exit 0 only describes this cycle. Neither certifies the
whole source queue or media intake. Preserve the result's native run identity
and inspect its state and file receipts when a workflow needs completion.

A host timer can invoke this as one oneshot service per reviewed profile, with
exit 2 accepted as pending work; an already-active service must not be launched
again by its timer. Host/n8n wrapper activation and their workflow receipt
conversion remain required. This command does not update installed launchers.

## n8n worker image

`Containerfile.n8n` adds the pinned producer and gallery-dl/yt-dlp dependencies
to `/opt/stash-ingest`, an isolated Python environment. Build on the existing
customized n8n image, pinned by digest or immutable local image ID; it must
already supply Python 3.12+, uv, git, ffmpeg and the retained conversion tools.

```sh
podman build --pull=never \
  --build-arg N8N_BASE_IMAGE=PINNED_CUSTOM_N8N_IMAGE \
  --build-arg STASH_REVISION=SOURCE_COMMIT \
  --tag localhost/n8n-native:SOURCE_COMMIT \
  --file integrations/gallery-dl/Containerfile.n8n integrations/gallery-dl
```

The build validates the exact supported gallery-dl commit and yt-dlp version.
Native launchers use `/opt/stash-ingest/bin/stash-ingest` and its sibling Python
interpreter. n8n's existing PATH, entry point and system Python environment stay
as supplied by the base image. Private website config, Stash token references,
profiles, outboxes, download archives and media are deployment mounts, never
image build inputs. Select the new image only at the verified cutover after
converting workflow/host entry points and their receipts.

## Converting existing gallery-dl settings

`stash-ingest-config` reads ordered JSON config layers and writes a **new,
inactive** worker profile. It requires the reviewed root UUID, media/lock paths
and device/inode identities. It does not register a root, contact a website,
start downloading, rewrite the input files or activate a launcher.

```sh
stash-ingest-config \
  --config /private/gallery-dl/config.json \
  --config /private/gallery-dl/worker-overrides.json \
  --category reddit \
  --root ROOT_UUID --root-path /media/porn --root-identity DEVICE INODE \
  --locks /persistent/download-locks --lock-identity DEVICE INODE \
  --working-directory /media/porn \
  --output /persistent/profiles/reddit.json
```

The command recursively merges objects and replaces arrays/scalars exactly as
gallery-dl does. It retains insertion order, filenames, archive formats/IDs,
original-quality flags, skip rules, pacing and remaining processors. Recognized
legacy `prepare`/`complete` catalog hooks are removed from named, typed and inline
definitions; unknown catalog callbacks require review. Archive segment lists
remain lists so formatted archive names still use gallery-dl's path handling.
Local Python/shell helpers in argument-array processors become asset bindings;
`--asset PATH` can identify additional helpers. Shell-string processors and
ambiguous path forms require explicit review rather than silent rewriting.

Access values reference their original input file and JSON Pointer, including
partial merges of structured headers/cookies and credential argument values.
Other yt-dlp arguments remain visible in the policy; changing format selection
must not be hidden as a credential change. Input files remain the private access
source, so the eventual cutover must keep those references valid when retiring
legacy settings. No credentials are printed in the conversion report.

`--category twitter` removes unrelated extractor settings and unused named
processors. Reddit can do the same when its existing whitelist limits children
to Reddit, Imgur, Redgifs and direct links; their base and parent-specific
settings remain. Unrecognized dependency graphs retain all configured sites.
This prevents unrelated ThisVid recovery settings from splitting otherwise
equivalent Reddit/Twitter worker policies. The root-category constraint also
prevents using the resulting profile for another service.

The report lists removed/excluded setting paths and binding counts, plus the
validated policy/root identity. Publication uses a flushed temporary file and
an exclusive atomic link with private permissions. An existing output is never
replaced. Missing mounts, helpers or pinned runtime prevent publication. Deploying
the new profiles and converting host/n8n/recovery launchers remain separate steps.

## Offline source requests

`run_queue.RunQueue` shares the event outbox database and producer/origin binding.
It accepts only a collection UUID/revision, operation, configuration fingerprint,
cooldown and an absolute half-open published-time window. It stores no commands
or website credentials. `since: null` means all earlier history; `until` is always
explicit. Window timestamps normalize to UTC with millisecond precision using
the same fixture corpus as the server.

Equivalent pending requests merge overlapping or adjacent windows. Disjoint
windows keep their gaps. Before HTTP submission, one window is frozen with a
stable request UUID, canonical bytes and digest. New requests preserve that
in-flight window and queue only additional coverage. Backoff and review block
further submissions for that configuration; a repeated timer does not reset them.
Ready download work precedes enrichment, and newer windows precede old history.
Concurrent senders use expiring fenced leases without holding a database
transaction during HTTP calls.

`submit_once` checks source-run capabilities, sends the frozen bytes, and requires
an admission response identifying that exact request and collection definition.
It atomically retains the response/run UUID and releases the request body.
Lost responses replay the original request. The state is **admitted**, including
when the response says the server run is queued or deferred. It never means the
scrape or media ingestion completed. Inspect the native run's current status;
claim a live source lease before starting extraction. An outage only queues work.

Callers that can retry a command after losing its response should supply a stable
`ticket_uuid` to `enqueue` (CLI `--ticket`). A ticket replay returns its existing
intent even after server admission. Changed request contents under that ticket
are rejected. Persist the absolute cutoff with the caller's execution identity;
recomputing a relative window on retry would change the request. A new scheduled
execution uses a new ticket. Ticket inspection includes its requested window and
the first applicable request sequence for paginated history; the history is not
a completion receipt for the whole requested range.

`ticket-status UUID` checks that range against the native runs originally
assigned to the ticket. Assignments are saved when a request is frozen, in the
same transaction as its pending-window removal. A caller joining an existing
frozen request shares its overlapping portion; any additional ranges retain
their own assignments. Neither retries nor later rescans replace those links.
A cancelled request stays cancelled even if an unrelated later run succeeds.

The inspector reads each assigned run's current completed windows, validates its
admitted collection/root/policy, and reports any remaining ranges. It reports
`source_succeeded` only when every assigned range is covered, even if a wider
shared run still has other work. Missing status, deferral, review and incomplete
coverage remain visible. An API outage returns `unavailable`, not a cached
success. Exit code 0 certifies this ticket's source traversal only; file intake
still requires the native event receipts. This is the completion check intended
for callers that record permanent backfill decisions.

Capacity defaults are 10,000 retained configuration groups, 64 disjoint pending
windows per group and 100,000 caller tickets. Exhaustion stops admission without
evicting work. Request acknowledgements and tickets remain as history, so these
limits are not a cap on total disk usage. Queue status reports pending windows,
submission states and age; a fresh schedule resets the age of an emptied group.

Producer schema 2 introduced request/ticket tables; schema 3 added dispatch
cursors and discovery backoff. Schema 4 adds durable ticket-to-submission links
and unassigned ranges. Opening a schema-1, schema-2 or schema-3 outbox promotes it
in one SQLite transaction, preserving event bytes, receipts, dependencies,
active delivery/submission leases, frozen requests and caller tickets. Old tickets
are linked to the first covering submissions from their original request sequence;
pending portions stay unassigned. Both tickets and requests are read in bounded
pages. A failed migration rolls back. Older producer code refuses the new schema when opening
it; preserve the queue in backups rather than recreating it during rollback.
This does not change the Stash database schema.

## Inspection and delivery

The CLI takes `--outbox PATH --endpoint ORIGIN --producer UUID`, followed by one
of these commands. `--token-env NAME` changes the environment reference; there
is no command-line token argument.

| Command | Result |
|---|---|
| `status` | Event counts/bytes/age plus the source-request backlog |
| `drain` | Attempts one ready batch of at most eight events; exits 2 while local event work remains |
| `retry EVENT_UUID` | Requeues one explicitly reviewed event with its original contents |
| `receipt-status EVENT_UUID` | Reads actual server ingestion/worker status |
| `queue-run --collection UUID --revision N --profile FILE --until TIME` | Records/coalesces a download request using the profile digest; low-level callers may use `--policy SHA256` instead |
| `submit-runs` | Submits one ready request; exits 2 while requests remain pending, in flight or in review |
| `dispatch --profile FILE` | Delivers/submits queued work and discovers at most one source attempt; retains pagination and discovery backoff |
| `runs-status [--intent UUID \| --ticket UUID] [--after N]` | Local request counts and up to 50 relevant historical submissions |
| `ticket-status UUID` | Checks the ticket's original source windows; exits 0 only for `source_succeeded`, otherwise 2 |
| `retry-run-request UUID` | Retries a reviewed submission with the original UUID and bytes |
| `worker-policy --profile FILE` | Validates the local worker profile and prints its portable policy/root identity |
| `execute-run RUN_UUID --profile FILE` | Executes one claimed source attempt; does not assert media intake completion |

`acknowledged` means Stash accepted the event transaction. For a file, that means
verification was queued; it does not assert a successful media import. A disabled
file processor leaves new file events pending while previously committed
receipts remain recoverable. `queue-run` succeeding means the request was recorded
locally; `submit-runs` succeeding means requests were admitted by Stash. Neither
command claims a completed scrape or starts a downloader.

## Validation

After `make pre-producer`, `make validate-producer` runs delivery and actual
gallery-dl runtime tests, including the same retention corpus used by Go.
`PRODUCER_PYTHON` can select another environment with the pinned dependencies.
Tests cover concurrent delivery, subprocess death,
receipt replay, token rotation, dependency ordering, capacity, partial batches,
redirect rejection, disabled file processing, download/skip/postprocessor paths,
Twitter transformations, filename budgets, lease loss, bounded resume, offline
window coalescing, caller tickets, split completion ranges, cancellation followed
by later rescans, schema promotion, shared window semantics,
portable configuration, changed assets, worker failure outcomes and log isolation.
The backend test
`TestPythonProducerDurableDeliveryAgainstNativeHTTP` runs this actual client
against the native HTTP router and SQLite, loses committed event and run-submission
responses, and checks replay after reopening, independent rejection, caller-ticket
deduplication, exact ticket completion and the submit/claim/renew/checkpoint/finish lease
cycle. `TestPythonDownloadWorkerRecoversFinishAndDeliversFiles` runs the worker
against the real API/SQLite, drains a capture during downloading, recovers a lost
attempt-completion response and admits a dependent file after reopening the
outbox. It checks the ticket-status CLI after restart and verifies that source
success still leaves actual media intake queued.
These checks are included in `make validate-fork`
and the build workflow. No production endpoint or source website is contacted.
