/**
 * Tiny render-null effect components and the poster overlay that
 * `<ScenePlayer>` mounts inside `Player.Provider`. Split out of
 * `scene-player.tsx` so the component file is layout + state, not
 * a grab-bag of one-off store subscribers.
 *
 * Each takes the `Player` factory as a prop so the same component can
 * be reused if the parent ever creates more than one `createPlayer(...)`.
 */
import { useEffect, useRef, useState, type RefObject } from "react";
import type { CreatePlayerResult, VideoPlayerStore } from "@videojs/react";

type PlayerInstance = CreatePlayerResult<VideoPlayerStore>;

// ── Poster overlay ────────────────────────────────────────────────────────────
// Rendered above the <video> until the video has actually reached the
// playhead the caller asked for. Latches once revealed so backward scrubs
// don't bring the poster back. Hidden when the caller signals it should be
// (e.g. during the source-change overlay so the poster doesn't bleed
// through the dim+spinner mask).
//
// `minMediaTime` is the media-element `currentTime` (NOT scene time) that
// must be reached before the poster releases. Defaults to 0 — i.e. just
// wait for `started`. Marker slides pass `clipRange.start - offsetStart`
// so the poster persists through the initial seek into the file: without
// this gate, `started` flips true on the seek itself and the poster
// vanishes before the seek-target frame is decoded, briefly exposing the
// browser's frame 0 (or the loading void) underneath.
//
// Reveal is also gated on `!seeking`: setting `currentTime` in
// `loadedmetadata` flips the store's `currentTime` to the target
// immediately, but Safari keeps the previously-painted frame 0 on
// screen until the seek actually completes (`seeked` fires). Without
// this gate the poster releases on the synchronous time write while
// the underlying paint is still on frame 0, producing a brief flash
// of the scene's first frame in the marker lightbox.

export function PlayerPoster({
  Player,
  src,
  hide,
  minMediaTime = 0,
}: {
  Player: PlayerInstance;
  src?: string;
  hide: boolean;
  minMediaTime?: number;
}) {
  const started = Player.usePlayer((s) => s.started);
  const currentTime = Player.usePlayer((s) => s.currentTime);
  const seeking = Player.usePlayer((s) => s.seeking);
  const [revealed, setRevealed] = useState(false);
  useEffect(() => {
    if (started && !seeking && currentTime >= minMediaTime) setRevealed(true);
  }, [started, seeking, currentTime, minMediaTime]);
  if (!src || revealed || hide) return null;
  return (
    <img
      src={src}
      className="absolute inset-0 w-full h-full object-contain pointer-events-none"
      style={{ zIndex: 0 }}
      alt=""
    />
  );
}

// ── Can-play latch ────────────────────────────────────────────────────────────
// Fires `onCanPlay` once per Provider mount, on the first DOM `canplay`
// event from the underlying `<video>` element. Source changes go
// through a Provider remount (keyed on the source URL) which gives
// this component a fresh ref, so the next source still gets its own
// fire. Subsequent `canplay` blips during playback (Safari's native
// HLS pipeline drops readiness briefly on seeks while it re-validates
// the new segment range) must NOT re-fire: the consumer's pending-resume
// work is one-shot, and its autoplay-recovery branch would un-pause the
// player after legitimate auto-stops at marker clip boundaries.
//
// We deliberately do NOT consume the player store's `canPlay` flag
// here. `@videojs/core` defines that flag as
// `media.readyState >= HAVE_ENOUGH_DATA` (readyState 4), which is the
// `canplaythrough` event's threshold — not `canplay`'s
// (`HAVE_FUTURE_DATA`, readyState 3). For HLS sources that aren't
// auto-playing yet (the new <video> after a remount, before the
// autoplay-recovery branch runs), browsers typically only buffer up to
// readyState 3; the store's `canPlay` then never flips true and the
// effect never fires. The DOM event itself fires reliably at readyState
// 3, so listening directly is what we want for the "we have enough to
// resume" signal — the autoplay-recovery branch the consumer runs in
// response will then call `play()` and the buffer will fill the rest of
// the way on its own.

