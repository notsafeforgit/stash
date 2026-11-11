# Scrape requests, checkpoints and metadata

The native download workers retain metadata from the same gallery-dl responses
that discover media. A normal capture also completes matching pending historical
metadata targets, including targets in another collection, by associating the
existing capture. Dispatch and execution check for reusable captures before
admitting or claiming a separate fetch. This preserves the original observation
time and does not copy the payload or manufacture another observation.

`post.enrich` is gap recovery, using bounded turns and idle source capacity. It fills old
imports that have no retained gallery-dl post capture; it does not refetch every
downloaded post to refresh detail-only fields, later edits or profile changes.
Running jobs and retained partial child transcripts are protected from reuse;
their durable delivery/checkpoint path remains responsible for completion.
Held, excluded and failed targets are not automatically reopened. Dispatch lists
still omit `post.verify_candidate` and `account.list_page`, avoiding separate
verification and duplicate profile listing requests.

Listing responses may omit detail-only fields. Existing original text, account
data, attachment membership and reply relationships remain available when the
source includes them. External-host resolution needed to download Reddit media
is retained; it is not optional metadata enrichment. Extended Twitter user
lookups, tweet expansion and Reddit comment fetches are disabled. Gap recovery
also disables Twitter expansion, replies and extra user lookups.

## Worker modes

Reviewed `stash-gallery-worker-v1` profiles have a `scan_mode`:

- `routine`: recent checks. Twitter selects its direct media listing; Reddit
  checks both the submitted-post listing and newest-first author search,
  under one profile subscription. Search remains independent during historical backfill. Once history is
  complete, missing/suspended profiles pause routine author searches until a
  profile recovery check succeeds (see below). Ordered
  listings stop after the first wholly archived, unpinned post. Every attachment
  and external child of that post is processed before deciding. Existing file
  archive entries or a committed execution-v3 Reddit post-completion entry establish
  completion. Dates alone, unfinalized missing items,
  empty/unsupported children and partial albums do not establish that boundary.
  Replayed checkpoint posts cannot end the run before unfinished work resumes.
  Instagram's posts/photos feed uses the same complete-post boundary, including
  its pinned-post and original carousel membership evidence. Stories and
  highlights always inspect all available items: stories can be oldest first,
  and an existing highlight can acquire new media. Initial and explicit recovery
  traversals still inspect the full requested range.
  Empty story/highlight containers produce no posts. A null/empty carousel field
  on an explicitly single-media post still identifies one attachment; an actual
  carousel without its attachment list remains an error.
- `initial` (default): preserve complete initial-import traversal, including the
  profile's explicitly configured historical retrieval strategy.
- `recovery`: explicitly requested historical traversal at the lowest request
  priority. Host launchers accept `--recovery` and select
  `STASH_INGEST_<SITE>_RECOVERY_PROFILE`. Reddit
  traverses the same submitted-post and author-search passes without routine
  completed-post stopping. The Twitter recovery profile selects the broader
  timeline traversal and its search fallbacks, using remote search cursors.
  Automatic weekly Reddit and Twitter historical sweeps are disabled in the
  operations deployment; recovery remains available for an explicit request.
  Routine retries resume their existing checkpoints without scheduling a new
  full-history sweep. Initial profile imports retain one n8n worker per site,
  and metadata gap recovery remains enabled.

Profile files and runtime modules contribute to the execution policy hash.
Updating existing work requires native policy upgrades that preserve original
requests, windows, progress, fences and retry deadlines. File transforms,
per-output locking, download archives and durable completion events are unchanged.

## Account access and retrieval passes

A Reddit profile is one subscribed source. Its submitted-post and author-search
passes retain separate runs and checkpoints under that source; the run count is
not an account count. A completed search updates the same source's last successful
check even if a profile listing is unavailable. It does not claim that another
pass's unfinished window was downloaded.

`not_found` attempts stay queued for an automatic recheck after 30 days;
with `source_run_account_status_protocol: 1`, explicit `account_suspended`,
`account_deactivated`, `account_unavailable` and `account_not_found` results
retain their distinction and use that same monthly check. None establishes
permanent deletion, and a generic 404 is not classified as a suspension.
`access_denied` attempts recheck after 24 hours (or a longer requested retry delay).
Each failed pass keeps its pending windows and progress. The delay applies to that run, so other passes for
the same account can continue after the normal short target cooldown. Shared
service budgets and routine-check priority still apply. Stopping a subscription
still stops its work.

With `source_reddit_monitoring_protocol: 1`, request permits carrying
`account_monitoring_protocol: 1` pause routine author-search runs after completed
Reddit history (`reddit-new` and `reddit-top`) when the latest profile evidence
is `account_not_found`, `account_suspended`, or the older `not_found`, attributed
to Reddit. The existing profile runs continue their 30-day checks. Initial and
explicit recovery requests, unrelated searches, attachment retries and other
sites remain eligible. HTTP 403/429, authentication errors and linked-host
failures do not establish account unavailability.

