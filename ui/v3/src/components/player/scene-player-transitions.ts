import { isHlsPlaylist, makeHlsStrategy } from "./hls";

/** Snapshot used by source policies; it carries no player methods or DOM state. */
export interface MediaSeekState {
  buffered?: readonly (readonly [number, number])[];
  seekable?: readonly (readonly [number, number])[];
}

export interface SourceStrategy {
  /**
   * Keep the requested scene-time as `seekTo`. `offset` is the scene-time
   * of media-element time zero: 0 for a full playlist, or the segment-aligned
   * clip start. Subtract it only when writing the media playhead.
   */
  planResume(trueTime: number): { offset: number; seekTo: number | null };
  /**
   * Whether a seek to `targetInternal` (player time, i.e. scene time
   * minus `offsetStart`) can be served by the existing source or
   * requires an engine restart. HLS checks buffered and seekable ranges;
   * a distant seek needs recovery even if the playlist contains that time.
   */
  canSeekDirectly(targetInternal: number, state: MediaSeekState): boolean;
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

export interface SourceResume {
  offset: number;
  seekTo: number | null;
  fragmentTime: number | null;
}

/** All targets remain in scene time until the media effect applies a seek. */
export function planSourceResume(
  strategy: SourceStrategy | null,
  sceneTime: number,
): SourceResume {
  if (strategy) {
    const resume = strategy.planResume(sceneTime);
    return { ...resume, fragmentTime: resume.seekTo };
  }
  return {
    offset: 0,
    seekTo: sceneTime,
    fragmentTime: sceneTime > 0 ? sceneTime : null,
  };
}

export type SceneSeekTransition =
  | { kind: "seek" | "restart-engine"; sceneTime: number; mediaTime: number }
  | { kind: "reload-source"; sceneTime: number; resume: SourceResume };

/** Decide from a snapshot; this function never pauses, seeks, or rebuilds media. */
export function planSceneSeek({
  intent,
  targetTime,
  duration,
  offsetStart,
  src,
  frameRate,
  clipRange,
  mediaState,
  ios,
  hasHlsEngine,
}: {
  intent: "seek" | "restart";
  targetTime: number;
  duration: number | undefined;
  offsetStart: number;
  src: string;
  frameRate: number | undefined;
  clipRange?: { start: number; end: number };
  mediaState: MediaSeekState;
  ios: boolean;
  hasHlsEngine: boolean;
}): SceneSeekTransition {
  // Preserve the restart behavior for a file whose duration is not yet known.
  const max =
    intent === "restart" && (duration == null || duration <= 0)
      ? Infinity
      : (duration ?? Infinity);
  const sceneTime = Math.max(0, Math.min(targetTime, max));
  const targetInternal = sceneTime - offsetStart;
  const mediaTime =
    intent === "restart" ? Math.max(0, targetInternal) : targetInternal;
  const strategy = startOffsetStrategyFor(
    src,
    frameRate,
    clipRange !== undefined,
    clipRange,
  );
  if (
    !strategy ||
    ((intent === "seek" || targetInternal >= 0) &&
      strategy.canSeekDirectly(targetInternal, mediaState))
  ) {
    return { kind: "seek", sceneTime, mediaTime };
  }

  // iOS ManagedMediaSource cannot reliably recover from a mid-flight flush.
  // Clips also need a source reload to retain their trimmed playlist contract.
  if (!ios && !clipRange && hasHlsEngine) {
    return { kind: "restart-engine", sceneTime, mediaTime };
  }
  return {
    kind: "reload-source",
    sceneTime,
    resume: planSourceResume(strategy, sceneTime),
  };
}
