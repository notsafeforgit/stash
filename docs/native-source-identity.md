# Native source-account identity evidence

The native archive separates captured account identifiers, an account's owner,
and performers depicted in media. `pkg/archive.ExtractCapturedAccount` implements
the versioned `captured-account-v1` parser for producer and catalog-import work.
It returns evidence; it does not allocate accounts, merge them, change ownership,
or assign scene/image performers. Integration with ingestion and catalog import
remains pending under the [transition plan](native-archive-transition-plan.md).

The input is a reconstructed capture object, bounded by the source JSON limit.
The output contains a qualified namespace, optional display label, policy version,
and identifier claims with their evidence basis and JSON pointer into the input.
An explicitly captured ID is required before pairing any handles with it. A
missing ID returns no account claim; a malformed claimed identifier is an error
that the caller must surface for review. Retained source data is not discarded
or replaced by this derived result. Existing handle-only inventory records,
explicit associations, and legacy keys still require their own lossless import;
a nil parser result does not authorize discarding them.

| Extractor | Account ID evidence | Additional captured identity |
| --- | --- | --- |
| Reddit | `author_fullname` | `author`, or the author object's `name`; never a feed owner's `user` object |
| Twitter | `author.id` | `author.name` |
| Instagram | `owner_id` | `username` |
| Bluesky | `author.did` | `author.handle` |
| TikTok | `author.id` and/or `author.secUid`, with distinct identifier kinds | `author.uniqueId` or its captured `name` |
| Tumblr | `blog.uuid` | `blog_name` or `blog.name` |
| Coomer/Kemono | `user` in `mirror:<extractor>:<service>` | A matching profile's `public_id` remains a separately typed mirror claim |
| yt-dlp | `channel_id` or `uploader_id`, qualified by extractor/site | Uploader/channel text remains a display label |
| Other gallery-dl extractors | The author's own `id`, `did`, or `uuid`; otherwise a captured owner/uploader/user/creator object or top-level `user_id`/`uploader_id` | Explicit `username`, `account`, or `handle`; a generic `name` remains a display label |

A Reddit parent capture takes precedence over a child media-host account, and
its pointers retain the `/_reddit/` prefix. For unfamiliar extractors, a separately
named author cannot borrow the ID of another `user` object. Directory names,
filenames, post IDs, captions, and scraper target URLs do not identify the author.
Generic yt-dlp URLs establish only a site namespace, never an account ID.

Opaque IDs retain spelling and case. JSON integer IDs remain exact even above
JavaScript's safe integer range. Fractional/exponent numeric IDs, nested objects,
booleans, invalid text, duplicate JSON keys, and oversized payloads are rejected.
Known case-insensitive native handles use the shared reference normalizer;
unfamiliar handles retain case. No requests or redirects are followed.

Mirror display names are labels, not native handles. A profile's public identifier
is accepted only if both its user ID and underlying service match the captured
mirror account. Neither a mirror user ID nor its public identifier is silently
promoted into a native-service ID. This keeps a Coomer/Fansly account distinct
from a native Fansly account even when their numeric values happen to match.

The account repository's indexed lookup returns all candidates for a qualified
identifier. A returned claim is not permission to consolidate those candidates:
reused handles and conflicting IDs still need the checked review operation in
[native schema 1000014](native-schema.md). Different services remain separate
accounts, even when explicitly linked to the same performer.

Tests cover the named services, unfamiliar extractors, native/mirror separation,
TikTok secondary IDs, Reddit parent context, misleading feed profiles, exact large
IDs, malformed input, replay, unchanged extractor input bytes, and unchanged
identity after source-retention reduction or irrelevant counters change.

## Publisher decisions

Native schema 1000016 adds `CapturePublisher.Preview` and `Apply` repository
operations. They load and verify one retained capture, derive its account claims,
and query only the corresponding identifiers and selected account. They never
assign scene/image performers or change account ownership. A post can expose
several selected publisher accounts for review; the service does not guess a
single owner from contradictory captures.

| Preview action | Meaning |
| --- | --- |
| `create` | A captured ID has no matching account or locator candidate. Automatic processing may create a source account. |
| `link` | Captured ID claims resolve to one canonical account, without a contradictory claimed ID kind on that account. Other handle matches remain visible candidates. |
| `review` | IDs are ambiguous/contradictory, only locator candidates match, or the capture's service contradicts known post identifiers. |
| `unavailable` | The capture has no usable ID, has a malformed claim, or its post is forgotten. Existing evidence remains retained. |
| `preserve` | A linked or explicitly unlinked decision already exists. Automatic processing leaves it intact. |

Handles and mirror public identifiers alone cannot establish equivalence. A
reviewer can select a candidate, choose a new account for a reused handle when
no captured ID already matches, unlink a capture's publisher, or return the
capture to automatic selection. Manual choices can resolve an ambiguous match
for that capture without merging accounts or removing other candidate evidence.
A selected account with contradictory IDs of the same claimed kind requires
account reconciliation; this operation cannot overwrite its identifiers.
Qualified native/mirror namespaces remain separate. A manual choice may also
supply a publisher when machine-readable identity is missing, with explicit
review provenance rather than fabricated source claims.

Preview signatures cover the capture, current choice, relevant candidates,
canonical identities, display labels, and contradictions. New competing IDs or
account consolidation invalidate stale review. Unrelated observations of the
same account do not: its general observation revision and creation time are
excluded from the signature. Candidate display is bounded at 100 and reports
truncation. Full candidate paging uses the existing qualified identifier lookup;
explicit review can select a target directly by UUID. Post-account queries begin
with the selected post's indexed capture range rather than scanning all decisions.

Apply requires a stable request UUID and the preview signature. Replaying the
same successful request returns its original decision even after a later unlink
or account consolidation; it does not reapply the old choice. Changed input
with that UUID is rejected. Automatic apply is valid only for a `create` or
`link` preview. For `preserve`, callers use the existing decision without making
another write or history entry. A later explicit `inherit` decision permits a
fresh automatic selection.

Decisions and evidence references are immutable. Identifier evidence records the
capture UUID, parser policy, source JSON pointer, and observed timestamp. Joined
claim records identify exactly which evidence a decision used; the source post
and profile bodies stay shared. Original account associations remain history
while current queries follow canonical accounts after consolidation. Durable
write context prevents a late failure from committing a new account or only
some claims when a caller ignores an error. Startup also checks publication
integrity. Anonymised exports remove publisher decisions and evidence references.

This service does not expose a public endpoint yet. Producer authorization,
ingestion receipts, native review UI, account-profile presentation, and catalog
import remain required integration work. The migration does not invent publisher
choices from old paths, names, or existing account ownership.
