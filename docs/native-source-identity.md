# Native source-account identity evidence

The native archive separates captured account identifiers, an account's owner,
and performers depicted in media. `pkg/archive.ExtractCapturedAccount` implements
the versioned `captured-account-v1` parser for producer work.
It returns evidence; it does not allocate accounts, merge them, change ownership,
or assign scene/image performers. Ingestion uses the publisher decisions described
below to select an account from that evidence.

The input is a reconstructed capture object, bounded by the source JSON limit.
The output contains a qualified namespace, optional display label, policy version,
and identifier claims with their evidence basis and JSON pointer into the input.
An explicitly captured ID is required before pairing any handles with it. A
missing ID returns no account claim; a malformed claimed identifier is an error
that the caller must surface for review. Retained source data is not discarded
or replaced by this derived result. Existing handle-only inventory records,
explicit associations, and legacy keys are retained as stored evidence; a nil
parser result does not authorize discarding them.

| Extractor | Account ID evidence | Additional captured identity |
| --- | --- | --- |
| Reddit | `author_fullname` | `author`, or the author object's `name`; never a feed owner's `user` object |
| Redgifs | No immutable ID in the item API response; automatic publisher selection remains unavailable | `userName` is retained as a handle for explicit review; `/users/<handle>` URLs identify the Redgifs profile |
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

An API-resolved Redgifs item linked from the author's Reddit profile can supply
a related `/users/<userName>` URL. It is retained on the Reddit publisher's
performer and can become a monitored source with the observed Reddit download
directory. Native discovery creates or reuses an unambiguous Redgifs handle
account and binds it to the new collection before its first run. A unique
existing performer profile URL links that account to the performer, including
when the Reddit owner is chosen later. This does not invent an immutable ID,
merge accounts, or assign depicted performers. The two services' handles may
differ. Existing URL removals, explicit ownership decisions and subscription
decisions remain authoritative; ambiguous handles or performer URLs need review.

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

The application-authenticated `/api/v3/archive` API exposes these reviews:

| Route | Purpose |
| --- | --- |
| `POST /capture-publisher/preview` | Inspect a retained `capture_uuid`, optionally selecting an `account_uuid`. Returns the captured identifiers, candidates, conflicts, current decision and `signature`. |
| `POST /capture-publisher/apply` | Submit `request_uuid`, `capture_uuid`, `signature`, `action`, optional `account_uuid`, and `reason`. Returns `{decision, replayed}`. |
| `GET /capture-publisher/requests/{request}` | Recover the original decision after a lost response, including its current canonical account resolution. |
| `GET /captures/{capture}/publisher-history` | Read decisions in revision order, using `after` and `limit` (1–100). |

Actions are `automatic`, `link`, `create`, `unlink`, and `inherit`. Only `link`
accepts an account UUID. Explicit choices always use review provenance; producer
credentials cannot perform these reviews. An account with only a handle
can be confirmed by previewing a captured author ID against that account and
applying `link`. This records the captured ID and handle with their source and
observation time. Other retained captures can then be previewed and applied with
`automatic`; future captures use the same confirmed identity. Existing explicit
choices remain preserved. This operation does not download media, change titles,
or assign depicted performers.

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

Producer ingestion applies unambiguous automatic publisher choices and reports
unresolved matches in its receipt. The application review API above handles later
confirmation; producer credentials cannot make explicit review decisions. Original
ingestion receipts remain immutable after review, while current publisher queries
reflect the selected decision. The migration does not invent publisher choices
from old paths, names, or existing account ownership.

## Performer page identities

Performer pages use the canonical archive UUID in their URL. Numeric bookmarks
resolve through the current local record and redirect to that UUID; a merged
UUID redirects to the surviving performer. Tabs, filters and the browsing origin
are preserved. Deleted identities remain unavailable even if a database key is
reused. Resolution works with source tracking disabled.

