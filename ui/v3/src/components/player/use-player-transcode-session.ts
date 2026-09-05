import { useEffect, useRef, type RefObject } from "react";
import { getPlatformURL } from "@/core/platform-url";
import { isHlsPlaylist } from "./hls";
import { hlsStreamTypeName, streamResolution } from "./scene-player-source-url";

/** Owns the server transcode lease: release on exit, keep alive while paused. */
export function usePlayerTranscodeSession(
  sceneId: string,
  finalSrc: string | undefined,
  rootRef: RefObject<HTMLDivElement | null>,
) {
  // Cleanup closes the transcode owned by this effect. Only quality changes
  // within the same scene preserve the incoming HLS variant.
  const currentSourceRef = useRef({ sceneId, src: finalSrc });
  currentSourceRef.current = { sceneId, src: finalSrc };
  useEffect(() => {
    if (!finalSrc || !isHlsPlaylist(finalSrc)) return;
    const sendStop = (keepUrl?: string) => {
      try {
        let url = getPlatformURL(`scene/${sceneId}/streams.stop`).toString();
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
      // At cleanup time, `currentSourceRef.current` holds the value
      // we're transitioning to (or the unchanged last value, on
      // unmount).
      const next = currentSourceRef.current;
      if (
        next.sceneId === sceneId &&
        next.src &&
        isHlsPlaylist(next.src) &&
        next.src !== finalSrc
      ) {
        // HLS → another HLS source (resolution / variant swap). Tell
        // the server which stream we're transitioning TO so it can
        // tear down the old one immediately without touching the
        // freshly-starting new one. The `ServeSegment` sibling-kill
        // still backs us up if the beacon races or is dropped.
        sendStop(next.src);
        return;
      }
      sendStop();
    };
  }, [finalSrc, sceneId]);

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
    const url = getPlatformURL(
      `scene/${sceneId}/streams.keepalive?${params.toString()}`,
    ).toString();

    let attachedVideo: HTMLVideoElement | null = null;
    let intervalId: ReturnType<typeof setInterval> | null = null;

    const ping = () => {
      if (typeof document !== "undefined" && document.hidden) return;
      try {
        void fetch(url, { method: "POST", keepalive: true }).catch(() => {});
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
  }, [finalSrc, rootRef, sceneId]);
}
