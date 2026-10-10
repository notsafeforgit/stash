# Native source albums

The native gallery repository can maintain a logical gallery for a source
post or an evidenced Twitter thread, with ordered image/video attachments and
explicit membership choices.

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
Missing or conflicting identity evidence requires review. Already separate
galleries are not silently merged, and manual gallery choices, exclusions and
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

The native association repository can retain a post-to-media link even when no
ordered list survives. Schema 1000036 permits legacy/review evidence without an
attachment or capture reference, and retains only the references actually known.
This preserves reposts and older catalog appearances without duplicating media
or inventing a capture, attachment ID, or album position. Such evidence does not
automatically select attachment media or create an album. Automatic galleries
still require a supported source list and appropriate media associations.

## Ordering and incomplete downloads

Manifest completeness means every source slot has an identity. It says nothing
about whether media files have downloaded. Deleted or unavailable items retain
their source IDs; unidentified slots leave gaps with the known count. Later
positions never shift to hide missing evidence. A source-declared Reddit album
remains an album even with one known item or temporarily missing gallery data.
Ordinary single-media posts are not automatically galleries.

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
