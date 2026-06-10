/**
 * Pure helpers for picking, filtering, and persisting the active scene-
 * player source. Lives apart from `scene-player.tsx` so the component
 * file stays focused on the player shell + state machine.
 *
 * No React, no DOM, no Apollo — just URL/string math over
 * `RawStream` / `PlayerSource` shapes plus a single `localStorage`
 * touch in `getPreferredSource`.
 */

import type { VideoPlayerStore } from "@videojs/react";
import { isHlsPlaylist, makeHlsStrategy } from "./hls";
import { canPlaySource, type PlayerSource } from "./player-utils";

export const QUALITY_STORAGE_KEY = "stash-player-quality";

export type RawStream = {
  url: string;
  mime_type?: string | null;
  label?: string | null;
};

// Backend-served path suffixes that v3 plays. Direct stream serves the
// source bytes directly (fast path, zero CPU); the HLS master playlist
// suffixes are the entry points for the segmented HLS pipelines
// (transcode, full codec-copy fMP4, video-copy + AAC-transcode fMP4).
// Progressive MP4/WebM/MKV transcodes and DASH have all been removed —
// see backend `pkg/ffmpeg/stream_segmented.go` and
// `internal/manager/scene.go`.
const ALLOWED_PATH_SUFFIXES = [
  "/stream",
  "/stream.master.m3u8",
  "/stream.fmp4.master.m3u8",
  "/stream.fmp4.aac.master.m3u8",
];

/**
 * Map an HLS master-playlist source URL to the backend stream-type
 * identifier baked into the segment cache dir (see
 * `pkg/ffmpeg/stream_segmented.go` — `StreamType.Name`). Used by the
 * client-initiated `streams.stop?keep_type=...&keep_resolution=...`
 * beacon so a resolution swap can tear down the previous transcode
 * without racing the new one's first segment request. Returns null
 * for non-HLS sources or URLs that don't parse.
 */
export function hlsStreamTypeName(src: string): string | null {
  try {
    const path = new URL(src, window.location.origin).pathname;
    if (path.endsWith("/stream.fmp4.aac.master.m3u8"))
      return "hls-copy-fmp4-aac";
    if (path.endsWith("/stream.fmp4.master.m3u8")) return "hls-copy-fmp4";
    if (path.endsWith("/stream.master.m3u8")) return "hls";
    return null;
  } catch {
    return null;
  }
}

/** Extract the `?resolution=` value from a stream URL, or "" if absent. */
export function streamResolution(src: string): string {
  try {
    return (
      new URL(src, window.location.origin).searchParams.get("resolution") ?? ""
    );
  } catch {
    return "";
  }
}

export function filterSources(
  streams: RawStream[],
  canDecode: boolean,
  canDecodeVideo: boolean,
): PlayerSource[] {
  return streams
    .filter((s) => {
      // `blob:` URLs come from the offline player (OPFS file →
      // `URL.createObjectURL`). They're inherently file-like — already
      // sandboxed, already on-device — so the path-suffix safety check
      // doesn't apply. Pass them through unconditionally and let the
      // downstream `canPlaySource` filter handle browser-decode gating.
      if (s.url.startsWith("blob:")) return true;
      try {
        const pathname = new URL(s.url).pathname;
        return ALLOWED_PATH_SUFFIXES.some((suffix) =>
          pathname.endsWith(suffix),
        );
      } catch {
        return false;
      }
    })
    .map((s) => {
      // The backend labels the original-resolution HLS transcode just
      // "HLS" — visually indistinguishable from the codec-copy fMP4 remux
      // ("HLS (remux)") for users who can't tell from the label alone
      // whether their fallback is a re-encode or a passthrough. Rewrite
      // to "HLS (transcode)" so the remux/transcode pair reads as
      // parallel options. Keep "HLS (remux)" / "HLS (remux + AAC)" as
      // the backend wrote them.
      let label = s.label ?? undefined;
      if (label === "HLS") label = "HLS (transcode)";
      return {
        src: s.url,
        type: s.mime_type ?? undefined,
        label,
      };
    })
    .filter((source) => {
      if (isHlsPlaylist(source.src)) {
        try {
          const path = new URL(source.src).pathname;
          // Full codec-copy fMP4 — requires both video AND audio source
          // codecs to be MSE/MMS-decodable in fMP4 (Opus inside MP4 fails
          // on iOS, for instance). Gated on the combined `canDecode`.
          if (path.endsWith("/stream.fmp4.master.m3u8")) return canDecode;
          // Video-copy + AAC-transcode fMP4 — only the source video codec
          // has to be MSE/MMS-decodable in fMP4; the audio is always AAC,
          // which every modern browser plays. Gated on `canDecodeVideo`.
          // Stays out of the source list for files whose video codec
          // can't ride in fMP4 at all (e.g. VP9 MKV) — those still need
          // the full transcode.
          if (path.endsWith("/stream.fmp4.aac.master.m3u8"))
            return canDecodeVideo;
        } catch {
          /* malformed URL — fall through to allowed */
        }
        return true;
      }
      return canPlaySource(source);
    });
}

