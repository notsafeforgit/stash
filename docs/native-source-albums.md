# Native source albums

The native gallery repository can maintain one logical gallery for a source
post, with ordered image/video attachments and explicit membership choices.
`pkg/archive.ExtractCapturedAlbum` derives the source list under
`captured-attachments-v2`. These are core services; production ingestion, catalog
backfill, and the ordered gallery UI still need their integrations under the
[transition plan](native-archive-transition-plan.md#source-post-albums-and-galleries).

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
directory, caption, filename, or local download count identifies an album.

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
