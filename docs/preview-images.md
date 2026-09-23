# V3 preview images

V3 scene covers and marker stills use a shared image pipeline. HDR10/PQ and HLG
sources produce 10-bit AVIF, preferably with an SDR base and an HDR gain map.
The same source frame also produces a tone-mapped sRGB JPEG. SDR sources produce
SDR AVIF and JPEG. A 10-bit source is not considered HDR unless its transfer
function identifies it as HDR.

Scene **Details** shows the primary file's dynamic range. **File Info** shows it
for each file, alongside bit depth, transfer, primaries, colour space and range.
These describe the source file, not the selected playback transcode. Missing
transfer metadata is shown as **Unknown**, even for 10-bit files; rescanning the
source fills in metadata that was not recorded by an older scan.

Cards, scene and marker tables, scene-marker detail thumbnails, lightbox posters
and the player poster consume the additive `preview_image` GraphQL field through
`PreviewImage`. AVIF gain maps can render the authored SDR base on an SDR display
or in a decoder without gain-map support. Supporting HDR decoders reconstruct
the HDR rendition according to available display headroom. Plain HDR AVIF is
offered only under `(dynamic-range: high)`; other displays receive the separately
tone-mapped JPEG. Browsers without AVIF support select JPEG automatically. A
failed rendition falls back to JPEG and then to the existing screenshot URL.

New scene cover generations also store card thumbnails bounded to **1280 pixels
on the longest edge**, without upscaling. They use the same AVIF/HDR policy and
have their own SDR JPEG fallback. Cards (including wall views and blurred
backgrounds) and table cells prefer `preview_image.thumbnail`; detail views,
lightboxes and player posters retain the full-size rendition. Older manifests
without thumbnails remain readable and fall back to their existing covers.

## Generation and requirements

Enable v3 using `--enable-v3-ui` / `STASH_ENABLE_V3_UI=true`. Generate a scene
cover, use **Set cover** at a player timestamp, or generate marker screenshots.
Existing covers need explicit regeneration to acquire HDR or stored card
thumbnails. **Regenerate selected cover** reuses the original file and exact
timestamp, or falls back to the default frame when no timestamp is known.
**Generate thumbnail from current** saves a new selection;
**Generate default thumbnail** explicitly replaces it with the frame 20% into
the primary video. Scene Details shows the saved time and source status.
For a batch refresh, select scenes and use **Generate…** with only **Scene
covers** selected and **Replace existing artifacts** enabled. The same applies
to Settings → Tasks → Generate with **Overwrite existing**. This regenerates
renditions while preserving known frame selections. A changed, missing or
detached source keeps its existing cover and requires a new selection (or
restoring an unavailable original). Individual scene IDs are logged; the job
reports a bounded failure summary after processing the remaining scenes.

Older v3 manifests can supply the timestamp when they still match both the
source video and saved cover, even when their rendition files are missing.
Legacy, uploaded and scraped covers without a recorded frame are shown as
unknown. Explicit regeneration with overwrite uses the frame 20% into the
primary video and records it for future refreshes. Scenes without any cover
receive the same default. Existing covers stay intact if extraction fails.

To discard custom frame selections in bulk, open **Generate… → Reset covers to
default** for selected scenes, or **Settings → Tasks → Generate → Reset covers
to default** for the whole library. The confirmation identifies the scope.
This queues a cover-only job that replaces selected, uploaded and scraped
covers with the default frame and regenerates full-size artwork and thumbnails.
It does not inherit other generation options or change their overwrite policy.
The reset is an explicit action, never a remembered generation default. Covers
and their timestamps are replaced together after successful generation; a
cancelled or failed run can leave some scenes completed and others unchanged.

Scene filters include **Cover frame** with **Default frame (20%)**, **Specific
frame**, and **Unknown / unrecorded**. Default means the recorded timestamp is
20% into the current primary video; another recorded timestamp or source is
specific, including frame zero. These describe the frame, not whether someone
picked it manually. Unrecorded legacy/uploaded artwork and scenes without a
cover are unknown, rather than assumed to use the default. Classification uses
the durable cover record and checksum; it does not inspect videos or rendition
manifests during list queries. These conditions compose with other filters and
can be saved or negated.

After filtering and selecting scenes, **Generate…** also offers **Apply to all
N matching**. This applies generation or a confirmed cover reset across every
page, including the list's search and context filters. The server freezes IDs
before queueing: scenes leaving the filter as their covers reset cannot cause
later rows to be skipped. Invalid filters and empty results fail without
expanding to the library. Active cover-filtered lists and counts refresh when
the job ends. The next operation starts with the explicit selection again.

Generating missing marker screenshots also backfills their v3 renditions because
marker timestamps are known. Regenerating with overwrite can upgrade a plain
AVIF after gain-map tooling is installed.

The encoder uses FFmpeg with PNG support and `zscale`/`tonemap` for HDR.
`avifenc` encodes plain AVIF, with FFmpeg's `libaom-av1` and AVIF muxer as a
fallback. `avifgainmaputil` from libavif 1.2 or later, available on `PATH`, enables
adaptive AVIF. Runtime images need `libavif-apps` on Alpine or `libavif-bin` on
Debian, including the separate stash-s6 wrapper, which copies only the Stash
binary. Jellyfin FFmpeg does not need its own libaom encoder when these tools
are installed. If gain-map encoding fails or is unavailable, the encoder tries
plain AVIF. If AVIF encoding fails, the completed SDR JPEG
remains usable. Failure of frame extraction falls back to the legacy generator
and is logged; HDR colour fidelity then depends on the legacy path.