function findSourceByPath(
  sources: PlayerSource[],
  pathSuffix: string,
): PlayerSource | undefined {
  return sources.find((s) => {
    try {
      return new URL(s.src).pathname.endsWith(pathSuffix);
    } catch {
      return false;
    }
  });
}

/**
 * Find the source whose URL carries `?resolution=ORIGINAL` — i.e. a
 * transcode targeting the source's own resolution. This is the "full
 * resolution but re-encoded" tier that fills in when direct and remux are
 * both absent (e.g. Safari with no hardware decode for the source codecs;
 * Chrome on a scene where the backend chose not to emit a direct stream).
 */
function findOriginalResolutionTranscode(
  sources: PlayerSource[],
): PlayerSource | undefined {
  return sources.find((s) => {
    try {
      return new URL(s.src).searchParams.get("resolution") === "ORIGINAL";
    } catch {
      return false;
    }
  });
}

// Saved labels that mean "best available quality": direct stream and
// the two fMP4 codec-copy variants (full remux, video-copy + AAC). All
// three preserve the source video bitstream — the remux variants differ
// only in whether the audio rides along untouched or gets re-encoded to
// AAC — and which one is offered varies per browser. The user's "give
// me the best" preference should follow them across scenes/devices, so
// they share a virtual preference slot at the persistence layer.
export const BEST_QUALITY_LABELS = new Set([
  "Direct stream",
  "HLS (remux)",
  "HLS (remux + AAC)",
]);

export function getPreferredSource(
  sources: PlayerSource[],
): PlayerSource | null {
  if (!sources.length) return null;

  const direct = sources.find((s) => {
    try {
      const url = new URL(s.src);
      return (
        url.pathname.endsWith("/stream") && !url.searchParams.has("resolution")
      );
    } catch {
      return false;
    }
  });
  const remux = findSourceByPath(sources, "/stream.fmp4.master.m3u8");
  const remuxAac = findSourceByPath(sources, "/stream.fmp4.aac.master.m3u8");
  const originalHls = findOriginalResolutionTranscode(sources);
  const hls = findSourceByPath(sources, "/stream.master.m3u8");

  const saved = localStorage.getItem(QUALITY_STORAGE_KEY);
  if (saved) {
    if (BEST_QUALITY_LABELS.has(saved)) {
      // Virtual "best quality" preference: direct → full remux →
      // video-copy + AAC remux → original-resolution transcode.
      // Non-exact label match so a saved "HLS (remux)" on one scene
      // still picks direct or AAC-remux on another scene where the
      // exact label happens to be unavailable.
      if (direct) return direct;
      if (remux) return remux;
      if (remuxAac) return remuxAac;
      if (originalHls) return originalHls;
    } else {
      const match = sources.find((s) => s.label === saved);
      if (match) return match;
    }
  }

  // Default preference order: direct > full fMP4 codec-copy >
  // video-copy + AAC fMP4 > original-resolution HLS transcode >
  // any remaining HLS variant > first source the filter let through.
  // Direct is zero-overhead (no ffmpeg); the full codec-copy fMP4 path
  // is fast to start when the source's codecs are all decodable in
  // fMP4; the video-copy + AAC variant fills the gap when only the
  // video side rides in fMP4 (Opus-source on iOS, etc.); the H.264
  // transcode is the universal fallback for everything else.
  if (direct) return direct;
  if (remux) return remux;
  if (remuxAac) return remuxAac;
  if (originalHls) return originalHls;
  if (hls) return hls;
  return sources[0];
}