`GET /api/v3/archive/entity-identities/{kind}/{identifier}` accepts a UUID or a
current numeric key and returns the active canonical identity. It checks the
entity kind before and after following redirects. Local keys remain internal to
library queries and relationships. Performer identity history exposes UUIDs and
merge redirects, without the redundant “Original library ID” field. Internal
`archive_entities.original_id` values are not URL aliases and are still used by
media-conversion integrity checks; this change requires no database migration.

## Post links and their evidence

Native schema 1000032 adds `SourcePostLinks` for post URLs, identifier evidence
and unselected publisher claims. The retained relationship evidence from schema
1000033 uses these records; its import routes were retired in schema 1000142.

`source_post_urls` stores each exact URL once per post. Repeated observations
share that row and retain their own UUID, origin, basis, observation time and
bounded JSON evidence in `source_post_url_evidence`. Observation time describes
when the association was recorded, not when the post was published. URL strings
are retained without fetching or canonicalising them; HTTP(S) URLs containing
userinfo are rejected. A shared URL never merges posts.

`source_post_identifier_evidence` records why a qualified identifier belongs to
the post. Adding it requires the reviewed post revision and cannot move an
identifier already owned by another post. Catalog aliases must keep their
physical-catalog scope until stronger evidence establishes a service identity.

`source_post_account_claims` retains an association with an original account
UUID. Reads also resolve its current canonical UUID after consolidation. A
claim does not choose a capture's publisher, change an explicit unlink, establish
account ownership or assign depicted performers. Those operations retain their
separate core review/selection contracts. Known post and account namespaces must
agree when adding a claim; legacy-only post identities remain unqualified.

All three evidence types use immutable request UUIDs. Exact retry returns the
stored evidence, while changed content conflicts. New evidence is refused for
forgotten posts; retrying an already committed request remains valid. Native
writes require a managed transaction, including rollback when a caller ignores
a late write error. Reads use bounded indexed UUID cursors. Startup checks the
schema and relationship integrity; anonymised exports remove the evidence.

## Comparing possible duplicate posts

`GET /api/v3/archive/posts/{post}/comparison?other={post_uuid}` inspects two
explicit native post UUIDs in one read transaction. It returns their identifiers,
retained URLs, compact latest-capture summaries, current source lists, gallery
choices, attachment decisions and explicit post-to-media choices. Media choices
retain their original UUIDs alongside the current resolved identity, including
deleted records without a live scene/image ID.

The comparison reports qualified identifier disagreements, differing service
namespaces, incompatible source order/counts, disabled versus enabled source
lists, conflicting gallery choices and contradictory media links. It compares
attachment references and resolved media identities, so independently allocated
attachment UUIDs and already-merged media do not create false differences.
Compatible partial source lists retain their gaps. Explicit unlinks and disabled
choices remain visible.

