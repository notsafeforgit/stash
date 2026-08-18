/**
 * Scene player — stable-`<video>` element.
 *
 * `Player.Player` mounts once per scene; source-URL changes flow
 * through `<VideoComponent>`'s `src` prop and its media bridge performs
 * an in-place reassignment on the existing element (`hls.loadSource()`
 * for HLS via `HlsJsMedia`, plain `<video>.src` for
 * direct). The `<video>` DOM node, the store, the controls, and every
 * overlay survive every source change — only hls.js's internal engine
 * (the `HlsJsMedia` delegate) is destroyed and recreated when the
 * source URL changes, and only the *fragment scheduler* is reset (no
 * MSE detach) for in-engine seek recovery.
 *
 * Source/seek dispatch (see `use-scene-player-sources.tsx` for the
 * code paths):
 *   - In-buffer seek: direct `video.currentTime` write. No engine
 *     intervention.
 *   - Out-of-buffer seek on a full HLS playlist (non-clipped):
 *     `engine.stopLoad()` + `BUFFER_FLUSHING` + `engine.startLoad()`
 *     — MediaSource stays attached, `<video>` holds the last decoded
 *     frame natively.
 *   - Out-of-buffer seek on a clipped HLS playlist (marker / clip
 *     mode), or in iOS Safari native fullscreen on any HLS source:
 *     URL-change remount (new `?start=` flows through `HlsJsMedia.src`
 *     to recreate the `HlsJsMedia` delegate). The freeze-frame canvas
 *     overlays the brief MSE-teardown window.
 *   - Quality swap / direct↔HLS engine swap: URL change + engine
 *     recreate; freeze-frame canvas masks the visible window.
 *
 * Backend coordination:
 *   - Full HLS playlist is the default; the server only trims when
 *     `?end=` is present (marker-clip mode). See
 *     `pkg/ffmpeg/stream_segmented.go`.
 *   - Per-scene sibling-kill in `ServeSegment` tears down stale
 *     transcodes on quality/codec swap with no idle wait.
 *   - The `streams.stop` beacon (fired on HLS→non-HLS source change
 *     and on `pagehide`) explicitly releases the previous
 *     transcode when no new HLS stream is about to take its place.
 */
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Container,
  createPlayer,
  type CreatePlayerResult,
  type VideoPlayerStore,
} from "@videojs/react";
import { GoogleCast } from "@videojs/react/media/google-cast";
import { Video, videoFeatures } from "@videojs/react/video";
import { StableHlsVideo } from "./stable-hls-video";
import { cn } from "src/lib/utils";
import { Badge } from "src/components/ui/badge";
import { Spinner } from "src/components/ui/spinner";
import { languageMap } from "src/utils/caption";
import { resolution as resolutionLabel } from "src/utils/file";
import type * as GQL from "src/core/generated-graphql";
import { isHlsPlaylist } from "./hls";
import {
  useCodecsDecodableInMp4,
  useVideoCodecDecodableInMp4,
} from "./player-utils";
import type { IMarker } from "./player-utils";
import { PlayerControls, type ClipBoundsEdit } from "./player-controls";
import {
  CanPlayEffect,
  ControlsVisibilityEffect,
  PlaybackRangeEffect,
  PlayerPoster,
  StartedEffect,
} from "./player-overlays";
import { usePlayDelay } from "./use-play-delay";
import { useScenePlayerSources } from "./use-scene-player-sources";
import { VideoFrameZoom, IDENTITY_TRANSFORM } from "./video-frame-zoom";
import type { ZoomTransform } from "./video-frame-zoom";
import "./player.css";

// ── Player factory ────────────────────────────────────────────────────────────
// Video.js v10's `createPlayer` is a factory that returns a typed Player root
// and hooks sharing one feature-set across every player instance rendered in
// the app. `Container` is shared across factories. Call the factory once at
// module scope.
const Player: CreatePlayerResult<VideoPlayerStore> = createPlayer({
  features: videoFeatures,
  displayName: "ScenePlayer",
});

// Non-HLS sources (direct stream) render through `<Video>` — plain
// `<video>` semantics, no media engine. `React.memo` skips reconciliation
// in non-context-driven re-render cases (e.g. menu open/close churn).
const MemoVideo = React.memo(Video);

// HLS sources route through `<StableHlsVideo>` (a wrapper around
// `@videojs/media`'s `HlsJsMedia`, which auto-selects hls.js on MSE-capable
// browsers and falls back to the browser's native HLS otherwise; see
// `./stable-hls-video.tsx` for the stable-attach-ref rationale). hls.js
// runs over MSE for every browser including Safari, giving JS-controlled
// segment loading, HTTP cache hits, and predictable pause/resume — versus
// AVFoundation, which bypasses the HTTP cache and tears down its segment
// loader on user pause (visible re-fetch freezes on resume). The backend
// serves a multivariant master playlist (`/stream.master.m3u8`,
// `/stream.fmp4.master.m3u8`) so hls.js can read codec/resolution/bandwidth
// metadata before allocating SourceBuffers.
const MemoHlsVideo = React.memo(StableHlsVideo);

// ── Persistence keys ──────────────────────────────────────────────────────────