/**
 * Per-source-family hook for the bits the player state machine has to
 * dispatch on. Each source family that pre-positions via `?start=`
 * provides one — see `transcodeStrategy` here and `hlsStrategy` in
 * `hls.ts`. The state machine itself stays generic.
 */
export interface SourceStrategy {
  /**
   * Split a desired scene-time target into the value to encode into the
   * URL's `?start=` and the residual fractional seek the player issues
   * post-canPlay. HLS rounds `offset` down to a segment boundary and
   * returns the in-segment fraction as `seekTo`; transcode accepts
   * arbitrary `?start=N` and returns `seekTo: null`.
   */
  planResume(trueTime: number): { offset: number; seekTo: number | null };
  /**
   * Whether a seek to `targetInternal` (player time, i.e. scene time
   * minus `offsetStart`) can be served by the existing source or
   * requires a remount with a fresh URL offset. Transcode checks
   * `s.state.buffered`; HLS allows any forward seek (the trimmed
   * playlist already lists every segment from the trim point to EOF).
   */
  canSeekDirectly(targetInternal: number, store: VideoPlayerStore): boolean;
}

/**
 * Returns the `SourceStrategy` for sources that pre-position via the
 * URL's `?start=` query param — HLS playlists (master + media). Returns
 * `null` for direct byte-range files (`/stream`), which use Media
 * Fragments URI (`#t=N`) via the separate `supportsFragmentTime` path,
 * and for anything unrecognised. Progressive MP4/WebM transcodes have
 * been removed — there's no more transcode strategy.
 *
 * `frameRate` parameterises the HLS strategy so its segment-boundary
 * floor matches the actual per-segment duration the backend emits
 * (which depends on the source's frame rate). See `hlsSegmentDuration`
 * in `hls.ts` for the math.
 *
 * `isClipped` selects between the two HLS playlist shapes the backend
 * serves: full playlists (mediaSequence=0, MSE-time = scene-time, used
 * for scene playback) and trimmed playlists (mediaSequence>0, MSE-time
 * rebased to 0 starting at the trim point, used for marker clips
 * where the native fullscreen slider needs to read "0:00 → clip
 * duration"). The strategy adapts its offset bookkeeping accordingly —
 * see `makeHlsStrategy` in `hls.ts`.
 */
export function startOffsetStrategyFor(
  src: string | null | undefined,
  frameRate: number | undefined,
  isClipped: boolean,
  clipRange?: { start: number; end: number },
): SourceStrategy | null {
  if (isHlsPlaylist(src))
    return makeHlsStrategy(frameRate, isClipped, clipRange);
  return null;
}

/** Convenience wrapper for callers that just want a yes/no. */
export function usesStartOffset(src: string | null | undefined): boolean {
  return isHlsPlaylist(src);
}

export function injectStartOffset(src: string, offset: number): string {
  if (offset <= 0) return src;
  try {
    const url = new URL(src);
    url.searchParams.set(
      "start",
      (Math.floor(offset * 1000) / 1000).toString(),
    );
    return url.toString();
  } catch {
    return src;
  }
}

/**
 * Append a `?end=<seconds>` clip-end parameter. The backend trims the
 * playlist to end at the segment containing this value (and trims the
 * leading segments to match `?start=`); together they produce a
 * clip-only playlist whose MSE timeline is rebased so the clip start
 * lands at MSE 0. Used for marker / clip playback where the native
 * fullscreen player should expose only the clip range as seekable.
 */
export function injectEndOffset(src: string, end: number): string {
  if (end <= 0) return src;
  try {
    const url = new URL(src);
    url.searchParams.set("end", (Math.floor(end * 1000) / 1000).toString());
    return url.toString();
  } catch {
    return src;
  }
}

/** True when the source URL is the direct-stream byte-range endpoint
 *  (`/scene/<id>/stream` with no `?resolution=`). Used to detect when
 *  to filter the source out of the candidate list for clipped mode
 *  on iOS Safari (where the OS-level fullscreen UI reads the slider
 *  bounds from `video.duration`, which a direct stream reports as
 *  `fileDuration` even with a `#t=` fragment URI). */
