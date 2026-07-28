/**
 * Source / seek / source-change state machine for `scene-player.tsx`.
 *
 * Lives apart from the component file so the component stays focused
 * on layout + render and this file owns the source-URL plumbing,
 * pending-resume bookkeeping, and the per-scenario seek dispatch
 * described in `scene-player.tsx`'s header. The hook returns the
 * computed `finalSrc`, the seek-display target, the freeze-frame
 * canvas element to render as a sibling, and the handlers
 * (`handleSourceChange` / `handleSeek` / `handleRestart` /
 * `handleCanPlay` / `handleLoadedMetadata`) the component wires up.
 */
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
  type SyntheticEvent,
} from "react";
import type { VideoPlayerStore } from "@videojs/react";
import { useConfigurationContext } from "src/hooks/config";
import { isIOS, type PlayerSource } from "./player-utils";
import { useFreezeFrameOverlay } from "./use-freeze-frame-overlay";
import { isHlsPlaylist, getHlsEngine, flushAndRestartAt } from "./hls";
import {
  BEST_QUALITY_LABELS,
  QUALITY_STORAGE_KEY,
  computeInitialResume,
  filterSources,
  getPreferredSource,
  hlsStreamTypeName,
  injectEndOffset,
  injectFragmentTime,
  injectFragmentTimeRange,
  injectStartOffset,
  isDirectStreamSrc,
  startOffsetStrategyFor,
  streamResolution,
  supportsFragmentTime,
  usesStartOffset,
  type RawStream,
} from "./scene-player-sources";

interface SourceScene {
  id: string;
  sceneStreams: RawStream[];
  resume_time?: number | null;
}

interface UseScenePlayerSourcesArgs {
  scene: SourceScene;
  initialTimestamp: number | undefined;
  /** Combined video + audio fMP4 decode capability (gates the full
   *  codec-copy remux variant). */
  canDecode: boolean;
  /** Video-only fMP4 decode capability (gates the video-copy +
   *  AAC-transcode remux variant — audio is always AAC). */
  canDecodeVideo: boolean;
  /** Source video frame rate. Parameterises the HLS strategy's
   *  segment-boundary floor — at non-integer NTSC rates (59.94 fps)
   *  the actual segment duration drifts from the nominal 2 s, and
   *  `effective_trim` has to use the real value to stay aligned
   *  with the trim point the server applied. See
   *  `hlsSegmentDuration` in `hls.ts`. */
  frameRate: number | undefined;
  fileDuration: number | undefined;
  /** Native video dimensions — used by the freeze-frame canvas to
   *  pre-allocate a backing buffer with the correct aspect ratio. */
  fileWidth: number | undefined;
  fileHeight: number | undefined;
  /** Outer wrapper used as the `querySelector("video")` root for the
   *  in-place seek choreography (`awaitSeekReady`). */
  rootRef: RefObject<HTMLDivElement | null>;
  /** Player.Provider's store, captured by `<StoreBridge>` in the
   *  parent component. Read synchronously inside the handlers. */
  storeRef: RefObject<VideoPlayerStore | null>;
  /** Player.Provider's active Media instance, captured by
   *  `<MediaBridge>`. For HLS sources this is the `HlsJsMedia` wrapper
   *  whose `.engine` returns the live hls.js `Hls` instance — the
   *  in-place out-of-buffer seek path reaches into it for
   *  `stopLoad` / `BUFFER_FLUSHING` / `startLoad` so the MediaSource
   *  stays attached during the seek (the `<video>` element holds its
   *  last decoded frame natively while hls.js catches up to the new
   *  position). Critical for iOS Safari native fullscreen where our
   *  canvas overlay is invisible and any MSE detach flashes black. */
  mediaRef: RefObject<unknown>;
  /**
   * Whether the page-level autoplay gate (autostart-video setting +
   * lightbox auto-advance override) currently permits playback to begin
   * automatically. Stamps `wasPaused` on the initial pending-resume so
   * that the deep-link `?t=` URL doesn't unconditionally autoplay when
   * the user has the global autostart toggle off. Source-change
   * pending-resumes (set by `handleSourceChange` / seek-past-buffer)
   * ignore this and faithfully preserve the live `paused` state across
   * the in-place src swap.
   */
  allowAutoplay: boolean;
  /**
   * When set, the HLS source URL is augmented with `?start=clipStart&end=clipEnd`
   * — the backend then serves a *trimmed* playlist covering only the
   * clip's segments. hls.js rebases MSE-time to 0 at the clip start,
   * so `<video>.currentTime` reads as clip-relative time and the
   * native iOS fullscreen slider labels its range "0:00 → clipDuration."
   * Used by the marker lightbox so each marker reads as a standalone
   * clip in every player surface.
   *
   * For scene playback (no marker), leave this undefined — the URL
   * gets `?start=` only (transcoder-startup hint for hls.js's
   * startPosition; full playlist server-side).
   */
  clipRange: { start: number; end: number } | undefined;
}

interface UseScenePlayerSourcesResult {
  sources: PlayerSource[];
  activeSource: PlayerSource | null;
  finalSrc: string | undefined;
  offsetStart: number;
  initialResume: { offset: number; seekTo: number | null };
  reloading: boolean;
  seekDisplayTarget: number | null;
  /** Persistent canvas that masks the gap between the old MediaSource
   *  detaching and the new one decoding its first frame on src-swap /
   *  out-of-buffer HLS seek paths. Render as a sibling of
   *  `<Player.Provider>` (z-[5]). */
  freezeFrameCanvas: ReactNode;
  handleSourceChange: (source: PlayerSource) => void;
  handleSeek: (targetTrueTime: number) => void;
  /** Restart playback from `targetTrueTime` (scene-time). Used when the
   *  user presses play after the natural `ended` event (scene mode) or
   *  the clip auto-stop (marker mode). Issues a fresh playlist URL with
   *  smaller `?start=` when the target sits before the current trim
   *  point — necessary after a quality swap, since the new playlist's
   *  MSE 0 is the swap-time scene-time and HTML5's ended-then-play
   *  default would replay from there instead of from the requested
   *  target. The src swap is in-place (no Provider remount). */
  handleRestart: (targetTrueTime: number) => void;
  handleCanPlay: () => Promise<void>;
  /** Wire as `onLoadedMetadata` on the `<video>` element. Pins the
   *  playhead to the pending-resume target before the browser's
   *  internal autoplay path can begin emitting frames — see body
   *  comment for the Safari rationale. */
  handleLoadedMetadata: (e: SyntheticEvent<HTMLVideoElement>) => void;
}

