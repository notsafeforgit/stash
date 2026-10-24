# Native source albums

The native gallery repository can maintain a logical gallery for a source
post, an evidenced Twitter thread, or overlapping source albums, with ordered
image/video attachments and explicit membership choices.

Gallery pages open on **Media**, which browses images and scenes together and
plays both kinds in one viewer. It reads the existing `galleries_images` and
`scenes_galleries` membership, including manual additions. Selected attachments
establish source order; shared Twitter thread galleries follow post ID order,
then attachment order. Repeated media appears once in gallery membership, while
the **Source albums** inspection retains repeated positions and missing slots.
Other gallery members follow in file order. Separate **Images** and **Scenes**
tabs retain the normal list filters and editing controls. Video-only galleries
use a scene screenshot as their cover, and card counts include both kinds.

`Gallery.media(offset, limit)` returns a bounded mixed page with source position
references and a signature of gallery membership and selected ordering. The
viewer stops if successive pages disagree. Reads use gallery membership indexes
and the selected source lists without reconstructing capture payloads. This is
a read API/UI change; no schema migration or catalog reimport is required.
`pkg/archive.ExtractCapturedAlbum` derives the source list under
`captured-attachments-v2`. Native intake and the application-authorized
[historical album backfill](native-ingestion.md#historical-source-album-backfill)
use the same gallery service. Ordered inspection, association review and mixed-
media playback are available on source-post and gallery pages. Production
activation remains governed by the
[transition plan](native-archive-transition-plan.md#source-post-albums-and-galleries).

Reddit links to Imgur albums retain the album API's original ordered media list
in `album.imgur_media` (version 1) before gallery-dl expands it into files. Each
member uses its own `native:imgur` media ID; the album ID is not a file identity.
The original album list and metadata live in the shared capture body rather
than being repeated in every member's stored patch. Historical captures without
this evidence marker retain their existing partition and replay signatures.
The server binds that list to the enclosing Reddit post's album link, including
crossposts and Imgur gallery redirects. Captured API rendition URLs identify the
download; `num`, output filenames and download order do not establish membership.
Direct Imgur GIFV links retain their existing URL identity when the same API
media item resolves to an MP4. These readers require no schema migration.

Gallery detail pages show a **Gallery source** summary before the cover and
metadata, on desktop and mobile. An evidenced post association labels the
gallery **Source-post album** and links to its parent posts. Otherwise the
summary distinguishes **Folder gallery**, **ZIP gallery**, and **Manual gallery**
from their actual backing references, showing folder and archive paths. A failed
lookup stays unresolved rather than implying a manual gallery. Merged galleries
can retain multiple parent posts; the Source albums tab shows all of them and
their individual attachment order. Shared media can retain titles from other
posts. The summary and tab reuse one paginated lookup, and viewing them changes
no metadata or membership.

Twitter/X posts have text, not a separate source title. Post headings and newly
created albums use the first nonempty line or sentence of that text, bounded to
200 characters. A media-only Twitter post uses its source post ID. Other sites
prefer their actual title and fall back to the same caption excerpt. These are
display labels: the complete original text stays in the source record and album
description. Creating a label never fabricates a title in captured metadata, and
later synchronization preserves manually edited gallery titles.

## Overlapping source albums

Source-post albums sharing at least one library image or scene consolidate into
the oldest gallery. The rule is transitive: A sharing with B, and B sharing with
C, produces one gallery containing the union. Manual, folder and ZIP galleries
are outside this rule. Files, media identities, and source posts are unchanged.
Existing UUID links redirect to the survivor; each post retains its caption and
attachment list. Mixed playback keeps the existing source publication/attachment
ordering and shows each member once.

Schema 1000117 adds deduplicated alternate gallery titles. `Gallery.alternate_titles`
excludes the current main title; the Gallery source card shows those alternatives
with an expandable remainder. Tags, performers, URLs and chapters are combined.
The old generated “Source album” placeholder is not an alternate post title; an
inherited placeholder yields to a caption title when albums consolidate.
The oldest selected cover is retained. Conflicting curated code, photographer,
studio, rating or custom fields prevent an automatic merge instead of discarding
those values. Membership exclusions absent from the union are retained.

Normal source synchronization checks overlaps after updating membership, so late
downloads can join previously separate albums. Shared members still required by
another post survive refreshes. For existing albums, application-authenticated
`GET /api/v3/archive/entities/{uuid}/gallery-consolidation` previews the entire
component, counts and conflicts. `POST` to the same URL with its `signature`
applies one transaction. A changed component rejects the stale preview. Repeating
the operation with a fresh preview is harmless; old UUIDs resolve to the result.
The schema upgrade itself does not perform a library-wide consolidation.

## Gallery covers

Any member image or scene can be the gallery's selected cover. Use **Set as
gallery cover** from a Media item's menu or from an image/scene context menu
inside the gallery. Only that gallery's cover is refreshed; its media list and
playback position stay in place. Resetting the cover restores the automatic
image selection, falling back to a scene screenshot for video-only galleries.

Schema 1000108 stores this choice once in `gallery_covers`, keyed by gallery ID
and the member's canonical media UUID. It preserves existing image choices
without changing gallery revisions during migration and removes the old image
membership flag. Reaffirming membership retains the choice. Removing its member
clears it; a same-kind merge follows the destination when it remains a member.
Source-album refreshes protect a selected scene just as they protect an image.

`Gallery.cover` resolves to `{ image, scene }` with one non-null member.
`setGalleryCover` takes `gallery_id` and exactly one of `image_id` or `scene_id`;
the member must belong to that gallery. Successful changes notify plugins of
the `cover` field. `/gallery/{id}/cover` uses the same selection and revalidates
scene artwork when it changes independently of gallery metadata.

## Twitter threads and replies

New native captures retain the exact numeric conversation, parent-post, author
and reply-target author IDs. The producer preserves the reply-target ID before
gallery-dl's normal transformation drops it. Root and parent links navigate to
local source posts when present, or to the source website when absent. The
**Thread and replies** section lists captured conversation posts oldest first,
25 at a time. It does not create placeholder posts or fetch missing ancestors.

Self-replies by the same numeric publishing account share one source gallery.
Each post retains its own text, captures, attachment order and associations;
reused media appears only once in the gallery. A thread can start with two
single-media posts, and replies can arrive before the root. Other authors'
replies remain navigable but do not automatically join that creator's gallery.
Missing or conflicting identity evidence requires review. Separate source
galleries consolidate when they share media; thread evidence alone does not
merge previously separate galleries. Manual gallery choices, exclusions and
deletions remain protected. Thread planning is bounded to 256 posts.

Schema 1000105 begins recording these relationships on incoming native captures;
it does not scan historical catalogs or infer reply relationships from filenames.
The read-only endpoint is `GET /api/v3/archive/posts/{uuid}/thread`, with optional
numeric `after` and bounded `limit` parameters. The database, including thread
relationships and shared gallery associations, remains part of normal native
backups.

## Evidence accepted by the parser

| Source | Post identity | Ordered attachment evidence |
| --- | --- | --- |
| Reddit | `id`, qualified as `native:reddit` | `gallery_data.items[].media_id`, with optional type hints from `media_metadata` |
| Reddit single media | `id`, qualified as `native:reddit` | A direct `i.redd.it`, `preview.redd.it` or `v.redd.it` post URL, or explicit Reddit video stream fields; one attachment without an album declaration |
| Reddit reached through another media host | The `_reddit` parent's `id` | The parent's own gallery or direct media evidence |
| Reddit crosspost | The crosspost's own `id` | The referenced parent's gallery or direct media evidence, retaining the evidence pointer |
| Twitter | Matching `tweet_id`, `rest_id`, or `id_str`, qualified as `native:twitter` | The full `extended_entities.media` list, including the raw API `legacy` wrapper when present |

The parser returns the qualified post reference, policy version, evidence JSON
pointer, and normalized attachment manifest. Its capture UUID is assigned only
when the caller records the matching capture in a transaction. Source IDs are
exact; contradictory aliases and malformed lists are reported for review. No
directory, caption, filename, or local download count identifies an album in
the automatic capture parser. Explicit historical filename recovery is described
below.

The gallery-dl 1.32.15-dev extractor code used for the local fixtures exposes two
relevant distinctions:

- Reddit increments its download number only for gallery items that yield a URL.
  Failed items can therefore make that number differ from the source position.
  The manifest uses the original gallery list and retains unavailable items.
- Twitter's `count` is the number of extracted output files. Configuration can
  omit media or add video previews and card/article images. `count` and `num`
  alone cannot prove the original post's attachment list or its order. The
  ordinary transformed Twitter capture does not include that full list, so the
  native adapter now captures it before file filtering/rendition expansion and
  preserves each output's media ID through the transformation.

Consequently, older Twitter captures containing only `num`, `count`, filenames,
or a per-file media ID remain unresolved for automatic gallery creation. They
are not guessed into complete source lists. Catalog import must report missing
evidence, use a verified retained source list where available, or offer review.
Other extractors can supply the existing typed attachment-manifest contract;
this parser does not assume their file counters have post-level semantics.

## Recovering historical Twitter albums

The application-authorized `legacy-twitter-filename-v1` album backfill policy
supports the archive's known historical `<tweet-id>_<number>.<extension>` naming
convention. It requires a matching retained `native:twitter` post ID and original
catalog file evidence. A generic download counter or an unrelated filename is
insufficient. The original observation supplies order even when deduplication or
conversion left the file under a different name or another post's directory.
Existing registered file matches must still verify the file generation and its
unique current media owner before the backfill links it.

The policy fills missing source lists only. `_1`, `_3` become zero-based positions
0 and 2, with a visible gap. The list is always partial, its expected total stays
unknown, and a lone `_1` does not create an album. A later ordinal establishes
album evidence without proving that all earlier or later attachments were
downloaded. Slots have `legacy:twitter:filename` references, not invented Twitter
media IDs. Conflicting explicit IDs, ambiguous media, changed files, existing
captured lists and saved choices are preserved for review. No usable local
matches means no new empty gallery.

Recovery stores one immutable attachment list and a migration selection in the
normal native tables, alongside attachment-to-media proof references. It creates
no source capture, duplicated post payload or media file. The selection's capture
reference is an existing **metadata anchor**, not a claim that this capture
contained the recovered list. New galleries use retained original post text,
then a meaningful title, then the tweet ID. Existing gallery and media titles
are preserved. The current recovered order is visible in Source albums; the
source-list picker offers actual captured alternatives. If a later captured list
conflicts with recovered slot identities, ordinary selection review resolves it;
the backfill does not equate ordinals with native media IDs.

Choose **Twitter file names** in album review, or use
`stash-backfill-source-albums prepare --policy legacy-twitter-filename-v1` with
explicit post IDs. Bulk discovery with this policy includes posts without a
selection; previews and durable apply jobs remain separate. Publication, replay,
manual membership protection and after-success notifications use the existing
album worker. These records are included in normal database backups.

### One-time recovery of unidentified historical Twitter files

`cmd/recover-twitter-albums` is isolated, offline upgrade tooling for files
downloaded before native ingestion. It is not part of scanning or scraping and
can be removed with the legacy import tools. It handles missing Twitter post IDs
and anonymous catalog fragments that were created separately for each file.

Supply a reviewed JSON array with `tweet_id` and `evidence` entries containing
the current `file_uuid`, `media_uuid` and `generation`. Original, deduplicated
locations additionally require their retained `observation_uuid` and `match_uuid`.
The command rechecks the file owner, generation and actual file size; the name
must match that tweet's numbered attachment in a historical `, twitter` folder.
It rejects changed files. Reused ordinals retain a confirmed post association
without selecting either file into that ambiguous slot. It only consolidates anonymous
fragments whose complete retained filename evidence agrees on this same tweet,
preserving source text and refusing explicit selection, gallery or unlink choices.
An automatic unordered gallery already made by the confirmed-post-media repair
is retained as the destination, including its membership choices.

Stop Stash and retain a current backup first. Run the command with `--database`,
`--input`, the reviewed input's `--expected-sha256` and
`--path-map /library/prefix=/host/prefix`. Omit `--apply` for a preview. Applying
recovers identities and ordered partial lists; it never deletes or renames media.
When no source text was retained, the only new observation is explicitly labeled
`filename-recovery`, has an unknown original observation date, and uses the post
ID as its title. Existing captured descriptions are reused.

After restarting Stash, submit the returned post UUIDs through the ordinary
album-backfill API. That durable worker creates mixed image/scene galleries and
delivers the usual notifications. Source position gaps and unknown totals remain
explicit, and rerunning the identity repair reuses the already identified posts.

The native association repository can retain a post-to-media link even when no
ordered list survives. Schema 1000036 permits legacy/review evidence without an
attachment or capture reference, and retains only the references actually known.
This preserves reposts and older catalog appearances without duplicating media
or inventing a capture, attachment ID, or album position. Such evidence does not
automatically select attachment media or create an album. Confirmed post-to-media
decisions can establish an album through the recovery below.

## Albums with confirmed media and unknown order

The `linked-post-media-v1` backfill groups two or more distinct, active library
items already linked to the same post. It supports images and scenes together,
including older catalog posts whose original attachment list was not retained.
Choose **Confirmed post media** in album review, or use
`stash-backfill-source-albums prepare --policy linked-post-media-v1`.

The saved partial album records that the post has multiple media. Its original
positions and expected total remain unknown; no attachment IDs or captures are
invented. Existing post decisions supply membership, and an existing capture
supplies the initial title, description and date. Files display in library file
order until actual source positions become available. A later automatic source
list can add known positions without dropping confirmed, unordered members.

Filename/title resemblance and retained evidence alone do not qualify. Merged
copies count once. Conflicting links, unselected retained source lists and
attachment rejections require review; pinned selections and gallery exclusions
remain protected. Removing a post link updates automatic membership while
preserving manual members and covers. No website requests are made by this
backfill, and no media files or existing titles are changed.

## Ordering and incomplete downloads

Manifest completeness means every source slot has an identity. It says nothing
about whether media files have downloaded. Deleted or unavailable items retain
their source IDs; unidentified slots leave gaps with the known count. Later
positions never shift to hide missing evidence. A source-declared Reddit album
with an unknown total retains its album evidence even with one known item or
temporarily missing gallery data. A complete list containing only one attachment,
or a declared total of one, does not create a gallery even when Reddit marks the
post as a gallery. Its post metadata and attachment captions remain retained.
Album evidence alone does not create a local gallery: at least two distinct
local images/scenes must be linked, including across a captured self-reply
thread. Repeated slots or replies pointing to the same media count once. A
partial album waits for another local item; source attachment records and their
gaps remain available independently. Ordinary single-media posts are not
automatically galleries.

After membership changes, an automatic source gallery with fewer than two
items is removed in the same transaction. Its posts, attachments and media
remain intact, and its automatic association is released so later downloads
can create a gallery again. Explicit gallery associations, selected covers and
manual membership choices are preserved, as are manual/folder/ZIP galleries.
For older data, the application-authenticated endpoint
`POST /api/v3/archive/source-galleries/prune` accepts
`{"galleries":[{"gallery_uuid":"<uuid>","revision":1}]}` with 1–100 entries.
Each gallery is rechecked inside the write transaction; changed revisions and
galleries with two or more items are retained. Pruning does not delete or
rename media files or disable scrapes.

Source galleries inherit the distinct performers assigned to their member
images and scenes. Membership changes, metadata imports and ordinary performer
edits refresh only the affected galleries in the same transaction. Explicit
gallery performer choices (including clearing the field) take precedence;
folder and manual galleries are unchanged. Performer merges and removal retain
inheritance instead of turning the derived list into a manual choice.

To repair source galleries created before inheritance was enabled, send
`POST /api/v3/archive/source-galleries/performers/reconcile` with
`{"gallery_uuids":["<gallery UUID>"]}` (1–100 distinct UUIDs per request).
This application-authenticated operation recalculates performers from current
image/scene members in one transaction. Each result reports `updated`,
`unchanged`, `protected`, or `not_source`. It preserves explicit choices and
manual/folder galleries, leaves membership untouched, and keeps updated fields
in inheritance mode. Retrying a batch is safe; unchanged values create no new
decisions or notifications. Source tracking must be enabled.

Account ownership does not itself identify everyone depicted in an account's
media. For a profile whose downloads should receive a performer by default,
configure a constant `performers` mapping in its existing image and scene
metadata policy. This is opt-in per collection; aggregator feeds need no such
mapping. The normal metadata rules protect explicit media edits.

Reddit animations leave their media-kind hint unknown because extraction may
yield GIF or MP4. Local file classification and attachment-to-media association
remain separate operations. Source evidence cannot fabricate a playable image
or scene. Repeated slots can reference the same attachment without copying its
file, and another post can reference the same existing media in its own gallery.

The core selection service merges compatible partial lists monotonically and
requires review for contradictory positions, counts, or types. Gallery sync
preserves manual additions, exclusions, cover choices, protected fields, and
explicit suppression after unlink/deletion. These choices are described in
[native schema 1000010](native-schema.md).

Focused parser tests cover source order, repeated slots, unavailable and missing
items, crosspost/parent identity, exact IDs, conservative media hints, reduced
payloads, bounds, replay, and rejected file-counter inference. SQLite integration
tests feed parsed lists through captured evidence, selection, and gallery sync,
then add an image and a later video to the same gallery and replay without
duplicating membership. Native producer tests also verify association using
captured CDN URLs/attachment IDs, including output numbers that differ from the
source position. Single-media file ingestion links its source without creating
a gallery. Other extractor and external-host associations remain unfinished.

## Erome albums

Native gallery-dl intake identifies an Erome post by its source `album_id`,
retains the album title and canonical `/a/<id>` URL, and records photos and videos
in their observed page order. It uses the existing version-1 `web_media`
manifest and URL-qualified attachment references. Local filenames and output
positions are not identities. Captures share the album body while retaining
per-file details separately.

HTML traversal cannot prove that inaccessible items are absent, so membership
remains partial. Two or more observed attachments declare an album; verified
library files join its gallery through ordinary source-gallery synchronization.
Single-file albums retain source links without creating an empty gallery.
The extractor's timestamp comes from a media URL version parameter and is not
treated as a publication date. An uploader name does not assign a performer.

Use a traversal profile with `stash-gallery download <album-url>`. Native Erome
archive keys include the source URL identity, so reordered positions cannot skip
different files. Native filenames also contain that identity, preventing an
existing file at an old position from being linked to a different attachment.
Custom filename and archive formats must retain `{web_media_id}`.
Older position-based gallery-dl entries remain intact and are
not accepted as proof that a native attachment completed. Interrupted downloads
use the existing durable requests, outbox, completion receipts and archive retry
path; this support requires no database migration.

## Download reports in the application

Each evidenced attachment offers Download history. Its card shows the latest
recorded transfer separately from library associations and registered files.
Download completion is a producer report; file checks have their own queued,
running or terminal status. Neither claims that previously registered bytes are
still online. Missing reports do not make older library media unavailable.

History groups a transfer's original start and outcome in one entry, ordered by
its first received report. Delayed delivery does not move that entry between
pages. Source collection/root labels, observed start/finish times and receipt
times are visible; report identifiers expand on request. Missing local files,
excluded media, failures and attempts without a completion report have distinct
labels. Older transfers load 25 at a time; refresh starts a new current read.

Cards share batches of at most 25 distinct attachments. Visible groups refresh
every 15 seconds without overlapping requests; hidden pages and offscreen groups
suspend polling. A failed refresh keeps the last successful check with an error
message. Navigation cancels obsolete reads. Viewing reports never changes links,
metadata, source runs or files.
## Deleting galleries and their media

Every gallery deletion entry point uses the same confirmation. The default
removes only the gallery and its memberships, preserving images, scenes and
files, including for folder- and ZIP-backed galleries. Source post metadata and
media-to-post associations remain available. A deleted source album is not
automatically recreated on the next scrape or reconciliation.

**Also delete the gallery’s images and scenes** removes its actual member
entities from the library. The confirmation counts each image and scene once,
even when multiple selected galleries share it. Media also belonging to galleries
outside the selection is kept by default; an explicit additional choice includes
that shared media. Deleting an entity removes it from every gallery. Other files
in the same folder are not implied members and are left alone.

**Also delete the media files from disk** is a separate option, available only
with media deletion. It uses the normal recoverable file-deletion/trash handling,
scene stream shutdown and post-success hooks. Generated supporting files have
their own option. An archive used by another gallery or retained media stays on
disk; individual members cannot be removed from a retained ZIP. Empty backing
folders may be removed, but library roots are preserved.

`galleryDeletePreview(ids: [ID!]!)` returns member and shared-member counts for
native gallery UUIDs. `galleryDestroy` accepts `delete_media` and
`delete_shared_media` (both default false), alongside `delete_file` and
`delete_generated`. File deletion, file-entry deletion and shared-media deletion
require `delete_media`. Planning is repeated inside the mutation transaction so
membership and sharing are checked against the state actually being changed.
