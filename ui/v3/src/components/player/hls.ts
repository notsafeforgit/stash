/**
 * HLS-specific source handling. The rest of the player should not need
 * to read this file.
 *
 * HLS sources route through `<StableHlsVideo>` (`@videojs/core`'s
 * `HlsJsMedia`, which auto-selects hls.js on MSE-capable browsers and
 * falls back to native HLS otherwise) on every browser. `canPlaySource`
 * gates HLS on `hasNativeHLS() || hasMSE()`, which covers Safari,
 * Chrome, and Firefox.
 *
 * What this file owns: HLS-specific seek policy + the scene-time ↔
 * MSE-time bridge.
 *
 * Playlist shapes
 * ---------------
 * The backend serves two HLS playlist shapes, selected by the URL:
 *
 *   1. **Full playlist** (`?end=` absent). Lists every segment from 0
 *      to EOF, mediaSequence=0. Used for normal scene playback. hls.js
 *      doesn't rebase MSE-time — segment K has fMP4 PTS K·segmentLength
 *      and the SourceBuffer's timestampOffset stays at 0 — so
 *      `<video>.currentTime` reads as absolute scene-time directly,
 *      `offsetStart` is always 0, and the native iOS fullscreen slider
 *      exposes [0, sceneDuration].
 *
 *      `?start=N` on the URL is *not* used for playlist trimming in
 *      this shape; the server emits the same full playlist regardless.
 *      The frontend uses the value to set hls.js
 *      `config.startPosition`, so the first segment hls.js fetches is
 *      the one containing N rather than segment 0 — avoiding a
 *      segment-0 cold-start detour when the user has a resume_time or
 *      deep-link `?t=`.
 *
 *   2. **Clipped playlist** (`?end=` present, used for marker / clip
 *      playback). Lists only segments
 *      `[⌊start/segDur⌋, ⌈end/segDur⌉-1]`, mediaSequence > 0. hls.js
 *      sets `SourceBuffer.timestampOffset = -initPTS` so the first
 *      delivered fragment lands at MSE PTS 0, rebasing MSE-time
 *      relative to the clip start. `<video>.currentTime` is then
 *      MSE-relative (= scene-time - effective_trim), and the native
 *      iOS fullscreen slider exposes [0, clipDuration] — labelled
 *      "0:00 → clipDur".
 *
 *      The scene-time ↔ MSE-time mapping for this shape is:
 *
 *         scene_time = effective_trim + media_currentTime
 *         media_currentTime = scene_time - effective_trim
 *
 *      The state machine runs in scene-time everywhere user-visible;
 *      the conversion to MSE-time happens at the `<video>.currentTime`
 *      write boundary (`handleLoadedMetadata` / `handleCanPlay`) and
 *      the matching read boundary (`s.state.currentTime + offsetStart`).
 *      `effective_trim` lives in `offsetStart`.
 *
 * `makeHlsStrategy(frameRate, isClipped)` returns the strategy variant
 * for either shape; the generic player state machine in
 * `useScenePlayerSources` dispatches through `SourceStrategy` and never
 * has to know which shape it's running.
 *
 * Backend setup that makes both shapes work:
 *
 *   - The encoder always emits segments with
 *     `output_ts_offset = N·segmentLength` so segment K's fMP4 PTS is
 *     always K·segmentLength regardless of which ffmpeg run produced
 *     it (a far-forward seek's restart-at-K produces segments whose
 *     PTS matches what's already on disk for earlier segments — no
 *     overlap, no conflict).
 *
 *   - For the clipped shape, the playlist carries
 *     `EXT-X-MEDIA-SEQUENCE = startSegment`. hls.js parses this,
 *     computes per-fragment playlist-time as
 *     `mediaSequence·targetDuration + Σ prior EXTINFs`, and uses the
 *     first parsed fMP4 PTS for `initPTS`. Both come out to
 *     `effective_trim = floor(?start / segmentLength) · segmentLength`.
 *
 * The strategy object exported here plugs into the generic
 * `SourceStrategy` dispatch in `scene-player-sources.ts`. Non-HLS code
 * paths see only that interface and never know HLS exists.
 */
import type { VideoPlayerStore } from "@videojs/react";
import type { SourceStrategy } from "./scene-player-sources";

// Mirror of `pkg/ffmpeg/stream_segmented.go:segmentLength`. The nominal
// segment-length target the backend feeds to `-hls_time`. The actual
// per-segment duration the encoder emits depends on the GOP cadence,
// which depends on the source frame rate — see `hlsSegmentDuration`
// below.
const SEGMENT_LENGTH = 2;