const AUTO_ADVANCE_STORAGE_KEY = "stash-player-auto-advance";

// ── Types ─────────────────────────────────────────────────────────────────────

export type ScenePlayerScene = NonNullable<GQL.FindSceneQuery["findScene"]>;

interface ScenePlayerProps {
  scene: ScenePlayerScene;
  initialTimestamp?: number;
  autoplay?: boolean;
  /**
   * Mirrors the global `interface.autostartVideo` config. When false,
   * the player loads the scene paused regardless of `autoplay`, the
   * stored resume-time, or a deep-link `?t=`. Cross-source-change
   * continuity is preserved by `handleCanPlay` in
   * `use-scene-player-sources.tsx` — if the user was already playing
   * when they swap quality, playback resumes after the in-place src
   * swap even with this off.
   */
  autostartEnabled: boolean;
  permitLoop?: boolean;
  maxLoopDuration?: number;
  onEnded?: () => void;
  onNext?: () => void;
  onPrevious?: () => void;
  sendSetTimestamp?: (setter: (t: number) => void) => void;
  /**
   * Mirror of `sendSetTimestamp` for read access — hands the parent a
   * getter that returns the current playhead in seconds (or `undefined`
   * when the store hasn't initialised). Used by the actions menu's
   * "thumbnail from current position" command.
   */
  sendGetCurrentTime?: (getter: () => number | undefined) => void;
  /**
   * Hands the parent a function that pauses the underlying store. Used by
   * lightbox slide content to pause playback when the user opens a link in
   * a new tab — our tab loses focus and continuing to play would be
   * confusing.
   */
  sendPause?: (pauser: () => void) => void;
  className?: string;
  disableSeekArrows?: boolean;
  /**
   * Media preload strategy, forwarded to the underlying `<video preload>`.
   * Defaults to `"metadata"`; pass `"auto"` when the player is mounted inside
   * an animating container that shouldn't stall on network activity.
   */
  preload?: "none" | "metadata" | "auto";
  fill?: boolean;
  /**
   * When set, replaces the player's built-in fullscreen logic. On
   * hover-capable devices the in-bar fullscreen button is hidden (the
   * assumption is that an external surface — e.g. the lightbox toolbar
   * — provides its own affordance there). Touch devices keep the
   * in-bar button so users aren't stuck hunting for the YARL toolbar's
   * 16px icon. The `f` hotkey calls this instead. Used by
   * `SceneLightbox` so fullscreen captures the entire slide UI (title,
   * swipe gestures, toolbar) rather than just the video element.
   *
   * Return `false` from the override to fall back to the player's
   * native fullscreen path — used when the external surface's fullscreen
   * is unavailable (e.g. YARL's Fullscreen plugin is disabled on iOS
   * Safari pre-16.4). Any other return — including `void` — counts as
   * handled.
   */
  onToggleFullscreenOverride?: () => boolean | undefined;
  /**
   * Controlled loop toggle. When both are provided, the parent owns the
   * loop state — used by `SceneLightbox` so the user's toggle persists
   * across the per-scene player remount that a slide swipe triggers.
   * When omitted, the player manages loop state internally and seeds it
   * from the auto-detected short-clip default.
   */
  loopEnabled?: boolean;
  onLoopToggle?: () => void;
  /**
   * Suppress visible playback for this many milliseconds after mount.
   * The `<video autoPlay>` attribute stays set so iOS's muted-fallback
   * autoplay path still works (a programmatic play() call after this
   * point is treated as a user-gesture-required action and routinely
   * rejected on iOS), but every `playing` event during the window
   * triggers an immediate `pause()` and the player resumes only after
   * the timer fires. Used by the lightbox so the video doesn't start
   * playing while the slide is still animating into place.
   */
  playDelayMs?: number;
  /**
   * Wrap the `<video>` in a transform layer that responds to pinch /
   * trackpad-pinch / drag-when-zoomed gestures. Controls and any other
   * overlays (poster, reload spinner) sit outside the transform so the
   * UI stays fixed while only the video frame scales. Page-level pinch
   * zoom is disabled app-wide via `installPagePinchZoomGuard`; the
   * wrapper marks itself with `data-pinch-zoom-allowed` to opt back in.
   */
  enablePinchZoom?: boolean;
  /**
   * Fired whenever the videojs/react store's `controlsVisible` flag
   * flips. Lets surrounding chrome (e.g. the scene-lightbox overlay
   * with title and performer badges) fade in lockstep with the player's
   * own control bar so a single user-active gesture reveals or hides
   * everything.
   */
  onControlsVisibilityChange?: (visible: boolean) => void;
  /**
   * Extra content rendered inside `Container` above the controls
   * overlay (e.g. the lightbox title / o-counter chrome). Rendering here
   * — rather than as a DOM sibling positioned over the player — keeps
   * the cursor "inside" the player when hovering interactive overlay
   * elements, so videojs/react's `mouseleave`-driven inactivity timer
   * doesn't immediately hide the controls. Pointer events from
   * `pointer-events-auto` descendants bubble to the container, keeping
   * `userActive` alive on hover.
   */
  topOverlay?: React.ReactNode;
  /**
   * Restrict the player's UI to a [start, end] window in scene time. When
   * set, the player presents the range as if it were the entire video:
   *   - The position slider, time display, and skip-step bounds use
   *     `(end - start)` as the effective duration; current time renders
   *     as `sceneTime - start`.
   *   - User seeks (slider, skip-arrow buttons) input clip-relative time;
   *     the parent's underlying `handleSeek` is still invoked in scene
   *     time, so resume / source-switch bookkeeping is unaffected.
   *   - When the playhead reaches `end`, the player either seeks back to
   *     `start` (if `loopEnabled`) or calls `onNext` (if auto-advance is
   *     on). The native `loop` attribute is suppressed so it doesn't
   *     fight the manual range-loop.
   *   - Other markers on the scene are hidden from the timeline; the
   *     lightbox slide is focused on the one marker that owns the clip.
   *
   * Used by the marker lightbox so each marker reads as a standalone
   * clip rather than a position inside a longer scene.
   */
  clipRange?: { start: number; end: number };
  /**
   * Override for the poster image shown above the `<video>` until playback
   * starts. Defaults to `scene.paths.screenshot`. The marker lightbox
   * passes the active marker's `screenshot` here so the load-time placeholder
   * matches the clip the user is about to see, not the scene's cover frame.
   */
  posterSrc?: string;
  /**
   * When set, two draggable handles render on the position slider for
   * editing a marker's start / end. Drags update the bounds via
   * `onChange` (in scene time); playback is unaffected. Set
   * `start`/`end` to `null` to suppress the corresponding handle (e.g.
   * when the marker has no end yet). Used by the scene detail page's
   * marker editor.
   */
  clipBoundsEdit?: ClipBoundsEdit;
}