HDR frames are decoded once and split before tone mapping. Both renditions stay
in floating point / 16-bit RGB intermediates until final encoding. HLG is
converted to BT.2020/PQ for the HDR still. The SDR rendition uses a 203-nit
reference white, Mobius tone mapping and BT.709 primaries with sRGB transfer.
Gain maps encode that SDR rendition as their base. The pipeline preserves the
source's ordinary HDR image signal, not Dolby Vision RPU or HDR10+ dynamic
grading metadata. Dolby Vision sources require a usable HDR10/HLG base layer.
Card thumbnails resize the same 16-bit SDR and HDR intermediates before
encoding, without decoding the video again or reducing HDR to an 8-bit JPEG.

Browser/OS HDR support and display headroom still determine visible highlight
brightness. Automated encoding tests verify depth, transfer and gain-map
metadata; perceived brightness and colour need review on real HDR hardware.
Animated WebP/video previews and timeline sprite sheets retain their existing
pipelines in this change.

## Storage and compatibility boundary

`pkg/previewimage` owns encoding, rendition metadata and storage independently
of the legacy paths. Generated files live below
`generated/preview_images/<recipe>/<scene-id>/<kind>/<source-key>/`.
New cover keys bind to the existing cover blob checksum. Already-generated
artwork remains visible when its source video changes or disappears. Marker
keys continue to include source path, size, modification time and the exact
fractional timestamp. Original path-based cover entries remain readable.
Replacing the cover itself invalidates its previous renditions.

Fork migration 7 adds `fork_scene_cover_sources`, independently of upstream's
schema version. It stores the cover checksum, historical source file ID, exact
timestamp and a versioned source fingerprint. File size and modification time
are checked against the filesystem, plus any MD5/oshash available at selection
time from a current scan. This avoids hashing entire videos during a backfill;
it is a source-change guard, not a cryptographic proof against edits that keep
all those properties unchanged. Derived phashes and newly added scan hashes
do not invalidate the selection. Paths and thumbnail recipe versions are not
part of this authored record: moving a file is safe, and removing the generated
cache does not remove the selection. A still-attached original file can be
used even after another file becomes primary.

Cover and origin writes share a transaction. Generation rechecks the artwork
and source before committing, so a concurrent cover edit or source replacement
is preserved. Ordinary cover edits invalidate mismatched provenance, and the
fork reconciles upstream-only edits when reopening the database. The upstream
scene schema and JPEG cover remain unchanged; an upstream server at the same
upstream schema version can continue using the database. The historical file
ID deliberately survives file deletion so v3 can explain why regeneration is
unavailable. No video or lossless-frame duplicate is stored.

The rendition manifest still records its timestamp for inspection, but the
database record owns future regenerations, independently of image format,
dimensions, encoder policy or cache lifetime.

Rendition filenames are content hashes. An atomic manifest publishes only a
complete generation, and its revision appears in every URL. Regenerating after
installing a new encoder cannot reuse a cached URL for different colour
semantics. Endpoints resolve the current scene/marker first, check marker
ownership, and serve only manifest-listed files under normal authentication.
URLs honour the server's public mount point and responses use private caching.

`internal/manager/preview_images.go` is the compatibility adapter: it writes the
new pipeline's full-size JPEG to the existing scene cover blob or legacy marker
path. Thumbnails are additional files; existing screenshot URLs are unchanged.
V2.5 operations, fields and URLs keep their existing types and behaviour. The
shared v3 renderer also accepts legacy-only artwork while libraries transition.
Deleting generated scene files includes the new store. Generated-file cleanup
removes obsolete cover/marker entries with the matching category, retaining
recent generations and entries whose source is temporarily offline.

The replacement boundary is the rendition catalog, not an extension change to
`Scene.paths.screenshot`. Additional image producers can use the same encoder,
store and UI component. Future sprite/animation work needs its own sampling and
timing model while sharing the colour and rendition policy. Once v2.5 is retired,
the JPEG compatibility writers and legacy-URL adapter can be removed without
relocating v3 assets or changing their display selection contract.

## References

- [libavif gain-map API](https://github.com/AOMediaCodec/libavif/blob/main/include/avif/avif.h)
- [libavif gain-map utility](https://github.com/AOMediaCodec/libavif/tree/main/apps/avifgainmaputil)
- [AVIF 1.2 specification](https://aomedia.org/docs/AV1%20Image%20File%20Format%20%28AVIF%29%20v1.2.0.pdf)
- [CSS dynamic-range media feature](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/At-rules/@media/dynamic-range)

## Anonymous sharing

Shares reuse this manifest and its existing files through a share-authorized
route, with the same `PreviewImage` rendering component for cards, lightbox
posters and playback. Each variant request checks the share and pinned file as
well as the current manifest revision; public responses remain `no-store`.
Original images are served unchanged and videos retain the normal compatibility
streaming options. Sharing creates no separate image rendition cache and does not
strip embedded metadata. See [sharing](sharing.md) for access scope and downloads.