// Mirror of `pkg/ffmpeg/stream_segmented.go:hlsGopSize`. Computes the
// GOP size (frames per segment) the backend will set on the encoder.
function hlsGopSize(frameRate: number): number {
  const fps = !Number.isFinite(frameRate) || frameRate <= 0 ? 30 : frameRate;
  const gop = Math.round(fps * SEGMENT_LENGTH);
  return gop < 1 ? 1 : gop;
}

// Mirror of `pkg/ffmpeg/stream_segmented.go:hlsSegmentDuration`. The
// real per-segment duration the encoder emits, given the source frame
// rate. For integer rates (24/30/60) this collapses to SEGMENT_LENGTH;
// for NTSC fractionals (59.94) it drifts upward (120/59.94 = 2.0020 s).
//
// The backend uses this for `-output_ts_offset`, EXTINF, and the
// `?start=` segment-boundary floor. The frontend has to use the same
// arithmetic to compute `effective_trim` (the scene-time value at
// MSE 0) — without it, `offsetStart` drifts from the segment boundary
// the server actually trimmed at and `<video>.currentTime` writes
// land on the wrong scene-time position. The drift is small per
// segment but accumulates linearly with seek depth: at 59.94 fps,
// segment 100 sits at 200.20 s scene time, not 200.00 s.
export function hlsSegmentDuration(frameRate: number | undefined): number {
  if (
    frameRate === undefined ||
    !Number.isFinite(frameRate) ||
    frameRate <= 0
  ) {
    return SEGMENT_LENGTH;
  }
  const gop = hlsGopSize(frameRate);
  if (gop <= 0) return SEGMENT_LENGTH;
  return gop / frameRate;
}

// All HLS playlist suffixes the backend serves. Master playlists are the
// canonical entry point (every HLS source URL surfaced via `sceneStreams`
// points at one of these); media-playlist suffixes are still listed here
// because variant URLs inside a master are media playlists, and any code
// that needs to detect "is this an HLS-formatted URL" has to recognise
// both shapes.
const PLAYLIST_PATH_SUFFIXES = [
  "/stream.master.m3u8",
  "/stream.fmp4.master.m3u8",
  "/stream.fmp4.aac.master.m3u8",
  "/stream.m3u8",
  "/stream.fmp4.m3u8",
  "/stream.fmp4.aac.m3u8",
];

export function isHlsPlaylist(src: string | null | undefined): boolean {
  if (!src) return false;
  try {
    const path = new URL(src).pathname;
    return PLAYLIST_PATH_SUFFIXES.some((suffix) => path.endsWith(suffix));
  } catch {
    return false;
  }
}