export type { ClipBoundsEdit };

// ── Store bridge ──────────────────────────────────────────────────────────────
// Parent component owns business logic (handleSeek, handleSourceChange) that
// needs to read store state and call actions. The store is only accessible
// from descendants of Player.Player via `Player.usePlayer()`, so this tiny
// child captures it into a ref the parent can read synchronously.

function StoreBridge({
  storeRef,
}: {
  storeRef: React.MutableRefObject<VideoPlayerStore | null>;
}) {
  const store = Player.usePlayer();
  useEffect(() => {
    storeRef.current = store;
    return () => {
      if (storeRef.current === store) storeRef.current = null;
    };
  }, [store, storeRef]);
  return null;
}

// Captures the live Media instance (HlsJsMedia for HLS sources, plain
// Media wrapper for direct) into a ref so handlers in
// `useScenePlayerSources` can reach hls.js's `Hls` instance via
// `media.engine`. Used by the in-place out-of-buffer seek path:
// `flushAndRestartAt` calls `engine.stopLoad()` +
// `BUFFER_FLUSHING` trigger + `engine.startLoad(target)` to reset
// hls.js's segment scheduler WITHOUT tearing down the MediaSource —
// keeps the `<video>` element's last decoded frame on screen during
// the catch-up window (critical for iOS native fullscreen where our
// canvas overlay isn't visible).
function MediaBridge({
  mediaRef,
}: {
  mediaRef: React.MutableRefObject<unknown>;
}) {
  const media = Player.useMedia();
  useEffect(() => {
    mediaRef.current = media;
    return () => {
      if (mediaRef.current === media) mediaRef.current = null;
    };
  }, [media, mediaRef]);
  return null;
}

// ── Component ─────────────────────────────────────────────────────────────────

