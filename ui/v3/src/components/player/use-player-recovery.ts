import { useEffect, useRef, type RefObject } from "react";
import { isHlsPlaylist } from "./hls";

/** Native fullscreen seeks and stalled playback feed the same transition handlers
 * as the custom controls. They do not decide how a source is restarted. */
export function usePlayerRecovery({
  finalSrc,
  rootRef,
  reloading,
  offsetStart,
  handleSeek,
  forceRemountAt,
}: {
  finalSrc: string | undefined;
  rootRef: RefObject<HTMLDivElement | null>;
  reloading: boolean;
  offsetStart: number;
  handleSeek: (time: number) => void;
  forceRemountAt: (time: number) => void;
}) {
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
}
