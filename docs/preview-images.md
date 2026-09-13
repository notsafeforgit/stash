# V3 preview images

V3 scene covers and marker stills use a shared image pipeline. HDR10/PQ and HLG
sources produce 10-bit AVIF, preferably with an SDR base and an HDR gain map.
The same source frame also produces a tone-mapped sRGB JPEG. SDR sources produce
SDR AVIF and JPEG. A 10-bit source is not considered HDR unless its transfer
function identifies it as HDR.

Cards, scene and marker tables, scene-marker detail thumbnails, lightbox posters
and the player poster consume the additive `preview_image` GraphQL field through
`PreviewImage`. AVIF gain maps can render the authored SDR base on an SDR display
or in a decoder without gain-map support. Supporting HDR decoders reconstruct
the HDR rendition according to available display headroom. Plain HDR AVIF is
offered only under `(dynamic-range: high)`; other displays receive the separately
tone-mapped JPEG. Browsers without AVIF support select JPEG automatically. A
failed rendition falls back to JPEG and then to the existing screenshot URL.

## Generation and requirements

Enable v3 using `--enable-v3-ui` / `STASH_ENABLE_V3_UI=true`. Generate a scene
cover, use **Set cover** at a player timestamp, or generate marker screenshots.
Existing covers need explicit regeneration to acquire HDR. Use **Set cover**
at a selected frame or **Generate default thumbnail** (20% into the video).
For a batch refresh, select scenes and use **Generate…** with only **Scene
covers** selected and **Replace existing artifacts** enabled. This replaces the
selected covers, including uploaded or scraped artwork. The legacy generator
did not persist cover timestamps, so matching a previously chosen frame requires
selecting it again.

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

Browser/OS HDR support and display headroom still determine visible highlight
brightness. Automated encoding tests verify depth, transfer and gain-map
metadata; perceived brightness and colour need review on real HDR hardware.
Animated WebP/video previews and timeline sprite sheets retain their existing
pipelines in this change.

## Storage and compatibility boundary

`pkg/previewimage` owns encoding, rendition metadata and storage independently
of the legacy paths. Generated files live below
`generated/preview_images/<recipe>/<scene-id>/<kind>/<source-key>/`.
The source key includes path, size, modification time and the artwork identity:
the existing cover blob checksum or the exact fractional marker timestamp.
An unchanged source's unrelated scene metadata edits do not invalidate artwork.
Replacing the source, switching primary files or replacing a cover in v2.5
invalidates its v3 renditions. No fork database migration is needed; scene reads
expose the already-stored cover checksum as transient internal metadata.

The manifest records the source timestamp in seconds as generation metadata
alongside its renditions. It lives with the generated files and adds no database
table or persistent scene field.

Rendition filenames are content hashes. An atomic manifest publishes only a
complete generation, and its revision appears in every URL. Regenerating after
installing a new encoder cannot reuse a cached URL for different colour
semantics. Endpoints resolve the current scene/marker first, check marker
ownership, and serve only manifest-listed files under normal authentication.
URLs honour the server's public mount point and responses use private caching.

`internal/manager/preview_images.go` is the compatibility adapter: it writes the
new pipeline's JPEG to the existing scene cover blob or legacy marker path.
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