A paused permit returns `pause_reason: "account_unavailable"`, `ready: false`
and `wait_ms: 0`. The worker releases its lease with `account_check_pending`;
this is a local scheduling pause, not a fresh website failure or completed
coverage. Pending windows and checkpoints remain queued for up to 30 days,
daily submissions coalesce, and no HTTP request or failure allowance is spent.
A successful profile pass or newer accepted standalone profile observation
wakes retained searches immediately, including when the subsequent listing or
media download fails. Author-search success never counts as profile recovery.
All detection uses already-saved observations and attempt histories, including
consolidated runs; no additional Reddit calls or per-post status records are
created. Generic 404 responses remain missing, not falsely labeled banned.

With `source_run_redgifs_deletion_protocol: 1`, an explicit Redgifs
`User account deleted.` response from the claimed account's listing permanently
retires that source. The worker finishes with `state: "deferred"`,
`error_code: "account_deleted"`, and `error_scope: "service:redgifs"`. The server
requires a current account definition and a reported listing request under the
same live lease, retires the collection, disables its subscription, and cancels
its queued/deferred work, including initial and manual runs. The returned run is
`cancelled`; its attempt retains the deletion outcome. Pending ranges, previous
captures and media remain preserved, without a claim of successful coverage.
Generic missing, restricted, rate-limited or embedded-account responses cannot
retire the parent source. Rediscovering the retired Redgifs profile cannot relink
or subscribe it; a different Redgifs profile can still be discovered and fetched.

An interrupted crawl with progress keeps its original traversal window when a
later scheduled request extends the pending coverage. It finishes that frozen
window before starting the additional ranges, so daily admission cannot restart
a large initial crawl from its beginning.

Application-authenticated `POST /api/v3/ingest-admin/run-consolidations` combines
compatible queued runs, or compatible deferred runs, using both expected revisions
and an idempotent request UUID. Collection/root revisions, retrieval pass, window
basis, execution policy, subscription revision and cooldown must agree. The target
retains the union of unfinished ranges and the later retry deadline. The original
run and its request/attempt history remain retained, with an immutable successor
record. Reading an original run preserves its admission identity and reports only
coverage established by its successor, including later completion. Consolidation
does not release a deferred run or transfer a live lease.

New clients may set `reuse_compatible_policy: true` when admitting a request.
The `source_run_compatible_admission_protocol: 1` capability is required. A matching
run with a reviewed execution-policy upgrade can then absorb the request without
changing its original policy identity. Admission receipts and backup verification
retain the original request digest and the policy upgrade that justified reuse.

With `source_run_attachment_retry_protocol: 2`, a failed individual Reddit file
can create a separate focused post retry. Admission requires that producer's exact
failed `attachment.download` receipt, matching collection revision, subscription,
and original window; it returns HTTP 425 while the receipt is still in delivery.
The parent advances only after the focused request is durably queued. Actual
failure history remains truthful; no absent file gets a successful file receipt.
A focused retry cannot recursively delegate its own failure.
Focused retries have a 48-hour recovery window from admission. A failed transfer
or missing post is checked again the next day; after that window, automatic
recovery becomes a local archive-finalization claim (`attachment_retry_finalize`).
The owned claim includes `attachment_retry_source` from the original capture, so
the worker can resolve the configured archive without another website request.
It commits `stash:completed:reddit:<id>` to that archive before finishing the run
as processed. The native adapter honors this post marker on subsequent crawls.
Individual file archive keys and completed-file counters still represent actual
downloads. Schema 1000137 retires the retry-only link when its run is terminal;
it preserves normal request/attempt history. A failed local archive write remains
retryable and cannot re-enable website fetching. Authentication, rate limits and local worker failures do
not establish that a file is permanently unavailable. The focused job's retry
delay does not hold up other passes for the same account.

An inaccessible Reddit profile alone is not proof of permanent account deletion.
Missing-profile rechecks preserve independent initial and explicit recovery passes, which can
still discover public posts. Confirmed deletion and reversible suspension must
not be inferred from the same generic `not_found` response.

Authentication/challenge, invalid configuration and rejected source metadata
remain explicit attention states. They are not treated as inaccessible accounts.

Redgifs listing throttles retain the shared HTTP backoff across successful
authentication and individual-media requests. After the cooldown, those requests
release their probe slot without clearing the backoff history; only a successful
listing response resets it. Redirects and account-specific errors do not establish
listing recovery. Both download and metadata workers use this rule. Repeated
throttles back off for 1, 2, 4, 8, then 15 minutes, unless the provider supplies
`Retry-After`; existing request spacing, probe leases and stale-response guards
still apply.

