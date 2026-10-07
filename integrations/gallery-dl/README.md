# Native Stash producer

This package implements durable delivery and a gallery-dl download adapter.
It is development code on `v3-rewrite`; the installed host and n8n download
helpers still use their existing catalogs. Durable caller URL snapshots now feed
the native dispatcher. Staged host launchers cover Twitter, Reddit, Instagram,
Coomer, Kemono, Bluesky and TikTok, preserving saved-list, mode and date inputs.
The staged n8n adapter preserves stable execution tokens,
historical backfill decisions and actual source completion. Recovery caller
conversion, operational-history migration, activation of reviewed worker profiles,
additional source adapters and production cutover remain unfinished.

Python 3.12 or newer is required. Runtime delivery uses only the standard
library. Install the local package with `python -I -m pip install ./integrations/gallery-dl`
in the runner's environment when integrating it; do not replace a live wrapper
with this CLI. The CLI can queue source requests, drain captured events and
execute one claimed download attempt using an explicitly reviewed local profile.
The optional `[gallery]` extra pins the gallery-dl source revision and yt-dlp
version used to verify downloader integration. Run `make pre-producer` to install
that test runtime into `.local/native-producer`, independently of live workers.
The setup target also checks installed modules against current sources without
workspace imports. Wheel builds remove obsolete modules from reused `build/lib`
output; they must not alter a worker's policy fingerprint. Use Python `-I` for
package installation so a development `PYTHONPATH` cannot redirect pip's uninstall
lookup to the source tree.

## Authentication boundary

`STASH_INGEST_TOKEN` is a token for Stash's ingestion API. The queue binds to a
Stash HTTP origin and stable producer UUID, independently of that token. Rotating
the token preserves queued event identity and receipts. The client reads the
environment reference for each request and sends the token only in the
Authorization header; it does not store the token, follow redirects, inherit
website proxies or load cookies. Website credentials remain with gallery-dl.

Stash can authorize named collection/root pairs or all registered collections at
an explicitly selected logical root. The latter also covers later additions,
without granting source registration or editing. Existing tokens do not acquire
root grants when the database upgrades; unbound metadata still requires a named
collection grant. Capabilities report `scopes` and `root_uuids` separately.

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
source capture must already be queued for the same run, collection revision and root.
Delivery waits for that capture's receipt. The queue never creates a filename or
infers an attachment from a directory name; those are producer integration tasks.

An `attachment.download` report retains the original source attempt, capture and
attachment, with a stable transfer sequence taken from the capture's retained
outbox row. Start, failure, exclusion and skip reports wait for the capture receipt;
a downloaded report waits for its exact `file.completed` receipt. Each report is
at most 16 KiB. Acknowledgements must match the original event and capture and
must not claim verified media intake. Queue schema 16 admits these reports while
preserving all older payloads, acknowledgements and request/job journals.

SQLite transactions use WAL and `synchronous=FULL`. Independent drainers claim
bounded batches with expiring, fenced delivery leases. Concurrent first opens
use a bounded retry for SQLite BUSY/LOCKED responses during WAL activation,
without replacing the database. Other setup failures propagate.
Interrupted work replays
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

Native jobs also hold a shared filesystem-publication lock through each file's
download, postprocessing, completion and archive update. Initialization,
directory/finalization work and callbacks outside a file are guarded too. A
current file can finish after lease loss; new file work still checks the lease.
The locks are reentrant within a worker thread, including nested callbacks.

The host backup coordinator uses
`stash_ingest.publication_lock.PublicationBarrier(lock_directories)` to exclude
these mutations across every reviewed worker lock root. It acquires the barrier
**before** requesting Stash's native database writer guard. A gate blocks new
mutations while existing downloads finish; the default acquisition timeout is
five minutes. A timeout fails capture instead of claiming a filesystem boundary.
Call `barrier.release()` as soon as immutable filesystem views have been retained,
before the large database copy, packing or uploads. Context exit also releases
locks after an error or an earlier explicit release. Multiple roots are acquired
in stable physical-identity order.

The two `native-publication-*.lock` files are permanent coordination objects.
Host/container profiles must share the same physical lock directories; never
remove or replace their lock files while workers are active. This protocol does
not pause legacy gallery-dl processes or unrelated filesystem tools. The existing
backup/dedupe lock is still required, and the host snapshot orchestration remains
to be integrated before production uses this barrier.

The executor requires native `source_run_pacing_protocol: 1`. It reserves the root
service and each supported linked service before extractor initialization, which
may log in. Currently the reviewed linked services are Redgifs and Imgur. A busy
service stops the attempt with `source_busy`; native attempt dependencies make an
exact-window retry wait before repeating source work. Reservations are released
by completion, cancellation or expiry, and expired retries retain dependencies.

The executor also requires `source_run_fairness_protocol: 1`. Its server-derived
`turn_until` is five minutes after the attempt starts and cannot be extended by
heartbeats. The worker finishes its current file and checkpoint before yielding
at a source boundary. A resumed traversal may finish replay and one new checkpoint
first, so a long saved prefix cannot prevent progress indefinitely. This is a
cooperative budget, not a hard wall-clock limit. An acknowledged yield returns
`state: "yielded"` and the incomplete CLI exit code. Native history records
`retry` / `source_turn_complete` while preserving progress and the normal target
cooldown, without consuming failure attempts or imposing failure backoff.

Source rate limits and request timeouts stop gallery-dl's immediate HTTP retries.
Controlled extraction, authentication and access errors identify the service
that failed, including linked extractors. Applicable cooldowns affect that service.
An individual media download failure does not become a website-wide outage, and
intentional archive/window stops retain their normal semantics. Failed ownership
checks take precedence over source error reporting. A lost finish acknowledgement
is recovered only from the exact attempt with the same outcome, code and service.

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
final file, then queues its dependent file event and downloaded report before updating gallery-dl's
archive. Metadata writes use atomic replacement. Failed processors, queue
capacity or persistence failures leave the archive unacknowledged. Existing
unarchived files retry metadata/exec processing; archived files can repair queued
delivery without downloading again. Unresolved archive skips retain the source
evidence and an explicit skipped report without asserting that bytes are present.
The existing GIF-to-MKV converter is recognized explicitly.
Filename budgeting preserves source IDs and handles UTF-8 and downloader
temporary suffixes without truncating the source metadata.

A start report is durable before the downloader writes bytes. Fallback URLs use
the same transfer, with failure recorded only when they are exhausted. Failed
postprocessing, unsupported mirror files and unresolved skips retain distinct
outcomes. A process interruption may leave only a start report; the server derives
interruption from the source attempt rather than fabricating a completion. A
current file can report its original attempt after lease loss. New worker claims
require the backend's attachment-download capability before source access.

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
ready event batch, resolve up to 50 pending caller URLs, submit one queued source
request, then discover up to 50
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
attempt outcome. Exit 2 denotes unfinished local resolution/delivery/admission or an
unsuccessful attempt; exit 0 only describes this cycle. Neither certifies the
whole source queue or media intake. Preserve the result's native run identity
and inspect its state and file receipts when a workflow needs completion.