// Factory returning the HLS `SourceStrategy` for a given source frame
// rate and playlist shape. Two shapes the backend serves:
//
//   - Full playlist (mediaSequence=0, `isClipped: false`) — every
//     segment from 0 to EOF. hls.js's MSE timeline = scene-time
//     directly (no rebase). Used for scene playback. The strategy is
//     near-identity: offset always 0, every seek is direct.
//
//   - Trimmed (clipped) playlist (mediaSequence>0, `isClipped: true`) —
//     only segments covering the marker / clip range. hls.js rebases
//     MSE-time to 0 at the first delivered fragment's scene-time PTS
//     (via `SourceBuffer.timestampOffset = -initPTS`). The strategy
//     keeps the trim point in `offset` so the state machine can do
//     `scene_time = offset + media_currentTime`.
//
// `frameRate` parameterises the segment-boundary floor used for
// `effective_trim` so it matches the per-segment duration the backend
// actually emits (which depends on source frame rate — see
// `hlsSegmentDuration`).
export function makeHlsStrategy(
  frameRate: number | undefined,
  isClipped: boolean,
  clipRange?: { start: number; end: number },
): SourceStrategy {
  const segDur = hlsSegmentDuration(frameRate);

  if (!isClipped) {
    // Full-playlist case: MSE-time = scene-time. No offset bookkeeping
    // needed; backward "past the trim" doesn't exist (no trim).
    //
    // Direct-seek (just `video.currentTime = N` with no engine churn)
    // works when:
    //   - target is inside one of the buffered ranges, OR
    //   - target is forward of the last buffered range by ≤ ~30 s
    //     (hls.js's stream-controller naturally prefetches ahead).
    //
    // Everything else needs the URL-change remount:
    //   - Far-forward seeks (target > bufferedEnd + 30 s) — hls.js's
    //     scheduler doesn't reorient cleanly when in-flight fragments
    //     are far from the new currentTime; they complete and append
    //     at unrelated MSE positions, BUFFER_EOS signals prematurely,
    //     playback stalls at readyState=1.
    //   - Backward seeks past the start of buffered — same scheduler
    //     issue. Despite the playlist exposing every segment as a
    //     valid seek target, hls.js continues loading the old position
    //     after a big backward currentTime jump, segments land at the
    //     old MSE position, currentTime never gets data. Confirmed by
    //     diagnostic: post-seek `hlsBufferAppended` events keep
    //     showing the pre-seek buffered range with no fragment at the
    //     new currentTime.
    //
    // Routing the failing cases through `beginSourceRemount` writes a
    // new `?start=` into the URL; our bridge reassigns `HlsJsMedia.src`
    // and its start-position config, which destroys the active delegate
    // and creates a fresh one with `config.startPosition = newTarget`.
    // The new engine requests
    // the right segment from a clean state. The freeze-frame canvas
    // (captured just before this routes through `beginSourceRemount`)
    // masks the engine-recreate window.
    return {
      planResume(trueTime: number) {
        return { offset: 0, seekTo: trueTime > 0 ? trueTime : null };
      },
      canSeekDirectly(targetInternal: number, store: VideoPlayerStore) {
        const buffered = store.state.buffered;
        if (!buffered || buffered.length === 0) {
          // No buffer yet — let hls.js's natural startup pick the
          // fragment. (This branch is mostly defensive — by the time
          // the user can issue a seek, there's almost always some
          // buffered data.)
          return true;
        }
        // In-buffer.
        for (let i = 0; i < buffered.length; i++) {
          if (
            targetInternal >= buffered[i][0] &&
            targetInternal <= buffered[i][1]
          ) {
            return true;
          }
        }
        // Forward within prefetch window.
        const bufferedEnd = buffered[buffered.length - 1][1];
        const FORWARD_PREFETCH_S = 30;
        if (
          targetInternal > bufferedEnd &&
          targetInternal <= bufferedEnd + FORWARD_PREFETCH_S
        ) {
          return true;
        }
        // Either far-forward or backward outside buffer — need the
        // remount path.
        return false;
      },
    };
  }

  // Clipped-playlist case: same semantics as the pre-full-playlist HLS
  // strategy — the playlist still trims, MSE is still rebased, the
  // state machine still tracks `offsetStart`. The clip-start parameter
  // is fixed for the lifetime of the source (the marker boundary), so
  // there is no more "backward-past-trim" seek path here — the marker
  // lightbox constrains the slider to the clip range. We still
  // implement `canSeekDirectly` defensively for the forward-buffer
  // case, where a too-far-forward seek would still want a remount.
  // For clipped playlists the trim point is fixed to the marker's
  // start. Resolution / quality swaps mid-clip must not narrow the
  // seekable window — if the URL `?start=` (and therefore `offset`)
  // tracked the user's current playhead, every swap would push the
  // clip floor forward and make the 22→playhead region unreachable.
  // Pin `offset` to `floor(clipRange.start / segDur) * segDur` so it
  // matches the server's trim for the full clip, regardless of where
  // the playhead is when the source rebuilds.
  const clipTrim = clipRange
    ? Math.floor(clipRange.start / segDur) * segDur
    : 0;

  return {
    planResume(trueTime: number) {
      // `seekTo` is scene-time. `offset` is the scene-time of MSE 0 —
      // pinned to `clipTrim` so it stays stable across source swaps.
      // `seekTo: null` for trueTime at or before clipTrim (no explicit
      // seek needed; the playlist's first segment is already there).
      if (trueTime <= clipTrim) {
        return { offset: clipTrim, seekTo: null };
      }
      return {
        offset: clipTrim,
        seekTo: trueTime,
      };
    },
    canSeekDirectly(targetInternal: number, store: VideoPlayerStore) {
      // `targetInternal` is MSE-time. With the playlist trimmed to a
      // clip, the seekable range is [0, clipDur] in MSE-time. Backward
      // seeks below 0 shouldn't happen (the player's effective slider
      // is constrained to the clip), but check defensively.
      const seekable = store.state.seekable;
      if (seekable && seekable.length > 0) {
        const start = seekable[0][0];
        if (targetInternal < start) return false;
      }

      // Forward seeks far past the buffered range force the backend to
      // kill its in-flight ffmpeg and restart at the seek-target
      // segment (`ensureTranscode` in `pkg/ffmpeg/stream_segmented.go`
      // triggers a kill once the requested segment overshoots current
      // progress by `maxSegmentGap`). The kill/restart cycle leaves
      // hls.js trying to splice the new run's first segment onto a
      // SourceBuffer whose existing buffered range is far behind the
      // new MSE position; the resulting discontinuity manifests as
      // playback stalling shortly after the first new segment plays.
      // Threshold roughly matches hls.js's default `maxBufferLength`
      // (~30 s).
      const buffered = store.state.buffered;
      if (buffered && buffered.length > 0) {
        const bufferedEnd = buffered[buffered.length - 1][1];
        const FORWARD_REMOUNT_THRESHOLD_S = 30;
        if (targetInternal > bufferedEnd + FORWARD_REMOUNT_THRESHOLD_S) {
          return false;
        }
      }

      return true;
    },
  };
}