export const ScenePlayer: React.FC<ScenePlayerProps> = ({
  scene,
  initialTimestamp,
  autoplay = false,
  autostartEnabled,
  permitLoop = true,
  maxLoopDuration = 0,
  onEnded,
  onNext,
  onPrevious,
  sendSetTimestamp,
  sendGetCurrentTime,
  sendPause,
  className,
  disableSeekArrows,
  preload = "metadata",
  fill,
  onToggleFullscreenOverride,
  loopEnabled: loopEnabledProp,
  onLoopToggle: onLoopToggleProp,
  playDelayMs = 0,
  enablePinchZoom = false,
  onControlsVisibilityChange,
  topOverlay,
  clipRange,
  posterSrc,
  clipBoundsEdit,
}) => {
  // Store handle captured by <StoreBridge> below — lets callbacks read
  // paused/currentTime/playbackRate and call play/pause/seek without being
  // descendants of Player.Player themselves.
  const storeRef = useRef<VideoPlayerStore | null>(null);

  // Media handle captured by <MediaBridge> — gives
  // `useScenePlayerSources` a route to the live hls.js `Hls` instance
  // (via `media.engine`) for the in-place out-of-buffer seek path.
  const mediaRef = useRef<unknown>(null);

  // Outer wrapper used as the fullscreen target. Fullscreening this
  // element (rather than Container) keeps the scope wide enough
  // that the controls bar and any overlays stay onscreen.
  const fullscreenContainerRef = useRef<HTMLDivElement>(null);

  // Pinch / wheel / double-tap zoom transform. Owned at this level so
  // it persists across source changes (including engine swaps where
  // `<VideoComponent>` toggles between `<Video>` and `<StableHlsVideo>`).
  const [zoomTransform, setZoomTransform] =
    useState<ZoomTransform>(IDENTITY_TRANSFORM);

  // Shared cancel hooks between `VideoFrameZoom`'s gesture recognizers and
  // `PlayerControls`'s tap-style timers on the coarse-pointer overlay.
  // PlayerControls populates each ref with a cancel function; VideoFrameZoom
  // invokes `handleZoomActiveGesture` once per recognized gesture (double-tap,
  // pinch start, or pan-active) so those tap-style timers don't accidentally
  // fire mid-gesture (a double-tap-zoom flashing controls, or a long-press
  // 2× kicking in while the user was actually starting to pan).
  const cancelPendingTapToggleRef = useRef<(() => void) | null>(null);
  const cancelPendingHoldRef = useRef<(() => void) | null>(null);
  const temporarySpeedActiveRef = useRef(false);
  const [temporaryPlaybackRate, setTemporaryPlaybackRate] = useState<
    number | null
  >(null);
  const handleZoomActiveGesture = useCallback(() => {
    cancelPendingTapToggleRef.current?.();
    cancelPendingHoldRef.current?.();
  }, []);
  const handleTemporaryPlaybackRateChange = useCallback(
    (rate: number | null) => {
      temporarySpeedActiveRef.current = rate != null;
      setTemporaryPlaybackRate(rate);
    },
    [],
  );
  const isTemporarySpeedActive = useCallback(
    () => temporarySpeedActiveRef.current,
    [],
  );

  const file = scene.files[0];
  const fileDuration = file?.duration ?? undefined;
  const sourceResolution = useMemo(
    () =>
      file && file.width > 0 && file.height > 0
        ? resolutionLabel(file.width, file.height)
        : undefined,
    [file],
  );
  // Mobile (non-fill) uses the file's intrinsic aspect-ratio so the
  // inline player matches the video shape — portrait videos no longer
  // pillarbox inside a 16:9 frame, and wide landscapes (e.g. 21:9) no
  // longer letterbox. Desktop ignores it because `lg:h-full lg:w-full`
  // sets both axes explicitly.
  const videoAspect =
    file && file.width > 0 && file.height > 0
      ? `${file.width} / ${file.height}`
      : undefined;
  const canDecodeSnapshot = useCodecsDecodableInMp4(
    file?.video_codec,
    file?.audio_codec,
  );
  const canDecodeVideoSnapshot = useVideoCodecDecodableInMp4(file?.video_codec);
  const canDecodeRef = useRef(canDecodeSnapshot);
  const canDecodeVideoRef = useRef(canDecodeVideoSnapshot);
  const sceneIdForCanDecodeRef = useRef(scene.id);
  if (sceneIdForCanDecodeRef.current !== scene.id) {
    sceneIdForCanDecodeRef.current = scene.id;
    canDecodeRef.current = canDecodeSnapshot;
    canDecodeVideoRef.current = canDecodeVideoSnapshot;
  }
  const canDecode = canDecodeRef.current;
  const canDecodeVideo = canDecodeVideoRef.current;

  // Auto-detected: true for short clips that should loop by default. The
  // user-controllable `loopEnabled` state below seeds itself from this
  // value on each scene mount; thereafter the user's toggle in the
  // settings menu drives the video element's `loop` attribute.
  const looping = useMemo(
    () =>
      !!fileDuration &&
      permitLoop &&
      maxLoopDuration > 0 &&
      fileDuration < maxLoopDuration,
    [fileDuration, permitLoop, maxLoopDuration],
  );

  // Loop preference. Uncontrolled by default — internal state seeds from
  // the auto-detected short-clip `looping` value and resets on scene
  // change (ScenePlayer remounts via the parent's `key=`). When the
  // parent supplies `loopEnabled` + `onLoopToggle` (lightbox), the parent
  // owns the value so it persists across the per-slide remount.
  const isLoopControlled = loopEnabledProp !== undefined;
  const [internalLoopEnabled, setInternalLoopEnabled] = useState(looping);
  const loopEnabled = isLoopControlled ? loopEnabledProp : internalLoopEnabled;
  const setLoopEnabled = useCallback(
    (value: boolean) => {
      if (isLoopControlled) {
        // Parent owns a simple toggle — only call when flipping is needed.
        if (value !== loopEnabledProp) onLoopToggleProp?.();
      } else {
        setInternalLoopEnabled(value);
      }
    },
    [isLoopControlled, loopEnabledProp, onLoopToggleProp],
  );

  // Session-wide auto-advance preference. Persisted to localStorage so
  // the choice carries across scenes / page loads. Only takes effect when
  // `onNext` is wired up by the parent (i.e. inside the lightbox); on the
  // standalone scene page there's no "next" so the playback-mode cycle
  // skips this state entirely.
  const [autoAdvanceEnabled, setAutoAdvanceEnabled] = useState(() => {
    try {
      return localStorage.getItem(AUTO_ADVANCE_STORAGE_KEY) === "true";
    } catch {
      return false;
    }
  });
  const setAutoAdvancePersisted = useCallback((value: boolean) => {
    setAutoAdvanceEnabled(value);
    try {
      localStorage.setItem(AUTO_ADVANCE_STORAGE_KEY, String(value));
    } catch {
      /* localStorage unavailable */
    }
  }, []);

  // Tri-state playback mode: normal → advance → loop → normal. Auto-advance
  // leads the cycle since users typically want to chain through a marker /
  // scene feed, and loop is the "stay here" exception. `advance` is only
  // available when `onNext` is wired up; otherwise the cycle collapses to
  // normal ↔ loop and the persisted auto-advance preference is treated as
  // off (the standalone scene-detail page should never present an
  // advance affordance even when the lightbox previously left auto-
  // advance turned on). Loop and auto-advance are mutually exclusive —
  // toggling into one always clears the other.
  const canAdvance = !!onNext;
  const advanceActive = canAdvance && autoAdvanceEnabled;
  const playbackMode: "normal" | "loop" | "advance" = loopEnabled
    ? "loop"
    : advanceActive
      ? "advance"
      : "normal";
  const handleCyclePlaybackMode = useCallback(() => {
    const current: "normal" | "loop" | "advance" = loopEnabled
      ? "loop"
      : advanceActive
        ? "advance"
        : "normal";
    let next: "normal" | "loop" | "advance";
    if (current === "normal") next = canAdvance ? "advance" : "loop";
    else if (current === "advance") next = "loop";
    else next = "normal";
    setLoopEnabled(next === "loop");
    // Only mutate the persisted auto-advance flag in contexts that
    // support advance — otherwise cycling through `loop` on the scene-
    // detail page would silently wipe the lightbox's preference.
    if (canAdvance && (next === "advance") !== autoAdvanceEnabled) {
      setAutoAdvancePersisted(next === "advance");
    }
  }, [
    loopEnabled,
    advanceActive,
    autoAdvanceEnabled,
    canAdvance,
    setLoopEnabled,
    setAutoAdvancePersisted,
  ]);

  // Wraps the parent's `onEnded` so auto-advance can intercept. When the
  // user has enabled auto-advance and a `onNext` callback is available,
  // advance instead of firing the generic ended hook (the lightbox uses
  // `onNext` to swipe to the next slide; firing both would double-fire
  // any analytics tied to the ended event).
  const handleEnded = useCallback(() => {
    if (autoAdvanceEnabled && onNext) {
      onNext();
      return;
    }
    onEnded?.();
  }, [autoAdvanceEnabled, onNext, onEnded]);

  const captions = useMemo(() => {
    const captionBasePath = scene.paths.caption;
    if (!captionBasePath) return [];
    return (scene.captions ?? []).map((caption) => {
      const { language_code: lang, caption_type: type } = caption;
      const label =
        (languageMap.get(lang) ?? lang) + (type ? ` (${type})` : "");
      return {
        kind: "captions" as const,
        src: `${captionBasePath}?lang=${lang}&type=${type}`,
        srclang: lang,
        label,
      };
    });
  }, [scene.captions, scene.paths.caption]);

  const markers = useMemo<IMarker[]>(
    () =>
      scene.scene_markers.map((m) => ({
        title: m.title,
        seconds: m.seconds,
        end_seconds: m.end_seconds ?? null,
        primaryTag: { name: m.primary_tag?.name ?? "" },
      })),
    [scene.scene_markers],
  );

  // Reset on scene change. Source-machinery resets are owned by the
  // hook below; only the parent-owned `started` latch is reset here.
  const lastSceneIdRef = useRef(scene.id);
  const [hasEverStarted, setHasEverStarted] = useState(false);
  useEffect(() => {
    if (lastSceneIdRef.current === scene.id) return;
    lastSceneIdRef.current = scene.id;
    setHasEverStarted(false);
  }, [scene.id]);

  // Page-level autoplay gate. Stamps `wasPaused` on the initial
  // pending-resume so a deep-link `?t=` doesn't autoplay when the user
  // has the global autostart toggle off.
  const allowAutoplay = autostartEnabled || (autoAdvanceEnabled && !!onNext);

  const {
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
  } = useScenePlayerSources({
    scene,
    initialTimestamp,
    canDecode,
    canDecodeVideo,
    frameRate: file?.frame_rate ?? undefined,
    fileDuration,
    fileWidth: file?.width ?? undefined,
    fileHeight: file?.height ?? undefined,
    sourceRevision: file?.updated_at,
    rootRef: fullscreenContainerRef,
    storeRef,
    mediaRef,
    allowAutoplay,
    clipRange,
  });

  // Hand the parent `handleRestart` (seek + play) rather than `handleSeek`
  // (seek only). Only consumer is the marker-tab "jump to marker" — user
  // expects playback to start at the marker. Direct-seek path runs
  // `seek + play` synchronously in the click handler so iOS keeps the
  // gesture context; remount path sets `wasPaused: false` so the autoplay
  // recovery in `handleCanPlay` engages once the new playlist is ready.
  useEffect(() => {
    if (!sendSetTimestamp) return;
    sendSetTimestamp(handleRestart);
  }, [sendSetTimestamp, handleRestart]);

  useEffect(() => {
    if (!sendGetCurrentTime) return;
    sendGetCurrentTime(() => {
      const store = storeRef.current;
      if (!store) return undefined;
      return offsetStart + store.state.currentTime;
    });
  }, [sendGetCurrentTime, offsetStart]);

  useEffect(() => {
    if (!sendPause) return;
    sendPause(() => storeRef.current?.pause());
  }, [sendPause]);

  useEffect(() => {
    if (!("mediaSession" in navigator)) return;
    navigator.mediaSession.setActionHandler("nexttrack", onNext ?? null);
    navigator.mediaSession.setActionHandler(
      "previoustrack",
      onPrevious ?? null,
    );
    return () => {
      navigator.mediaSession.setActionHandler("nexttrack", null);
      navigator.mediaSession.setActionHandler("previoustrack", null);
    };
  }, [onNext, onPrevious]);

  // Force-abort the <video>'s network activity on unmount. Rapid lightbox
  // swipes otherwise queue stale fetches behind Chrome's per-host
  // connection pool (default 6) and saturate the backend transcoder, so
  // the next scene's metadata request stalls. `pause()` + clear `src` +
  // `load()` cancels in-flight requests before React detaches the node.
  useEffect(() => {
    const root = fullscreenContainerRef.current;
    return () => {
      const video = root?.querySelector("video");
      if (video instanceof HTMLVideoElement) {
        try {
          video.pause();
          video.removeAttribute("src");
          video.load();
        } catch {
          /* ignore — element already torn down */
        }
      }
    };
  }, []);

  const handleStarted = useCallback(() => setHasEverStarted(true), []);

  // Latched true between the clip-end auto-stop and the next "fresh
  // playthrough" trigger (user-initiated play after the stop, or a manual
  // scrub back into the clip range). Lives at this layer — not inside
  // PlaybackRangeEffect — because the user-initiated-play side of the
  // replay needs to run *inside* the play button's click handler so it
  // can issue `seek()` + `play()` synchronously in the user's gesture
  // context. Doing the seek across an `await` (or even waiting for React
  // to re-render the new `paused: false` state) loses Safari's gesture
  // permission and the programmatic `play()` gets blocked, leaving the
  // user with a "click play twice" experience. See `handleTogglePaused`
  // below for the synchronous replay path. Resets on scene change via
  // the parent's per-scene player remount.
  const stoppedAtEndRef = useRef(false);
  // Latched true on the first explicit playback gesture (play/pause
  // toggle, pre-start play click). Read by `usePlayDelay` — see the
  // hook's header comment. Never reset: the gate only spans the first
  // `playDelayMs` after mount, and a scene change remounts the player.
  const userPlaybackIntentRef = useRef(false);
  const handleClipStop = useCallback(() => {
    if (!clipRange) return;
    stoppedAtEndRef.current = true;
    const s = storeRef.current;
    if (!s) return;
    s.pause();
    // Pin the playhead exactly at `clipRange.end` so the timeline
    // displays the marker as "finished". Use `s.seek` directly
    // instead of `handleSeek` — `handleSeek`'s `runDirectSeek`
    // captures `wasPlaying = !s.state.paused` and auto-resumes
    // playback in `awaitSeekReady`, which would un-pause the video
    // we just stopped (and `PlaybackRangeEffect.firedRef` would stay
    // latched, so it wouldn't re-fire when playback then runs past
    // the marker end into the playlist's leftover seconds).
    void s.seek(Math.max(0, clipRange.end - offsetStart));
  }, [clipRange, offsetStart]);
  const handleClipResume = useCallback(() => {
    stoppedAtEndRef.current = false;
  }, []);
  const handleTogglePaused = useCallback(() => {
    const s = storeRef.current;
    if (!s) return;
    // Every toggle is an explicit user gesture — from here on the user
    // owns the playback state, so the play-delay gate must neither
    // suppress the resulting play nor auto-resume at its deadline.
    userPlaybackIntentRef.current = true;
    // Replay-from-stop fast path: if the user is unpausing after the
    // marker auto-stop, restart from clip start. `handleRestart` keeps
    // the seek+play in the click-handler stack when the current playlist
    // covers the target (preserves Safari's gesture context, and dodges
    // iOS's backward-seek-during-startup A/V desync — the decode
    // pipelines initialise from `currentTime` rather than racing the
    // seek into mid-startup). When the playlist's trim is past
    // `clipRange.start` (post-quality-swap), `handleRestart` issues a
    // fresh playlist with `?start=clipRange.start` (in-place src swap)
    // so playback genuinely begins at the marker's start frame instead
    // of the swap-point segment boundary.
    if (clipRange && stoppedAtEndRef.current && s.state.paused) {
      stoppedAtEndRef.current = false;
      handleRestart(clipRange.start);
      return;
    }
    // Scene-mode replay: HTML5's default for `play()` on an `ended`
    // video is to seek to the start of the *currently loaded* media.
    // After a quality swap, the loaded media's start is the swap
    // scene-time (not 0), so the default would replay from there.
    // Force a real restart from scene-time 0 — `handleRestart` issues
    // a fresh playlist when the trim is past 0.
    if (!clipRange && s.state.ended) {
      handleRestart(0);
      return;
    }
    s.togglePaused();
  }, [clipRange, handleRestart]);

  // Initial-load autoplay covers three triggers: caller-driven autoplay,
  // a resume-time on the scene, and a deep-link `?t=` timestamp. All
  // three are gated on `autostartEnabled` (the global "Auto-start video"
  // config), unless the lightbox auto-advance toggle is on — auto-advance
  // is an explicit per-session opt-in for continuous playback, so it
  // overrides the global setting and keeps the next slide playing after
  // a swipe. Gated on `!!onNext` so the override only takes effect in
  // contexts that actually have a next-scene callback (i.e. the
  // lightbox); the standalone detail page never gets it.
  //
  // The cross-source-change continuity path is handled by
  // `handleCanPlay` in `use-scene-player-sources.tsx`: when the
  // captured pending-resume's `wasPaused === false`, it explicitly
  // calls `s.play()` after the in-place src swap. No
  // `<video autoplay>`-attribute hack needed.
  const initialAutoplayIntent =
    autoplay || initialResume.offset > 0 || (initialResume.seekTo ?? 0) > 0;
  const autoAdvanceOverride = autoAdvanceEnabled && !!onNext;
  const effectiveAutoPlay =
    (autostartEnabled || autoAdvanceOverride) && initialAutoplayIntent;

  const autoplayIntentRef = useRef(effectiveAutoPlay);
  autoplayIntentRef.current = effectiveAutoPlay;

  usePlayDelay(
    fullscreenContainerRef,
    playDelayMs,
    autoplayIntentRef,
    userPlaybackIntentRef,
  );

  const handleUserPlaybackGesture = useCallback(() => {
    userPlaybackIntentRef.current = true;
  }, []);

  // Memoized so the array reference is stable when only an unrelated state
  // change re-renders ScenePlayer — keeps `MemoVideo`'s shallow equality
  // check passing through such renders.
  const trackElements = useMemo(
    () =>
      captions.map((c) => (
        <track
          key={c.src}
          kind="captions"
          src={c.src}
          srcLang={c.srclang}
          label={c.label}
        />
      )),
    [captions],
  );

  // Clip-display transform. When `clipRange` is set the player UI is
  // shifted so it reads as a clip from 0 → (end - start). The underlying
  // store / source machinery still operates in scene time — only the
  // values handed to `PlayerControls` are translated, and user seeks are
  // un-translated on the way back into `handleSeek`. See the prop
  // docstring above for the full behaviour summary.
  const clipStart = clipRange?.start ?? 0;
  const effectiveFileDuration = clipRange
    ? clipRange.end - clipRange.start
    : fileDuration;
  const effectiveOffsetStart = offsetStart - clipStart;
  const effectiveSeekDisplayTarget =
    seekDisplayTarget != null ? seekDisplayTarget - clipStart : null;
  const handleClipSeek = useCallback(
    (clipTime: number) => handleSeek(clipTime + clipStart),
    [handleSeek, clipStart],
  );
  const effectiveOnSeek = clipRange ? handleClipSeek : handleSeek;
  // Hide the scene's other markers in clip mode — the slide is dedicated
  // to one marker, and other markers' tick-positions have no meaning on a
  // timeline that no longer represents the whole scene.
  const effectiveMarkers = clipRange ? [] : markers;

  // HLS playlists route through hls.js (`<StableHlsVideo>` →
  // `HlsJsMedia`); direct byte-range files use plain `<Video>`. See the
  // comment on MemoVideo / MemoHlsVideo above for the rationale.
  const VideoComponent = isHlsPlaylist(finalSrc) ? MemoHlsVideo : MemoVideo;
  const mediaElement =
    finalSrc !== undefined ? (
      <VideoComponent
        src={finalSrc}
        autoPlay={effectiveAutoPlay}
        loop={loopEnabled && !clipRange}
        playsInline
        preload={preload}
        onEnded={handleEnded}
        onLoadedMetadata={handleLoadedMetadata}
        className="w-full h-full"
      >
        {trackElements}
      </VideoComponent>
    ) : null;

  return (
    <div
      ref={fullscreenContainerRef}
      className={cn(
        "relative w-full bg-black overflow-hidden",
        fill ? "h-full" : "lg:h-full",
        className,
      )}
      data-scene-player
      data-fill={fill ? "" : undefined}
    >
      <div
        className={cn(
          // `isolate` (CSS `isolation: isolate`) makes this div a new
          // stacking context, so the z-index values below — canvas (5),
          // dim (6), spinner (7), and PlayerControls' bottom bar (10) —
          // resolve against each other instead of escaping to whatever
          // ancestor context happens to exist. Without this, the canvas
          // would race with sibling UI elsewhere on the page.
          "relative w-full vjs-player-container isolate",
          fill
            ? "h-full"
            : cn(
                // Fall back to 16:9 only when we don't know the file's
                // dimensions yet; otherwise the inline `aspectRatio`
                // style below wins.
                !videoAspect && "aspect-video",
                "lg:aspect-auto lg:h-full lg:w-full lg:max-w-none lg:mx-0",
              ),
        )}
        style={!fill && videoAspect ? { aspectRatio: videoAspect } : undefined}
      >
        {/* No `key=` here — Player stays mounted across the scene's
            lifetime; source URL changes flow through `<VideoComponent>`'s
            `src` prop as in-place reassignments on the same `<video>`. */}
        <Player.Player>
          <StoreBridge storeRef={storeRef} />
          <MediaBridge mediaRef={mediaRef} />
          {/* `srcKey={finalSrc}` resets the once-per-mount latch on
              every source URL change. Required because Player is no
              longer keyed on source URL — without this, only the first
              swap's new `<video>` ever fires `canplay` through to
              `handleCanPlay`, and the spinner stays pinned on every
              subsequent swap (most visibly on direct ↔ HLS engine swaps,
              where `<video>` is also recreated and the effect's
              `querySelector("video")` would otherwise capture the old
              torn-down element). */}
          <CanPlayEffect
            onCanPlay={handleCanPlay}
            rootRef={fullscreenContainerRef}
            srcKey={finalSrc}
          />
          <StartedEffect Player={Player} onStarted={handleStarted} />
          {onControlsVisibilityChange && (
            <ControlsVisibilityEffect
              Player={Player}
              onChange={onControlsVisibilityChange}
            />
          )}
          {clipRange && (
            <PlaybackRangeEffect
              Player={Player}
              start={clipRange.start}
              end={clipRange.end}
              offsetStart={offsetStart}
              loopEnabled={loopEnabled}
              onLoop={handleSeek}
              onAdvance={autoAdvanceEnabled ? onNext : undefined}
              onStop={handleClipStop}
              onClipResume={handleClipResume}
            />
          )}
          <Container className="absolute inset-0 overflow-hidden">
            {enablePinchZoom ? (
              <VideoFrameZoom
                transform={zoomTransform}
                onTransformChange={setZoomTransform}
                onActiveGesture={handleZoomActiveGesture}
                isTemporarySpeedActive={isTemporarySpeedActive}
              >
                {mediaElement}
              </VideoFrameZoom>
            ) : (
              mediaElement
            )}

            {/* Also hide while reloading: the dim layer of the
                source-change overlay is partially transparent
                (`bg-black/40`), so without this guard the screenshot
                would read through whenever the frozen frame failed to
                capture (e.g. iOS canvas CORS taint), giving the
                "cover briefly visible" flash the user sees. */}
            <PlayerPoster
              Player={Player}
              src={posterSrc ?? scene.paths.screenshot ?? undefined}
              hide={reloading}
              minMediaTime={
                clipRange ? Math.max(0, clipRange.start - offsetStart) : 0
              }
            />

            <PlayerControls
              Player={Player}
              sources={sources}
              activeSource={activeSource}
              onSourceChange={handleSourceChange}
              markers={effectiveMarkers}
              fileDuration={effectiveFileDuration}
              frameRate={file?.frame_rate ?? undefined}
              sourceResolution={sourceResolution}
              offsetStart={effectiveOffsetStart}
              onSeek={effectiveOnSeek}
              disableSeekArrows={disableSeekArrows}
              hasEverStarted={hasEverStarted}
              reloading={reloading}
              seekDisplayTarget={effectiveSeekDisplayTarget}
              fullscreenContainerRef={fullscreenContainerRef}
              playbackMode={playbackMode}
              canAdvance={canAdvance}
              onCyclePlaybackMode={handleCyclePlaybackMode}
              onTogglePaused={handleTogglePaused}
              onUserPlaybackGesture={handleUserPlaybackGesture}
              onToggleFullscreenOverride={onToggleFullscreenOverride}
              clipBoundsEdit={clipBoundsEdit}
              cancelPendingTapToggleRef={cancelPendingTapToggleRef}
              cancelPendingHoldRef={cancelPendingHoldRef}
              onTemporaryPlaybackRateChange={handleTemporaryPlaybackRateChange}
            />

            {topOverlay}
            {temporaryPlaybackRate != null && (
              <Badge
                variant="secondary"
                role="status"
                aria-live="polite"
                className="pointer-events-none absolute top-4 left-1/2 z-10 -translate-x-1/2"
              >
                {temporaryPlaybackRate}×
              </Badge>
            )}
          </Container>
          <GoogleCast />
        </Player.Player>

        {/* Freeze-frame canvas masks the engine-detach gap between
            the old MediaSource clearing and the new one decoding its
            first frame. Sits at z-[5], beneath the dim+spinner
            overlay (z-[6]/z-[7]) and the controls bar (z-[10]).
            Lives outside `Player.Player` so it's not affected by
            anything the player root does (and so the canvas's persistent
            backing buffer survives across source swaps). */}
        {freezeFrameCanvas}

        {/* Always-mounted dim + spinner. Conditionally rendering them
            on `reloading` meant the first reload was also the first
            time their compositor layers were created, which on iOS
            Safari can cause a brief compositor restructure flash
            adjacent to the <video>'s layer. Mounting them up-front
            with `translateZ(0)` keeps the layers alive from start;
            opacity-toggle is the visibility flip. */}
        <div
          aria-hidden
          className="absolute inset-0 bg-black/40 pointer-events-none z-[6]"
          style={{
            opacity: reloading ? 1 : 0,
            transform: "translateZ(0)",
            willChange: "opacity",
          }}
        />
        <div
          aria-hidden
          className="absolute inset-0 flex items-center justify-center pointer-events-none z-[7]"
          style={{
            opacity: reloading ? 1 : 0,
            transform: "translateZ(0)",
            willChange: "opacity",
          }}
        >
          <Spinner className="size-12 text-white" />
        </div>
      </div>
    </div>
  );
};