Other recoverable download-run failures retry continuously, with exponential backoff
starting at five minutes and capped at 24 hours (a longer provider-requested
delay wins). Eight failures no longer create an automatic review hold. Schema
1000116 removes the database counter ceiling while preserving run and attempt
history, checkpoints and existing explicit holds. Routine request priority and
shared service budgets continue to apply during retries.

An explicit Redgifs item deletion (HTTP 404/410 with the provider's deletion
message) is handled individually. Execution v3 records whole-post completion in
the downloader archive only after all members finish processing. It creates no
new permanent missing-file ledger or fictitious completed-file receipt.
Reddit's available fallback can still download, and the remaining posts continue.
Temporary observations are removed after the post marker commits and caches older
than 48 hours are pruned when a Reddit worker starts. Generic 404s, authentication errors,
rate limits, server failures and timeouts are not treated as confirmed deletions.

## Remote pagination

Reddit, Twitter, Instagram posts/photos and Redgifs user checkpoints include the extractor,
public retrieval URL,
opaque cursor for the current remote page, and last completed attachment within
that page. The page start is committed before requesting it. A restart fetches
that page again, processes it safely through the normal durable delivery path,
and advances only after consuming it. Profile lookups and external-host child
requests cannot replace the parent listing cursor. Retrying a page request does
not advance its checkpoint. The supported checkpoint has a 4 KiB serialized
limit; unexpectedly large cursors pause rather than lose the checkpoint.

An older attachment-only checkpoint still replays until its anchor before it
is upgraded to a page checkpoint. A missing anchor in an already restored page
is safe to pass only after replaying that entire page. Other extractors retain
the existing bounded item replay until they have a dedicated pagination adapter.

Instagram feed checkpoints require `source_run_instagram_pagination_protocol: 1`.
Only the post-feed GraphQL connection updates them; profile lookups, story
containers and media detail requests cannot replace a saved feed page. Stories
and highlights retain item replay and never inherit the feed's pagination cursor.

Redgifs user listings require `source_run_redgifs_pagination_protocol: 1` and resume
the saved numbered page. Authentication and media-detail requests cannot replace
that checkpoint. An explicit provider history-page limit defers the crawl with
`source_history_limit`, preserving unfinished coverage for review. It never proves
the inaccessible history complete. Execution contract v2 introduces these cursor
and independent-file-retry semantics; deployments explicitly upgrade compatible
queued policies after upgrading the native server and all backup receipt readers.

## Shared source budget

Schema 1000111 adds a native per-service request budget. Each source HTTP attempt
requires a fenced permit, including internal retries and yt-dlp source extraction.
Host and n8n use the same API. Media transfer sessions remain independent.
Requests currently have conservative minimum spacing of 20 seconds for Twitter,
5 seconds for Reddit and 1 second for other services. These are local pacing
choices, not claims about the platforms' published quotas. Existing gallery-dl
sleeps may further reduce traffic. Retry-After and exhausted response quota
headers extend the shared wait; existing service cooldowns still apply.

Waiting routine checks take precedence over initial imports; both precede
historical recovery. A higher-priority worker downloading media does not reserve
unused API capacity. Waiters expire and must hold a current lease. Permits are
idempotent, scoped to a run/fence and short lived. No source URLs, cookies or
response bodies are retained in budget records.

Schema 1000112 extends the same budget to historical metadata jobs. Every HTTP
attempt, redirect, retry and external-media resolution requires a permit. These
jobs register scheduling interest when polled. After four download starts or two
minutes of live waiting, an eligible polling requester gets a turn once current
downloads drain. A claimed metadata attempt reserves its services for at most 60
seconds before queued downloads regain priority; lease renewal cannot extend
that turn. Metadata checks both its parent service and the contacted host, and
yields before its next request when its turn ends and downloads are waiting.
It may continue while the service is otherwise idle. An already in-flight HTTP
request can finish. Provider cooldowns and request spacing still apply throughout.
`budget_yield` retains partial records and checkpoints without consuming the
failure/retry budget. Historical requests and rate limits appear in the existing
recovery metrics. The capability `enrichment_gap_protocol: 1` prevents older
servers from running a producer that expects these guarantees.

Archive activity → Scrape runs shows hourly aggregates for the preceding 24
hours, with 30-day retention. Requests count granted attempts; an interrupted
worker may not send its last grant. Responses, rate limits and errors are
reported separately. Captured posts count once per run across all attachments;
new means absent from Stash when first captured. Account checks show the latest
successfully covered cutoff and overdue time beyond 24 hours, including accounts
that have never completed a check. Page and item totals are exact.