export function CanPlayEffect({
  onCanPlay,
  rootRef,
  srcKey,
}: {
  onCanPlay: () => void;
  /** Outer fullscreen wrapper. The effect does a `querySelector("video")`
   *  from this root to find the live media element. */
  rootRef: RefObject<HTMLElement | null>;
  /** Optional re-fire key. When supplied, every change resets the
   *  once-per-mount latch and re-attaches the listener — needed by the
   *  stable-`<video>` variant of `ScenePlayer` where source URL changes
   *  don't remount the Provider but still need a fresh `canplay` fire
   *  (and, when the engine swaps, a fresh `<video>` listener target).
   *  The original scene-player keys `Player.Provider` on `finalSrc`, so
   *  the Provider remount already supplies that reset; it can leave this
   *  prop undefined. */
  srcKey?: string;
}) {
  const firedRef = useRef(false);
  // The readyState >= 3 fast-path below is only safe on the *first*
  // run. On srcKey-driven re-runs the <video> element's readyState
  // still reflects the *previous* source (HlsMedia's engine teardown
  // and the resulting `emptied` event come a couple of microtasks
  // after the React effect runs), so the fast-path would otherwise
  // fire `onCanPlay` before the new source has even started loading
  // — see the comment block below for the diagnostic trace.
  // Records the srcKey generation the watcher last attached for; `null`
  // means "never attached" (the true first run).
  const attachedForRef = useRef<{ srcKey?: string } | null>(null);
  useEffect(() => {
    firedRef.current = false;
    const root = rootRef.current;
    if (!root) return;
    const video = root.querySelector("video");
    if (!(video instanceof HTMLVideoElement)) return;
    const handler = () => {
      if (firedRef.current) return;
      firedRef.current = true;
      onCanPlay();
    };
    // `readyState >= HAVE_FUTURE_DATA` (3) — first-mount race fix:
    // Safari can finish loading a warm-cache HLS playlist before this
    // effect attaches, so we check up-front. Skip on re-runs: at that
    // point readyState reflects the OLD source.
    const isFirstRun = attachedForRef.current === null;
    attachedForRef.current = { srcKey };
    if (isFirstRun && video.readyState >= 3) {
      handler();
      return;
    }
    video.addEventListener("canplay", handler);
    video.addEventListener("loadeddata", handler);
    return () => {
      video.removeEventListener("canplay", handler);
      video.removeEventListener("loadeddata", handler);
    };
  }, [onCanPlay, rootRef, srcKey]);
  return null;
}

// ── Started latch ─────────────────────────────────────────────────────────────
// Fires `onStarted` the first time `started` becomes true. Used to latch a
// `hasEverStarted` flag in the parent without subscribing it to every
// store update itself.

export function StartedEffect({
  Player,
  onStarted,
}: {
  Player: PlayerInstance;
  onStarted: () => void;
}) {
  const started = Player.usePlayer((s) => s.started);
  useEffect(() => {
    if (started) onStarted();
  }, [started, onStarted]);
  return null;
}

// ── Controls-visibility relay ─────────────────────────────────────────────────
// Surfaces the videojs/react store's `controlsVisible` flag to the parent so
// surrounding chrome (e.g. the lightbox slide overlay with title /
// performers) can fade in lockstep with the player's own control bar.

export function ControlsVisibilityEffect({
  Player,
  onChange,
}: {
  Player: PlayerInstance;
  onChange: (visible: boolean) => void;
}) {
  const visible = Player.usePlayer((s) => s.controlsVisible);
  useEffect(() => {
    onChange(visible);
  }, [visible, onChange]);
  return null;
}

// ── Playback-range gate ───────────────────────────────────────────────────────
// Watches the player's `currentTime` against a [start, end] window in scene
// time. When the playhead reaches `end` it picks one of three actions in
// order: loop back to `start` (`loopEnabled`), advance to the next slide
// (`onAdvance` provided), or stop — pause the player (`onStop`). The
// "stop" branch is what makes a clip behave like a self-contained video
// when neither loop nor auto-advance is on; without it the underlying
// stream keeps playing past the boundary until the file's natural end.
// `firedRef` debounces against the store's high-frequency `currentTime`
// updates so the boundary action fires once per crossing rather than
// every frame. The store reports media time; we add `offsetStart` to
// land in scene time (matches the rest of the player's seek/onSeek API
// surface).

export function PlaybackRangeEffect({
  Player,
  start,
  end,
  offsetStart,
  loopEnabled,
  onLoop,
  onAdvance,
  onStop,
  onClipResume,
}: {
  Player: PlayerInstance;
  start: number;
  end: number;
  offsetStart: number;
  loopEnabled: boolean;
  /** Seek the player to `start`. Caller forwards to the parent's `handleSeek`
   *  so the seek goes through the same scene-time pipeline as user-driven
   *  scrubs (handles transcode-source offset bookkeeping). */
  onLoop: (start: number) => void;
  /** Advance to the next slide / scene. When omitted, reaching `end`
   *  with loop off falls through to `onStop`. */
  onAdvance?: () => void;
  /** Pause playback when the boundary is hit and neither loop nor
   *  advance is configured. The caller typically also seeks the
   *  playhead exactly to `end` so the timeline reads as "finished".
   *  Caller is also responsible for tracking the "stopped at end" state
   *  so the next user-initiated play can synchronously seek back to
   *  `start` inside the gesture handler — see `handleTogglePaused` in
   *  scene-player.tsx. */
  onStop?: () => void;
  /** Fired on every render where the playhead is inside the clip range
   *  (`trueTime < end`). The caller uses this edge to clear any
   *  "stopped at end" latch they're keeping — a manual scrub back into
   *  the range counts as the start of a fresh playthrough. Idempotent;
   *  may fire repeatedly. */
  onClipResume?: () => void;
}) {
  const currentTime = Player.usePlayer((s) => s.currentTime);
  const trueTime = offsetStart + currentTime;
  const firedRef = useRef(false);
  useEffect(() => {
    if (trueTime < end) {
      firedRef.current = false;
      onClipResume?.();
      return;
    }
    if (firedRef.current) return;
    firedRef.current = true;
    if (loopEnabled) {
      onLoop(start);
    } else if (onAdvance) {
      onAdvance();
    } else {
      onStop?.();
    }
  }, [
    trueTime,
    end,
    start,
    loopEnabled,
    onLoop,
    onAdvance,
    onStop,
    onClipResume,
  ]);
  return null;
}
