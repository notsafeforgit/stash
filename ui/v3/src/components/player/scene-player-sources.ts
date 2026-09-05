import { startOffsetStrategyFor } from "./scene-player-transitions";
export { startOffsetStrategyFor } from "./scene-player-transitions";
export {
  isDirectStreamSrc,
  supportsFragmentTime,
} from "./scene-player-source-url";

/**
 * Helpers for picking, filtering, and persisting the active scene-
 * player source. Lives apart from `scene-player.tsx` so the component
 * file stays focused on the player shell + state machine.
 *
 * Source eligibility includes browser decode checks; preference lookup reads
 * localStorage. Transition decisions live in `scene-player-transitions.ts`.
 */

import { isHlsPlaylist } from "./hls";
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
