import { useCommittedRef } from "@/hooks/use-committed-ref";
import { useEffect, useRef, type RefObject } from "react";
import { isHlsPlaylist, isIOSNativeFullscreen } from "./hls";

/** Native fullscreen seeks and stalled playback feed the same transition handlers
 * as the custom controls. They do not decide how a source is restarted. */
export function usePlayerRecovery({
  finalSrc,
  rootRef,
  reloading,
  offsetStart,
  isSeekPreviewActive,
  handleSeek,
  forceRemountAt,
}: {
  finalSrc: string | undefined;
  rootRef: RefObject<HTMLDivElement | null>;
  reloading: boolean;
  offsetStart: number;
  isSeekPreviewActive: () => boolean;
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
  const reloadingRef = useCommittedRef(reloading);

  const offsetStartRef = useCommittedRef(offsetStart);

  const handleSeekRef = useCommittedRef(handleSeek);
  const isSeekPreviewActiveRef = useCommittedRef(isSeekPreviewActive);

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
      if (timer) clearTimeout(timer);
      timer = null;
      // A held scrub owns these native seeks until release. Buffer eviction
      // during a pause between preview frames must not commit or reload it.
      if (reloadingRef.current || isSeekPreviewActiveRef.current()) return;
      const video = e.currentTarget;
      if (!(video instanceof HTMLVideoElement)) return;
      timer = setTimeout(() => {
        timer = null;
        if (reloadingRef.current || isSeekPreviewActiveRef.current()) return;
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

    // Cover a late media attachment while retaining the same element through
    // normal source changes.
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
  //   2. Presented video frames not advancing while `currentTime` is —
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
  const forceRemountAtRef = useCommittedRef(forceRemountAt);
  // A recovery changes finalSrc. Keep its cooldown across that effect restart.
  const lastRecoveryAtRef = useRef<number | null>(null);

  useEffect(() => {
    if (!finalSrc) return;
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
    let lastPresentedTime: number | null = null;
    let frameRequest: number | null = null;
    let usesFrameCallbacks = false;

    const readFrameCount = (v: HTMLVideoElement): number | null => {
      try {
        const quality = v.getVideoPlaybackQuality?.();
        if (!quality) return null;
        // totalVideoFrames includes dropped frames. A decoder can keep
        // discarding frames while the displayed picture stays frozen.
        const displayed = quality.totalVideoFrames - quality.droppedVideoFrames;
        return Number.isFinite(displayed) ? Math.max(0, displayed) : null;
      } catch {
        return null;
      }
    };

    const cancelFrame = (v: HTMLVideoElement) => {
      if (frameRequest !== null) v.cancelVideoFrameCallback?.(frameRequest);
      frameRequest = null;
    };
    const observeNextFrame = (v: HTMLVideoElement) => {
      if (frameRequest !== null || !v.requestVideoFrameCallback) return;
      try {
        usesFrameCallbacks = true;
        // One sample per watchdog tick is sufficient; no per-frame render or
        // React state update. Audio/timeupdate cannot refresh this timestamp.
        frameRequest = v.requestVideoFrameCallback((_now, metadata) => {
          frameRequest = null;
          if (attachedVideo === v && metadata.mediaTime !== lastPresentedTime) {
            lastPresentedTime = metadata.mediaTime;
            lastFrameAt = Date.now();
          }
        });
      } catch {
        usesFrameCallbacks = false;
      }
    };

    const resetBaseline = (v: HTMLVideoElement) => {
      lastProgressTime = v.currentTime;
      lastProgressAt = Date.now();
      const frames = readFrameCount(v);
      if (frames != null) lastFrameCount = frames;
      lastFrameAt = Date.now();
      lastPresentedTime = null;
    };

    const onLoadStart = (e: Event) => {
      // New media resource loading. Disarm so cold-start buffering
      // (which can exceed the stall threshold for 4K HLS transcodes)
      // doesn't trigger recovery; will re-arm on the next `playing`.
      armed = false;
      const v = e.currentTarget;
      if (v instanceof HTMLVideoElement) {
        cancelFrame(v);
        resetBaseline(v);
        observeNextFrame(v);
      }
    };
    const onPlaying = (e: Event) => {
      armed = true;
      const v = e.currentTarget;
      if (v instanceof HTMLVideoElement) {
        resetBaseline(v);
        observeNextFrame(v);
      }
    };
    const onPlay = (e: Event) => {
      const v = e.currentTarget;
      if (v instanceof HTMLVideoElement) resetBaseline(v);
    };
    const onTimeUpdate = (e: Event) => {
      const v = e.currentTarget;
      if (!(v instanceof HTMLVideoElement)) return;
      if (Math.abs(v.currentTime - lastProgressTime) > PROGRESS_EPSILON_S) {
        lastProgressTime = v.currentTime;
        lastProgressAt = Date.now();
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
      if (!v || !armed || reloadingRef.current) return;
      if (v.paused || v.ended || isSeekPreviewActiveRef.current()) {
        resetBaseline(v);
        return;
      }
      // Skip while the page is hidden (iPhone screen locked / tab
      // backgrounded). Frame-count progress legitimately stalls
      // there because the compositor isn't drawing, and
      // `forceRemountAt` would issue network requests that may be
      // throttled / fail under iOS background restrictions. The
      // `visibilitychange` handler below resets the baselines on
      // resume so the 4 s grace window applies after the user comes
      // back, not from when they left.
      if (document.hidden) {
        resetBaseline(v);
        return;
      }
      const now = Date.now();
      observeNextFrame(v);
      const frames = usesFrameCallbacks ? null : readFrameCount(v);
      if (frames != null && frames !== lastFrameCount) {
        lastFrameCount = frames;
        lastFrameAt = now;
      }
      const timeStalledMs = now - lastProgressAt;
      // Browsers may stop presenting an offscreen video while its audio
      // continues. Only diagnose the picture when it can be seen.
      const bounds = v.getBoundingClientRect();
      const visible =
        bounds.width > 0 &&
        bounds.height > 0 &&
        bounds.bottom > 0 &&
        bounds.right > 0 &&
        bounds.top < window.innerHeight &&
        bounds.left < window.innerWidth;
      const externalPresentation =
        isIOSNativeFullscreen(v) ||
        document.pictureInPictureElement === v ||
        ("webkitPresentationMode" in v &&
          v.webkitPresentationMode === "picture-in-picture");
      const observesVideo =
        v.videoWidth > 0 &&
        (visible || externalPresentation) &&
        (usesFrameCallbacks || frames != null);
      if (!observesVideo) lastFrameAt = now;
      const framesStalledMs = observesVideo ? now - lastFrameAt : 0;
      if (
        timeStalledMs < STALL_THRESHOLD_MS &&
        framesStalledMs < STALL_THRESHOLD_MS
      ) {
        return;
      }
      if (
        lastRecoveryAtRef.current !== null &&
        now - lastRecoveryAtRef.current < RECOVERY_COOLDOWN_MS
      )
        return;
      lastRecoveryAtRef.current = now;
      const sceneTime = v.currentTime + offsetStartRef.current;
      forceRemountAtRef.current(sceneTime);
    }, CHECK_INTERVAL_MS);

    const attach = (video: HTMLVideoElement) => {
      if (attachedVideo === video) return;
      if (attachedVideo) detach(attachedVideo);
      attachedVideo = video;
      armed = false;
      usesFrameCallbacks = false;
      video.addEventListener("loadstart", onLoadStart);
      video.addEventListener("playing", onPlaying);
      video.addEventListener("play", onPlay);
      video.addEventListener("timeupdate", onTimeUpdate);
      video.addEventListener("seeking", onSeeking);
      resetBaseline(video);
      observeNextFrame(video);
    };
    const detach = (video: HTMLVideoElement) => {
      cancelFrame(video);
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
      if (attachedVideo) {
        cancelFrame(attachedVideo);
        resetBaseline(attachedVideo);
        observeNextFrame(attachedVideo);
      }
    };
    document.addEventListener("visibilitychange", onVisibility);

    return () => {
      observer.disconnect();
      clearInterval(intervalId);
      document.removeEventListener("visibilitychange", onVisibility);
      if (attachedVideo) detach(attachedVideo);
      attachedVideo = null;
    };
  }, [rootRef, finalSrc]);
}
