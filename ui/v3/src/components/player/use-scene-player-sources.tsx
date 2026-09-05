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
import { scenePlayerSourceURL } from "./scene-player-source-url";
import { usePlayerTransitionFeedback } from "./use-player-transition-feedback";
import { usePlayerTranscodeSession } from "./use-player-transcode-session";
import { usePlayerRecovery } from "./use-player-recovery";
import { planSceneSeek, planSourceResume } from "./scene-player-transitions";
import { getHlsEngine, flushAndRestartAt } from "./hls";
import {
  BEST_QUALITY_LABELS,
  QUALITY_STORAGE_KEY,
  computeInitialResume,
  filterSources,
  getPreferredSource,
  isDirectStreamSrc,
  startOffsetStrategyFor,
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
  /** Changes when the underlying file bytes are replaced. The hook reloads
   *  the active URL in place while preserving playhead and paused state. */
  sourceRevision: string | undefined;
  /** Outer wrapper used as the `querySelector("video")` root for the
   *  in-place seek choreography (`awaitSeekReady`). */
  rootRef: RefObject<HTMLDivElement | null>;
  /** Player.Player's store, captured by `<StoreBridge>` in the
   *  parent component. Read synchronously inside the handlers. */
  storeRef: RefObject<VideoPlayerStore | null>;
  /** Player.Player's active Media instance, captured by
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
   *  `<Player.Player>` (z-[5]). */
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
   *  target. The src swap is in-place (no player-root remount). */
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
  sourceRevision,
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
  // Cachebuster bumped by every path that explicitly rebuilds the media
  // engine. A logical target can otherwise produce the same URL that's
  // already loaded — most notably an iOS out-of-buffer seek to scene-time
  // 0, where `planResume(0)` removes `?start=`. Without the nonce React
  // wouldn't reassign `<video>.src`, no new `canplay` would arrive, and the
  // loading overlay would remain pinned forever.
  const [reloadNonce, setReloadNonce] = useState(0);
  const {
    reloading,
    setReloading,
    freezeFrameCanvas,
    captureFrame,
    clearCapturedFrame,
    beginSourceRemount,
    seekDisplayTarget,
    armSeekDisplay,
    awaitSeekReady,
  } = usePlayerTransitionFeedback({ rootRef, storeRef, fileWidth, fileHeight });

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

  const finalSrc = useMemo(
    () => scenePlayerSourceURL(activeSrc, fragmentTime, clipRange, reloadNonce),
    [activeSrc, fragmentTime, clipRange, reloadNonce],
  );

  // A metadata edit keeps every stream endpoint URL stable, but the bytes
  // behind it have changed. Bump the existing cachebuster and use the same
  // pending-resume choreography as a quality swap so the player immediately
  // reflects the new orientation without losing its playhead or play state.
  const sourceRevisionRef = useRef({ sceneId: scene.id, sourceRevision });
  useEffect(() => {
    const previous = sourceRevisionRef.current;
    sourceRevisionRef.current = { sceneId: scene.id, sourceRevision };
    if (previous.sceneId !== scene.id) return;
    if (previous.sourceRevision === sourceRevision) return;

    const s = storeRef.current;
    if (!s || !activeSrc) {
      setReloadNonce((nonce) => nonce + 1);
      return;
    }

    const trueTime = offsetStart + s.state.currentTime;
    const wasPaused = s.state.paused;
    const playbackRate = s.state.playbackRate;
    armSeekDisplay(trueTime);
    beginSourceRemount(() => {
      const strategy = startOffsetStrategyFor(
        activeSrc,
        frameRate,
        isClipped,
        clipRange,
      );
      const resume = planSourceResume(strategy, trueTime);
      pendingResumeRef.current = {
        wasPaused,
        playbackRate,
        seekTo: resume.seekTo,
      };
      setOffsetStart(resume.offset);
      setFragmentTime(resume.fragmentTime);
      setReloadNonce((nonce) => nonce + 1);
    });
  }, [
    activeSrc,
    armSeekDisplay,
    beginSourceRemount,
    clipRange,
    frameRate,
    isClipped,
    offsetStart,
    scene.id,
    sourceRevision,
    storeRef,
  ]);

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
        // No player-root remount; HlsJsMedia decides whether the changed
        // structured source requires a fresh hls.js engine, so no
        // `startTransition` wrapper.
        const newStrategy = startOffsetStrategyFor(
          source.src,
          frameRate,
          isClipped,
          clipRange,
        );
        const resume = planSourceResume(newStrategy, trueTime);
        pendingResumeRef.current = {
          wasPaused,
          playbackRate,
          seekTo: resume.seekTo,
        };
        setOffsetStart(resume.offset);
        setFragmentTime(resume.fragmentTime);
        setManualSource(source);
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

  // Effects apply the selected transition in a fixed order. The root and video
  // remain mounted; only a source reload asks the bridge for a fresh engine.
  const performSeek = useCallback(
    (targetTime: number, intent: "seek" | "restart") => {
      const store = storeRef.current;
      if (!store || !activeSrc) return;
      const engine = getHlsEngine(mediaRef.current);
      const transition = planSceneSeek({
        intent,
        targetTime,
        duration: fileDuration,
        offsetStart,
        src: activeSrc,
        frameRate,
        clipRange,
        mediaState: store.state,
        ios: isIOS(),
        hasHlsEngine: engine !== null,
      });
      if (intent === "seek") armSeekDisplay(transition.sceneTime);
      const wasPaused = intent === "restart" ? false : store.state.paused;
      const playbackRate =
        intent === "seek" ? store.state.playbackRate : undefined;
      store.pause();

      if (transition.kind === "reload-source") {
        beginSourceRemount(() => {
          const { resume } = transition;
          pendingResumeRef.current = {
            wasPaused,
            playbackRate,
            seekTo: resume.seekTo,
          };
          setOffsetStart(resume.offset);
          setFragmentTime(resume.fragmentTime);
          // A reload must change the URL even if the requested time is unchanged.
          setReloadNonce((nonce) => nonce + 1);
        });
        return;
      }

      if (transition.kind === "restart-engine") captureFrame();
      setReloading(true);
      if (transition.kind === "restart-engine" && engine) {
        flushAndRestartAt(engine, transition.mediaTime);
      }
      // Seek after flushing, so the write targets the clean buffer state.
      void store.seek(transition.mediaTime);
      const video = rootRef.current?.querySelector("video");
      if (video instanceof HTMLVideoElement) {
        awaitSeekReady(video, !wasPaused);
      } else {
        if (!wasPaused) void store.play();
        setReloading(false);
      }
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
      clipRange,
      rootRef,
      awaitSeekReady,
      setReloading,
    ],
  );
  const handleSeek = useCallback(
    (time: number) => performSeek(time, "seek"),
    [performSeek],
  );
  const handleRestart = useCallback(
    (time: number) => performSeek(time, "restart"),
    [performSeek],
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
        const resume = planSourceResume(strategy, clamped);
        pendingResumeRef.current = {
          wasPaused: false,
          playbackRate,
          seekTo: resume.seekTo,
        };
        setOffsetStart(resume.offset);
        setFragmentTime(resume.fragmentTime);
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

  usePlayerTranscodeSession(scene.id, finalSrc, rootRef);
  usePlayerRecovery({
    finalSrc,
    rootRef,
    reloading,
    offsetStart,
    handleSeek,
    forceRemountAt,
  });

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
