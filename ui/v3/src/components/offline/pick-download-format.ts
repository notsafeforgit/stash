/**
 * Per-device format selection for the offline-download endpoint.
 *
 * The backend `/scene/{id}/download.mp4` accepts a `mode` query param
 * (`copy` / `copy-aac` / `h264` / `hevc` / `auto`); this module picks
 * the best mode for the current device + user's max-resolution
 * preference. The "best" choice prefers no-re-encode paths when they
 * preserve quality and play on the device, then falls through to the
 * most efficient transcode the device can decode and the server can
 * encode.
 *
 * Decision tree:
 *
 *   1. No scaling needed AND device decodes (source video + audio) in
 *      MP4         → "copy"
 *   2. No scaling needed AND device decodes source video in MP4
 *                  → "copy-aac" (audio re-encoded to AAC; source-audio
 *                                support irrelevant)
 *   3. Else        → most efficient transcode codec the device decodes
 *                    AND the server can encode, in priority order:
 *                      AV1    if device.av1  && server.av1   (best, ~30%
 *                                                             smaller than
 *                                                             HEVC at the
 *                                                             same visual
 *                                                             quality)
 *                      HEVC   if device.hevc && server.hevc
 *                      H.264  fallback (every device decodes, every
 *                             ffmpeg builds with libx264)
 *
 * AV1 and HEVC are gated on server-side HW encoder availability
 * (advertised via `serverCapabilities.downloadFormats`). HW AV1 is
 * rare today (Intel Arc / Meteor Lake+, RTX 40-series+); on servers
 * without it the decision falls through to HEVC or H.264.
 *
 * Resolution semantics: the user's max-resolution preference is a
 * **ceiling**. If the source is shorter (vertical) than the ceiling,
 * we don't artificially upscale — we use Original. The "would scale"
 * test is `source_height > ceiling_height`; only then does the
 * codec-copy fast path get rejected and a transcode picked.
 */

import { StreamingResolutionEnum } from "src/core/generated-graphql";

export type DownloadMode = "copy" | "copy-aac" | "h264" | "hevc" | "av1";

export interface DeviceCaps {
  /** Source video AND audio decodable in MP4 by the device — drives
   *  the pure-copy path. From `useCodecsDecodableInMp4(v, a)`. */
  videoAndAudioInMp4: boolean;
  /** Source video alone decodable in MP4 by the device. From
   *  `useVideoCodecDecodableInMp4(v)`. Drives the copy-aac path,
   *  where the audio is always AAC regardless of source. */
  videoInMp4: boolean;
  /** Whether the device can decode HEVC at all. Drives transcode
   *  target selection. */
  decodesHevc: boolean;
  /** Whether the device can decode AV1 at all. Drives transcode
   *  target selection — AV1 is preferred over HEVC when both
   *  device and server support it (smaller files for the same
   *  visual quality). */
  decodesAv1: boolean;
}

export interface ServerCaps {
  /** Whether the server has an HEVC encoder available (HW or libx265). */
  hevcAvailable: boolean;
  /** Whether the server has an HW AV1 encoder available. */
  av1Available: boolean;
}

export interface PickFormatInput {
  /** Source dimensions — used only to decide whether the user's
   *  max-resolution preference would actually cause a downscale. */
  source: {
    width: number;
    height: number;
  };
  device: DeviceCaps;
  server: ServerCaps;
  /** User's preference from settings. `ORIGINAL` means "do not scale". */
  maxResolution: StreamingResolutionEnum;
}

export interface PickFormatResult {
  mode: DownloadMode;
  /** Resolution to send on the URL. For copy / copy-aac this is
   *  always `ORIGINAL` (codec-copy can't scale); for transcode it
   *  passes the user's preference through. */
  resolution: StreamingResolutionEnum;
  /** Post-pick effective output height the user can show in the UI
   *  ("Will download as 1080p / Original"). Estimate only — the
   *  server's actual scale filter rounds and may differ by ≤1 px. */
  effectiveHeight: number;
}

/**
 * Frontend mirror of `pkg/models/resolution.go:streamingResolutionMax`.
 * The `min` height for each `StreamingResolutionEnum` value — the
 * maximum the source can be without being scaled down to that bucket.
 */
const RESOLUTION_HEIGHT: Record<StreamingResolutionEnum, number> = {
  [StreamingResolutionEnum.Low]: 240,
  [StreamingResolutionEnum.Standard]: 480,
  [StreamingResolutionEnum.StandardHd]: 720,
  [StreamingResolutionEnum.FullHd]: 1080,
  [StreamingResolutionEnum.FourK]: 2160,
  [StreamingResolutionEnum.Original]: 0, // sentinel: no cap
};

export function pickDownloadFormat({
  source,
  device,
  server,
  maxResolution,
}: PickFormatInput): PickFormatResult {
  const cap = RESOLUTION_HEIGHT[maxResolution] ?? 0;
  const sourceShort = Math.min(source.width || 0, source.height || 0);
  const willScale = cap > 0 && sourceShort > cap;

  // 1. Pure copy — fastest path. No scale possible (codec-copy can't
  //    filter), so only when the user's resolution cap doesn't bite.
  if (!willScale && device.videoAndAudioInMp4) {
    return {
      mode: "copy",
      resolution: StreamingResolutionEnum.Original,
      effectiveHeight: source.height,
    };
  }

  // 2. Video-copy + AAC re-encode. Same scale constraint on video
  //    (still codec-copied), but source-audio support no longer
  //    matters — AAC is always produced.
  if (!willScale && device.videoInMp4) {
    return {
      mode: "copy-aac",
      resolution: StreamingResolutionEnum.Original,
      effectiveHeight: source.height,
    };
  }

  // 3. Transcode. Pick the most efficient codec the device decodes
  //    AND the server can encode. Priority: AV1 > HEVC > H.264.
  let targetMode: DownloadMode = "h264";
  if (device.decodesAv1 && server.av1Available) {
    targetMode = "av1";
  } else if (device.decodesHevc && server.hevcAvailable) {
    targetMode = "hevc";
  }

  // For transcode, the effective height is min(source, cap). When
  // cap=0 (Original) we keep source. When source < cap we don't
  // upscale.
  const effective = cap > 0 ? Math.min(source.height || 0, cap) : source.height;
  return {
    mode: targetMode,
    resolution: maxResolution,
    effectiveHeight: effective,
  };
}

/** Build the `?mode=…&resolution=…` querystring for the download URL. */
export function downloadQueryString(pick: PickFormatResult): string {
  const params = new URLSearchParams();
  params.set("mode", pick.mode);
  if (pick.resolution !== StreamingResolutionEnum.Original) {
    params.set("resolution", pick.resolution);
  }
  return params.toString();
}
