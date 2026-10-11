# Scrape requests, checkpoints and metadata

The native download workers retain metadata from the same gallery-dl responses
that discover media. Production dispatch lists omit `post.enrich`,
`post.verify_candidate` and `account.list_page`: these would fetch individual
posts or duplicate profile listings separately. Captured, pending deliveries
remain recoverable without reopening the source. Explicit metadata recovery
adapters remain available for an intentional repair.

This means old imports do not automatically gain missing fields, and later post
edits, profile changes or deletion states are not independently refreshed. A
normal scrape can still update a post it encounters again. Listing responses
may omit detail-only fields. Existing original text, account data, attachment
membership and reply relationships remain available when the source includes
them. External-host resolution needed to download Reddit media is retained;
it is not optional metadata enrichment. Extended Twitter user lookups, tweet
expansion and Reddit comment fetches are disabled in the download adapter.

## Worker modes

Reviewed `stash-gallery-worker-v1` profiles have a `scan_mode`:

- `routine`: recent checks. Twitter selects its direct media listing; Reddit
  uses the submitted-post listing without an additional author search. Ordered
  listings consume four whole posts beyond the requested completed boundary.
  A first check without a time boundary stops after four whole archived posts.
  A new attachment anywhere in a post resets that archive overlap. Pinned posts
  do not establish the stopping boundary.
- `initial` (default): preserve complete initial-import traversal, including the
  profile's explicitly configured historical retrieval strategy.
- `recovery`: historical traversal at the lowest request priority. Host launchers
  accept `--recovery` and select `STASH_INGEST_<SITE>_RECOVERY_PROFILE`. Reddit
  includes its alternative author searches in this mode. Production recovery
  schedules run weekly, independently of daily routine checks; an existing
  running service is not started again by its timer. The weekly Twitter profile
  selects the broader timeline traversal and its search fallbacks, using remote
  search cursors. Initial profile imports retain one n8n worker per site.

Profile files and runtime modules contribute to the execution policy hash.
Updating existing work requires native policy upgrades that preserve original
requests, windows, progress, fences and retry deadlines. File transforms,
per-output locking, download archives and durable completion events are unchanged.

## Remote pagination

Reddit and Twitter checkpoints include the extractor, public retrieval URL,
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

Archive activity → Scrape runs shows hourly aggregates for the preceding 24
hours, with 30-day retention. Requests count granted attempts; an interrupted
worker may not send its last grant. Responses, rate limits and errors are
reported separately. Captured posts count once per run across all attachments;
new means absent from Stash when first captured. Account checks show the latest
successfully covered cutoff and overdue time beyond 24 hours, including accounts
that have never completed a check. Page and item totals are exact.