Shared URLs are supporting evidence only. For example, distinct Instagram
stories can share a highlights URL. Matching content, titles or a shared URL
does not establish that two records represent the same post. An empty conflict
list likewise does not establish identity. This endpoint performs no merge,
returns no Apply authorization, and does not rewrite captures or job receipts.
It inspects current choices; original history, evidence and pending work remain
under their existing post scopes. The separate
[reviewed merge](native-ingestion.md#reviewed-post-merges) binds explicit choices
and a fresh digest before applying them together.

Each post is limited to 512 identifiers, 512 URLs, 8,192 retained attachment
identities and 8,192 explicit media choices. Larger scopes return HTTP 422 with
`post_comparison_limit`; the response never silently truncates choices. Source
list loading also retains its existing manifest/entry limits. Queries start from
the selected post indexes. Full post/profile payloads, plugin settings and
library-wide searches are not included.

## Canonical post identities and original evidence

Schema 88 introduces `source_post_identities`. Each original post begins as its
own canonical identity. A reviewed consolidation can group duplicate records
under one surviving UUID while retaining every original post, identifier,
capture, source list and receipt under its original owner. This native relation
does not combine distinct posts that happen to share a caption, URL or media.

Immutable consolidation receipts retain the direct source/destination UUIDs,
reviewed revisions, signatures and reason. Canonical lookup is flattened after
later merges, while the original redirect history remains available. An exact
request retry recovers its original receipt even after another merge, restart
or retained deletion. Forgetting a member tombstones its entire identity group.

Identity reads use indexed lookups and paginated members/history. The storage
primitive limits a reviewed group to 256 original posts and 8,192 qualified
identifiers. Different non-legacy qualified upstream identifiers reject a merge;
shared URLs cannot override them. Updates require a managed transaction and
matching publication context, including rollback when a caller catches a late
write failure. Startup checks reject inconsistent identity/history relationships.

The internal choice review inspects all members of both groups, including
choices retained on earlier aliases. It reports incompatible source order,
gallery choices, explicit media unlinks and differing attachment decisions.
Its signature includes selected library revisions, so a scene/image edit also
invalidates the review. Indexed preflight limits reject oversized evidence before
loading it; an empty conflict list still does not establish post identity.

Schema 89 lets an attachment selection reference original captures and manifests
from any member of its post identity. Foreign keys retain the original evidence
UUIDs; scope guards require matching canonical identities. Neither post payloads
nor manifest entries are copied. The migration preserves existing decision rows,
their row IDs, history and incoming review/gallery references. Startup validation
rejects source evidence belonging to an unrelated post.

Compatible partial lists can contribute to one selection across original post
owners. Attachment representatives are deterministic and use only contributing
manifests; an unused candidate cannot alter the existing selection in a preview.
Pinned and disabled selections remain protected. A gallery created from such a
selection retains the original capture as its metadata provenance. Original
selection-history scopes and exact review receipts remain unchanged.

The internal source-list consolidation operation validates the latest post merge
and the complete reviewed set of current choices. It can combine compatible
lists, pin one original capture or explicitly disable source selection. Additional
lists must come from the reviewed current choices; unrelated, duplicate and
unreviewed lists are rejected. It retires only current pointers, preserving all
original captures, manifests, decisions and saved review receipts. Late failures
roll back the transaction even if a caller catches the error. The encompassing
merge still owns its final receipt and subsequent gallery synchronization.

Schema 90 permits an explicit consolidated post-media choice to supersede the
current choices of every original member. Each cross-owner replacement records
the consolidation that established their shared identity. The database checks
that original owner ancestry existed at that event, rather than comparing
unrelated posts' revision counters. A deferred foreign key prevents committing
replacement proof without the replacement itself. Original decisions and
capture ownership remain immutable, and exact media-decision retries survive
later merges and restart. Existing associations to deleted media can be carried
forward without restoring library records or making them eligible for new intake.

The internal gallery choice operation requires the complete reviewed set of
current associations, retires those associations and records one new choice.
It retains both selected and unselected galleries and all original decisions.
Later source synchronization recognizes automatic members contributed by earlier
post identities while preserving manual additions, exclusions, covers and edited
metadata. Gallery claims outside the consolidated group still block adoption;
a caught late failure cannot commit partially retired associations.

Equivalent attachments share a current media choice by canonical post identity
and exact attachment namespace/value. The original attachments, manifests and
immutable decisions keep their UUIDs; no separate attachment-identity table or
copied decisions are needed. The internal consolidation operation reviews every
original head, publishes one choice and retires the others atomically. Multiple
unresolved heads fail explicitly. Later ingestion honors a shared unlink and
can fill an undecided choice only with unique file evidence across the group.
Album inspection resolves shared choices in a bounded batch while retaining the
original attachment on every source slot.

Current post-media association reads resolve the canonical post and include all
its original heads. New ordinary decisions require the canonical UUID and cannot
retire another owner's head without consolidation proof; committed request retry
still returns the original result before inspecting current state. Metadata source
pickers follow shared attachment and whole-post choices across original captures.
They retain original capture/attachment provenance and suppress competing heads
or a canonical post unlink. Attachment review contexts distinguish the requested
UUID from the current choice owner, and the UI recovers saved original requests
before following that owner for new choices.

Current source-list and gallery reads resolve the canonical post, including
ordered album pages and gallery-to-post links. They reject unsettled choices on
earlier owners instead of silently selecting one. Source-list discovery pages
the indexed manifests of all original members without reconstructing payloads.
Ordinary new choices require the current canonical UUID and revision; ingestion
uses that identity while retaining its original capture as provenance.

The identity context returns both requested and canonical identities. Album
responses likewise separate the requested UUID from the current post. Editors
recover an original saved request before any pending canonical request, then use
the canonical identity for new edits. Original history, full merge comparison,
capture and ingestion lookup methods keep their original scopes so evidence and
saved requests remain replayable.

Current post browsing, media reviews, selected publishers and expanded evidence
pages follow the canonical identity. Exact UUID/qualified-ID/URL lookups resolve
the original owner before applying the canonical cursor. A shared URL alone does
not combine unrelated posts. Responses distinguish the requested identity from
the current one; capture and URL references retain their original owners.
Current URL pages select one stable witness per exact URL before pagination.
Capture summaries share revision metadata and page the indexed ranges of the
original members without reconstructing their payloads. Metadata policies use
the same deduplicated current URLs while retaining the selected original capture
and post as provenance. Original-owner evidence getters remain available to
history, recovery and complete merge comparison.

Historical media matching uses the indexed evidence of every original post in
the current identity. Equivalent source attachments can use original evidence
with the same qualified source-media ID. Filename matching checks the original
file observation against the group's qualified post IDs. New derived evidence
keeps the original attachment owner; post/media proof references keep their
original appearance and file-match chain. Unrelated posts and unresolved choices
remain excluded or in review.

New backfill requests require the current post UUID and a current preview.
An unpublished album job whose post was subsequently merged stops with
`album_preview_changed`. A committed publication can still finish notification
delivery under its original post and event, without rerunning gallery changes.
The browser recovers saved original requests before inspecting any saved request
for the current post. Viewing historical jobs leaves pending requests intact.
Command-line preparation resolves and deduplicates explicitly supplied historical
UUIDs; existing plans, receipts and job histories retain their original scopes.

The identity writer remains internal. The application uses an encompassing
reviewed transaction to resolve source-list, gallery, post-media and attachment
choices before publishing the canonical identity. Its preview includes every
original member and the resulting source order and gallery membership. Conflicts
require explicit choices; distinct upstream post IDs from one namespace cannot
be forced together. Current library metadata, manual membership and exclusions
remain subject to the shared gallery services.

Schema 1000092 retains the complete request and original result alongside the
identity event. Canonical identity changes, copied decisions, gallery effects,
notification admission and the immutable receipt commit in one managed
transaction. A late failure invalidates that transaction even if a caller
catches its error. Retries compare the saved request before consulting current
identity, so later merges do not rewrite the original result. Captures and
source revisions remain under their original owners.

The Source posts detail page offers this review on desktop and mobile. It saves
the exact application request in browser storage before sending, checks the
original receipt after a lost response, and keeps unresolved requests attached
to their original post UUID after redirection. A definitively stale preview
requires a new review. Merge history and notification retries are available
without depending on the browser that performed the merge.

## Historical post collection membership

Schema 1000039 adds `source_collection_post_evidence`. Each immutable membership
names a post and a historical collection definition, with its own evidence UUID,
origin, basis, observation time and provenance. It needs no scrape capture,
media file or performer. One post can belong to several groups, and several retained sources can independently support the same post/group relationship.
New evidence advances the post's review revision; exact replay does not. A
forgotten post rejects new evidence while retaining historical replay.
This supplements existing capture-to-collection membership and manual
media-intake provenance. The endpoints below return direct post-membership
evidence; capture-based membership remains available through collection captures.

Native membership evidence is paged by its UUID using `after` and `limit`
(default 50, maximum 100):

| Method | Endpoint | Result |
| --- | --- | --- |
| GET | `/api/v3/archive/collections/{collection}/post-memberships` | Post membership evidence for one collection |
| GET | `/api/v3/archive/posts/{post}/collection-memberships` | Collection membership evidence for one post |

These lists contain provenance records. Multiple records can support the same
post/group relationship; a collection's distinct-post view should group those
references without discarding their evidence.

The retired catalog membership import mapped each registry-qualified collection key
to one native group across its downloaded catalogs. The group keeps the original kind,
label and first native definition; later renames or retirement leave that definition
intact. Equal labels alone do not combine different source keys or registries. Legacy
`creator` memberships, which meant that a directory name contained commas, map to
directory groups without an owner. Other directory memberships map to directory groups,
and subreddit memberships retain their kind and Reddit namespace. Imported groups start
disabled, with no account, target URL or media root. Membership does not create an album
or imply depicted performers, publishers, or ownership. Schema 1000142 retired the
membership import routes and command; the groups and their evidence remain.

## Matching media to source albums

The core `SourceGallery.PreviewBackfill` service operates on one post's selected
attachment list. It proposes media choices and shows the resulting gallery
membership without writing. The post UUID identifies the album; the selected
source manifest supplies attachment positions, including repeated attachments
and missing slots. Ordinary matching does not derive order from folder labels
or download counters. An explicit historical Twitter policy can recover a
missing partial list from the known original-filename convention.

Callers must choose a matching policy explicitly:

| Policy | Accepted historical evidence |
|---|---|
| `source-identifiers-v1` | A retained qualified Reddit or Twitter media ID equal to the typed attachment ID, plus a retained file match for the same post |
| `legacy-reddit-filename-v1` | The identifier policy, plus the original Reddit `post-id_media-id_...` or `post-id_media-id.ext` filename convention when no explicit media ID was retained |
| `legacy-twitter-filename-v1` | Recover missing partial order from original `<tweet-id>_<number>.<extension>` evidence and associate verified surviving media; preserve gaps, unknown total, existing selections and conflicting evidence |

The Reddit filename policy checks both IDs against existing native post/attachment
records. It uses the original file observation, which can differ from the
surviving file after deduplication or conversion. A conflicting or malformed
explicit ID suppresses filename fallback. Unsupported namespaces and delegated
media services cannot borrow another service's identifier convention.

Proofs reference the original post-media evidence, file observation and file
match. Current library file generations, ZIP archive generations and unique
scene/image ownership are revalidated. A historical path match remains legacy
evidence; it does not add a verified content digest or assert fresh filesystem
verification. Multiple proofs for the same canonical media UUID form one
candidate. Different candidates, changed files/ownership and media-kind conflicts
remain review items. Existing attachment evidence also participates in ambiguity
detection. Bounded queries fail explicitly if they cannot inspect the complete
candidate set.

Every existing attachment decision is preserved, including explicit unlinks
and deliberately undecided reviews. For unresolved attachments, a unique current
file-backed candidate can be proposed. Applying the exact preview signature
records attachment-specific legacy evidence and a migration decision together
with gallery sync. It does not manufacture a capture association. Ambiguous and
unavailable attachments remain gaps, and the shared gallery service preserves
manual membership, exclusions, cover choices, metadata and deletion suppression.

`Backfill` requires a managed write transaction. It rechecks the preview before
writing and revalidates the selected file proofs and resulting gallery before
commit. A partial failure cannot commit even if its caller swallows the error.
A fresh preview after successful application is a no-op for those choices and
reuses the same gallery. `gallery.AlbumBackfill` now admits the reviewed signature
as durable work, with publication and hook delivery tracked separately. Its
[application API](native-ingestion.md#historical-source-album-backfill) supports
status, cancellation and explicit retry. Schema 1000040 retains existing file
jobs while admitting the new album job kind. 
