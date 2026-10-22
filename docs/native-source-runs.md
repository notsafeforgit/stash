# Scrape requests, checkpoints and metadata

The native download workers retain metadata from the same gallery-dl responses
that discover media. A normal capture also completes matching pending historical
metadata targets, including targets in another collection, by associating the
existing capture. Dispatch and execution check for reusable captures before
admitting or claiming a separate fetch. This preserves the original observation
time and does not copy the payload or manufacture another observation.

`post.enrich` is gap recovery only, using idle source capacity. It fills old
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
  under one profile subscription. Search is queued independently even when the
  profile listing is hidden or unavailable. Ordered
  listings stop after the first wholly archived, unpinned post. Every attachment
  and external child of that post is processed before deciding; the successful
  download archive must already contain every item. Dates alone, deleted items,
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

`not_found` and `access_denied` attempts stay queued for an automatic recheck
after 24 hours (or a longer requested retry delay). Each failed pass keeps its pending
windows and progress. Its daily delay applies to that run, so other passes for
the same account can continue after the normal short target cooldown. Shared
service budgets and routine-check priority still apply. Stopping a subscription
still stops its work.

Authentication/challenge, invalid configuration and rejected source metadata
remain explicit attention states. They are not treated as inaccessible accounts.

Recoverable download-run failures retry continuously, with exponential backoff
starting at five minutes and capped at 24 hours (a longer provider-requested
delay wins). Eight failures no longer create an automatic review hold. Schema
1000116 removes the database counter ceiling while preserving run and attempt
history, checkpoints and existing explicit holds. Routine request priority and
shared service budgets continue to apply during retries.

An explicit Redgifs item deletion (HTTP 404/410 with the provider's deletion
message) is handled individually. Its parent post and a failed source-transfer
report remain recorded; it is never entered into the successful download archive.
Reddit's available fallback can still download, and the remaining posts continue.
Outbox schema 18 caches that specific deleted item for 30 days, across restarts,
without extending the expiry on cache hits. Generic 404s, authentication errors,
rate limits, server failures and timeouts are not treated as confirmed deletions.

## Remote pagination

Reddit, Twitter and Instagram posts/photos checkpoints include the extractor,
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
jobs cannot gain priority through age or a fairness turn: any ready same-service
download takes precedence, even after a metadata job has claimed a lease.
Download claims do not wait for an idle metadata lease. Metadata checks both its
parent service and the contacted host, and yields before its next request when
higher-priority work appears. An already in-flight HTTP request can finish.
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