The [host worker units](#host-worker-service-and-timer) run `dispatch-all` across
the reviewed local profiles. Host/n8n wrapper activation and migration of old
operational receipts remain required. These commands do not update installed
launchers.

### Host worker service and timer

The templates in [`systemd/`](systemd/) provide one user service instance per
producer runtime. The `host` instance reads
`~/.config/stash-ingest/host.env`; its profile list uses the
[`stash-gallery-dispatch-v1` format](#dispatch-across-local-profiles). Configure
the installed executable, durable outbox, Stash origin, registered producer
UUID, reviewed profiles and scoped ingestion token with absolute paths. The
example contains placeholders. Keep the completed environment file private
(mode `0600`) and include it, the profile dependency closure and the outbox in
the deployment's backup inventory.

At the reviewed native cutover, after registering the producer and validating
the installed runtime and its profile policies, install and start the units:

```sh
install -d -m 0700 ~/.config/stash-ingest
install -d -m 0755 ~/.config/systemd/user
install -m 0644 integrations/gallery-dl/systemd/stash-ingest-worker@.service \
  integrations/gallery-dl/systemd/stash-ingest-worker@.timer \
  ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now stash-ingest-worker@host.timer
```

Provision the completed `host.env` before starting the timer; copying the
placeholder file is not configuration. These are user units, so unattended
operation requires the user's service manager to remain running after logout.
Use a separate producer identity, outbox and profile list for each runtime.
An n8n container needs its worker in the container's reviewed runtime with its
own path mappings; a host unit cannot substitute for that worker just by
pointing at container paths.

The timer starts its first cycle one minute after the user manager starts
(immediately if enabled later) and schedules subsequent cycles 30 seconds after
the service becomes inactive. Timer coalescing and random delay are configured
at five seconds each; system load can postpone execution further. A cycle can
contain a long source attempt. The
service has no start timeout, and an already-running instance is not launched
again by either its timer or another `systemctl start` for that same instance.
This prevents timer overlap for that instance; native source leases still
coordinate other producers and manual/n8n requests.

Exit `2` is accepted because a cycle may leave pending, deferred or review work;
exit `0` also describes only that cycle. Inspect its JSON outcome and native
run/file receipts for completion. Configuration and execution failures remain
visible in the journal, and the timer can retry on its next cycle. The service
stops its whole process group; unfinished durable work resumes through outbox
delivery and fenced lease recovery.

```sh
systemctl --user status stash-ingest-worker@host.timer stash-ingest-worker@host.service
journalctl --user -u stash-ingest-worker@host.service -n 50
# Stop the timer and any active cycle before replacing its runtime/configuration.
systemctl --user stop stash-ingest-worker@host.timer stash-ingest-worker@host.service
```

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

`--category twitter`, `instagram`, `coomer`, `kemono`, `bluesky` and `tiktok` remove unrelated extractor
settings and unused named processors. Reddit can do the same when its existing whitelist limits children
to Reddit, Imgur, Redgifs and direct links; their base and parent-specific
settings remain. Unrecognized dependency graphs retain all configured sites.
This prevents unrelated ThisVid recovery settings from splitting otherwise
equivalent Reddit/Twitter worker policies. The root-category constraint also
prevents using the resulting profile for another service.

Create a separate profile with `--full-history` for the host wrappers' historical
`-o skip=true` override. This sets the global gallery-dl `skip` option, which
overrides extractor and child-specific `abort:4` settings without removing them.
The normal and full-history profiles have different policy digests. This option
does not clear date limits or bypass the download archive; already downloaded
files can still be skipped while traversal continues past them.

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
cursors and discovery backoff. Schema 4 added durable ticket-to-submission links
and unassigned ranges. Schema 5 adds caller snapshots and source bindings;
schema 6 adds durable backfill calls, history checks and completion proof.
Schema 7 adds retained n8n receipt history; schema 8 adds the enrichment execution
journal; schema 9 adds enrichment discovery cursors and backoff; schema 10 adds
rotation across worker profiles and permitted metadata collections; schema 11
adds durable discovery page delivery; schema 12 adds its dispatch cursors and
backoff; schema 13 adds discovery collection rotation and separates discovery
and enrichment delivery cursors in the shared worker. Schema 14 adds candidate
detail execution, collection dispatch and an independent delivery cursor. Schema
15 adds the cursor for automatic candidate inspection. Schema 16 admits durable
attachment-download reports and their capture/file dependencies.
Opening an outbox from schemas 1–15 promotes it
in one SQLite transaction, preserving event bytes, receipts, dependencies,
active delivery/submission leases, frozen requests and caller tickets. Old tickets
are linked to the first covering submissions from their original request sequence;
pending portions stay unassigned. Both tickets and requests are read in bounded
pages. A failed migration rolls back. Older producer code refuses the new schema when opening
it; preserve the queue in backups rather than recreating it during rollback.
This does not change the Stash database schema.

## Durable caller lists

`queue-sources` records one execution with a caller-provided UUID, an ordered
URL list, reviewed policy, logical root, operation and frozen absolute window.
It works before Stash is reachable or the sources have collection bindings.
The UTF-8 file uses the first token of each nonempty, non-comment line;
duplicates retain their first position. Entries must already be exact HTTP(S)
source URLs. The host launchers below expand Twitter handles and Reddit modes
before recording the snapshot; this low-level command does not reinterpret those
lists. The n8n adapter below adds its permanent-history and workflow-result contract.

```sh
stash-ingest --outbox /persistent/producer.sqlite --endpoint STASH_ORIGIN \
  --producer PRODUCER_UUID queue-sources --call CALL_UUID \
  --targets-file /persistent/source-urls.txt \
  --profile /persistent/profiles/reddit.json --lookback-seconds 604800
```

When `--until` is omitted, the first committed snapshot fixes the current UTC
cutoff. `--lookback-seconds` subtracts from that cutoff; `--since` supplies an
absolute lower bound instead. Omitting both requests all earlier history.
The time window does not change the reviewed profile's archive/skip behavior.
Full-history host launchers require the separate reviewed global `skip=true`
profile described above.

Repeat the same caller UUID and options after a lost command response. The
original URL list, policy and window are retained even if the files changed or
disappeared; preparation is not repeated. Changed options under that UUID are
rejected. A fresh scheduled execution uses a fresh UUID. Concurrent first
requests retain whichever snapshot commits first. Local acknowledgement is not
source or media completion.

`resolve-sources` and `dispatch` resolve at most 50 targets per cycle. Resolution
prioritizes downloads over enrichment and newer cutoffs within each operation,
matching the subsequent submission queue. Each unique
active collection match and its deterministic source ticket commit in one local
transaction. Interruption cannot leave a queued ticket without its retained
binding. Existing source-request coalescing still combines equivalent work from
different callers. Bound targets are never looked up again or silently moved to
another collection/revision. Unresolved, ambiguous, disabled and retired matches
remain in review. After fixing the source definitions or access grants,
`retry-call UUID` retries only the reviewed targets; already queued work remains
bound. Transient lookup failures retain fenced leases and persistent backoff.

`calls-status --call UUID --after N` pages through 50 local targets at a time.
`call-status UUID` checks every target's original source ticket and requested
window, sharing a bounded cache only within that inspection. It reports success
only when all targets have confirmed source coverage. Later rescans cannot
complete a cancelled earlier call. Outages and mismatched bindings remain
visible; diagnostics include at most 20 affected URLs with an explicit truncation
flag. Media intake still requires the native event receipts.

A snapshot holds at most 10,000 distinct URLs and 8 MiB of URL data. The default
queue limits are 10,000 retained calls and 100,000 retained targets. Capacity
failure preserves old records and rolls back the new snapshot. These records,
bindings and ticket relationships are part of the persistent producer outbox
and its backup/restore boundary; schema migration invents no past caller runs.

## Staged host launchers

The package entry points `stash-ingest-twitter`, `stash-ingest-reddit`,
`stash-ingest-instagram`, `stash-ingest-coomer`, `stash-ingest-kemono`,
`stash-ingest-bluesky` and `stash-ingest-tiktok` record source calls through this queue. The first two
are also available as `bin/update-twitter-media` and `bin/update-reddit-media`.
Run the scripts with the installed producer's
Python. They are staged replacements; the existing host scripts and n8n workflow
commands have not switched.

| Input | Retained behavior |
|---|---|
| Twitter `--username`, `--user-id`/`--gid` | Handles, profile URLs and numeric account IDs use the existing normalized X URLs |
| Instagram `--username` | Handles become profile URLs; explicit post/reel/story/highlight URLs keep their path and query |
| Coomer/Kemono `--url` | Explicit account, post or posts-listing URLs retain their underlying service, account ID, path and query; other container types require separate adapters |
| Bluesky/TikTok `--url` | Keeps exact supported post/profile/collection URLs and TikTok shortlinks; standalone artwork and following traversal require separate adapters |
| `--config-file` | Twitter retains first-token/comment handling and input order; Reddit sorts profile/community names before expansion |
| Instagram/Coomer/Kemono/Bluesky/TikTok `--config-file` | Preserves URL order and removes exact duplicates; blank/comment-only lines are ignored and invalid lines stop the request |
| Reddit `--mode new\|top`, `--subreddit`, `--saved` | Retains profile/search URLs, all/year top variants, communities and explicit saved-post targets |
| Reddit `--date-min`, `--date-min-relative`, `--date-min-days` | Freezes the first request's lower bound; the absolute option takes precedence |
| `--full-history`, Reddit top mode | Requires the reviewed profile with global `skip=true`; retains any date minimum |
| `--dry-run` | Prints URL/window expansion without a profile, API request or outbox creation |

Saved-list defaults remain `~/.config/gallery-dl/twitter-list.conf`,
`reddit-list.conf`, `instagram-list.conf`, `coomer-list.conf`, `kemono-list.conf`,
`bluesky-list.conf` and `tiktok-list.conf`,
overridden by the corresponding `<SERVICE>_LIST_CONFIG` variable, such as
`COOMER_LIST_CONFIG`. A missing list requires an explicit target or list path;
installing a launcher does not create a list or schedule.
Ignored input lines are reported by line number. Empty lists and invalid date
filters fail instead of claiming completed work. Reddit `me` is accepted only
through the saved-post option; use Twitter's ID option for `/i/user/ID` URLs.
Raw gallery-dl flags and `--gallery-dl-bin` are replaced by the reviewed profile.

Set `STASH_INGEST_OUTBOX`, `STASH_INGEST_ENDPOINT`, `STASH_INGEST_PRODUCER`, and
the Stash API token environment reference. `--profile` chooses an explicit
profile; otherwise the launcher selects `STASH_INGEST_<SERVICE>_PROFILE`, where
`<SERVICE>` is `TWITTER`, `REDDIT`, `INSTAGRAM`, `COOMER`, `KEMONO`, `BLUESKY` or
`TIKTOK`. Full-history/top requests select
the corresponding `STASH_INGEST_<SERVICE>_FULL_HISTORY_PROFILE`.
These profiles must have the matching
source category. Website access remains in their local gallery-dl references.

```sh
stash-ingest-reddit --username Example --mode top --call CALL_UUID \
  --profile /persistent/profiles/reddit-full-history.json
stash-ingest --outbox /persistent/producer.sqlite --endpoint STASH_ORIGIN \
  --producer PRODUCER_UUID call-status CALL_UUID
```

`--call` must stay fixed across retries of one execution. Under systemd, omitting
it derives a UUID from the producer, `INVOCATION_ID`, service and mode. Each new
invocation gets new work; interactive calls without an explicit UUID create a new
execution. The response includes the call UUID. Repeating it with the same
options retains the original lists, policy and dates even if input files vanish.

By default exit 0 means **recorded locally**, with JSON `state: recorded`.
Dispatchers/workers execute the call separately. `--strict-errors` also inspects
completion and exits 0 only for `source_succeeded`, 2 for unfinished/review work,
and 1 when inputs, storage or API access fail. It does not wait for completion.
Source success still requires separate file-intake receipts. An existing n8n
completion branch must never treat local recording as completed backfill.

The old relative date options formatted local wall time without an offset,
which gallery-dl interpreted as UTC. This conversion preserves that exact
boundary and freezes it once. Use an explicit timezone in `--date-min` when a
new caller needs an unambiguous cutoff. ISO string bounds retain gallery-dl's
second precision; numeric Unix bounds retain milliseconds. Native windows reject
empty/future lower bounds and sub-millisecond numeric values.

## Inspection and delivery

The CLI takes `--outbox PATH --endpoint ORIGIN --producer UUID`, followed by one
of these commands. `--token-env NAME` changes the environment reference; there
is no command-line token argument.

| Command | Result |
|---|---|
| `status` | Event counts/bytes/age plus source-request and caller backlogs |
| `lookup-collections --target URL [--target URL ...] [--root UUID]` | Current scoped collection candidates; exits 0 only when each URL resolves to one active collection, otherwise 2 |
| `queue-sources --call UUID --targets-file FILE --profile FILE` | Freezes one caller execution and its source list locally; optional absolute or relative time window |
| `resolve-sources` | Resolves up to 50 caller URLs and queues bound tickets; exits 2 while unresolved or reviewed targets remain |
| `calls-status [--call UUID] [--after N]` | Local caller counts and an optional page of retained source bindings |
| `call-status UUID` | Checks all original source tickets; exits 0 only for `source_succeeded`, otherwise 2 |
| `retry-call UUID` | Retries reviewed targets without changing bound sources or the frozen window |
| `drain` | Attempts one ready batch of at most eight events; exits 2 while local event work remains |
| `retry EVENT_UUID` | Requeues one explicitly reviewed event with its original contents |
| `receipt-status EVENT_UUID` | Reads actual server ingestion/worker status |
| `queue-run --collection UUID --revision N --profile FILE --until TIME` | Records/coalesces a download request using the profile digest; low-level callers may use `--policy SHA256` instead |
| `submit-runs` | Submits one ready request; exits 2 while requests remain pending, in flight or in review |
| `dispatch --profile FILE` | Resolves/delivers/submits queued work and discovers at most one source attempt; retains pagination and backoff |
| `dispatch-all --profiles FILE` | Delivers saved work and rotates across local download, enrichment and account-listing profiles and permitted collections |
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

Collection lookup uses the native API's `collection_lookup` capability and
`POST /collections/lookup`. A request contains up to 50 distinct exact source
URLs and one logical root; omit the root only for unbound metadata collections.
The server applies the token's collection/root grants to current revisions,
then returns candidate UUIDs, revisions and states grouped by URL. Each URL has
at most 128 candidates and a `has_more` flag. Truncation always requires review;
the client validates the flag and allows up to 4 MiB for the entire response.
It does not send collection labels, account associations, directory names or
local mounts.

An absent visible match is `unresolved`; it does not prove the URL is absent
from the whole library. Multiple matches are `ambiguous`, including when one is
inactive. One disabled or retired match retains that state. Callers must review
those cases rather than select a candidate automatically. Historical targets,
changed media roots and different query/sort parameters never silently match.
The Python client rejects inconsistent roots, URLs, duplicate IDs and malformed
revisions before producing a usable binding. Lookup neither creates collections
nor enqueues work. The durable caller queue freezes the original URLs and time
window, then commits selected bindings with their source tickets before native
submission. Installed launcher conversion remains unfinished.

## Retaining the scan journal

`stash-import-scan-journal` prepares a frozen journal database through one
read-only SQLite snapshot. It inventories every table/view, rejects unknown
families or changed columns, retains all recognized scan-history rows and reports
the account-backfill tables handled by `stash-import-backfills` separately.
Preparation is local and prints counts/digests without raw commands or payloads.
The server performs semantic validation when applying the snapshot.

```sh
stash-import-scan-journal --journal /migration/run-journal.sqlite \
  --root ROOT_UUID --source ORIGINAL_DATABASE_UUID --snapshot SNAPSHOT_UUID \
  --captured-at 2026-10-01T12:00:00Z
stash-import-scan-journal --journal /migration/run-journal.sqlite \
  --root ROOT_UUID --source ORIGINAL_DATABASE_UUID --snapshot SNAPSHOT_UUID \
  --captured-at 2026-10-01T12:00:00Z --expected-sha256 PREPARED_INPUT_SHA256 \
  --endpoint https://native-stash.example --apply
```

Use the same database UUID as the account-backfill importer, and a distinct
snapshot UUID and fixed capture time for this backup boundary. Apply requires
an explicit endpoint, the reviewed preparation digest and `STASH_API_KEY`
(or `--api-key-env`). It makes one atomic application API request. Repeat the
same frozen inputs after a lost response; an existing snapshot identity cannot
be repurposed for changed bytes, another root or another source database.
No proxy, redirect, website login or producer token participates in this import.

The native receipt inventories `scan_jobs`, `extractor_jobs`, `scan_deferrals`,
`backfill_scan_completion`, `collection_backfill_completion`,
`backfill_policy_migrations` and `legacy_handoffs`, including empty tables.
Older extractor tables can omit their later checkpoint columns. Valid unknown
historical provenance inside JSON strings is preserved; unsupported command
options must be reviewed in the private source snapshot before importing them.
Limits are 10,000 retained records and 8 MiB per atomic document. Oversized
snapshots fail rather than silently dropping rows or splitting the boundary.

The result is `retained`, with `jobs_activated: 0`. This is an evidence-import
step, not completion of the operational migration. Original retry delays,
deferrals, date minima and cursor positions remain available for the next
binding/activation step described below. Manual/orphan extractor scopes and historical
process handoffs remain review records. Historical collection and hashed per-scan
completions never become native source-window proof. No old command or systemd
unit is executed, and no PID is resumed.

Retained rows and their manifest now belong to Stash's database and its normal
backup boundary. Keep the frozen original and preparation manifest for semantic
reconciliation and rollback. The original journal and active old workers are
untouched by this development importer.

## Activating retained scans

`stash-activate-scan-journal` previews a binding through the application API;
`--apply` requires its reviewed plan digest. Prepare a JSON object with a new
activation `uuid`, the selected `scan_record_uuid`, native `collection_uuid`,
`collection_revision`, `root_revision`, reviewed `policy_sha256`,
`cooldown_seconds` and fixed RFC3339 `cutoff` at millisecond precision. The source
URL and logical root must match exactly. The cutoff must cover the frozen
snapshot. The native profile replaces the old command; verify its destination,
archive and skip policy as part of the source binding.

```sh
stash-activate-scan-journal --binding recovery-binding.json \
  --endpoint https://native-stash.example > recovery-preview.json
stash-activate-scan-journal --binding recovery-preview.json \
  --endpoint https://native-stash.example --expected-sha256 REVIEWED_PLAN_SHA256 --apply
```

Both commands use `STASH_API_KEY` (or `--api-key-env`), with no proxy or redirect.
Keep the returned normalized binding and plan digest for retries. A lost response
is retried with exactly those inputs; activation never resets an existing native
run. The result is one run for all equivalent pending requests at this exact
snapshot/context/URL. Differing old command policies require review. Existing
deferrals and retry delays survive; normal source-run review controls retry or
cancellation. No worker starts merely because the snapshot was retained.

Optionally set `checkpoint_record_uuid` to a retained extractor checkpoint whose
scope covers the consolidated lower bound. The native worker reproduces that
legacy archive-key hash during replay and then records native cursors. A missing
checkpoint leaves the run unfinished. Full-history/no-skip profiles without an
archive stop rule keep their old behavior of ignoring legacy stop checkpoints.
Omit the checkpoint to replay the window without archive stopping. An expanded
window also replays without archive stopping so unfinished older work is covered.
No existing files are downloaded again solely because archive stopping is disabled;
the reviewed profile still controls archive skips and forced downloads.

The final production binding requires the common quiesced migration boundary.
Development rehearsals use isolated roots and do not authorize production worker
activation. Manual/orphan checkpoints, historical handoffs and completion hashes
remain evidence; this command creates no historical lease or completion proof.

## Frozen catalog registry import

`stash-import-catalog-identities` imports saved performer UUIDs, local bindings,
merge history and explicit account choices from a frozen registry. After that
reviewed import, `stash-import-catalog-registry` imports the complementary account
identifiers, catalog groupings and retained routing/history evidence from the same
frozen database. Both are maintenance commands authorized by `STASH_API_KEY`;
producer ingest tokens cannot perform these imports.

```sh
stash-import-catalog-registry --registry /migration/registry.sqlite3 \
  --identity-import /migration/identity-receipt.json --snapshot SNAPSHOT_UUID \
  > /migration/registry-binding.json
stash-import-catalog-registry --binding /migration/registry-binding.json \
  --endpoint https://native-stash.example > /migration/registry-preview.json
stash-import-catalog-registry --binding /migration/registry-preview.json \
  --endpoint https://native-stash.example --expected-sha256 REVIEWED_PLAN_SHA256 --apply
```

Keep the frozen snapshot, saved binding and reviewed digest privately for retry.
The second phase obtains the original registry UUID and capture time from the
first phase's saved binding, preview or receipt. Unknown schema shapes are
rejected, and uncertain identities remain review records. The imported catalog
collections are disabled and have no scrape target/root binding. These commands
do not import individual catalog bodies or activate workers. See the
[native registry contracts](../../docs/native-source-identity.md#importing-the-performer-registry)
for performer-import preparation, limits, ownership rules and inspection APIs.

### Individual catalog snapshots

`stash-prepare-catalog` prepares the individual catalog databases for the next
import phase. It reads a frozen SQLite input in one read-only transaction,
validates recognized schema versions 1–3, checks declared and logical references,
and retains every physical record family. Unknown tables, columns and views fail
preparation. Normalized sidecar views are inventoried without copying the same
physical document twice.

```sh
stash-prepare-catalog --catalog /migration/catalogs/CATALOG_ID.sqlite3 \
  --output /migration/prepared/CATALOG_ID --source REGISTRY_SOURCE_UUID \
  --snapshot SNAPSHOT_UUID --captured-at FIXED_RFC3339_TIME
stash-prepare-catalog --verify /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256
```

The output directory contains `manifest.json` and ordered `records-NNNNNN.jsonl`
chunks. Each chunk holds at most 1,000 records and 16 MiB. SQLite text stays text,
including original embedded JSON strings; binary sidecar bytes use an explicit
base64 value. Hashes/counts cover every table, chunk and reconstructed capture.
The manifest includes the original catalog identity, schema definitions and
reference counts. Profile bodies and shared observations remain stored once per
physical source row. Capture reconstruction verifies their references and patches
without persisting expanded payload copies.

Snapshots publish from a private temporary directory after file/directory flushes;
existing destinations are never overwritten. Verification checks the saved
manifest digest, chunk names/counts/hashes, ordered unique keys and binary
checksums. After a lost preparation response, inspect the same directory with
`--verify`. Preparation creates no native records, jobs or media and explicitly
reports `imported:false`. Individually consistent live backups do not replace the
coordinated production cutover snapshot.

After importing the corresponding registry/collection mapping, receive a reviewed
snapshot in a native database with:

```sh
stash-upload-catalog --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

The command validates the complete local snapshot before its first request. It
uses the existing Stash application key from `STASH_API_KEY` (or `--api-key-env
NAME`), never a producer token or website credential. It sends original bytes and
the frozen manifest digest, checks each receipt's identity/counts, and resumes at
the server's next chunk. Repeat the same command after interruption or response
loss; an earlier committed chunk is not applied twice. Files changed after local
verification cannot be uploaded under the original digest.

The result has `state: "received"`, `imported: false`, and explicitly pending
record families. This is temporary migration staging in native schema 1000030,
not native metadata import or job activation.

After receipt, map the frozen source evidence through native schema 1000031:

```sh
stash-import-catalog-evidence --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

This command uses the same application key and origin rules. It verifies the
local snapshot and completed upload, then advances bounded native transactions
from the server's last committed ordinal. Repeat the same command after a lost
response or interruption. It retains native posts, shared revisions, captures and
profiles, including profiles without capture references; it does not apply media
or metadata changes. Qualified post IDs can share evidence across catalogs while
unqualified local keys remain scoped to their original catalog. Mirror service
namespaces stay distinct from native services.

Exit 0 means these four evidence families were mapped; exit 2 means the pass
finished with identity review outcomes; exit 1 means it failed or the response
was unavailable. Every result remains `imported:false`: other families, semantic
reconciliation and final migration completion are separate. Inspect bounded row
summaries at `/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/evidence-import/records`.
The original frozen rows remain available for review. No website credentials,
source scheduling or existing selected Stash metadata are changed.

Once the evidence pass has finished, import its account references, handle
history, post URLs, aliases and historical account claims with schema 1000033:

```sh
stash-import-catalog-relations --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

This uses the same application API-key reference and frozen manifest. It first
validates local files and the received snapshot, then resumes bounded batches
from `/relations-import`. The server requires a completed evidence pass. Exit 0
means mapped, exit 2 means completed with review outcomes, and exit 1 means
failure or an unavailable response; rerun the same command after a lost response.

Accounts use the snapshot's frozen registry mappings. Mirror handle fields are
retained as legacy labels; ambiguous or invalid records remain for review with
their complete original values. Historical post/account links are unselected
claims, so they cannot silently choose publishers or depicted performers. Posts
without an old account are counted as `unassigned`.

Inspect bounded summaries at
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/relations-import/records`;
append `/ORDINAL` to that path for full evidence. Summary keys above 8 KiB
are omitted with `key_omitted:true`; the individual record retains the full key
and source values. These receipts also remain `imported:false`. Importing media,
memberships, sidecars, edits and other histories, choosing captured publishers,
and final reconciliation remain separate work.

After the evidence and relationship passes, select publishers from actual
captured account IDs with native schema 1000034:

```sh
stash-import-catalog-publishers --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

The command uses the same application API-key reference, frozen manifest and
resume behavior. It advances bounded batches through `/publisher-import`, using
the core captured-account policy. Qualified IDs can link or create accounts;
ambiguous candidates and invalid identities remain for review. Existing
publisher choices and explicit unlinks are preserved. Captures without author
IDs remain `unavailable`; folder names, scraped feed owners and old catalog
links do not supply a publisher. Shared observation parents are not extra
captures. This pass does not assign depicted performers or selected metadata.

Exit 0 means completed without review outcomes, including any unavailable or
preserved captures. Exit 2 means completed with review outcomes; exit 1 means
failure or an unavailable response. Rerun the same command to resume a lost
response. Inspect bounded summaries at
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/publisher-import/records`;
append `/ORDINAL` for the full key and retained decision context. Receipts refer
to existing captures and decisions rather than duplicating their source payloads.
Every result remains `imported:false` until the remaining catalog families and
final reconciliation are complete.

Map supported source attachment lists from the received captures with schema
1000035:

```sh
stash-import-catalog-attachments --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

This pass requires completed evidence mapping and uses the same application
API-key reference, manifest checks and resume behavior through
`/attachment-import`. It shares identical source lists, combines compatible
partial lists, preserves source order and protects pinned/disabled choices.
Contradictory lists retain review outcomes. Counters and filenames cannot supply
missing album evidence: older Twitter captures without retained original lists
remain unavailable even when individual downloaded files are known.

Exit 0 means completed without review outcomes, including unavailable or
preserved records; exit 2 means completed with review outcomes; exit 1 means
failure or an unavailable response. Rerun the same command after interruption.
Inspect summaries at
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/attachment-import/records`;
append `/ORDINAL` for source keys and decision context. Conflict samples are
bounded and explicitly marked when truncated. Original source manifests remain
intact. Every result remains `imported:false`: file matching, legacy appearances,
gallery construction and the other catalog families are subsequent work.

Map assets, files and appearances with schema 1000038 and an explicit historical
root mapping:

```sh
stash-import-catalog-media --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN \
  --root-uuid ROOT_UUID --root-revision ROOT_REVISION \
  --collection-revision COLLECTION_REVISION --library-root-path /media/porn
```

Use the path prefix stored in the copied Stash database, which may differ from
the host's mount. The native logical root can remain disabled and offline. This
binding neither activates a worker nor grants filesystem access. The command
uses application authentication, validates the completed evidence pass and
resumes the same immutable binding after interruptions.

Shared asset claims retain declared digests without treating path-derived IDs
as hashes. File observations retain missing, pending, deduplicated and converted
paths. Matches require a unique literal library path or existing verified
content; ZIP matches include archive/member identity. Appearances become native
post-file evidence and, where one existing scene/image owns the matched file,
post-media evidence. Unavailable files stay recorded; ambiguous or inconsistent
matches require review. Download positions do not establish album order, and
this pass preserves existing library metadata, attribution and galleries.

Exit 0 means completed without review outcomes, exit 2 means completed with
review outcomes, and exit 1 means failure or an unavailable response. Progress
uses the processed-record count across the asset/file/appearance phases.
Inspect `/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/media-import/records`
for bounded summaries and append `/ORDINAL` for one full receipt. Results remain
`imported:false` pending the other catalog families and final reconciliation.

Map file edit, state and deduplication history with schema 1000066 after the
reviewed media import completes:

```sh
stash-import-catalog-file-history --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

This pass reuses the frozen root/collection binding and retains the original
event IDs, source timestamps, file locations and content claims. Each batch
handles at most 50 records with a 16 MiB source-data budget. Resume the same
command after an interrupted response; its cursor is the last source ordinal.
Completed receipts remain stable after later collection edits.

Metadata edits preserve each path separately. Old `null` values mean inherit;
relationship values remain source names pending identity review. Unknown fields
and unsupported representations remain inspectable extensions. Missing or
conflicting deduplication members retain their declared paths for review.
Historical state changes and deduplication never replay filesystem actions, and
this import leaves current selected Stash fields untouched.

Inspect `/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/file-history-import/records`
for summaries and append `/ORDINAL` for the retained source row. Follow
`history_uuid` to `/api/v3/archive/file-history/UUID` for the native event.
Observation and claim history endpoints are documented in
[source file history](../../docs/native-schema.md#source-file-history).
Exit codes remain 0 (mapped), 2 (review), and 1 (failure/unavailable response).
`imported:false` means final migration reconciliation and other families remain
outstanding; a mapped history event does not mean its edits were applied.

Map historical collection memberships with schema 1000039:

```sh
stash-import-catalog-memberships --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

The evidence import must have completed first. File matching is independent:
a post can belong to a collection even when its media is missing. Original
collection keys within the same catalog registry reuse one native group across
all imported catalogs. Directory groups, including legacy `creator` groups,
remain separate from performer ownership; subreddit groups retain their kind.
New groups are disabled, with no inferred account, source URL or root binding.
Unknown definitions and conflicting labels are retained for review. Existing
native edits and historical definition revisions remain intact.

Each transaction handles at most 50 memberships. Retry the same snapshot and
manifest after interruption; the client reads committed progress before
continuing. Exit 0 means mapped, exit 2 means review is required, and exit 1
means a failure or unavailable response. Inspect
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/membership-import/records`
and append `/ORDINAL` for the original values and native references. Native
membership evidence is also readable by collection or post. This pass creates
no albums and continues to report `imported:false` until the remaining families
and final reconciliation are complete.

Map retained NFO documents and historical selections with schema 1000042:

```sh
stash-import-catalog-documents --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

Run the evidence pass first. This command uses the existing Stash application
API key; no website login or cookie is involved. It retains original document
bytes, parser results, path/post associations and explicit selected heads.
Older catalogs without a head at a path keep the original reader's latest
text-timestamp/hash fallback, recorded as `legacy_fallback`. An invalid explicit
head requires review. Existing native selections and manual unlinks are
preserved. No scene/image metadata is applied, and paths are never opened.

The same frozen snapshot and manifest digest resume after interruption. The
checkpoint is the processed-record count across document/source/head phases.
Exit 0 means mapped, exit 2 means review outcomes, and exit 1 means a failure or
unavailable response. Inspect
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/document-import/records`
for bounded summaries, or append `/ORDINAL` for original values and native
references. Completion remains `imported:false` until the entire catalog
migration and reconciliation have finished.

Map retained translation results and provenance with schema 1000043:

```sh
stash-import-catalog-translations --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

Run the evidence pass first. The command uses the Stash application API key.
It shares exact original/output text and language/provider facts across posts
while retaining each catalog's provenance and source timestamp. Unknown languages
remain unknown; they do not become English translations. Original declared input
hashes are verified using the historical catalog JSON encoding. Missing hashes
or originals remain explicit; conflicting hashes require review. Importing does
not change scene/image fields or submit provider work. Translation jobs/cache
state are migrated separately.

Retry the same snapshot and manifest digest after interruption. The client
resumes from the last committed source ordinal. Exit 0 means mapped, exit 2 means
review outcomes, and exit 1 means failure or an unavailable response. Inspect
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/translation-import/records`
for bounded summaries, or append `/ORDINAL` for original values and native
references. Post evidence and shared results also have application read APIs;
see [retained translations](../../docs/native-schema.md#retained-source-translations).
Completion remains `imported:false` pending the remaining catalog migration.

Map historical enrichment receipts with native schema 1000057 after the evidence
pass:

```sh
stash-import-catalog-enrichment --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

The command uses the application API key and resumes the last committed source
ordinal after an interrupted or lost response. Exit 0 means mapped (including a
catalog without this optional family), exit 2 means review outcomes, and exit 1
means failure or an unavailable response. Inspect
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/enrichment-import/records`, or
append `/ORDINAL` for the original values and native receipt reference.

Original completion times and unresolved-child counts stay intact. Importing a
historical receipt does not schedule a website request, invent a native worker
attempt, or assert that all child metadata was retrieved. Queue/cooldown import
and activation remain separate; `imported:false` still means the entire catalog
migration is not complete. See [historical enrichment receipts](../../docs/native-schema.md#historical-enrichment-receipts).

Retain historical metadata cleanup with native schema 1000080 after the evidence
pass:

```sh
stash-import-catalog-cleanup --snapshot /migration/prepared/CATALOG_ID \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

The command uses the application API key and resumes the last committed source
ordinal. Exit 0 means all queue entries were retained as held intent, including
an absent or empty queue; exit 2 means malformed entries need review; exit 1
means failure or an unavailable response. Retry the same frozen snapshot and
digest after an interruption or lost response. Inspect
`/api/v3/archive/catalog-snapshots/SNAPSHOT_UUID/cleanup-import/records`, or
append `/ORDINAL` for the original values and native intent reference.

This old queue records unfinished background metadata cleanup following a catalog
prune. Importing it preserves the original catalog scope and timestamp without
deleting media, removing recreated posts or cancelling native work. The intents
remain held; execution and reviewed disposition require separate native retention
work. `imported:false` still means the whole catalog migration is incomplete.
See [historical metadata cleanup](../../docs/native-schema.md#historical-metadata-cleanup).

## Frozen automation input

Prepare the separate automation database from a consistent SQLite backup:

```sh
stash-prepare-automation --automation /migration/automation.sqlite3 \
  --output /migration/prepared/automation --snapshot SNAPSHOT_UUID \
  --source REGISTRY_SOURCE_UUID --captured-at SNAPSHOT_TIME
stash-prepare-automation --verify /migration/prepared/automation \
  --expected-sha256 MANIFEST_SHA256
```

Use the same registry source UUID as the catalog imports, a stable snapshot UUID,
and the backup's capture time. Preparation requires the complete recognized
automation schema (application `SCPC`, versions 1–3). It rejects active journals,
unknown tables/columns, corrupt inputs and changes to the source file during
export. It opens the frozen database immutably and never runs its stored SQL or
changes its source files. The manifest retains the original database checksum,
schema inventory, per-table hashes and bounded ordered record chunks.

All ten maintenance, translation, enrichment and discovery families are retained.
Values include exact original/cached text, raw JSON strings, nulls, binary values,
SQLite integers and retry timestamps. Malformed legacy JSON and broken foreign-key
references remain evidence for domain review; preparation never repairs them or
reclassifies their jobs. Output files are private and flushed before publication;
an existing destination is never replaced. After an interrupted acknowledgement,
verify the published directory using its original manifest digest.

This command only prepares and verifies input. Its summary reports
`prepared:true`, `imported:false` and every pending family. The production cutover
requires catalogs, automation and other writers to share a coordinated backup
boundary.

Retain the prepared input in native schema 1000046 after importing its registry:

```sh
stash-upload-automation --snapshot /migration/prepared/automation \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

This command uses the existing Stash application API key in `STASH_API_KEY`, or
the variable named by `--api-key-env`. Scoped producer tokens cannot administer
migration input. The registry source UUID must already have an imported registry.
One frozen automation snapshot is bound to that source in the target database;
another snapshot or changed manifest conflicts instead of replacing its evidence.

Retry the same directory and manifest digest after interruption. The client
verifies local files, reads the committed checkpoint and continues at the next
chunk. Each chunk's original records and progress commit together, including
after a lost response. Exit 0 means every chunk was received; exit 1 means an
error or unavailable response, which does not prove that the last write failed.
Inspect the receipt at
`/api/v3/archive/automation-snapshots/SNAPSHOT_UUID`.

`state:received` proves byte/count agreement with the frozen input. It still
reports `imported:false` and all ten families pending, including for an empty
snapshot. No jobs, targets or provider requests are created. Native mapping of
historical outcomes and pending work, followed by reviewed activation, remains
separate work; upload completion is not a completed automation migration.

### Frozen translation history and work

After receiving automation input and importing catalog post evidence, use:

```sh
stash-import-automation-translations --snapshot /migration/prepared/automation \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

This application-authorized command maps `translation_jobs` and
`translation_targets` in schema 1000047. It verifies the frozen files before
network access, resumes the server's exact source ordinal, and processes bounded
transactions. Repeat the same command after a lost response or interruption.
Completed replay only reads its receipts. Exit 0 means these two families mapped;
exit 2 means processing finished with retained review items; exit 1 means an error
or unavailable response. Every result still reports `imported:false`: the other
automation families and activation have separate completion requirements.

Shared requests retain exact original text and the legacy worker's English
translation policy. Valid cached outcomes are shared; conflicting native cache
results remain unchanged and the source rows go to review. Historical English
rewrites are retained in the original receipt, while native `unchanged` results
preserve the exact original text. Job update times are not provider capture times.

Applied targets become historical completions only when their post and cached
outcome are proven. Unapplied targets are held with their original priority and
retry deadline. No worker or provider is activated and no selected scene/image
metadata changes. Old attempts, errors and raw outcomes remain available through
the source record. Known unfinished catalog evidence imports must finish first;
unmatched or forgotten posts stay reviewable. Exact legacy post aliases may share
a target, while preexisting targets and later native edits retain their choices.

Inspect progress and paginated records under
`/api/v3/archive/automation-snapshots/SNAPSHOT_UUID/translation-import`, with
`/records?after=ORDINAL&limit=100` for bounded summaries and `/records/ORDINAL`
for the exact retained source values. These application routes do not accept
producer tokens. The original source snapshot, receipts, held targets and
historical evidence survive ordinary database backup and restore.

### Activate imported translation holds

`stash-activate-automation-translations` prepares and resumes a reviewed bulk
activation through the application API. The translation mapping must have
finished first. Here `--snapshot` is its native snapshot UUID;
`--manifest-sha256` is the original frozen automation manifest digest.

```sh
stash-activate-automation-translations prepare --endpoint STASH_ORIGIN \
  --snapshot SNAPSHOT_UUID --manifest-sha256 AUTOMATION_MANIFEST_SHA256 \
  --output /migration/translation-activation

stash-activate-automation-translations show \
  --plan /migration/translation-activation --expected-sha256 PLAN_SHA256 --page 0

stash-activate-automation-translations apply --endpoint STASH_ORIGIN \
  --plan /migration/translation-activation --expected-sha256 PLAN_SHA256

stash-activate-automation-translations status --endpoint STASH_ORIGIN \
  --plan /migration/translation-activation --expected-sha256 PLAN_SHA256
```

Preparation reads indexed pages of original held receipts. Each saved page
contains at most 100 candidates, their current schedules and exclusion reasons,
and a preview for its eligible targets. The private plan directory retains every
operation UUID and reviewed server hash before any Apply. Its returned
`plan_sha256` is the digest used by the remaining commands. Preparation never
releases work or overwrites an existing plan. At most one million original holds
can enter one plan; separate JSON pages keep individual records bounded.
Show validates the manifest and requested page without reading every other page.

Apply validates all saved pages before its first mutation and checks each page
again when reading it. It verifies existing activation receipts before issuing
new Apply requests, preserving the original operation after a committed response
is lost. Resume with the same directory, endpoint and reviewed manifest digest.
Each page commits atomically; a stale member leaves that batch in `conflict`
while other batches can finish. Apply lists those page/operation references;
Status checks retained activation receipts and reports missing ones as pending.
Later native holds, completed targets and forgotten posts remain excluded from
the original migration selection. Changed native holds require separate
application review.

Activation preserves priorities and retry deadlines and makes those targets
pending. An enabled provider worker may then admit due targets through its
ordinary limits. `activation_complete` reports release of this plan's eligible
targets; `execution_status: not_checked` explicitly leaves provider completion
unverified. This command does not complete the overall catalog migration.

The command uses `STASH_API_KEY` or `--api-key-env`, with application
authorization. Exit codes are 0 for preparation/show or complete activation,
1 for invalid input/transport failure, 2 for conflicting batches requiring
review, and 3 for work not yet activated. Saved plans contain source identifiers
and schedules, never the application key. Redirected, malformed and oversized
responses are rejected. See [activation API contracts](../../docs/native-schema.md#reviewed-translation-activation).

### Frozen enrichment history and work

After receiving the automation snapshot and completing catalog post evidence,
relationship and enrichment-receipt imports, run:

```sh
stash-import-automation-enrichment --snapshot /migration/prepared/automation \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

The command uses the application key in `STASH_API_KEY` (or `--api-key-env`).
It verifies frozen input before contacting Stash, resumes from native progress
and recovers a committed response being lost by reading that progress on rerun.
Pages contain at most 200 source records or 16 MiB. A terminal replay performs
only reads. Exit 0 means this pass mapped its inputs; exit 2 means it retained
review outcomes; exit 1 means invalid input or unavailable/invalid responses.
All outcomes retain `imported:false` for the overall migration.

Pending/retry enrichment becomes held work. Original attempts, per-account or
platform delays, seed cursors and source progress are preserved. No website is
contacted and no job is activated. Historical completion requires a scoped old
receipt; an `already_native` assertion requires a scoped gallery-dl capture.
Missing proof stays in review. Original staged JSON requires conversion/review
and is never thrown away to refetch. Coalesced work requires an explicit catalog
alias. Existing native choices are preserved.

Inspect application API progress and mapped/source records under
`/api/v3/archive/automation-snapshots/SNAPSHOT_UUID/enrichment-import`, including
`/records?after=ORDINAL&limit=100` and `/records/ORDINAL`. The
[schema contract](../../docs/native-schema.md#frozen-enrichment-work) explains
historical completion bases and retained URL evidence. This command does not
replace coordinated cutover inputs, review resolution or reviewed activation.

### Frozen discovery and maintenance history

After the frozen enrichment mapping finishes, retain discovery continuations and
maintenance history from the same received snapshot:

```sh
stash-import-automation-discovery --snapshot /migration/prepared/automation \
  --expected-sha256 MANIFEST_SHA256 --endpoint STASH_ORIGIN
```

This application-authorized command verifies the frozen inputs, resumes from
the committed source ordinal, and uses batches of at most 200 records or 16 MiB.
Repeat the same command after an interruption; a terminal replay only reads.
Exit 0 means all records in this pass mapped, 2 means some require review, and
1 means input or transport failure. Overall migration remains `imported:false`.

Discovery recovers the source post associated with old media. It does not link
accounts to performers. Account listings retain their exact saved cursor,
historical page/attempt counts and retry delays, including imported cooldowns.
Pending targets and candidate URLs remain unconfirmed. Existing lookup work
references the enrichment mapping rather than creating duplicate targets.
Completed assertions require retained receipts or source captures; a status
label alone cannot establish completion. Staged page/detail results and
ambiguous candidates remain available for review without fetching them again.

Maintenance watermarks, seed summaries, source exclusions and pruning summaries
are historical evidence. They do not replace native cursors or policies or
certify that native work has run. Unknown formats keep their original values
with a review reason. No scraping, job activation, performer assignment or
scene/image metadata changes occur in this pass.

Inspect `/api/v3/archive/automation-snapshots/SNAPSHOT_UUID/discovery-import`,
`/records?after=ORDINAL&limit=100`, and `/records/ORDINAL`. Compact summaries link
native accounts, posts and collections where validated; selected-record details
read original values from the retained snapshot rather than another payload
copy. See the [schema contract](../../docs/native-schema.md#frozen-discovery-and-maintenance-history).
Use the separate reviewed activation command below to bind selected held targets.
Importing this history never activates them automatically.

### Reviewed discovery activation

`stash-activate-automation-discovery` prepares a private review file for one
account listing and 1–1,000 explicitly selected original discovery targets.
It uses an application API key from `STASH_API_KEY`, as the importers do.
Producer credentials cannot authorize these bindings.

The input JSON contains `manifest_sha256`, `listing`, and `targets`:

```json
{
  "manifest_sha256": "FROZEN_AUTOMATION_MANIFEST_SHA256",
  "listing": {
    "account_uuid": "NATIVE_ACCOUNT_UUID",
    "collection_uuid": "REVIEWED_COLLECTION_UUID",
    "collection_revision": 2,
    "root_uuid": null,
    "profile_url": "https://www.reddit.com/user/example/submitted/?sort=new",
    "policy_sha256": "REVIEWED_DISCOVERY_WORKER_POLICY_SHA256",
    "extractor_version": "1.32.15-dev",
    "initial_cursor": {"after": "t3_SAVED_CURSOR"},
    "historical_pages": 67,
    "legacy": {"snapshot_uuid": "FROZEN_AUTOMATION_SNAPSHOT_UUID", "account_ordinal": 7},
    "not_before": "2026-10-04T00:00:00Z"
  },
  "targets": [{"source_ordinal": 10, "source_sha256": "ORIGINAL_TARGET_RECORD_SHA256"}]
}
```

Replace the example values with the selected retained records. The account and
target details come from the discovery-import routes above; targets must belong
to that account and collection. Preserve the original profile URL, cursor,
historical count and retry deadline. Use `initial_cursor: null` when no cursor
was retained. Select the reviewed current collection revision and its actual
root, which may be null; `GET /api/v3/archive/collections/{collection}` retrieves
that current definition directly. Imported collections start disabled. Worker policy and
runtime come from the producer's profile inspection command:

```sh
stash-ingest --outbox /persistent/producer.sqlite --endpoint STASH_ORIGIN \
  --producer PRODUCER_UUID discovery-policy --profile PROFILE_PATH
```

Website credentials and the worker's private configuration stay on the producer.
The backend rejects unresolved historical staging and stale source bindings.

```sh
stash-activate-automation-discovery prepare --endpoint STASH_ORIGIN \
  --input /migration/review/discovery-input.json \
  --output /migration/review/discovery-plan.json
stash-activate-automation-discovery show \
  --plan /migration/review/discovery-plan.json --expected-sha256 PLAN_FILE_SHA256
stash-activate-automation-discovery apply --endpoint STASH_ORIGIN \
  --plan /migration/review/discovery-plan.json --expected-sha256 PLAN_FILE_SHA256
stash-activate-automation-discovery status --endpoint STASH_ORIGIN \
  --plan /migration/review/discovery-plan.json --expected-sha256 PLAN_FILE_SHA256
```

`prepare` only previews. It saves the returned post UUIDs/revisions, complete
listing definition, original record hashes and operation ID before any Apply.
The returned `plan_sha256` hashes the saved file; pass that value to subsequent
commands. The server's separate preview hash is retained inside the file.
An optional input `uuid` or `listing.uuid` can pin an existing identity; omitted
ones are generated once during preparation. Additional batches for the same
listing must reuse its saved UUID and exact definition.

`show` works offline. Apply and status recheck the file and endpoint and recover
the original server receipt after a lost response. Reuse the same file and hash;
do not generate another operation to retry. A changed source returns exit 2 for
review, an unapplied status returns 3, and invalid input or transport failure
returns 1. Exit 0 from Apply proves the bindings were retained. It does not prove
scraping, comparison, post identity, historical coverage or catalog import
completion; those have separate native execution and review steps.

For a listing that resumes after unavailable historical batches, prepare a
separate recovery plan with a new listing UUID. Keep the same account, collection,
profile URL and `legacy` reference, set `initial_cursor: null` and
`historical_pages: 0`, and add:

```json
"recovery_of": {"listing_uuid": "ORIGINAL_LISTING_UUID", "sha256": "ORIGINAL_LISTING_SHA256"}
```

The original definition is available through `GET /api/v3/archive/discovery-listings/{listing}`.
Select targets already bound to it, preserve its deadline and the latest attempt's
available time, and review current collection/root and worker policy values.
Use the same prepare/show/apply/status commands and a new saved plan file. Further
target batches reuse the same recovery listing. A running producer must finish
or be recovered before Apply; queued predecessor work is cancelled atomically.
Earlier pages and candidate evidence remain reviewable and can still block a
conflicting match. See [recovery semantics](../../docs/native-ingestion.md#recovering-a-search-with-missing-historical-batches).

### Legacy enrichment checkpoints

After the frozen enrichment mapping finishes, convert saved collector results:

```sh
stash-import-enrichment-checkpoints \
  --snapshot /migration/frozen-automation \
  --expected-sha256 AUTOMATION_MANIFEST_SHA256 \
  --endpoint STASH_ORIGIN
```

The command uses the same application key environment setting as the other
importers. It verifies the frozen files and exact number of staged records,
then resumes native conversion from the last committed source ordinal. Repeat
the same command after a lost response. Exit 0 means conversion finished, 2 means
some formats require review, and 1 means input or transport failure.

Inspect progress and paged summaries at
`/api/v3/archive/automation-snapshots/{snapshot}/enrichment-checkpoints` and
`.../records?after=0&limit=100`. Select `.../records/{ordinal}` to inspect original
values and the converted body. Repeated metadata is shared, source record order
is retained, and pending children keep their saved parent context. The old
collector did not record observation times, so conversion explicitly leaves them
unknown. Unsupported formats retain their original evidence for review.

This preserves staging without fetching sources, creating captures, releasing
review holds or claiming completion of the underlying work. The resulting
`legacy-enrichment-staging-v1` documents still require a reviewed handoff before
native worker execution. `imported` remains false for the overall migration.

### Reviewed enrichment activation

After completing the frozen enrichment mapping, review and enable the relevant
native collection definitions. Imported collections start disabled; enabling
one creates a new revision. The activation preview explicitly connects the old
held work to that reviewed revision, retaining its URL, priority and retry delay.
It does not change the original catalog scope or start a collector itself.

```sh
stash-activate-automation-enrichment prepare --endpoint STASH_ORIGIN \
  --snapshot SNAPSHOT_UUID --manifest-sha256 AUTOMATION_MANIFEST_SHA256 \
  --output /migration/enrichment-activation

stash-activate-automation-enrichment show \
  --plan /migration/enrichment-activation --expected-sha256 PLAN_SHA256 --page 0

stash-activate-automation-enrichment apply --endpoint STASH_ORIGIN \
  --plan /migration/enrichment-activation --expected-sha256 PLAN_SHA256

stash-activate-automation-enrichment status --endpoint STASH_ORIGIN \
  --plan /migration/enrichment-activation --expected-sha256 PLAN_SHA256
```

The returned `plan_sha256` binds all saved pages. Review both original and release
targets, the exact URL, post revision and chosen collection revision. Several
legacy aliases for one target appear once. A destination already used by native
work stays in review. If the collection revision changes, Apply excludes the old
hold with `activation_rebound` and creates the corresponding pending target at
the reviewed revision. Both histories remain available. For unchanged scope it
advances the existing hold. It never modifies a later native hold or exclusion.

Preparation and Show are read-only. Apply validates the complete saved plan,
checks each atomic batch again at the server, and reads existing receipts before
retrying a lost response. Resume with the same plan directory and digest. A
changed post, schedule or collection requires a new preview; already successful
operations keep their original receipts. No application key is saved in a plan.

Exit codes follow translation activation: 0 for preparation/show or complete
activation, 1 for invalid input/transport failure, 2 for batches or discovered
candidates needing review, and 3 for targets awaiting Apply. Disabled/retired
collections, forgotten posts, changed targets and occupied replacements make
`needs_review` true and prevent an empty selection from claiming completion.
`execution_status: not_checked` leaves collector completion unverified.

The client supports up to one million candidates in pages of 100. Saved pages
allow long URLs and escaped JSON without loading the entire library into memory.
It uses `STASH_API_KEY` or `--api-key-env` and requires application authorization.
See [activation API contracts](../../docs/native-schema.md#reviewed-enrichment-activation).

### Reviewing pending work after editing a collection

Pending metadata lookups retain the collection revision under which they were
created. If that definition changes before work starts, use
`stash-review-enrichment-collections` to review transfer to its current revision.
The original target, URL, priority, retry deadline and all history are preserved.
Targets with any previous worker attempt need checkpoint/recovery review instead.

```sh
stash-review-enrichment-collections candidates --endpoint STASH_ORIGIN \
  --collection COLLECTION_UUID --revision CURRENT_REVISION --limit 100

stash-review-enrichment-collections prepare --endpoint STASH_ORIGIN \
  --input /migration/collection-selection.json --output /migration/collection-review.json

stash-review-enrichment-collections show \
  --plan /migration/collection-review.json --expected-sha256 PLAN_SHA256

stash-review-enrichment-collections apply --endpoint STASH_ORIGIN \
  --plan /migration/collection-review.json --expected-sha256 PLAN_SHA256

stash-review-enrichment-collections status --endpoint STASH_ORIGIN \
  --plan /migration/collection-review.json --expected-sha256 PLAN_SHA256
```

The selection JSON contains `collection_uuid`, `collection_revision`, `reason`
and `targets`, an array of 1–100 `{target_uuid, revision}` objects from eligible
candidates. An optional `uuid` fixes the review identity; preparation otherwise
creates one and retains it in the private plan. Each plan handles one explicit
batch. For another candidate page, pass both `--after-collection-revision` and
`--after-target` from the returned `next_cursor`. Keep every prepared plan and
its digest when processing several batches. Preparation and Show are read-only.

Review the collection definition, original source URLs and schedules in the
saved plan. Apply atomically excludes the old pending target and creates its
replacement at the reviewed revision. It starts no collector and does not change
profiles, policies or media metadata. Existing replacements and changed source,
schedule or collection revisions require a fresh review. Prior worker attempts
are never discarded or cancelled by this operation.

Plans are never overwritten and their hashes are checked again before use.
Apply reads the original receipt before submitting; after an interrupted response,
reuse the same plan and digest. `rebound` confirms this transfer only;
`execution_status: not_checked` does not claim the lookup ran. Exit codes are 0
for preparation/show/candidates or a committed transfer, 1 for invalid input or
transport failure, 2 for a conflict, and 3 for a saved transfer awaiting Apply.
Authentication uses `STASH_API_KEY` or `--api-key-env`, never a key inside the plan.
See [collection review API contracts](../../docs/native-schema.md#reviewing-unstarted-enrichment-after-a-collection-change).

## Historical post-to-media links

`stash-backfill-post-media` connects retained post/file appearances to existing
scenes and images even when an old NFO capture has no attachment manifest. It
uses native file-generation, path/content and current ownership checks. It
preserves explicit choices, leaves conflicting matches in review and creates
no attachment order or album. Run it after catalog media evidence has been
imported; source-album matching below remains a separate operation.

```sh
stash-backfill-post-media prepare --endpoint STASH_ORIGIN --output /migration/post-media
stash-backfill-post-media show --plan /migration/post-media --expected-sha256 PLAN_SHA256 --post POST_UUID
stash-backfill-post-media apply --endpoint STASH_ORIGIN --plan /migration/post-media --expected-sha256 PLAN_SHA256
stash-backfill-post-media status --endpoint STASH_ORIGIN --plan /migration/post-media --expected-sha256 PLAN_SHA256
```

Preparation only reads the native API and saves a new private plan in bounded
parts. Use its printed digest after inspecting the proposals. `--posts-file`
can restrict preparation to a JSON array of UUIDs; otherwise the client pages
all retained post-level evidence, up to one million posts. A per-post review
limit remains an explicit item and does not hide other eligible posts. The plan
is bound to its original endpoint and never contains `STASH_API_KEY` or the value
of `--api-key-env`.

Explicit post UUIDs resolve to their current identity during preparation. Several
old IDs for one merged post produce one reviewed entry. An existing plan keeps
its original post/request IDs; a later merge requires a new preview for work
that has not committed, while completed receipts remain recoverable.

Resume Apply with the same plan and digest after a network error or restart.
The client reads durable receipts before submitting missing requests. A later
unlink does not cause an earlier request to recreate the link. Stale previews
require a new plan; completed request identities and history remain intact.
Every part is validated before submission and checked again before use. Changed
plan bytes, unexpected target identities and mismatched receipt counts fail.

Exit codes are 0 for preparation/show or processed work without review, 1 for
input/transport failures, 2 for review items/conflicts, and 3 for unsubmitted
requests. `processed` concerns the saved matching pass; `needs_review` and the
outcome counts retain unresolved candidates. Matching does not apply metadata
policies or enable scrapers. See the [native contract](../../docs/native-ingestion.md#historical-post-to-media-matching).

## Historical source albums

`stash-backfill-source-albums` uses the native application API to match imported
media to source post attachments and construct the corresponding galleries.
It runs after the relevant source evidence, attachment manifests and media
associations have been imported. It requires the existing application API key
in `STASH_API_KEY` (or a named `--api-key-env`), not a scoped scraper token.
No website login or cookie is involved, and plans contain no API key.

First prepare a new private plan directory. This fetches read-only previews and
saves the exact post, matching policy, preview signature and request UUID for
every operation. Choose `source-identifiers-v1` for retained qualified source
media IDs. Use `legacy-reddit-filename-v1` only when explicitly accepting the
historical Reddit `post-id_media-id` filename convention as matching evidence.

```sh
stash-backfill-source-albums prepare --endpoint STASH_ORIGIN \
  --policy legacy-reddit-filename-v1 --all-selected \
  --output /migration/source-albums

stash-backfill-source-albums show --plan /migration/source-albums \
  --expected-sha256 PLAN_SHA256 --post POST_UUID

stash-backfill-source-albums apply --endpoint STASH_ORIGIN \
  --plan /migration/source-albums --expected-sha256 PLAN_SHA256

stash-backfill-source-albums status --endpoint STASH_ORIGIN \
  --plan /migration/source-albums --expected-sha256 PLAN_SHA256
```

`PLAN_SHA256` is the manifest digest printed by `prepare`; retain it with the
reviewed plan. `show` displays one complete saved preview without contacting
Stash. `apply` validates every saved record before making changes. The endpoint
must match the saved origin; redirects and ambient proxies are not used. Existing
plan directories are never overwritten. Instead of `--all-selected`, use repeated
`--post POST_UUID` arguments or `--posts-file FILE` containing a JSON array.
New preparation resolves those explicit UUIDs and includes each current post
once, including when several supplied IDs have merged. Saved plans and retries
keep their original post UUIDs. A merge before gallery publication requires
fresh review; notification retries for published changes keep their original
event and do not repeat the gallery changes.

Discovery includes only posts with selected attachment lists, including disabled
and forgotten posts. Forgotten entries remain in the report without a submission.
Previews requiring review are retained without application. Disabled selections
and single-media posts can complete as no-ops. Bounded UUID pagination is not a
global snapshot while writers run: migration uses a quiesced boundary or an
explicit reviewed post list. Plans allow at most 100,000 posts and 64 MiB per
preview; exceeding a limit fails explicitly rather than dropping records.

After interruption, rerun `apply` or `status` with the same directory and digest.
The client looks up each original request before submitting it, including when a
response was lost. It never refreshes a stale preview or silently replaces a
failed/cancelled job. `publication_committed` means the database changes exist;
`hooks_finished` additionally means the worker finished the applicable plugin
notifications. Gaps without usable media remain visible in publication counts.

Exit 0 means the selected command succeeded; for `apply` and `status`, every
submitted operation in that plan has finished and no review outcome remains.
Exit 2 means review is required (including stale, failed or cancelled work),
exit 3 means queued/running/unsubmitted work remains, and exit 1 means an error
or unavailable response. An error does not prove that a preceding request failed
to commit. Plan completion does not mean all source media was downloaded, all
catalog families were imported, or the full migration is complete.

To cancel, inspect the job revision, then use `cancel` with the plan arguments,
`--post POST_UUID` and `--expected-revision REVISION`. Cancellation cannot undo
already-committed gallery changes or notifications. Explicit retry creates a
separate saved plan before submitting anything:

```sh
stash-backfill-source-albums prepare-retry --endpoint STASH_ORIGIN \
  --plan /migration/source-albums --expected-sha256 PLAN_SHA256 \
  --post POST_UUID --output /migration/source-album-retry

stash-backfill-source-albums apply --endpoint STASH_ORIGIN \
  --plan /migration/source-album-retry --expected-sha256 RETRY_PLAN_SHA256
```

The retry pins its parent job and revision. If that job already published,
retry resumes notification delivery with the original event identity, preserving
later library edits. If unpublished evidence has changed, prepare and review a
fresh plan instead. Old terminal job history remains available.

## Native n8n backfills

`stash-ingest-n8n` replaces the account backfill runner's record/inspect contract.
It supports the existing eight Reddit/Twitter modes and their exact URL order.
New calls require a reviewed matching service profile with global `skip=true`.
Configure `STASH_INGEST_OUTBOX`, `STASH_INGEST_ENDPOINT`, `STASH_INGEST_PRODUCER`
and `STASH_INGEST_TOKEN` in the execution environment. Set
`STASH_INGEST_REDDIT_FULL_HISTORY_PROFILE` and
`STASH_INGEST_TWITTER_FULL_HISTORY_PROFILE`, or pass `--profile` explicitly.

```sh
stash-ingest-n8n --mode reddit-new --identity Example \
  --workflow WORKFLOW_ID --execution EXECUTION_NUMBER --node NODE_UUID --item 0 \
  --profile /persistent/profiles/reddit-full-history.json
stash-ingest-n8n --inspect RESULT_TOKEN
```

The first command records a bounded local snapshot and returns a 32-character
result token. Its UUID derives from the producer, workflow, execution, node,
item, mode and account; retrying the same node cannot create a new caller after
a lost response. `--call UUID` can supply an explicit identity instead. The
first call freezes its profile policy, exact URLs and absolute full-history
cutoff. Replays do not reopen a removed/changed profile or recalculate the window.

Inspection advances that same call; the normal dispatcher also advances queued
backfills. A root-authorized native history lookup must succeed before any
source call is created. Prior completion or deliberate skip produces a retained
result with no source tickets. A `needed` decision and its child source snapshot
commit together. API outages retain the pending check with backoff. Concurrent
inspectors use fenced leases, and no network request holds a SQLite transaction.

Once source work exists, only its original tickets can establish completion.
Later account history cannot turn an unfinished/cancelled call into a success.
After every target's original windows complete, the adapter saves the exact
native proof before sending it. Lost responses replay the same proof and decision
UUID. Native completion and file-intake completion remain separate.

Known-token inspection retains the old `command_failed`, `exit_code`,
`network_blocked`, `stdout_tail` and `stderr_tail` result fields, adding `token`,
`backfill_pending`, the native state, retained decisions and source-call identity.
These fields contain bounded status messages, not downloader logs or settings.
While pending, `exit_code` is 2 and `backfill_pending` is true. Normal command
exit 0 means the JSON result was produced; it does not certify scraping success.
`--strict` exits 2 while pending, 1 for review/failure, and 0 for native completion
or a retained historical acceptance/skip. `backfill_cached` identifies the latter;
`account_backfill_complete` does not claim exhaustive website availability.
Finished results remain available locally after restart, independently of token
rotation or later API outages. `--inspect TOKEN --retry-reviewed` rechecks the
same original work; it does not replace a cancelled ticket with a fresh scrape.
If a source lookup or native job needs review, resolve and retry that underlying
work first using its existing caller/job controls, then recheck this token.

Producer schema 7 stores these calls and receipts in the existing outbox. Its
default limit is 10,000 retained calls; reaching capacity stops admission without
evicting history. Include this outbox in the migration/backup boundary. Stash's
database schema does not change in this adapter increment.

### Retaining old n8n result tokens

`stash-import-n8n-receipts` imports a frozen copy of the old `n8n-receipts`
directory into the native producer outbox. Preflight reads every file, reports
each token/digest/outcome and performs no writes. Apply requires that reviewed
snapshot digest and an explicit outbox binding. Use a stable receipt-source UUID
from the migration manifest, distinct from the run-journal database UUID.

```sh
stash-import-n8n-receipts --receipts /migration/n8n-receipts --source RECEIPT_SOURCE_UUID
stash-import-n8n-receipts --receipts /migration/n8n-receipts --source RECEIPT_SOURCE_UUID \
  --expected-sha256 REVIEWED_INPUT_SHA256 --apply \
  --outbox /persistent/native-outbox.sqlite --endpoint https://stash.example \
  --producer PRODUCER_UUID
```

The entire import commits atomically, including its manifest. Repeat the same
command after an interruption or lost command response. Original JSON bytes,
tokens, input hashes and import provenance are immutable; conflicting bytes,
source identities or native caller tokens roll back the whole import. Unknown
directory entries, nonregular files, changing files and malformed JSON block
import. Valid JSON with unsupported result semantics is retained for review.
The retained limits are 10,000 receipts/64 MiB, plus 1,000 manifests/16 MiB;
capacity failure never evicts earlier results.

`stash-ingest-n8n --inspect OLD_TOKEN` then works locally, even without the old
directory, an API token or network access. It returns `state: legacy_*`,
`backfill_pending: false` and a `legacy_receipt` provenance object. Successful
and deliberately skipped command results retain their old result fields. Failed
results remain failures; a recorded network block sets `command_failed: true`
and a nonzero exit code even if its original child exit code was zero. The exact
original is still retained in `legacy_n8n_receipts.body`. Unsupported results
return a review error, and `--strict` exits 1 for failure/review, 0 otherwise.
Historical receipts cannot be retried as native work. General producer status
lists their counts separately from active work.

Old receipts do not record an account or execution identity. The importer never
guesses those from log text, queues a scrape, or creates native completion proof.
Even an old `account_backfill_complete` flag is only a retained historical claim;
permanent account history uses the separate journal importer below. File intake
remains unverified. Include the outbox and its import manifests in backups.
This preserves token inspection during cutover; saved n8n execution graphs still
need explicit drain/resume handling before their old command paths are removed.

Schema 6 → 7 preserves existing delivery events, source calls, tickets and
backfill results, adding two initially empty receipt/history tables. It does not
read any old receipt directory automatically. Older producer binaries reject
schema 7; restore the pre-migration outbox copy when rehearsing a rollback.

### Staging the existing n8n graphs

`stash-ingest-n8n-config --input workflow-export.json --output staged.json`
accepts a reviewed workflow export array and atomically creates a private file.
It refuses unknown command/connection/result shapes and never overwrites an
existing output. It does not access, publish or activate the n8n database.
Existing workflow IDs, node IDs, credential references, unrelated parameters,
inputs and success/error branches remain. The converted command records its
execution context, and the inspection parser retains the pending flag and token.
Pending results enter a 90-second Wait and inspect the same token again. Other
results continue through the existing error/success handling.

The interval uses n8n's persisted Wait path; waits shorter than 65 seconds keep
the execution in the running process. See the [n8n Wait documentation](https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.wait/).
The `tests/n8n_workflow_runtime.cjs` rehearsal evaluates real staged commands and
result expressions with the installed n8n evaluator, verifies retained IDs and
credential references, and calls the actual Wait implementation with an isolated
checkpoint context. It runs no scraper commands and needs no network.

Before activation, preserve/import the old account decisions, per-scan state and
result receipts, finish or reconcile old executions, register the exact source
collections/root grants, and provision the profiles, outbox and dispatcher.
Compare both current and published graphs at cutover; the staged converter must
not overwrite later workflow edits. Old result-file tokens are a separate input
format, not native outbox tokens. The installed workflows remain on the old
runner until that conversion and deployment boundary is complete.

## Backfill journal import

`stash-import-backfills` migrates permanent account completion/skip decisions
through the native application API. It handles `backfill_completion` and
`legacy_backfill_skip`; per-scan completions and the remaining journal/catalog
families still require the broader migration. This is a maintenance command,
separate from normal worker ingestion and the staged native n8n adapter.

```sh
stash-import-backfills --journal /migration/run-journal-snapshot.sqlite \
  --root ROOT_UUID --source INPUT_DATABASE_UUID
# After reviewing the preflight, target the intended native database instance:
stash-import-backfills --journal /migration/run-journal-snapshot.sqlite \
  --root ROOT_UUID --source INPUT_DATABASE_UUID \
  --endpoint STASH_ORIGIN --apply
```

The first form only validates/counts records and needs no API key or connection.
Use a consistent journal snapshot from the migration backup boundary. The source
database UUID belongs in that migration's manifest and must stay fixed across
retries and relocated copies; the root must already exist in the native database.
The importer opens SQLite read-only, rejects unknown columns/unsupported row
shapes, and validates all records before its first network write. Both passes
use the same SQLite read transaction, so concurrent changes cannot alter a batch
halfway through import.

`--apply` reads the existing Stash **application** API key from `STASH_API_KEY`
(or `--api-key-env NAME`) and sends it only in the `ApiKey` header to the selected
origin. Producer tokens cannot import historical acceptances. Redirects, website
cookies and proxy inheritance are disabled. Batches contain at most 50 records
and 4 MiB; acknowledgements must match the stable decision UUID, component,
outcome and historical basis for every record.

Rerun the same command after interruption or a lost response. Already committed
records replay unchanged; a different payload under an imported primary key is
a conflict, not an overwrite. Errors report confirmed acknowledgement counts
without printing record bodies, historical logs or keys. A lost response can
leave more rows committed than that count indicates, which replay resolves.

The native database retains the original `result_json` string and all provenance.
An old user acceptance remains an acceptance even when it states that exhaustive
history was unverified. Imported skips remain distinguishable from completion;
neither kind creates native source-run coverage or successful media receipts.

## Instagram downloads and source albums

The pinned adapter adds `instagram_media` version 1 before gallery-dl filters
unavailable media or reverses its output order. It contains the enclosing post ID,
an explicit album flag and ordered media IDs/kinds; null entries retain missing
source slots. Per-file `media_id` must match that evidence. Carousels can create a
source gallery as files arrive; one ordinary image/reel does not become an album.

Stories and highlights are containers of individual posts. The adapter preserves
the container ID/type as evidence but keys each post by its original media ID and
filters on that item's publication time. Finding the same story in a highlight
therefore does not create another post. A profile's posts, photos, reels, tagged,
stories and highlights children each apply the original run window. Container
timestamps, download order and output numbering cannot supply post identity/order.

Extra audio, previews and highlight covers must be disabled for native download
intake. Music stickers still retain their metadata while their disabled audio
outputs are ignored. The existing static-video choice is preserved. New captures
share post fields and keep per-file dates, sizes, URLs and attribution in patches;
older captures without the versioned evidence retain their original partition
for migration, proof verification and replay.

The installed `stash-ingest-instagram` command records work for a separate native
dispatcher. An exit code of zero does not confirm that a timer has finished
scraping. The host timer, durable outbox, dispatcher/recovery schedule and API
registration must be converted together at reviewed cutover.

## Kemono/Coomer downloads and source albums

The pinned user, post and posts-listing extractors require `original=true`.
The adapter records `mirror_media` version 1 before configured file selection,
download ordering or the extractor's mutation of file dictionaries. Its qualified
post identity includes the mirror, underlying service, account ID and post ID;
mirror IDs do not establish an association with a native OnlyFans/Patreon account.

Membership preserves original attachment-array order, including unavailable slots
and repeated positions. A primary file precedes that list unless it aliases an
existing attachment. Inline source files follow in text order with existing paths
deduplicated. Selecting only attachments or changing download order cannot shrink
or reorder this source evidence. A singleton primary-file alias does not create an
album; multiple evidenced source slots can create a gallery as visual files arrive.

The backend revalidates the manifest against the retained original file,
attachment and inline-content fields. Full source paths identify attachments;
their hash-like filenames are not verified local content checksums. New captures
share the post body while retaining selected-file paths, URLs, types and exclusions
in per-capture patches. Unmarked historical payloads keep their original partition.

Audio, archives and other unsupported file extensions retain source captures with
`native_file_exclusion: "unsupported_image_or_video_extension"` and an excluded
download report. They produce no downloaded bytes, file receipt or download-archive acknowledgement. Supported GIF conversion
still uses the reviewed host processor. Scan windows use the original `published`
timestamp, preserving fractional precision; missing publication time fails rather
than substituting the mirror's import timestamp.

`stash-ingest-coomer` and `stash-ingest-kemono` record durable source requests for
the separate dispatcher. Their reviewed profiles retain existing filenames,
archive/skip rules and helper bindings. Native root/producer registration, outbox,
dispatcher/recovery and actual host/n8n caller activation remain deployment work.
Discord, favorites and artist-dispatch containers are explicitly unsupported by
this adapter and require their own identity/traversal contracts.

## Bluesky/TikTok downloads and source albums

`bluesky_media` and `tiktok_media` version 1 retain the qualified post reference
and original ordered media entries before selection. Producer and backend derive
and validate membership against the same original source fields. Null entries
retain unavailable positions, and repeated source positions remain distinct.
One source item is attributable without becoming a gallery; multiple evidenced
slots can create an album before every image/video is available.

Bluesky posts use author DID plus record key, while attachments use original blob
CIDs. Image alt text remains per-file metadata. Embedded mixed media and quoted
posts retain their own membership; a quoted post has its own identity and source
date. Disabling videos does not remove them from the original attachment list.
Windows use full `createdAt` precision instead of the extractor's truncated date.

TikTok photo keys derive from the original CDN filename before rendition suffixes;
changing a URL signature or CDN host does not create another attachment. Video
keys are scoped to the source post. Unavailable photos are omitted from download
iteration while their original list and source numbering remain in captures.
Photo numbers and the chosen image cannot replace the post ID. Windows use the
original `createTime`. Extractor errors remain failed work, including errors the
pinned upstream loop catches internally.

TikTok requires `audio`, `covers` and `subtitles` disabled for native media intake.
The original music, cover and subtitle references remain source metadata. Native
profile dispatch defaults to posts; explicit includes may select posts, reposts,
stories, likes and saved posts. An explicit unsupported include fails before
source access. Bluesky supports media/posts/replies/video/likes profile routes and
post-producing feed/list/search/hashtag/bookmark targets. TikTok shortlinks route
only to the supported post extractor. Child routes enforce the original window.
Standalone avatar/banner/info targets and following traversal require separate
account/artwork adapters, and are not silently treated as posts.

Only versioned new captures opt into shared post-body partitioning. Per-file
descriptions, dimensions, selected images and generated titles remain patches;
historical capture partitions stay replayable. The packaged
`stash-ingest-bluesky --url ...` and `stash-ingest-tiktok --url ...` launchers
record durable work for the dispatcher. Native registration, reviewed effective
profiles and observed file receipts are still required before replacing callers.

## Metadata-only extraction for enrichment

`stash_ingest.metadata_fetch.fetch(url, settings, resume=None, timeout=180,
check=...)` runs one metadata lookup in an isolated instance of the pinned
gallery-dl runtime. This is the extraction component for the native enrichment
worker. Native target/job binding, producer-owned checkpoint storage, verified
capture publication, scoped HTTP routes and a transport/lease client are
implemented. The selected-job executor below persists returned metadata before
delivery. Scoped producer dispatch and shared source scheduling are implemented
below; conversion of the existing scheduled service is still pending. Calling this helper alone
does not create a capture, complete a native job or import media.

The initial root extractors are Reddit submissions, Twitter tweets, Bluesky and
TikTok posts, Instagram posts/reels, Kemono/Coomer posts, Patreon posts and Fansly
posts. Kemono/Coomer retain their mirror category and service-specific metadata;
a mirror's `onlyfans` label does not identify a native OnlyFans account. Admission
checks the extractor class, since gallery-dl replaces the Kemono/Coomer instance
subcategory with the service name for both posts and whole creator feeds. Profile
URLs and unsupported root extractors return explicit failures. Linked Redgifs
images and Imgur images/albums/galleries can be resolved to two child levels.
Other links remain unresolved references without expanding into another feed.

The Python producer and Go server share post-identity fixtures for these root
services. Bluesky uses a DID plus record key; Kemono/Coomer use mirror, service,
account and post ID together. Instagram regular posts use the enclosing post ID,
never a selected carousel image ID. Story downloads use each original story's
media ID, retaining the story/highlight container separately. Linked media hosts inherit their
enclosing post; a social account/feed parent does not replace its post. These
adapters allow verified metadata capture/publication, while file selection and
source-window adapters for services beyond Reddit, Twitter, Instagram, Coomer,
Kemono, Bluesky and TikTok remain unfinished.

Metadata projection preserves source post captions and dates rather than image
alt text or per-file dates. Kemono/Coomer only use `published` for publication
time because their transformed `date` may instead be the mirror's import time.
Offline fixtures run the pinned extractors' real transformations as well as the
shared Go/Python contract, including multi-image posts and exact large IDs.

Only source access and pacing settings survive configuration projection. There
are no download jobs, file paths, archive updates, user filters, custom actions
or postprocessors. Cookies can be read but are not updated, the extractor cache
is in memory, and originals are selected for Kemono/Coomer metadata. The child
process disables downloader construction and stops at the first HTTP 429. The
caller-supplied `check` callback can cancel a running lookup; the parent kills
the process group on cancellation, timeout or oversized output. Website access
values enter through a private pipe and are excluded from results and logs.

Successful extraction returns a `stash-metadata-fetch-v1` checkpoint containing
the exact requested URL, runtime/retention versions, compact records, pending
children and unresolved references. Each record retains its original observation
time. Records share unchanged post fields through backward base references;
patches and explicit removals reconstruct each retained metadata object. Child
records refer to their parent context, preserving Reddit attribution without
copying the whole parent onto every attachment. These references only compact
the producer transcript; they do not create native post/performer associations.

`metadata_bundle.Bundle(url, extractor_version, checkpoint).metadata(index,
with_parent=True)` reconstructs a record for subsequent identity validation.
The same source retention policy used by the download adapter reduces Reddit
previews and incidental profile fields before checkpoint serialization. Input
metadata stays unchanged. Checkpoints are bounded to 32 MiB, 1,024 records and
256 entries in each child-reference list. Each reconstructed record, including
parent context, must fit the 4 MiB source payload limit, and all reconstructed
records together must fit 128 MiB. These limits also account for native JSON's
Unicode separator escaping. Invalid references, duplicate records, invalid
observation times, policy/request changes, oversized results and unretained data
are rejected before a record is appended.

The server's `archive.ParseEnrichmentTranscript` independently validates the
same compact representation and reconstructs records without losing large
identifiers or original observation times. Checkpoint and subprocess decoding
retain original number tokens, including decimal precision, exponent spelling
and negative zero, so resuming cannot silently rewrite earlier record hashes.
`Extends` checks that a resumed
checkpoint retains all earlier records and unresolved references. A pending
child must remain explicit or have newly returned records for that URL and
parent; retry cannot silently erase it. A shared Go/Python fixture verifies
these semantics. The native coordinator separately checks immutable job bindings,
producer-owned attempts and current source eligibility. Its checkpoint store
keeps one current body plus small acknowledgements and original per-record
producer provenance across resumed attempts. Publication separately consumes the
saved revision/digest, verifies every source identity against the existing target
post and commits native captures, target completion and job outcome together.
Only supported post adapters can publish; retained unresolved references remain
explicit limitations. Equal observations within a job can share captures without
losing their record associations. Successful publication now releases verified
staging atomically while preserving native captures, original acknowledgements,
producer attribution and unresolved references. A release receipt distinguishes
completed cleanup from a job without a checkpoint. Older publications retain
staging through migration until verified cleanup. The scoped producer API accepts
these checkpoints through a separate enrichment contract. Shared native source
reservations and bounded download preference now coordinate the worker with
downloads. Dispatch across local profiles is described below; legacy scheduling
import and service conversion remain required before activation.

Temporary child failures retain the parent and discard that child's partial
records. Persist the entire returned checkpoint before retrying its pending
children; the resumed fetch skips the parent request and retains its observation
times. Missing children remain explicit source limitations. A root error or
empty result cannot appear successful. The caller must distinguish a retained
checkpoint with pending children from a finished lookup, and must still verify
the result against the intended existing post before native publication. The
helper neither applies field mappings nor mutates selected scene/image metadata.

### Enrichment transport and ownership

`enrichment_client.EnrichmentClient(Client(...))` exposes capability checks,
permitted collection discovery, unadmitted target discovery, revision-pinned admission,
job/target inspection, claim/renew, source reservations, checkpoint save/read, verified publication
and controlled failure acknowledgements. See the
[HTTP contract](../../docs/native-ingestion.md#producer-enrichment-api).
Checkpoint bodies remain JSON objects. Requests bind the owner/fence; server
credentials supply the producer identity. Checkpoint reads are bounded to
32 MiB plus 4 KiB and verify the body digest, counts, URL and extractor version.
An unchanged checkpoint may replay an older receipt without changing provenance.

`job_lease.JobLease.claim(client, job, owner=..., seconds=180)`
returns an owned lease or `None` when the selected job is currently unavailable.
`start()` starts heartbeats; pass `check` to the isolated extractor and call
`close()` when the attempt ends. Deadlines use the server date and monotonic
request start, with a safety margin. Failed renewal stops further source work.
Retain an owner UUID across a lost claim response to recover the same attempt.

Pass `reserve_source=lease.reserve_source` to `metadata_fetch.fetch`. The executor
does this automatically and requires native `enrichment_source_pacing_protocol: 1`.
Its bounded subprocess protocol requests a native reservation before initializing
each root or linked extractor. A busy child becomes a pending `source_busy`
reference with the parent transcript intact; no child request is made. Resuming
fetches only pending children, whose services are reserved by the new native claim.
Reservations end with their fenced attempt, including cancellation or expiry.
Website access values stay in the worker and never enter scheduling records.

Only publication certifies success. Failure calls accept controlled codes;
temporary errors receive server backoff and the eighth attempt becomes terminal.
Exact failure replay cannot affect a newer attempt. Checkpoint/publication
acknowledgements remain recoverable after staging release. Transport tests lose
committed responses, exercise large Unicode checkpoints, and preserve original
number tokens against the real native HTTP server and SQLite.

Use the executor below to persist fresh extraction results before network
delivery. Ready-target discovery lists unadmitted targets; the separate ready-job
route finds admitted retries. The native server now maintains expired/stale
enrichment jobs independently of other workers. Shared service cooldowns and
download/enrichment exclusion and typed download service failures are implemented.
Legacy queue mapping and production host/n8n launchers remain transition work.

### Durable selected-job execution

The current client requires native `enrichment_protocol: 2`. After application
review of a legacy checkpoint, `EnrichmentClient.admit_handoff(handoff_uuid,
plan_sha256, policy_sha256, extractor_version)` consumes that exact review and
returns its native job. Exact retry returns the same job after a lost response.
The application review/seed API is documented in
[native schema](../../docs/native-schema.md#reviewing-an-exact-checkpoint-handoff).

The executor automatically resumes the job's verified retained seed when no
checkpoint exists. Its prefix keeps original capture UUIDs and null historical
observation times. It fetches only pending children and gives fresh observations
their own times and parent bindings. Saved outbox deliveries take priority over
seed reads or extraction after restart. Admission does not write a fake initial
checkpoint. The same execution commands below handle ordinary and handoff jobs.

`enrichment_worker.execute(outbox, client, profile, job_uuid)` executes or recovers
one admitted native enrichment job. `profile=None` only recovers persisted
delivery; it never claims an attempt or contacts a source website. The CLI accepts
the usual `--outbox`, `--endpoint`, `--producer` and `--token-env` arguments:

| Command | Effect |
| --- | --- |
| `enrichment-policy --profile PATH` | Validate a metadata-only profile and print its portable policy hash and pinned extractor version |
| `execute-enrichment JOB_UUID --profile PATH` | Recover saved deliveries, then claim/fetch only if the native job and reviewed profile still match |
| `deliver-enrichment JOB_UUID` | Recover saved checkpoint/publication/failure intents without loading website credentials or a profile |
| `enrichment-status [--job JOB_UUID]` | Inspect local phases, pending operation, retained bytes and publication proof |

Execution/delivery exit 0 only for a confirmed completed job, 2 for pending,
retry, capacity, review or failed outcomes, and 1 for invalid input or an
exception. A completed metadata lookup does not mean media has downloaded.
Native source evidence and target completion remain separate from selected
scene/image metadata.

Metadata profiles use `stash-gallery-enrichment-v1` and require no media root,
destination lock directory or download archive:

```json
{
  "schema": "stash-gallery-enrichment-v1",
  "source_category": "reddit",
  "gallery": {
    "extractor": {
      "sleep-request": 5,
      "reddit": {"cookies": "${stash:reddit_cookies}"}
    }
  },
  "bindings": {
    "reddit_cookies": {"kind": "private", "env": "REDDIT_COOKIES_FILE"}
  }
}
```

Private references and reviewed local assets follow the download profile
contract. Access values stay local and do not enter the policy digest, native
job or execution journal. The digest binds the projected access/pacing template,
source category, reviewed assets and pinned runtime. Changes to ignored filename,
archive or postprocessor settings do not change metadata policy. Website
credential rotation also preserves that identity. Feed/profile URLs are rejected
before a claim; the category must identify the job's supported post extractor.

Producer schema 8 stores stable claim requests, lease acknowledgements, exact
returned checkpoint bytes and pending delivery intents in the existing outbox.
A nonblocking process lock allows one enrichment executor per outbox; ordinary
download event delivery uses independent short database transactions. A process
crash releases the lock without discarding the journal. Before fetching, the
executor reserves the full 32 MiB checkpoint allowance against the shared outbox
byte limit. Other events cannot consume that reservation. Failure to reserve
records a retryable native attempt without contacting the website.

Returned checkpoints are committed locally before checking for a late ownership
failure. A matching native acknowledgement releases those bytes and atomically
records the next publication or child-failure intent. Lost replies therefore
replay the original requests after restart. Partial child lookups receive native
backoff and resume the accepted head without refetching the original parent.
If an expired attempt's undelivered body conflicts with a successor checkpoint,
it remains in local review with its original bytes; it is never rewritten to fit
the successor. Rejected evidence also stays inspectable. Review resolution is
still transition work.

The journal permits at most 10,000 active/review jobs by default. Registration
prunes finished convenience records beyond the newest 1,000; native publication
receipts remain authoritative. Those limits bound local execution history, not
total SQLite file size. Include the outbox in backups: an unacknowledged body
can exist only there. Schema 7 → 8 preserves all prior tables and receipt bytes;
older producer binaries refuse schema 8. The native Stash schema is unchanged.

The selected-job executor requires a known job UUID. The dispatcher below adds
discovery and admission. Shared native pacing is described above; replacement of
the scheduled enrichment service remains transition work.

### Enrichment dispatch and native maintenance

```sh
stash-ingest --outbox /persistent/producer.sqlite --endpoint STASH_ORIGIN \
  --producer PRODUCER_UUID dispatch-enrichment --collection COLLECTION_UUID \
  --profile /private/reddit-metadata.json
```

Each invocation makes one bounded dispatch step for the explicitly selected
collection. It first replays pending local delivery, including evidence produced
with an older profile, then resumes compatible local attempts. It discovers
eligible already-admitted jobs before admitting a fresh target. A lost admission
reply therefore remains recoverable after restart even if no local job record
was created. Admission and claim still validate the exact target/source revisions
and producer grants. No producer is allowed to release a held target.

Omit `--profile` to process only saved delivery for that collection, without
loading website credentials or claiming a new attempt. If expired delivery needs
new ownership, the result is `ownership_required`. With a matching profile,
dispatch can reclaim the job and deliver its original bytes without refetching.
A different profile produces `profile_required`, preserving the local body.
Rejected or divergent evidence remains in review.

Producer schema 9 stores separate delivery, local-job, native-job and target
cursors, plus discovery failure/idle delays. Pages contain at most 20 candidates;
target traversal retains priority, deadline and UUID order, including native
nanosecond precision. Unsupported URLs do not stall every subsequent page.
Selection is persisted before network work, and competing cursor updates cannot
execute a stale selection. Native ownership remains authoritative. Cursors wrap
to revisit temporarily unavailable jobs; they never certify completion.

Native/API failures back off from five seconds, honoring a longer `Retry-After`
for failed discovery/admission. An idle pass waits 30 seconds before rediscovery.
The local queue retains unacknowledged bodies throughout backoff. Exit 0 means
one job completed or no eligible work was found in that pass; `idle` is not a
completed backfill or evidence that the whole collection has been enriched.
Waiting, backoff, review, unavailable and profile/ownership requirements exit 2.
Native publication receipts continue to prove individual job completion.

The application runs trusted enrichment maintenance every 30 seconds while its
HTTP server is active. It uses the partial active-job index, bounded to the 64
permitted active enrichment jobs. It cancels work whose source/target changed
and recovers expired ownership with native retry delay/attempt limits. It leaves
checkpoints, publication receipts and source evidence intact, never performs
website requests, and stops with the server before database shutdown. Producer
discovery itself is read-only and does not recover jobs or confer a lease.

Schema 8 → 9 preserves existing events, requests, receipts and staged enrichment
bytes, adding only discovery state. A table-name collision rolls back promotion.
Older producer binaries refuse schema 9. Include the outbox in backups. Blocked
ready-job claims cause a later retry pass, rather than repeated fresh admissions.
Actual blocked claims register 90 seconds of interest; polling refreshes it.
After four download starts or two minutes of live waiting, the oldest eligible
enrichment requester gets the next turn after current downloads drain. Abandoned
requests expire, and a metadata start restores download preference. Held/stale
targets and cooling dependencies cannot reserve unrelated services. This server
policy coordinates collections even when different workers submit claims.
Legacy enrichment queue mapping, review resolution and production launcher
conversion still precede service activation.

### Dispatch across local profiles

`dispatch-all` rotates across reviewed download, enrichment, account-listing and
candidate-detail profiles without
requiring callers to enumerate every collection. Keep a stable UUID for the local
worker list and stable entry IDs across restarts:

```json
{
  "schema": "stash-gallery-dispatch-v1",
  "uuid": "3425f421-119e-41dd-86cb-b2f71337afca",
  "profiles": [
    {"id": "reddit-download", "operation": "download", "profile": "reddit-download.json"},
    {"id": "reddit-metadata", "operation": "post.enrich", "profile": "reddit-metadata.json"},
    {"id": "twitter-metadata", "operation": "post.enrich", "profile": "twitter-metadata.json"},
    {"id": "reddit-discovery", "operation": "account.list_page", "profile": "reddit-discovery.json"},
    {"id": "reddit-details", "operation": "post.verify_candidate", "profile": "reddit-details.json"}
  ]
}
```

```sh
stash-ingest --outbox /persistent/producer.sqlite --endpoint STASH_ORIGIN \
  --producer PRODUCER_UUID dispatch-all --profiles /private/worker-profiles.json
```

The list accepts 1–32 distinct entries. Relative profile paths resolve beside
the list. Each entry uses its operation's download, enrichment, account-listing or detail profile
contract; website access stays local, and the list does not confer native source
permissions or enable unsupported extractors.

One invocation delivers an ordinary event batch, advances a backfill call,
resolves a caller page and submits one source request. It then tries one saved
enrichment, discovery and detail delivery before loading any website
profile, and rotates through the list until one profile performs work or all
profiles have been checked. Saved
metadata can therefore finish delivery even if its original website profile was
removed or its access binding is unavailable. Native historical scope and
ownership checks still apply; conflicting evidence remains retained for review.

Download entries use existing scoped run discovery. Enrichment entries require
`enrichment_collections_protocol: 1`: Stash lists permitted active collections
with eligible unadmitted targets or due jobs for that runtime/policy. Current
collection/root grants apply before pagination. A root grant includes subsequent
collections at that root; an unbound collection grant stays specific to that
collection. The worker follows pages of at most 20 collections and uses the
existing collection dispatcher for admission and execution. Unsupported URLs
are skipped during target traversal; the server still validates every claim.

Account-listing entries use `stash-gallery-discovery-v1` profiles and require
`discovery_collections_protocol: 1`. Their collection lookup returns permitted
active containers with existing listing definitions; each container's dispatcher
then checks policy/runtime readiness. It can return idle when all its definitions
are stale, complete or ineligible for that profile. Later registrations under a
root grant are discovered without changing this worker list. Moved collections
no longer belong to the old root grant. This does not create listing definitions
or activate imported accounts.

Producer schema 15 stores profile rotation, independent saved-delivery cursors
for discovery, enrichment and candidate details, and each operation's per-policy collection
cursors/backoff. Selection commits before execution, so a restart or a
continually busy first profile/collection does not reset traversal.
Revision checks reject competing stale selections. Idle or blocked entries give
peers a turn; an unreadable profile reports `worker_profile_unavailable` without
printing its path, exception or access values. Metadata discovery honors native
retry delays and retains its own failure/idle backoff. Native service fairness
and leases remain authoritative across different workers.

Exit 0 means a successful or idle dispatch cycle with no reported local pending
or review work. Exit 2 means work, backoff or review remains, or an inspected
profile was unavailable. Neither `idle` nor exit 0 certifies that the library or
a backfill is complete: callers must check their native receipts and source
window completion. The dispatcher performs a bounded cycle; host/n8n scheduling
and legacy queue/cooldown/history conversion remain activation work.

Schema 12 → 13 adds discovery collection cursors and splits the worker's delivery
cursor, retaining its original value for enrichment. Existing outbox rows,
staged metadata/page bodies, claims and receipt bytes remain unchanged in one
transaction. Unknown table/column collisions roll back promotion, including
any preceding column rename. Schema 13 → 14 adds the detail journal and its
dispatch tables/cursor while preserving every existing value. Schema 14 → 15
adds the candidate-inspection cursor without replacing pending work or resetting
backoff. Older producer binaries refuse schema 15;
include the outbox in backups and retain its pending evidence during rollback.
These outbox cursor tables do not require a separate native database migration.

### Account listing page collector

`stash_ingest.discovery_fetch` provides the isolated metadata collector for the
native account-discovery worker. Its `fetch` helper reads one Reddit submitted
listing page or Twitter timeline page using the pinned gallery-dl runtime. It
accepts the original Reddit `after` or Twitter `cursor` continuation, acquires
the service reservation before extractor initialization, and stops at the page
boundary. It constructs no download job, archive or postprocessor and never
follows queued media/child URLs. Website settings stay in the private child
process input. The in-process `collect` helper is only for that child and tests.

The `stash-discovery-page-v1` result binds the exact profile URL, input cursor,
next cursor, extractor/retention versions and compact metadata records. Empty
final pages are explicit; an unchanged cursor is a pagination error. Shared
fields, original observation times, source number precision and reduced preview/
profile data use the existing metadata bundle codec. Pages retain the historical
4,096-record limit, within 32 MiB serialized and 128 MiB reconstructed bounds;
ordinary single-post enrichment keeps its 1,024-record limit.

The native backend validates this same page contract with
`archive.ParseDiscoveryPage`; both implementations exercise a shared corpus for
profile URLs, source numbers, compact metadata, cursor advancement and malformed
pages. Fresh discovery records cannot claim retained capture provenance. Parsing
is separate from binding the page to an authorized job or accepting a post match.

This collector does not submit a job, advance a stored cursor, accept a source
post match or certify an account scan as completed. Native discovery storage and
the [scoped worker HTTP protocol](../../docs/native-ingestion.md#account-listing-discovery)
now retain one page per owned job, releasing the service before the next page.
The protocol advertises `discovery_protocol: 1`,
`discovery_source_pacing_protocol: 1` and `discovery_dispatch_protocol: 1`.
It accepts existing definition UUIDs and their pinned hashes, not replacement
source definitions or worker-selected initial cursors.

`discovery_readiness_protocol: 1` adds bounded discovery for a selected collection
and exact policy/runtime. Negotiate with `client.capabilities(readiness=True)`.
`ready_jobs(...)` returns due admitted jobs by increasing sequence;
`ready_listings(...)` returns definitions eligible for a new page, plus an
inspection cursor and `has_more`. Follow that cursor even when the candidate
array is empty. Filtering still inspects at most the requested number of
definitions, so mismatched policies or future deadlines cannot force an
unbounded historical scan. Listing summaries never repeat the retained bodies.
Readiness is read-only; admission and claim validate eligibility again.

The native server maintains stale/expired ownership every 30 seconds without a
producer connection. It cancels changed sources and recovers expired claims
with existing backoff and attempt limits. Account merges and disabled roots
cannot redirect a previously authorized listing. Prior pages and original
receipts survive; failed/cancelled jobs still require explicit reviewed retry.

`discovery_client.DiscoveryClient(Client(...))` implements that protocol. It
validates each listing's canonical definition hash, requested cursor, page job
and completion receipt. `append_page(description, lease.job, page)` checks the
page against its original request before sending its compact JSON body, then
verifies the receipt's exact body hash, ordinal, producer and attempt fence.
Admission, page and controlled-failure acknowledgements can be replayed after a
lost response without creating another attempt or skipping the next cursor.

Use `JobLease.claim(client, description, owner=..., seconds=180)` with the full
discovery description; its immutable listing supplies the pinned policy/runtime.
The returned lease shares enrichment's server-clock deadline, heartbeat and
source-reservation handling. Keep the owner UUID across a lost claim response,
start the heartbeat, pass its checks/reservations to extraction and close it when
the attempt ends. Lease renewal alone does not persist a fetched page.

Producer schema 11 adds `discovery_journal.DiscoveryJournal` for durable delivery
of a selected page job. It records the immutable listing/cursor binding and
stable claim owner before network access, then retains exact compact page bytes
with their original attempt. A verified acknowledgement and local body removal
commit together. Nonfinal receipts finish only that page's delivery. Retryable
failure acknowledgements clear the ended claim while preserving its outcome.

Reserve the full 32 MiB page allowance before extraction; after saving a page,
only its actual bytes remain reserved. Discovery, enrichment and ordinary event
admission share the same outbox byte limit under SQLite transactions. Pending or
review evidence is never evicted to make space. The discovery process lock is
independent of enrichment and ordinary delivery; a process crash releases that
lock while preserving saved work. A current owned attempt can rebind the same
producer's saved bytes after the original attempt is definitively rejected.
An observed terminal native job can settle an unfetched local claim, but cannot
discard an unacknowledged page or failure intent.

Schema 10 → 11 adds only the discovery journal and its guards/index. Promotion
preserves prior outbox tables, staged enrichment bytes and receipt history in
one transaction. Unknown table collisions roll back. Older producer code
refuses schema 11; include the outbox and its pending page bodies in coordinated
backups. This does not change the native Stash schema or activate any worker.

### Selected discovery page execution

`discovery_worker.execute(outbox, client, profile, job_uuid)` executes or recovers
one already-admitted page job. A discovery profile uses
`schema: "stash-gallery-discovery-v1"`, `source_category` set to `reddit` or
`twitter`, and the same `gallery` and private `bindings` structure as metadata
enrichment profiles. Its policy hash explicitly binds `account.list_page` and
cannot substitute for a `post.enrich` policy. Existing enrichment hashes remain
unchanged. Website access values stay local; delivery of a saved page requires
only the native producer API token.

The CLI accepts the usual `--outbox`, `--endpoint`, `--producer` and `--token-env`
arguments:

| Command | Effect |
| --- | --- |
| `discovery-policy --profile PATH` | Validate an account-listing profile and print its portable policy hash and pinned runtime |
| `execute-discovery JOB_UUID --profile PATH` | Deliver saved work first, then claim/fetch one page if its original listing and reviewed profile still match |
| `deliver-discovery JOB_UUID` | Replay saved page/failure intents without loading a website profile or claiming another attempt |
| `discovery-status [--job JOB_UUID]` | Inspect local delivery phases, retained bytes, pending intent and page receipt |

Execution reserves local capacity before source access, passes lease and source
reservation checks into the isolated collector, and persists its result before
checking potentially expired ownership. A lost claim reuses its recorded owner.
Delivery after an expired attempt first tries the original receipt; new ownership
may reuse the exact same page bytes for this producer and job. A conflicting
later completion leaves unacknowledged data in review. Capacity exhaustion ends
the claimed attempt with a controlled failure before fetching source data.

Execution/delivery exit 0 only for `page_delivered`, 2 for waiting, retry, capacity,
review or failed outcomes, and 1 for invalid input or an exception. The receipt's
`complete` flag distinguishes a final page from a nonfinal one. Neither a delivered
page nor completed enumeration establishes a candidate match or completed catalog
import. `status` now includes the discovery journal's retained work.

Reviewed activation, candidate matching and verified native page release remain
required. These commands do not
create listing definitions or activate imported accounts. Existing production
workers and host/n8n launchers have not switched to native discovery.

### Discovery dispatch across existing listings

```sh
stash-ingest --outbox /state/outbox.sqlite --endpoint https://stash.example \
  --producer PRODUCER_UUID dispatch-discovery --collection COLLECTION_UUID \
  --profile /worker/reddit-discovery.json
```

Each invocation first replays saved page/failure delivery for that collection,
including evidence fetched with an older profile. It then resumes local owned
work, discovers due admitted jobs, and finally admits one eligible existing
listing by its pinned definition hash. Original page receipts remain the only
acknowledgement of delivered observations. A matching profile can reclaim the
same producer's expired pending page; a different profile leaves it waiting for
the original one. Without `--profile`, this command only attempts saved delivery
and never claims a lease or contacts a source website.

Schema 12 persists independent delivery, local recovery, job and listing cursors
per collection/policy, plus revision-checked changes and retry/idle delays. The
dispatcher inspects at most 20 candidates per traversal, follows empty filtered
pages and wraps admitted-job traversal before considering fresh admission.
Contention on an existing claim must not fill the queue with additional jobs.
Lost admission responses are recovered through the ready-job index. Saved
selection and backoff survive process restart; failed polls use 5–320 second
exponential delays, honoring longer server delays up to one day. Idle traversal
waits 30 seconds. No loop inside the command continuously polls a website.

Exit 0 means `page_delivered` or `idle`; neither proves all listings completed,
candidate matches were accepted, or catalog import finished. Inspect the page
receipt's `complete` flag and `discovery-status` separately. Other pending/review/
retry outcomes return 2, and invalid command input returns 1. Use `dispatch-all`
for rotation across local profiles and permitted collections. Production host/n8n
activation remains separate transition work.

Promotion from schema 11 adds only the dispatch table; exact pending pages,
claims, receipts and every previous outbox table are preserved transactionally.
Unknown name collisions roll back without replacing evidence. Schema-11 producer
code refuses schema 12, so include the outbox in coordinated backups and preserve
it during rollback. Native Stash schema 1000071 is unchanged.

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
Caller tests cover a 500-source list, lost command responses, frozen relative
windows, concurrent admission, expired resolution leases, atomic binding/ticket
rollback, reviewed matches, later rescans and schema-4 preservation.
The backend test
`TestPythonProducerDurableDeliveryAgainstNativeHTTP` runs this actual client
against the native HTTP router and SQLite, loses committed event and run-submission
responses, and checks replay after reopening, independent rejection, caller-ticket
deduplication, exact ticket completion and the submit/claim/renew/checkpoint/finish lease
cycle. `TestPythonDownloadWorkerRecoversFinishAndDeliversFiles` runs the worker
against the real API/SQLite from a persisted URL-list caller, drains a capture during downloading, recovers a lost
attempt-completion response and a committed download-report response, and admits
dependent file/report events after reopening the outbox. It checks the ticket-status
and call-status CLIs after restart and verifies that source success still leaves
actual media intake queued. It exports/restores the library and real producer
outbox together, compares receipt-boundary proofs and reopens the relocated native
database to recover the original report receipt.
The same real download fixture exercises the n8n adapter's stable record/inspect
token, pending result, lost permanent-completion response and exact-proof replay
after reopening the outbox. Source completion still leaves file intake queued.
`TestPythonBackfillImporterReplaysAfterLostNativeResponse` executes the maintenance
importer against the real application router/SQLite, loses a committed batch's
response and verifies replay without duplicate decisions or source-file changes.
`TestPythonScanJournalImporterRetainsAtomicSnapshotAfterLostResponse` does the
same for a whole journal, then checks paginated summaries and every original
record through the real HTTP inspection routes. Migration fixtures preserve
the seven families, old extractor columns, timestamps and embedded JSON strings;
unknown inputs and conflicting snapshot identities cannot partially publish.
These checks are included in `make validate-fork`
and the build workflow. No production endpoint or source website is contacted.
`TestPythonDiscoveryHTTPPreservesLargePagesAndLostAcknowledgements` also runs the
supported discovery client and shared job lease against the native server,
preserving a page larger than 4 MiB, exact source numbers and compact metadata.
Lost admission, claim, page and failure responses replay without duplicate
attempts. The local journal reopens between claim/delivery attempts and releases
staged bytes only with the checked native receipt. The fixture checks lease
renewal and source reservation, the next page's original cursor and the first
page's receipt after the next job fails. Retaining
a nonfinal page does not mark enumeration complete or publish native post matches.
`TestPythonDiscoveryExecutionRecoversOwnedPagesWithoutRefetch` runs the selected
worker through separate Python processes against the native API. Ten scenarios
cover lost claims/pages/failures, expired ownership, pause after extraction,
capacity, rejected pages, a later attempt's completion, empty final pages and a
changed policy. Delivery-only CLI recovery has no website profile and preserves
the original source numbers without refetching. Private access values never
enter the outbox or command output.
`TestPythonDiscoveryDispatchResumesWithoutRepeatingPages` starts a separate
producer process for each cycle against native HTTP. Lost admission and page
acknowledgements, delivery-only CLI recovery and filtered readiness pages reach
the original final cursor with exactly two page fetches and one native attempt
per page. Its global-profile scenario uses the actual `dispatch-all` CLI and a
root grant issued before a later collection registration; both listings finish
without duplicate fetches or new local configuration. Producer migration tests
compare retained tables and exact pending bodies across schema 11 → 12 and
12 → 13, and reject table/column collisions without changing their data. Shared
worker tests exercise all three busy operations across restarts, independently
recover both delivery journals without website profiles, and retain a completed
delivery receipt when another process advances the dispatch revision.

### Candidate detail worker

A detail worker fetches one inferred Reddit or Twitter post URL and saves a
comparison with the original catalog/listing evidence. Its result can be
`corroborated` or `uncorroborated`. The comparison receipt itself does not publish
metadata or accept identity. Stash's publication worker can subsequently use a
corroborated result once listing coverage, uniqueness and native-edit checks pass.
Review retains the original weak candidate and exposes its latest result. Listing
discovery and post enrichment have separate profiles and receipts.

Use a detail profile for the candidate's service:

```json
{
  "schema": "stash-gallery-discovery-detail-v1",
  "source_category": "reddit",
  "gallery": {"extractor": {"sleep-request": 5}},
  "bindings": {}
}
```

`source_category` supports `reddit` and `twitter` for the implemented candidate
policy. Website access uses the same producer-local bindings as other metadata
profiles. The policy hash includes the detail operation, runtime and reviewed
settings; an enrichment or account-listing profile cannot replace it. The
collector does not create media download writers, update cookies or write a
gallery-dl archive. Successful empty responses are retained as negative detail
evidence; authentication, missing-source and network failures remain failures.

Use the usual `--outbox`, `--endpoint` and `--producer` arguments with these
commands:

| Command | Behavior |
| --- | --- |
| `detail-policy --profile PATH` | Validate a detail profile and print its policy/runtime |
| `admit-detail TARGET_UUID --revision N --candidate SEQUENCE --profile PATH` | Pin one selected candidate and target revision to this detail policy |
| `execute-detail JOB_UUID --profile PATH` | Claim, fetch or resume a selected job and deliver its comparison |
| `deliver-detail JOB_UUID` | Recover saved checkpoint/completion/failure intents without website access |
| `detail-status [--job JOB_UUID]` | Inspect local pending evidence and retained `comparison` receipts |
| `retry-detail JOB_UUID` | Explicitly create or recover the next generation of a failed/cancelled job, retaining its original selection and retry delay |
| `dispatch-detail --collection UUID [--profile PATH]` | Resume saved work and admit eligible candidates through the selected profile; omitting the profile permits delivery only |

Target UUID, revision and candidate sequence come from native candidate review.
Repeating identical admission returns its original job. Changing the profile or
selection does not reinterpret an existing job. The dispatcher admits a new weak
candidate only after complete retained listing comparison and current review
identify exactly one candidate with no other blocker. Any existing detail-job
history for that target/candidate prevents automatic readmission, including
older target revisions, negative results and terminal jobs. Use explicit review
and, for failed/cancelled work, `retry-detail` to resolve those cases.

Add a `post.verify_candidate` entry to the shared worker list to discover
permitted collections with retained listings. Automatic dispatch requires
`discovery_detail_admission_protocol: 1`, in addition to the detail execution
capability. Collection inspection includes blocked containers; readiness still
depends on the selected profile, current source choices and native review.
Candidate requests inspect at most 32 listing definitions and 32 targets,
advancing over empty or blocked rows. The producer checks profile URL support;
the server rechecks automatic eligibility when admitting and committing the job.
Saved delivery runs before profile loading, existing jobs run before new
admission, and collection/profile selections persist before execution. A lost
admission reply is recovered through queued-job discovery. Completed collection
traversals pause before polling again, including passes spanning multiple pages.

Outbox schema 14 stores detail evidence in `discovery_detail_executions`, separate
from enrichment and listing journals. A full checkpoint's capacity is reserved
before source access; events and all three metadata journals share one byte
budget. Checkpoint acknowledgement removes local source bytes only while staging
the exact completion or child-failure intent in the same transaction. The native
server retains the original observing producer and time for every record.
A lost completion reply recovers the same comparison receipt. Unacknowledged
local evidence stays available for review if a native job changes or ends.

Include the producer outbox in backups. Schema 15 adds a persisted candidate
cursor to the schema-14 detail dispatcher while preserving every existing table,
pending body, claim, receipt, retry deadline and rotation cursor. Conflicting
schema objects roll back the upgrade. Native archive schema 77 already contains
the required jobs, evidence and indexes; automatic admission requires no new
native migration. Production launcher conversion remains a cutover step.

### Review a discovery search after a collection change

`stash-review-discovery-collections` prepares an application review when a saved
search's collection acquires a new root or directory association. It preserves
the original definition, cursor, retry deadline, targets and recovery links.
Searches with any native worker-job history or an existing replacement need their
existing recovery workflow; this command does not transfer their running state.

List bounded candidates using an application API key:

```sh
stash-review-discovery-collections candidates --endpoint "$STASH_ENDPOINT" \
  --collection "$COLLECTION_UUID" --revision "$COLLECTION_REVISION"
```

Use `--after` with the returned UUID cursor to continue. Save the chosen
`listing_uuid`, its current `expected_definition_sha256`, the new
`collection_revision` and a `reason` in a JSON input file. An omitted operation
`uuid` is generated once during preparation:

```sh
stash-review-discovery-collections prepare --endpoint "$STASH_ENDPOINT" \
  --input selection.json --output reviewed-search.json
stash-review-discovery-collections show --plan reviewed-search.json \
  --expected-sha256 "$SAVED_PLAN_SHA256"
stash-review-discovery-collections apply --endpoint "$STASH_ENDPOINT" \
  --plan reviewed-search.json --expected-sha256 "$SAVED_PLAN_SHA256"
```

Use the file digest printed by `prepare` as `SAVED_PLAN_SHA256`. Plans are private,
created without overwriting an existing file, pinned to the endpoint, and checked
again immediately before use. `status` takes the same arguments as `apply` and
reads only the receipt. After interruption, reuse the same file and digest;
`apply` checks for a committed receipt before sending anything again. A 409
requires review of the changed state. Successful review does not enable a root,
issue credentials, admit a source job or claim that scraping completed.