export function useScenePlayerSources({
  scene,
  initialTimestamp,
  canDecode,
  canDecodeVideo,
  frameRate,
  fileDuration,
  fileWidth,
  fileHeight,
  rootRef,
  storeRef,
  mediaRef,
  allowAutoplay,
  clipRange,
}: UseScenePlayerSourcesArgs): UseScenePlayerSourcesResult {
  // Presence of `clipRange` selects the trimmed-playlist code path
  // (URL gets `?end=`, hlsStrategy keeps offsetStart bookkeeping).
  // Without it we ride the full-playlist path (no `?end=`, offsetStart
  // always 0, every seek is direct).
  const isClipped = clipRange !== undefined;
  const [manualSource, setManualSource] = useState<PlayerSource | null>(null);

  const sources = useMemo(() => {
    const all = filterSources(scene.sceneStreams, canDecode, canDecodeVideo);
    if (!isClipped) return all;
    // Marker / clip mode + iOS Safari: drop the direct-stream source
    // from the candidate pool. iOS Safari's `webkitEnterFullscreen`
    // native UI reads the slider bounds from `video.duration` (the
    // full file's duration), and Media Fragments URI `#t=start,end`
    // doesn't override that on this browser — so a direct-stream
    // marker would show a full-scene slider with currentTime jumping
    // into the middle. The HLS-copy variant delivers a trimmed
    // playlist where `video.duration` = clip duration, fixing the
    // slider. Same bitstream — HLS-copy is just codec-copy fMP4
    // fragmenting, not a re-encode.
    //
    // Every other browser uses our React-rendered fullscreen
    // (`Element.requestFullscreen` on the player container) which
    // keeps clipRange-aware custom controls visible, so the direct
    // stream + `#t=start,end` path is fine there.
    if (!isIOS()) return all;
    return all.filter((s) => !isDirectStreamSrc(s.src));
  }, [scene.sceneStreams, canDecode, canDecodeVideo, isClipped]);

  // Honour the `alwaysStartFromBeginning` UI preference for full-scene
  // playback only. When set, the player ignores the scene's persisted
  // resume_time on open. Explicitly NOT honoured when:
  //   - `isClipped` (marker / clip-range mode) — opening a marker is an
  //     intentional seek to that marker's time, so "start from the
  //     beginning" doesn't apply.
  //   - An explicit `initialTimestamp` is supplied (e.g. deep-link
  //     `?t=N`, marker click, in-page seek) — that target is more
  //     specific than the preference. This second case is enforced
  //     downstream by `computeInitialResume`'s
  //     `initialTimestamp ?? resumeTime ?? 0` ordering, so we don't
  //     need to gate it here, but the comment is here so the next
  //     reader doesn't think the gate's behaviour is incomplete.
  const ui = useConfigurationContext().configuration.ui as {
    alwaysStartFromBeginning?: boolean;
  };
  const effectiveResumeTime =
    !isClipped && ui.alwaysStartFromBeginning ? null : scene.resume_time;

  // `initialResume` is a deliberate once-per-scene capture: resume_time,
  // sources, etc. keep changing during playback, and recomputing would
  // re-fire the offset/fragment reset effect below mid-session. Captured
  // as state keyed on scene.id (the documented "storing information from
  // previous renders" pattern) rather than a useMemo with a partial dep
  // list.
  const captureResume = () => ({
    sceneId: scene.id,
    resume: computeInitialResume(
      getPreferredSource(sources)?.src,
      initialTimestamp,
      effectiveResumeTime,
      frameRate,
      isClipped,
      clipRange,
    ),
  });
  const [capturedResume, setCapturedResume] = useState(captureResume);
  if (capturedResume.sceneId !== scene.id) {
    setCapturedResume(captureResume());
  }
  const initialResume = capturedResume.resume;

  const [offsetStart, setOffsetStart] = useState(initialResume.offset);
  // `fragmentTime` mirrors the pending-seek target into the URL so the
  // first network round-trip lands on (or near) the target frame instead
  // of time 0. The exact mechanism depends on the source type:
  //   - Direct byte-range files: Media Fragments URI (`#t=N`) — browsers
  //     honouring the fragment set `<video>.currentTime` before any data
  //     is fetched.
  //   - HLS playlists (`.m3u8` / `.fmp4.m3u8`): `?start=N` query param —
  //     the server trims the playlist to begin at `floor(N/segmentLength)`,
  //     so Safari fetches the segment containing the target time first
  //     instead of loading from segment 0 and then jumping (which stalls
  //     on the first frame for several seconds).
  // Cleared when no pre-position is needed (transcode `?start=` sources,
  // or scene-time 0). The post-`canPlay` `s.seek(target)` runs either way
  // as a fallback / fine seek; for HLS it lands as a fast in-segment seek
  // since the playlist trim has already loaded the right range.
  const [fragmentTime, setFragmentTime] = useState<number | null>(
    initialResume.seekTo,
  );
  // Cachebuster bumped by `forceRemountAt` so a stall-recovery
  // remount triggers even when the seek target lands on the same
  // segment-aligned `?start=` (HLS) or `#t=` (direct) the engine is
  // already on. Without this, `finalSrc` would be identical to the
  // current src and React wouldn't reassign `<video>.src` / hls.js
  // would skip the playlist reload — the stuck connection would
  // never get replaced. See the watchdog effect below.
  const [reloadNonce, setReloadNonce] = useState(0);
  const [reloading, setReloadingRaw] = useState(false);
  // Hold the dim+spinner visible for a minimum window after a source
  // change begins. With an in-place src swap the new playlist's first
  // `canplay` can fire within a frame or two of the state update,
  // leaving the spinner's opacity-1 paint indistinguishable from no
  // spinner at all. Anchoring the clear to a min-duration timer ensures
  // the user actually sees the loading affordance before the new video
  // takes over.
  const RELOADING_MIN_DURATION_MS = 250;
  const reloadingStartTsRef = useRef<number | null>(null);
  const reloadingClearTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const setReloading = useCallback((value: boolean) => {
    if (value) {
      if (reloadingClearTimerRef.current) {
        clearTimeout(reloadingClearTimerRef.current);
        reloadingClearTimerRef.current = null;
      }
      reloadingStartTsRef.current = performance.now();
      setReloadingRaw(true);
      return;
    }
    const elapsed = performance.now() - (reloadingStartTsRef.current ?? 0);
    const remaining = RELOADING_MIN_DURATION_MS - elapsed;
    if (remaining <= 0) {
      reloadingStartTsRef.current = null;
      setReloadingRaw(false);
      return;
    }
    if (reloadingClearTimerRef.current) {
      clearTimeout(reloadingClearTimerRef.current);
    }
    reloadingClearTimerRef.current = setTimeout(() => {
      reloadingClearTimerRef.current = null;
      reloadingStartTsRef.current = null;
      setReloadingRaw(false);
    }, remaining);
  }, []);
  useEffect(
    () => () => {
      if (reloadingClearTimerRef.current) {
        clearTimeout(reloadingClearTimerRef.current);
      }
    },
    [],
  );

  // Freeze-frame canvas: snapshots the current `<video>` frame just
  // before the state setters fire, so the engine-detach / blob-URL-
  // swap path (which natively goes black) sits under the captured
  // pixels instead of nothing. Cleared in `handleCanPlay` once the
  // new source is decoding. Direct-stream in-buffer seeks don't go
  // through this — the browser holds the last frame natively. See
  // `use-freeze-frame-overlay.tsx` for the canvas-content vs.
  // opacity-toggle rationale.
  const {
    canvasElement: freezeFrameCanvas,
    captureFrame,
    clear: clearCapturedFrame,
  } = useFreezeFrameOverlay({
    rootRef,
    fileWidth,
    fileHeight,
    setReloading,
  });

  // Replaces `beginSourceRemount` from the original variant. With an
  // in-place src swap the helper is: snapshot the current frame for
  // the freeze-frame canvas, flip the spinner, commit the state.
  // No <video> abort (HlsJsMedia hands the new URL to the active engine,
  // or replaces that engine when its start-position config changes) and
  // no rAF defer (the canvas masks first paint via z-ordering, not
  // opacity timing).
  const beginSourceRemount = useCallback(
    (applyChanges: () => void) => {
      captureFrame();
      setReloading(true);
      applyChanges();
    },
    [captureFrame, setReloading],
  );

  // While a seek is in flight, the player UI displays this value
  // instead of the live `currentTime` — otherwise the time-display and
  // progress bar bounce to 0:00 / 0% the instant the source URL swap
  // detaches the old MediaSource (which resets `video.currentTime` to
  // 0 until the new source's `loadedmetadata` pre-positions it back to
  // the target). The display target stays pinned for the entire
  // `reloading` window (covers both in-buffer direct seeks and
  // out-of-buffer remount seeks), then clears with a short fade so the
  // live `currentTime` can take back over once playback resumes.
  const [seekDisplayTarget, setSeekDisplayTarget] = useState<number | null>(
    null,
  );
  const seekDisplayClearTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const armSeekDisplay = useCallback((target: number) => {
    if (seekDisplayClearTimerRef.current) {
      clearTimeout(seekDisplayClearTimerRef.current);
      seekDisplayClearTimerRef.current = null;
    }
    setSeekDisplayTarget(target);
  }, []);
  // Clear the display target shortly after `reloading` flips false.
  // The brief delay lets the player render one frame at the resume
  // position before swapping from `seekDisplayTarget` back to the live
  // `currentTime`, which avoids a visible bounce when the
  // post-`canPlay` `currentTime` update lags the spinner-clear by a
  // few ms.
  useEffect(() => {
    if (reloading) return;
    if (seekDisplayTarget == null) return;
    if (seekDisplayClearTimerRef.current) {
      clearTimeout(seekDisplayClearTimerRef.current);
    }
    seekDisplayClearTimerRef.current = setTimeout(() => {
      seekDisplayClearTimerRef.current = null;
      setSeekDisplayTarget(null);
    }, 100);
    return () => {
      if (seekDisplayClearTimerRef.current) {
        clearTimeout(seekDisplayClearTimerRef.current);
        seekDisplayClearTimerRef.current = null;
      }
    };
  }, [reloading, seekDisplayTarget]);
  useEffect(
    () => () => {
      if (seekDisplayClearTimerRef.current) {
        clearTimeout(seekDisplayClearTimerRef.current);
      }
    },
    [],
  );

  // `wasPaused: !allowAutoplay` on the initial / scene-change resume so
  // a deep-link `?t=` doesn't autoplay when the user has the global
  // autostart toggle off. Source-change pending-resumes — written by
  // `handleSourceChange` and the seek-past-buffer src-swap path —
  // overwrite this with the live `paused` state, which is the desired
  // behaviour for cross-swap continuity.
  const allowAutoplayRef = useRef(allowAutoplay);
  useEffect(() => {
    allowAutoplayRef.current = allowAutoplay;
  }, [allowAutoplay]);
  const pendingResumeRef = useRef<{
    wasPaused: boolean;
    seekTo: number | null;
    playbackRate?: number;
  } | null>(
    initialResume.seekTo != null
      ? { wasPaused: !allowAutoplay, seekTo: initialResume.seekTo }
      : null,
  );

  const lastSceneIdRef = useRef(scene.id);
  useEffect(() => {
    if (lastSceneIdRef.current === scene.id) return;
    lastSceneIdRef.current = scene.id;
    setOffsetStart(initialResume.offset);
    setFragmentTime(initialResume.seekTo);
    setManualSource(null);
    setReloading(false);
    setReloadNonce(0);
    pendingResumeRef.current = {
      wasPaused: !allowAutoplayRef.current,
      seekTo: initialResume.seekTo,
    };
  }, [scene.id, initialResume, setReloading]);

  const activeSource = manualSource ?? getPreferredSource(sources);
  const activeSrc = activeSource?.src;

  const finalSrc = useMemo(() => {
    const withReloadNonce = (url: string): string => {
      if (reloadNonce === 0) return url;
      // Cachebuster appended only to the playlist / file URL. For HLS
      // the in-manifest segment URIs are absolute paths returned by
      // the server and don't carry this param, so segment caching is
      // unaffected — only the playlist re-fetch is forced. The server
      // ignores unknown query params.
      const sep = url.includes("?") ? "&" : "?";
      return `${url}${sep}_r=${reloadNonce}`;
    };
    if (!activeSrc) return undefined;
    if (usesStartOffset(activeSrc)) {
      // HLS: encode the desired scene-time start position into the URL
      // as `?start=N`. The frontend parses this back out in
      // `StableHlsVideo` to set hls.js `config.startPosition`, so the
      // FIRST segment request lands on the segment containing N (no
      // segment-0 cold-start detour). Server-side, the value is
      // ignored for playlist generation when `?end=` is absent (the
      // playlist always covers segments 0..last) — see
      // `serveHLSManifestFMP4`.
      //
      // For the marker / clip case (clipRange set), `?start=` is
      // pinned to `clipRange.start` (NOT the live playhead) so the
      // server's playlist always covers the full clip [start, end].
      // Otherwise a mid-clip resolution swap would walk the playlist
      // floor forward to the playhead, leaving the clip's earlier
      // range unreachable until the user re-opens the marker. `?end=`
      // pins the upper bound. Together they trim to
      // `[⌊start/segDur⌋, ⌈end/segDur⌉-1]`, MSE-time rebases to 0 at
      // the clip start, and native iOS fullscreen sees only the clip
      // range as seekable.
      const trim = clipRange ? clipRange.start : (fragmentTime ?? 0);
      let url = injectStartOffset(activeSrc, trim);
      if (clipRange) {
        url = injectEndOffset(url, clipRange.end);
      }
      return withReloadNonce(url);
    }
    if (
      clipRange &&
      isDirectStreamSrc(activeSrc) &&
      supportsFragmentTime(activeSrc)
    ) {
      // Direct-stream marker on non-iOS browsers (the sources
      // candidate filter above drops direct stream from clip mode on
      // iOS). Use the Media Fragments URI temporal range `#t=N,M`:
      // browsers respecting it seek to N at load and stop playback at
      // M. Combined with `PlaybackRangeEffect`'s scene-time stop, the
      // clip boundary is enforced both natively and at the React
      // layer. Our custom fullscreen (`Element.requestFullscreen` on
      // the player container) keeps the clipRange-aware custom
      // controls visible, so the fullscreen slider already reads
      // clip-relative — no `video.duration` rewrite needed.
      return withReloadNonce(
        injectFragmentTimeRange(activeSrc, clipRange.start, clipRange.end),
      );
    }
    if (
      fragmentTime != null &&
      fragmentTime > 0 &&
      supportsFragmentTime(activeSrc)
    ) {
      return withReloadNonce(injectFragmentTime(activeSrc, fragmentTime));
    }
    return withReloadNonce(activeSrc);
  }, [activeSrc, fragmentTime, clipRange, reloadNonce]);

  const handleSourceChange = useCallback(
    (source: PlayerSource) => {
      const s = storeRef.current;
      if (!s) {
        return;
      }

      // Saved label collapses direct + remux into one preference slot
      // ("Direct stream"). They're interchangeable best-quality tiers,
      // and only one is offered per browser+scene; saving them under
      // the same key means the user's "best available" choice persists
      // even when the highest tier on the next scene is the other of
      // the two.
      const persistedLabel =
        source.label && BEST_QUALITY_LABELS.has(source.label)
          ? "Direct stream"
          : source.label;

      // No-op when the picked source is already active. Without this
      // guard the rest of the function still flips `reloading` true
      // and stashes a `pendingResumeRef`, but the URL doesn't change —
      // `finalSrc` stays the same — so the `<video>` doesn't reload,
      // `canPlay` never re-fires, and `handleCanPlay` never clears the
      // reloading state. Result: spinner pinned forever. Still update
      // the saved-quality preference so a click on an already-active
      // "Direct stream" entry locks in the user's preference even when
      // nothing else changes.
      if (source.src === activeSrc) {
        if (persistedLabel) {
          localStorage.setItem(QUALITY_STORAGE_KEY, persistedLabel);
        }
        return;
      }

      if (persistedLabel) {
        localStorage.setItem(QUALITY_STORAGE_KEY, persistedLabel);
      }

      // Clip-ended source changes restart from scene-time 0 rather than
      // carrying the end-of-clip playhead through the URL's `?start=`.
      // The server falls back to a usable playlist when `?start=` lands
      // at/past EOF (so the video itself replays fine), but `offsetStart`
      // gets pinned at the segment-aligned end-of-scene and the time
      // display — `offsetStart + currentTime` — then reads sceneDuration
      // + N and ticks upward past the actual duration.
      const endedAtSwitch = s.state.ended;
      const trueTime = endedAtSwitch ? 0 : offsetStart + s.state.currentTime;
      const wasPaused = endedAtSwitch ? false : s.state.paused;
      const playbackRate = s.state.playbackRate;

      // Pin the seekbar / time display at the current scene-time
      // through the reload window. Without this, the engine recreate
      // resets `video.currentTime` to 0 the moment the old
      // MediaSource detaches, and the player UI bounces to "0:00 /
      // 0%" until `handleLoadedMetadata` pre-positions the new
      // source. Same lifecycle as `handleSeek`'s `armSeekDisplay`
      // call — cleared by the `reloading` watcher once playback
      // resumes.
      armSeekDisplay(trueTime);

      beginSourceRemount(() => {
        // State updates flow through to `<VideoComponent>`'s `src`
        // prop and our bridge assigns it directly to HlsJsMedia,
        // performing an in-place source swap on the existing `<video>`.
        // No Provider remount, no fresh hls.js instance, so no
        // `startTransition` wrapper.
        const newStrategy = startOffsetStrategyFor(
          source.src,
          frameRate,
          isClipped,
          clipRange,
        );
        if (newStrategy) {
          const split = newStrategy.planResume(trueTime);
          pendingResumeRef.current = {
            wasPaused,
            seekTo: split.seekTo,
            playbackRate,
          };
          setOffsetStart(split.offset);
          setFragmentTime(split.seekTo);
          setManualSource(source);
        } else {
          // Direct stream (no HLS-style strategy). `<video>.currentTime`
          // reads as scene-time directly — for clipped mode the URL
          // carries `#t=start,end` which constrains playback to the
          // range but doesn't rebase the timeline. So `offsetStart`
          // stays at 0.
          pendingResumeRef.current = {
            wasPaused,
            seekTo: trueTime,
            playbackRate,
          };
          setOffsetStart(0);
          setFragmentTime(trueTime > 0 ? trueTime : null);
          setManualSource(source);
        }
      });
    },
    [
      activeSrc,
      offsetStart,
      beginSourceRemount,
      storeRef,
      frameRate,
      isClipped,
      clipRange,
      armSeekDisplay,
    ],
  );

  // Hold the dim+spinner up through an in-place seek (no Provider
  // remount) until the new position is actually decoding frames.
  //
  // The caller is expected to have already paused the video and called
  // `s.seek(...)` so the spinner overlays a frozen frame instead of an
  // old-position video that keeps playing under the dim. After `seeked`
  // fires (frame at target is decoded), this helper either:
  //   - calls `s.play()` and waits for the `playing` event to clear
  //     the spinner (`shouldResume` — slider seek that was playing,
  //     or marker-tap which always wants playback), or
  //   - clears the spinner immediately on `seeked` (paused-state seek;
  //     no `playing` event will ever fire).
  // A 5 s safety timeout catches the rare case where neither fires
  // (e.g. seek aborted by a follow-up source change). Combined with
  // the 250 ms minimum duration baked into `setReloading`, this gives
  // short in-buffer seeks a brief flicker and long out-of-buffer seeks
  // the full dim+spinner through the buffer wait — all paths converge
  // to "old frame holds, spinner shows, new position plays, spinner
  // clears".
  const awaitSeekReady = useCallback(
    (video: HTMLVideoElement, shouldResume: boolean) => {
      let cleared = false;
      const clear = () => {
        if (cleared) return;
        cleared = true;
        video.removeEventListener("seeked", onSeeked);
        video.removeEventListener("playing", onPlaying);
        clearTimeout(timeoutId);
        // Idempotent — direct-seek paths don't snapshot, so this is a
        // no-op there. Guards the case where a captured frame survived
        // a follow-up direct seek without going through handleCanPlay.
        clearCapturedFrame();
        setReloading(false);
      };
      const onSeeked = () => {
        if (shouldResume) {
          // Restart playback; spinner clears once the new position is
          // actually decoding frames (`playing` event below).
          storeRef.current?.play();
        } else {
          // Stay paused — clear immediately, no `playing` event will
          // ever fire to release the spinner otherwise.
          clear();
        }
      };
      const onPlaying = () => clear();
      const timeoutId = setTimeout(clear, 5000);
      video.addEventListener("seeked", onSeeked, { once: true });
      video.addEventListener("playing", onPlaying, { once: true });
    },
    [setReloading, storeRef, clearCapturedFrame],
  );

  const handleSeek = useCallback(
    (targetTrueTime: number) => {
      const s = storeRef.current;
      if (!s || !activeSrc) {
        return;
      }
      const maxT = fileDuration ?? Infinity;
      const clamped = Math.max(0, Math.min(targetTrueTime, maxT));
      armSeekDisplay(clamped);

      // Both seek paths below run the same dim+spinner sequence so
      // in-buffer slider seeks and out-of-buffer/before-trim seeks feel
      // identical: pause + frame holds + spinner + dim during the seek
      // / buffer wait, then resume the moment the new position is
      // decoding. Pausing up-front matters visually — without it, the
      // old position keeps playing under the dim while the spinner
      // spins, which reads as "the player ignored my seek".
      const runDirectSeek = (mediaTime: number) => {
        const wasPlaying = !s.state.paused;
        s.pause();
        setReloading(true);
        void s.seek(mediaTime);
        const video = rootRef.current?.querySelector("video");
        if (video instanceof HTMLVideoElement) {
          awaitSeekReady(video, wasPlaying);
        } else {
          if (wasPlaying) void s.play();
          setReloading(false);
        }
      };

      const strategy = startOffsetStrategyFor(
        activeSrc,
        frameRate,
        isClipped,
        clipRange,
      );
      // Translate scene-time → media-element time before handing to
      // `runDirectSeek`. For direct stream (no HLS strategy):
      //   - non-clip: offsetStart=0, targetInternal == clamped (scene-time).
      //   - clip: offsetStart=clipStart, targetInternal is clip-relative
      //     time, which is what `<clip.mp4>`'s `<video>.currentTime`
      //     expects.
      // For HLS: targetInternal is MSE-time (scene-time minus
      // segment-aligned trim) — what hls.js's `s.seek` writes.
      const targetInternal = clamped - offsetStart;
      if (!strategy) {
        runDirectSeek(targetInternal);
        return;
      }

      if (strategy.canSeekDirectly(targetInternal, s)) {
        runDirectSeek(targetInternal);
        return;
      }

      // canSeekDirectly returned false — needs an hls.js reset.
      //
      // Two reset paths depending on whether the playlist is clipped:
      //
      // (1) Non-clipped (full playlist): in-place flush + restart on
      //     the EXISTING hls.js instance. The MediaSource stays
      //     attached, so the `<video>` element holds its last decoded
      //     frame natively during the catch-up window. Critical for
      //     iOS Safari native fullscreen where our React-side canvas
      //     mask is invisible and any MediaSource detach would flash
      //     black.
      //
      //     `flushAndRestartAt` calls `engine.stopLoad()`,
      //     `engine.trigger("hlsBufferFlushing", ...)` to clear stale
      //     SourceBuffer contents, then `engine.startLoad(target)` to
      //     reorient the segment scheduler. We avoid the
      //     `detachMedia/attachMedia` sequence that
      //     `InterstitialsController` interferes with by tracking and
      //     restoring playback position across re-attach.
      //
      // (2) Clipped (marker / clip mode): the source URL has to
      //     change because the playlist's segment range is encoded in
      //     `?start=&end=`. URL-change → our bridge reassigns
      //     `HlsJsMedia.src` and start-position config → `load()`
      //     destroys + recreates the
      //     `HlsJsMedia` delegate. Freeze-frame canvas masks the
      //     brief MSE-teardown.
      // The in-place flush+restart path doesn't survive iOS Safari:
      // hls.js's BUFFER_FLUSHING + appendBuffer cycle against the MMS
      // SourceBuffer leaves `video.buffered` stuck at empty after a
      // far-forward seek, and playback never resumes (readyState
      // pinned at 1 indefinitely). The behaviour is the same in or
      // out of native fullscreen — iOS's ManagedMediaSource doesn't
      // recover from a mid-flight flush the way desktop MSE does. We
      // fall back to a URL-change remount on every iOS far-seek (new
      // `?start=<target>` → fresh hls.js engine with
      // `config.startPosition = target` cold-starts at the right
      // fragment). Briefly flashes the freeze-frame canvas instead of
      // a clean in-place reset, but completes correctly. Every
      // non-iOS context (desktop / Android Chrome) stays on the
      // in-place path.
      const engine =
        !isClipped && !isIOS() ? getHlsEngine(mediaRef.current) : null;
      if (engine) {
        const wasPlaying = !s.state.paused;
        s.pause();
        captureFrame();
        setReloading(true);
        flushAndRestartAt(engine, targetInternal);
        // Set currentTime AFTER stopLoad/flush so the seek lands on
        // the new (clean) buffer state. hls.js's natural seek handler
        // will pick up the new currentTime; `startLoad(target)` has
        // already told the scheduler where to fetch from.
        void s.seek(targetInternal);
        const video = rootRef.current?.querySelector("video");
        if (video instanceof HTMLVideoElement) {
          awaitSeekReady(video, wasPlaying);
        } else {
          if (wasPlaying) void s.play();
          setReloading(false);
        }
        return;
      }

      // Clipped or iOS fallback (no in-place engine reset): URL change.
      const wasPausedAtSeek = s.state.paused;
      const playbackRateAtSeek = s.state.playbackRate;
      s.pause();
      beginSourceRemount(() => {
        const split = strategy.planResume(clamped);
        pendingResumeRef.current = {
          wasPaused: wasPausedAtSeek,
          seekTo: split.seekTo,
          playbackRate: playbackRateAtSeek,
        };
        setOffsetStart(split.offset);
        setFragmentTime(split.seekTo);
      });
    },
    [
      activeSrc,
      offsetStart,
      fileDuration,
      armSeekDisplay,
      beginSourceRemount,
      captureFrame,
      mediaRef,
      storeRef,
      frameRate,
      isClipped,
      clipRange,
      rootRef,
      awaitSeekReady,
      setReloading,
    ],
  );

  const handleRestart = useCallback(
    (targetTrueTime: number) => {
      const s = storeRef.current;
      if (!s || !activeSrc) {
        return;
      }
      const max =
        fileDuration != null && fileDuration > 0 ? fileDuration : Infinity;
      const clamped = Math.max(0, Math.min(targetTrueTime, max));

      const strategy = startOffsetStrategyFor(
        activeSrc,
        frameRate,
        isClipped,
        clipRange,
      );
      const targetInternal = clamped - offsetStart;

      // Direct path: the current playlist already covers `clamped`.
      // Pause + seek up-front so the dim+spinner overlay sits over a
      // frozen frame instead of an old-position video that keeps
      // playing under it; `awaitSeekReady` calls `s.play()` once the
      // seek completes (`seeked` event) and clears the spinner once
      // playback actually resumes (`playing` event). The play call
      // running outside the click handler is fine — iOS's "user
      // activation" carries for ~5 s past the tap, and the in-buffer
      // direct path resolves `seeked` within milliseconds.
      if (
        !strategy ||
        (targetInternal >= 0 && strategy.canSeekDirectly(targetInternal, s))
      ) {
        s.pause();
        setReloading(true);
        void s.seek(Math.max(0, targetInternal));
        const video = rootRef.current?.querySelector("video");
        if (video instanceof HTMLVideoElement) {
          awaitSeekReady(video, true);
        } else {
          void s.play();
          setReloading(false);
        }
        return;
      }

      // canSeekDirectly returned false. Two paths — same split as
      // handleSeek: in-place flush+restart for non-iOS non-clipped
      // (keeps the MediaSource attached, video shows last decoded
      // frame natively), URL-change remount for clipped or iOS
      // (playlist range must change, or MMS can't recover from a
      // mid-flight flush — see handleSeek for the full rationale).
      const engine =
        !isClipped && !isIOS() ? getHlsEngine(mediaRef.current) : null;
      if (engine) {
        const target = Math.max(0, targetInternal);
        s.pause();
        captureFrame();
        setReloading(true);
        flushAndRestartAt(engine, target);
        void s.seek(target);
        const video = rootRef.current?.querySelector("video");
        if (video instanceof HTMLVideoElement) {
          awaitSeekReady(video, true);
        } else {
          void s.play();
          setReloading(false);
        }
        return;
      }
      s.pause();
      beginSourceRemount(() => {
        const split = strategy.planResume(clamped);
        pendingResumeRef.current = {
          wasPaused: false,
          seekTo: split.seekTo,
        };
        setOffsetStart(split.offset);
        setFragmentTime(split.seekTo);
      });
    },
    [
      activeSrc,
      offsetStart,
      fileDuration,
      beginSourceRemount,
      captureFrame,
      mediaRef,
      storeRef,
      frameRate,
      isClipped,
      clipRange,
      rootRef,
      awaitSeekReady,
      setReloading,
    ],
  );

  // Force a URL-change remount at `targetTrueTime` (scene-time)
  // regardless of whether the current playlist could cover the seek
  // in-place. Used by the stall watchdog to recover from a stuck
  // resume — the engine's existing connection has died (transcode
  // reaped after the server's idle window, or the mobile browser
  // closed the idle HTTP socket) and the only reliable fix is to drop
  // it and start over. Models on `handleRestart`'s URL-change branch,
  // but skips the `canSeekDirectly` shortcut and bumps `reloadNonce`
  // so the URL differs from the current `finalSrc` even when the
  // target lands on the same segment-aligned `?start=` (HLS) or `#t=`
  // (direct).
  const forceRemountAt = useCallback(
    (targetTrueTime: number) => {
      const s = storeRef.current;
      if (!s || !activeSrc) return;
      const max =
        fileDuration != null && fileDuration > 0 ? fileDuration : Infinity;
      const clamped = Math.max(0, Math.min(targetTrueTime, max));

      const strategy = startOffsetStrategyFor(
        activeSrc,
        frameRate,
        isClipped,
        clipRange,
      );
      const playbackRate = s.state.playbackRate;
      s.pause();
      beginSourceRemount(() => {
        if (strategy) {
          const split = strategy.planResume(clamped);
          pendingResumeRef.current = {
            wasPaused: false,
            seekTo: split.seekTo,
            playbackRate,
          };
          setOffsetStart(split.offset);
          setFragmentTime(split.seekTo);
        } else {
          // Direct stream: <video>.currentTime is scene-time, offsetStart
          // stays 0. The cachebuster (below) is what makes the URL change.
          pendingResumeRef.current = {
            wasPaused: false,
            seekTo: clamped,
            playbackRate,
          };
          setOffsetStart(0);
          setFragmentTime(clamped > 0 ? clamped : null);
        }
        setReloadNonce((n) => n + 1);
      });
    },
    [
      activeSrc,
      fileDuration,
      beginSourceRemount,
      storeRef,
      frameRate,
      isClipped,
      clipRange,
    ],
  );

  // Pre-position the `<video>` synchronously inside `loadedmetadata`,
  // before the user agent's internal autoplay path can begin emitting
  // frames. The URL already carries `#t=N` (direct stream) or `?start=N`
  // (HLS / transcode), but Safari races the autoplay path against its
  // own fragment-seek processing and briefly flashes frame 0 before
  // snapping to the target — the symptom is most visible on the marker
  // lightbox, where the user sees a slice of the scene start before the
  // marker. Setting `currentTime` here pins the playhead before play()
  // can fire (per HTML spec, `loadedmetadata` precedes the autoplay
  // task), and `handleCanPlay`'s subsequent `s.seek(target)` becomes a
  // no-op when the playhead is already there.
  //
  // `pending.seekTo` is in scene-time. For HLS, the MSE timeline is
  // rebased by hls.js to start at 0 (= scene-time `offsetStart`), so
  // `<video>.currentTime` is in MSE-time. Subtract `offsetStart` to
  // land on the right frame. For non-HLS sources `offsetStart === 0`
  // and the subtraction is a no-op.
  const handleLoadedMetadata = useCallback(
    (e: SyntheticEvent<HTMLVideoElement>) => {
      const pending = pendingResumeRef.current;
      if (!pending || pending.seekTo == null || pending.seekTo <= 0) return;
      const max =
        fileDuration != null && fileDuration > 0 ? fileDuration : Infinity;
      const sceneTarget = Math.min(Math.max(0, pending.seekTo), max);
      const mediaTarget = Math.max(0, sceneTarget - offsetStart);
      try {
        e.currentTarget.currentTime = mediaTarget;
      } catch {
        /* ignore — not seekable yet, canPlay path will retry */
      }
    },
    [fileDuration, offsetStart],
  );

  const handleCanPlay = useCallback(async () => {
    const pending = pendingResumeRef.current;
    const s = storeRef.current;
    if (!s) return;

    let seekPromise: Promise<unknown> | undefined;

    if (pending) {
      if (pending.seekTo != null) {
        // Clamp to known file bounds, NOT to `s.state.seekable`. For HLS
        // playlists (both transcoded and fMP4 remux) seekable is populated
        // incrementally as the player parses segments, and at the first
        // `canPlay` it can report a range narrower than the marker target —
        // clamping to that would erroneously snap a 30s marker down to a
        // few seconds. `fileDuration` comes from the file metadata, is
        // stable, and accurately bounds the valid seek range.
        const max =
          fileDuration != null && fileDuration > 0 ? fileDuration : Infinity;
        const sceneTarget = Math.min(Math.max(0, pending.seekTo), max);
        // `pending.seekTo` is scene-time. `s.seek` writes through to
        // `<video>.currentTime`, which for HLS is MSE-time (hls.js
        // rebases the timeline to start at 0 via `timestampOffset =
        // -initPTS`). Subtract `offsetStart` to land on the right frame;
        // for non-HLS `offsetStart === 0` so this is a no-op.
        const mediaTarget = Math.max(0, sceneTarget - offsetStart);
        // `s.seek()` resolves on the `seeked` event (see @videojs/core
        // dom/store/features/time). Awaiting it holds the overlay open
        // until the new <video> is showing the resume frame rather than
        // the URL's start frame, so source switches don't flash PTS 0
        // before settling at the seek target.
        //
        // Skip the seek when the playhead is already there — HTML5
        // suppresses the `seeked` event when `currentTime` is set to
        // its current value, so awaiting `s.seek()` would hang forever.
        // This hits on every HLS source change that goes through
        // `handleLoadedMetadata`'s pre-positioning (Safari race fix):
        // by canplay, `<video>.currentTime` is already at `mediaTarget`
        // and `s.seek(mediaTarget)` queues a no-op write whose seeked
        // event never fires. Without this guard the spinner stays
        // pinned, `setReloading(false)` never runs, and the video sits
        // paused after a large seek / marker tap.
        const video = rootRef.current?.querySelector("video");
        const liveCurrent =
          video instanceof HTMLVideoElement ? video.currentTime : null;
        const alreadyAtTarget =
          liveCurrent != null && Math.abs(liveCurrent - mediaTarget) < 0.05;
        if (!alreadyAtTarget) {
          seekPromise = s.seek(mediaTarget);
        }
      }
      if (pending.playbackRate != null && pending.playbackRate !== 1) {
        s.setPlaybackRate(pending.playbackRate);
      }
      pendingResumeRef.current = null;
    }

    if (seekPromise) {
      await seekPromise;
      // Source-changed-again-while-seeking: a fresh source-change
      // populated pendingResumeRef again. Its own canplay handler
      // owns the overlay teardown — bail so we don't tear down
      // through it.
      if (pendingResumeRef.current != null) {
        return;
      }
    }
    clearCapturedFrame();
    setReloading(false);

    // Resume / pause based on the captured pre-swap state. The
    // `<video autoplay>` attribute only fires on the initial resource
    // selection; subsequent src reassignments (which is every in-place
    // swap here) don't trigger it. So we explicitly drive the play /
    // pause decision from `pendingResumeRef.wasPaused`.
    if (pending) {
      if (pending.wasPaused) {
        s.pause();
      } else {
        // Stable `<video>` retains user-gesture activation across the
        // src swap, so this `play()` works without the muted hack the
        // remount-era code needed.
        void s.play();
      }
    }
  }, [
    rootRef,
    storeRef,
    fileDuration,
    offsetStart,
    setReloading,
    clearCapturedFrame,
  ]);

  // Fire `navigator.sendBeacon` to release any HLS transcode the
  // server has running for this scene when:
  //   - The active source switches from HLS to a non-HLS source
  //     (direct stream). The server-side sibling-kill in
  //     `ServeSegment` doesn't fire for this case because direct
  //     stream doesn't go through `ServeSegment`.
  //   - The player unmounts (tab close, page navigation).
  //
  // We deliberately SKIP the beacon for HLS→HLS swaps. The
  // sibling-kill handles those server-side and would otherwise race
  // with the beacon — if the beacon arrived after the new stream's
  // first segment request, it would tear down the freshly-created
  // stream too. For HLS→non-HLS and unmount, no new HLS stream is
  // about to be created, so killing every HLS stream for the scene
  // is safe.
  //
  // `finalSrcRef` mirrors the current `finalSrc` synchronously each
  // render so the cleanup callback can see the *new* value (the
  // source we're transitioning TO) and skip the beacon when it's
  // still HLS.
  const finalSrcRef = useRef(finalSrc);
  finalSrcRef.current = finalSrc;
  const sceneIdRef = useRef(scene.id);
  sceneIdRef.current = scene.id;
  useEffect(() => {
    if (!finalSrc || !isHlsPlaylist(finalSrc)) return;
    const sendStop = (keepUrl?: string) => {
      try {
        let url = `/scene/${sceneIdRef.current}/streams.stop`;
        if (keepUrl) {
          const keepType = hlsStreamTypeName(keepUrl);
          if (keepType) {
            const params = new URLSearchParams({ keep_type: keepType });
            const keepRes = streamResolution(keepUrl);
            if (keepRes) params.set("keep_resolution", keepRes);
            url = `${url}?${params.toString()}`;
          }
        }
        navigator.sendBeacon(url);
      } catch {
        /* best-effort — if sendBeacon isn't supported, the
           server-side idle timeout still cleans up after `maxIdleTime` */
      }
    };
    // `pagehide` covers the cases React unmount can't see: tab close,
    // navigation away from the SPA, browser back/forward, hard
    // refresh. Fires *during* unload, which is exactly what
    // `sendBeacon` is designed for. Not using `beforeunload` because
    // it can trigger a "leave site?" prompt on some browsers.
    const onPageHide = () => sendStop();
    window.addEventListener("pagehide", onPageHide);
    return () => {
      window.removeEventListener("pagehide", onPageHide);
      // At cleanup time, `finalSrcRef.current` holds the value
      // we're transitioning to (or the unchanged last value, on
      // unmount).
      const next = finalSrcRef.current;
      if (next && isHlsPlaylist(next) && next !== finalSrc) {
        // HLS → another HLS source (resolution / variant swap). Tell
        // the server which stream we're transitioning TO so it can
        // tear down the old one immediately without touching the
        // freshly-starting new one. The `ServeSegment` sibling-kill
        // still backs us up if the beacon races or is dropped.
        sendStop(next);
        return;
      }
      sendStop();
    };
  }, [finalSrc]);

  // HLS transcode keepalive. While the player is paused, segment
  // fetches stop, and the server reaps the transcode after
  // `maxIdleTime` (`pkg/ffmpeg/stream_segmented.go`). On resume the
  // user then sees a buffer-exhaust → forced remount path that's
  // visibly clunky. This effect prevents that by POSTing
  // `/streams.keepalive` every ~15 s while the live `<video>` is
  // paused, which bumps `lastAccessed` server-side without
  // requesting any actual media. During playback we send nothing —
  // segment fetches keep the timestamp fresh on their own.
  //
  // No-op when the active source is non-HLS (direct stream isn't
  // backed by a transcode process; the watchdog handles its idle
  // socket case separately) or when the page is hidden (iOS
  // background tab restrictions can throttle / block fetch; nothing
  // to keep alive when the user isn't there anyway).
  useEffect(() => {
    if (!finalSrc || !isHlsPlaylist(finalSrc)) return;
    const root = rootRef.current;
    if (!root) return;

    const KEEPALIVE_INTERVAL_MS = 15000;
    const keepType = hlsStreamTypeName(finalSrc);
    if (!keepType) return;
    const keepRes = streamResolution(finalSrc);
    const params = new URLSearchParams({ keep_type: keepType });
    if (keepRes) params.set("keep_resolution", keepRes);
    const sceneId = sceneIdRef.current;
    const url = `/scene/${sceneId}/streams.keepalive?${params.toString()}`;

    let attachedVideo: HTMLVideoElement | null = null;
    let intervalId: ReturnType<typeof setInterval> | null = null;

    const ping = () => {
      if (typeof document !== "undefined" && document.hidden) return;
      try {
        void fetch(url, { method: "POST", keepalive: true });
      } catch {
        /* best-effort — server's idle timeout is the safety net */
      }
    };
    const start = () => {
      if (intervalId != null) return;
      // Fire once immediately so a quick pause→resume after a long
      // idle (e.g. user just got back from being away) refreshes the
      // timestamp before the first 15 s tick.
      ping();
      intervalId = setInterval(ping, KEEPALIVE_INTERVAL_MS);
    };
    const stop = () => {
      if (intervalId == null) return;
      clearInterval(intervalId);
      intervalId = null;
    };

    const onPause = () => start();
    const onPlay = () => stop();

    const attach = (video: HTMLVideoElement) => {
      if (attachedVideo === video) return;
      if (attachedVideo) detach(attachedVideo);
      attachedVideo = video;
      video.addEventListener("pause", onPause);
      video.addEventListener("play", onPlay);
      if (video.paused) start();
    };
    const detach = (video: HTMLVideoElement) => {
      video.removeEventListener("pause", onPause);
      video.removeEventListener("play", onPlay);
    };

    const current = root.querySelector("video");
    if (current instanceof HTMLVideoElement) attach(current);

    const observer = new MutationObserver(() => {
      const next = root.querySelector("video");
      if (next instanceof HTMLVideoElement && next !== attachedVideo) {
        attach(next);
      } else if (!next && attachedVideo) {
        detach(attachedVideo);
        attachedVideo = null;
        stop();
      }
    });
    observer.observe(root, { childList: true, subtree: true });

    return () => {
      observer.disconnect();
      if (attachedVideo) detach(attachedVideo);
      stop();
    };
  }, [finalSrc, rootRef]);

  // Intercept native `seeking` events on the `<video>` element. iOS
  // Safari's native fullscreen player drives `video.currentTime`
  // directly (the user drags the OS slider, the player sets
  // currentTime, no `handleSeek` callsite runs). For HLS, big native
  // seeks land outside the buffered range and trigger the same hls.js
  // scheduler stall we work around for our custom-UI seeks — in-flight
  // fragment loads complete at the OLD MSE position, playback never
  // resumes at the new currentTime.
  //
  // The fix mirrors what we'd do if the seek came through our slider:
  // when the user has stopped scrubbing (no `seeking` events for
  // 250 ms) and the final `currentTime` is outside `buffered`, route
  // it through `handleSeek` for the URL-change remount. `reloading`
  // guards against re-entry — once our remount starts, the listener
  // ignores the resulting cascade of programmatic seeking events from
  // the engine swap.
  const reloadingRef = useRef(reloading);
  reloadingRef.current = reloading;
  const offsetStartRef = useRef(offsetStart);
  offsetStartRef.current = offsetStart;
  const handleSeekRef = useRef(handleSeek);
  handleSeekRef.current = handleSeek;
  useEffect(() => {
    if (!finalSrc || !isHlsPlaylist(finalSrc)) return;
    const root = rootRef.current;
    if (!root) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let attachedVideo: HTMLVideoElement | null = null;

    const isInBuffered = (target: number, buffered: TimeRanges): boolean => {
      for (let i = 0; i < buffered.length; i++) {
        if (target >= buffered.start(i) && target <= buffered.end(i)) {
          return true;
        }
      }
      return false;
    };

    const onSeeking = (e: Event) => {
      if (reloadingRef.current) return;
      const video = e.currentTarget;
      if (!(video instanceof HTMLVideoElement)) return;
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        timer = null;
        if (reloadingRef.current) return;
        const target = video.currentTime;
        if (isInBuffered(target, video.buffered)) return;
        // `target` is MSE-time. handleSeek expects scene-time. For
        // full playlist `offsetStart === 0`, so the addition is a
        // no-op; for clipped (marker) sources, it converts.
        handleSeekRef.current(target + offsetStartRef.current);
      }, 250);
    };

    const attach = (video: HTMLVideoElement) => {
      if (attachedVideo === video) return;
      if (attachedVideo)
        attachedVideo.removeEventListener("seeking", onSeeking);
      attachedVideo = video;
      video.addEventListener("seeking", onSeeking);
    };

    const current = root.querySelector("video");
    if (current instanceof HTMLVideoElement) attach(current);

    // Watch for <video> element replacement (engine swap direct↔HLS
    // changes the React component type, which destroys + recreates
    // the underlying <video>).
    const observer = new MutationObserver(() => {
      const next = root.querySelector("video");
      if (next instanceof HTMLVideoElement && next !== attachedVideo) {
        attach(next);
      } else if (!next && attachedVideo) {
        attachedVideo.removeEventListener("seeking", onSeeking);
        attachedVideo = null;
      }
    });
    observer.observe(root, { childList: true, subtree: true });

    return () => {
      observer.disconnect();
      if (attachedVideo)
        attachedVideo.removeEventListener("seeking", onSeeking);
      if (timer) clearTimeout(timer);
    };
  }, [finalSrc, rootRef]);

  // Stall watchdog. Forces a URL-change remount when either of two
  // independent progress signals stalls past STALL_THRESHOLD_MS:
  //
  //   1. `currentTime` not advancing — engine's connection died
  //      (transcode reaped after the server's idle window in
  //      `maxIdleTime`, or mobile browser tore down an idle HTTP
  //      socket). The user hits play, the resume request goes
  //      nowhere, no bytes flow.
  //
  //   2. `totalVideoFrames` not advancing while `currentTime` is —
  //      iOS Safari's "ghost playback" state. After a screen
  //      lock / unlock cycle, ManagedMediaSource can leave the audio
  //      clock running but the video decoder detached: the seek bar
  //      advances and audio plays, but the `<video>` shows nothing
  //      (black frame frozen at lock-time). No `pause`, no `stalled`,
  //      no `error` — it just looks broken.
  //
  // Either failure → `forceRemountAt(currentPlayhead)` to cold-start
  // a fresh engine / new Range request.
  //
  // Only armed AFTER the first `playing` event for the currently
  // loaded media — during initial cold-start buffering (4K HLS
  // transcode startup can exceed the threshold) `paused=false` +
  // `currentTime=0` is the normal autoplay state, not a stall, and
  // firing the watchdog there would trigger a recovery loop that
  // never lets buffering complete. `loadstart` disarms again so
  // every src swap re-enters the cold-start grace window.
  //
  // Bounded by a recovery cooldown so a remount that itself stalls
  // doesn't loop. `reloadingRef` additionally suppresses the
  // watchdog during the React-side source-change window.
  const forceRemountAtRef = useRef(forceRemountAt);
  forceRemountAtRef.current = forceRemountAt;
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;

    const STALL_THRESHOLD_MS = 4000;
    const CHECK_INTERVAL_MS = 1000;
    const RECOVERY_COOLDOWN_MS = 15000;
    const PROGRESS_EPSILON_S = 0.1;

    let attachedVideo: HTMLVideoElement | null = null;
    let armed = false;
    let lastProgressTime = 0;
    let lastProgressAt = 0;
    let lastFrameCount = 0;
    let lastFrameAt = 0;
    let lastRecoveryAt = 0;

    const readFrameCount = (v: HTMLVideoElement): number | null => {
      // `getVideoPlaybackQuality` is the standard spec name; iOS
      // Safari 15+ and every desktop engine implement it. Returns
      // `null` on the rare browser that doesn't, which disables the
      // frame-stall check while keeping the time-stall check active.
      try {
        return v.getVideoPlaybackQuality?.().totalVideoFrames ?? null;
      } catch {
        return null;
      }
    };

    const resetBaseline = (v: HTMLVideoElement) => {
      lastProgressTime = v.currentTime;
      lastProgressAt = Date.now();
      const frames = readFrameCount(v);
      if (frames != null) lastFrameCount = frames;
      lastFrameAt = Date.now();
    };

    const onLoadStart = (e: Event) => {
      // New media resource loading. Disarm so cold-start buffering
      // (which can exceed the stall threshold for 4K HLS transcodes)
      // doesn't trigger recovery; will re-arm on the next `playing`.
      armed = false;
      const v = e.currentTarget;
      if (v instanceof HTMLVideoElement) resetBaseline(v);
    };
    const onPlaying = (e: Event) => {
      armed = true;
      const v = e.currentTarget;
      if (v instanceof HTMLVideoElement) resetBaseline(v);
    };
    const onPlay = (e: Event) => {
      const v = e.currentTarget;
      if (v instanceof HTMLVideoElement) resetBaseline(v);
    };
    const onTimeUpdate = (e: Event) => {
      const v = e.currentTarget;
      if (!(v instanceof HTMLVideoElement)) return;
      if (Math.abs(v.currentTime - lastProgressTime) > PROGRESS_EPSILON_S) {
        resetBaseline(v);
      }
    };
    const onSeeking = (e: Event) => {
      // Resets the progress baseline — seek arrivals at the new
      // position aren't "stall recovery"; they're the user moving the
      // playhead.
      const v = e.currentTarget;
      if (v instanceof HTMLVideoElement) resetBaseline(v);
    };

    const intervalId = setInterval(() => {
      const v = attachedVideo;
      if (!v || !armed || v.paused || reloadingRef.current) return;
      // Skip while the page is hidden (iPhone screen locked / tab
      // backgrounded). Frame-count progress legitimately stalls
      // there because the compositor isn't drawing, and
      // `forceRemountAt` would issue network requests that may be
      // throttled / fail under iOS background restrictions. The
      // `visibilitychange` handler below resets the baselines on
      // resume so the 4 s grace window applies after the user comes
      // back, not from when they left.
      if (typeof document !== "undefined" && document.hidden) return;
      const now = Date.now();
      // Refresh the frame-count baseline. Done in-loop rather than
      // via an event because there's no DOM event for decoded-frame
      // progress — iOS only exposes the cumulative counter.
      const frames = readFrameCount(v);
      if (frames != null && frames > lastFrameCount) {
        lastFrameCount = frames;
        lastFrameAt = now;
      }
      const timeStalledMs = now - lastProgressAt;
      const framesStalledMs = frames != null ? now - lastFrameAt : 0;
      if (
        timeStalledMs < STALL_THRESHOLD_MS &&
        framesStalledMs < STALL_THRESHOLD_MS
      ) {
        return;
      }
      if (now - lastRecoveryAt < RECOVERY_COOLDOWN_MS) return;
      lastRecoveryAt = now;
      const sceneTime = v.currentTime + offsetStartRef.current;
      forceRemountAtRef.current(sceneTime);
    }, CHECK_INTERVAL_MS);

    const attach = (video: HTMLVideoElement) => {
      if (attachedVideo === video) return;
      if (attachedVideo) detach(attachedVideo);
      attachedVideo = video;
      armed = false;
      video.addEventListener("loadstart", onLoadStart);
      video.addEventListener("playing", onPlaying);
      video.addEventListener("play", onPlay);
      video.addEventListener("timeupdate", onTimeUpdate);
      video.addEventListener("seeking", onSeeking);
      resetBaseline(video);
    };
    const detach = (video: HTMLVideoElement) => {
      video.removeEventListener("loadstart", onLoadStart);
      video.removeEventListener("playing", onPlaying);
      video.removeEventListener("play", onPlay);
      video.removeEventListener("timeupdate", onTimeUpdate);
      video.removeEventListener("seeking", onSeeking);
    };

    const current = root.querySelector("video");
    if (current instanceof HTMLVideoElement) attach(current);

    const observer = new MutationObserver(() => {
      const next = root.querySelector("video");
      if (next instanceof HTMLVideoElement && next !== attachedVideo) {
        attach(next);
      } else if (!next && attachedVideo) {
        detach(attachedVideo);
        attachedVideo = null;
      }
    });
    observer.observe(root, { childList: true, subtree: true });

    // Reset progress baselines whenever the page becomes visible
    // again, so the 4 s stall window starts ticking from "user came
    // back" rather than "user left." Without this, the watchdog
    // would immediately fire on resume after a long lock with a
    // stale `lastFrameAt` from before the screen turned off, even in
    // cases where the engine recovers on its own within a second.
    const onVisibility = () => {
      if (document.hidden) return;
      if (attachedVideo) resetBaseline(attachedVideo);
    };
    document.addEventListener("visibilitychange", onVisibility);

    return () => {
      observer.disconnect();
      clearInterval(intervalId);
      document.removeEventListener("visibilitychange", onVisibility);
      if (attachedVideo) detach(attachedVideo);
    };
  }, [rootRef]);

  return {
    sources,
    activeSource,
    finalSrc,
    offsetStart,
    initialResume,
    reloading,
    seekDisplayTarget,
    freezeFrameCanvas,
    handleSourceChange,
    handleSeek,
    handleRestart,
    handleCanPlay,
    handleLoadedMetadata,
  };
}
