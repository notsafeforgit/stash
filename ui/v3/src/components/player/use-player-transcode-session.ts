import { useCommittedRef } from "@/hooks/use-committed-ref";
import { useEffect } from "react";
import { getPlatformURL } from "@/core/platform-url";
import { isHlsPlaylist } from "./hls";
import { hlsStreamTypeName, streamResolution } from "./scene-player-source-url";

/** Owns the server transcode lease while this visible player has an HLS source. */
export function usePlayerTranscodeSession(
  sceneId: string,
  finalSrc: string | undefined,
) {
  // Cleanup closes the transcode owned by this effect. Only quality changes
  // within the same scene preserve the incoming HLS variant.
  const currentSourceRef = useCommittedRef({ sceneId, src: finalSrc });

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

  // Playing does not guarantee segment requests: a fast connection, slower
  // playback rate, or ManagedMediaSource scheduling can leave the server idle
  // past its 60 s timeout while the player consumes its buffer. Keep the lease
  // independent of pause state so the encoder and cached fragments survive.
  // The server still suspends encoding once its lookahead is full. Hidden
  // pages stop renewing; the existing idle timeout reclaims abandoned streams.
  useEffect(() => {
    if (!finalSrc || !isHlsPlaylist(finalSrc)) return;

    const KEEPALIVE_INTERVAL_MS = 15000;
    const keepType = hlsStreamTypeName(finalSrc);
    if (!keepType) return;
    const keepRes = streamResolution(finalSrc);
    const params = new URLSearchParams({ keep_type: keepType });
    if (keepRes) params.set("keep_resolution", keepRes);
    const url = getPlatformURL(
      `scene/${sceneId}/streams.keepalive?${params.toString()}`,
    ).toString();

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
      // Renew immediately on source selection and foreground return.
      ping();
      intervalId = setInterval(ping, KEEPALIVE_INTERVAL_MS);
    };
    const stop = () => {
      if (intervalId == null) return;
      clearInterval(intervalId);
      intervalId = null;
    };

    const onVisibility = () => {
      if (document.hidden) stop();
      else start();
    };
    document.addEventListener("visibilitychange", onVisibility);
    onVisibility();

    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      stop();
    };
  }, [finalSrc, sceneId]);
}