/**
 * Parse the `?start=` value from an HLS source URL. Used by
 * `StableHlsVideo` to feed hls.js's `config.startPosition` so the
 * first segment fetch lands on the requested scene-time instead of
 * segment 0 (no cold-start detour). Returns -1 (hls.js's "use
 * defaults" sentinel) when absent / invalid / non-positive.
 */
export function parseStartPosition(src: string | null | undefined): number {
  if (!src) return -1;
  try {
    const url = new URL(src, window.location.origin);
    const raw = url.searchParams.get("start");
    if (raw == null) return -1;
    const v = parseFloat(raw);
    if (!Number.isFinite(v) || v <= 0) return -1;
    return v;
  } catch {
    return -1;
  }
}

/** Whether the given source URL is a clipped HLS playlist (carries
 *  `?end=`). The presence of `?end=` is the explicit "this is a clip"
 *  signal — `?start=` alone never trims server-side. */
export function isClippedHls(src: string | null | undefined): boolean {
  if (!isHlsPlaylist(src)) return false;
  try {
    const url = new URL(src as string, window.location.origin);
    return url.searchParams.has("end");
  } catch {
    return false;
  }
}

/**
 * Duck-typed surface of the hls.js `Hls` instance — the methods we
 * reach into for in-place out-of-buffer seek recovery
 * (`flushAndRestartAt`). Avoids a hard dependency on hls.js's type
 * exports for callers that only need this subset.
 */
export interface HlsEngineLike {
  stopLoad(): void;
  startLoad(startPosition?: number): void;
  trigger(event: string, data: unknown): void;
}

/**
 * Extract the hls.js engine from a `@videojs/core` Media instance, if
 * one is attached. Returns null for non-HLS media, for the early-mount
 * window before the engine is created, or for cases where the engine
 * is missing the expected API.
 */
export function getHlsEngine(media: unknown): HlsEngineLike | null {
  if (!media) return null;
  const engine = (media as { engine?: unknown }).engine;
  if (
    engine &&
    typeof (engine as HlsEngineLike).stopLoad === "function" &&
    typeof (engine as HlsEngineLike).startLoad === "function" &&
    typeof (engine as HlsEngineLike).trigger === "function"
  ) {
    return engine as HlsEngineLike;
  }
  return null;
}

/**
 * Returns true if the given `<video>` element is currently in iOS
 * Safari's native fullscreen mode. iOS-specific (uses the WebKit
 * `webkitDisplayingFullscreen` IDL attribute); returns false on every
 * other browser, including non-iOS Safari and standard
 * `:fullscreen` mode.
 *
 * Used to fork the out-of-buffer seek path: iOS Safari fullscreen
 * fails the in-place `flushAndRestartAt` route (hls.js's
 * `BUFFER_FLUSHING` + `appendBuffer` cycle leaves `video.buffered`
 * empty in this mode), so we fall back to the URL-change remount for
 * fullscreen and accept the brief black flash. Every other context
 * (desktop, mobile non-fullscreen, Android Chrome) stays on the
 * in-place path.
 */
export function isIOSNativeFullscreen(video: HTMLVideoElement): boolean {
  return (
    (video as unknown as { webkitDisplayingFullscreen?: boolean })
      .webkitDisplayingFullscreen === true
  );
}

/**
 * In-place "reset hls.js's playback position" without tearing down
 * the MediaSource. Used for out-of-buffer seeks on non-clipped HLS so
 * the `<video>` element can hold the last decoded frame natively
 * during the engine's catch-up window.
 *
 * Why each call:
 *   - `stopLoad()` aborts in-flight fragment requests so they don't
 *     complete and append at MSE positions unrelated to the new seek
 *     target.
 *   - `BUFFER_FLUSHING` for the full range clears the existing
 *     SourceBuffer contents. Without it, hls.js's buffer tracker
 *     thinks data is already loaded around the old position and may
 *     refuse to load fragments at the new position (or splice the
 *     new fragment onto the stale buffer and trip an
 *     InvalidStateError).
 *   - `startLoad(target)` re-anchors the segment scheduler at the new
 *     position so the first request after the seek goes to the
 *     containing fragment.
 *
 * Critically, this does NOT call `detachMedia/attachMedia`. That
 * sequence triggers `InterstitialsController.onMediaAttached` which
 * restores the playback position from its own schedule tracker —
 * overriding our seek target. Staying attached avoids the override
 * and also keeps the `<video>` element's last-decoded-frame visible.
 */
export function flushAndRestartAt(engine: HlsEngineLike, target: number): void {
  engine.stopLoad();
  try {
    engine.trigger("hlsBufferFlushing", {
      startOffset: 0,
      endOffset: Infinity,
      type: null,
    });
  } catch {
    /* defensive — older hls.js may not accept type:null */
  }
  engine.startLoad(target);
}