export function isDirectStreamSrc(src: string | null | undefined): boolean {
  if (!src) return false;
  try {
    const url = new URL(src);
    return (
      url.pathname.endsWith("/stream") && !url.searchParams.has("resolution")
    );
  } catch {
    return false;
  }
}

/**
 * Append a Media Fragments URI `#t=<start>,<end>` so the browser
 * initialises `<video>.currentTime` to `start` and stops playback at
 * `end`. Used for clip mode on direct-stream sources outside iOS
 * Safari — desktop browsers and Android Chrome respect this fragment
 * for both initial seek and end-of-clip stop. iOS Safari does not
 * (it ignores the `end` half), which is why the player filters
 * direct stream out of the source candidates for clipped mode on
 * that platform.
 *
 * Strips any existing `#…` fragment first so the URL stays idempotent
 * across re-renders.
 */
export function injectFragmentTimeRange(
  src: string,
  start: number,
  end: number,
): string {
  if (end <= start) return src;
  const base = src.split("#")[0];
  const s = Math.max(0, Math.floor(start * 1000) / 1000);
  const e = Math.floor(end * 1000) / 1000;
  return `${base}#t=${s},${e}`;
}

/**
 * Append a Media Fragments URI `#t=<seconds>` so the browser initialises
 * `<video>.currentTime` to that value before any media data is fetched.
 *
 * Used for sources where the server can't pre-position the stream
 * (direct byte-range files). Browsers honouring the fragment skip the
 * time-0 buffer pass entirely and request the byte range that contains
 * the target frame on the first round trip. Browsers that ignore it
 * fall back to the existing post-`canPlay` `s.seek(target)`, which
 * lands as a no-op when the fragment was honoured.
 *
 * Truncates to 3 decimals to match `injectStartOffset`. Strips any
 * existing `#…` fragment first so the URL stays idempotent under
 * repeated calls (e.g. across source changes that reuse the same
 * activeSrc).
 */
export function injectFragmentTime(src: string, t: number): string {
  if (t <= 0) return src;
  const base = src.split("#")[0];
  return `${base}#t=${(Math.floor(t * 1000) / 1000).toString()}`;
}

/**
 * True for sources where appending `#t=<seconds>` lands on the right frame
 * without breaking loading: direct byte-range file streams (`/stream`).
 * Excludes:
 *   - HLS playlists (`.m3u8`) — Safari's native HLS does not honour Media
 *     Fragments URI on the playlist URL; setting `#t=N` either gets
 *     silently dropped or trips up the loader so playback stalls at 0.
 *     HLS uses `?start=` on the playlist URL instead (server trims the
 *     playlist to start at the matching segment).
 *   - Transcode sources (`/stream.mp4` etc.) — they use server-side
 *     `?start=` which is strictly better.
 */
export function supportsFragmentTime(src: string | null | undefined): boolean {
  if (!src) return false;
  try {
    const path = new URL(src).pathname;
    return path.endsWith("/stream");
  } catch {
    return false;
  }
}

export function computeInitialResume(
  preferredSrc: string | undefined,
  initialTimestamp: number | undefined,
  resumeTime: number | null | undefined,
  frameRate: number | undefined,
  isClipped: boolean,
  clipRange?: { start: number; end: number },
): { offset: number; seekTo: number | null } {
  const t = initialTimestamp ?? resumeTime ?? 0;
  const strategy = startOffsetStrategyFor(
    preferredSrc,
    frameRate,
    isClipped,
    clipRange,
  );
  if (t <= 0) {
    // Even at scene-time 0, clipped HLS needs `offset` pinned to the
    // clip's trim point so MSE-time<->scene-time conversion keeps
    // working. Ask the strategy.
    if (strategy) return strategy.planResume(0);
    return { offset: 0, seekTo: null };
  }
  if (strategy) {
    return strategy.planResume(t);
  }
  // No strategy → direct stream. `<video>.currentTime` reads as
  // scene-time directly (Media Fragments URI `#t=start,end` doesn't
  // rebase the timeline, just constrains it), so `offset` stays at 0.
  return { offset: 0, seekTo: t };
}
