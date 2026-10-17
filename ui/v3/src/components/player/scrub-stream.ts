import { Hls } from "@videojs/hlsjs-video";
import { isTimeBuffered, readTimeRanges } from "./buffered-ranges";
import { PlayerTranscodeSession } from "./player-transcode-session";

/** Only authenticated library sources expose this endpoint. Do not manufacture
 * library access from an offline blob or a guest/share media URL. */
export function scrubStreamURL(
  streams: readonly { url: string }[],
): string | null {
  for (const stream of streams) {
    try {
      const url = new URL(stream.url);
      if (!/\/scene\/[^/]+\/stream(?:\.[\w.]+)?$/.test(url.pathname)) continue;
      if (!url.pathname.endsWith(".master.m3u8")) continue;
      url.pathname = url.pathname.replace(
        /\/stream[^/]+$/,
        "/stream.scrub.master.m3u8",
      );
      for (const key of ["start", "end", "stream_session", "v3_hls"])
        url.searchParams.delete(key);
      url.searchParams.set("resolution", "LOW");
      return url.toString();
    } catch {
      /* Not a server playback URL. */
    }
  }
  return null;
}

/** Buffered seeks reuse decoded low-resolution media. Unbuffered targets are
 * throttled, not debounced: continuous dragging must still request frames.
 * This video never joins the main Video.js store or browser media session. */
export function createScrubStream(
  video: HTMLVideoElement,
  sceneId: string,
  source: string,
  onFrame: (visible: boolean, time?: number) => void,
  onFailure: () => void,
) {
  let engine: Hls | null = null;
  let owner: PlayerTranscodeSession | null = null;
  let target: number | null = null;
  let seekTimer: ReturnType<typeof setTimeout> | undefined;
  let idleTimer: ReturnType<typeof setTimeout> | undefined;
  let recoveryTimer: ReturnType<typeof setTimeout> | undefined;
  let recovered = false;
  let recovering = false;
  let ready = false;
  let native = false;
  let disposed = false;
  let failed = false;
  let foreground = false;

  const release = () => {
    clearTimeout(seekTimer);
    seekTimer = undefined;
    clearTimeout(idleTimer);
    clearTimeout(recoveryTimer);
    recoveryTimer = undefined;
    recovering = false;
    if (!engine && !owner && !video.hasAttribute("src")) return;
    engine?.destroy();
    engine = null;
    owner?.dispose();
    owner = null;
    ready = false;
    video.pause();
    video.removeAttribute("src");
    video.load();
    onFrame(false);
  };
  const fail = () => {
    if (disposed || failed || !owner) return;
    failed = true;
    release();
    onFailure();
  };
  const mediaError = () => {
    if (disposed || failed || !owner || recovering) return;
    if (!engine || recovered || target === null) {
      fail();
      return;
    }
    // A native decoder can reject its first buffer transiently. Reattach once
    // under the same server lease; never restart the main player or loop on a
    // genuinely unsupported stream. Defer past both native and hls.js errors.
    recovered = true;
    recovering = true;
    ready = false;
    clearTimeout(seekTimer);
    seekTimer = undefined;
    engine.stopLoad();
    onFrame(false);
    recoveryTimer = setTimeout(() => {
      recoveryTimer = undefined;
      if (target === null || document.hidden) {
        release();
        return;
      }
      engine?.recoverMediaError();
      recovering = false;
      engine?.startLoad(target);
    }, 100);
  };
  const seek = (replace = false) => {
    if (!ready || recovering || target === null || disposed || document.hidden)
      return;
    if (engine && !engine.hasEnoughToStart) return;
    if (video.seeking && !replace) return;
    try {
      video.currentTime = target;
    } catch {
      /* Metadata is still loading; loadeddata retries. */
    }
  };
  const showFrame = () => {
    if (recovering || target === null || video.seeking || video.readyState < 2)
      return;
    // loadeddata can arrive before the first fragment's append completes.
    // Let hls.js establish its initial position without an overlapping seek.
    if (engine && !engine.hasEnoughToStart) return;
    video.pause();
    if (Math.abs(video.currentTime - target) < 0.05) {
      video.dataset.scrubTime = String(video.currentTime);
      // A paused preview must not continue filling after it has its frame.
      // The next foreground target or background sample explicitly resumes it.
      engine?.stopLoad();
      onFrame(foreground, video.currentTime);
    } else seek();
  };
  const metadata = () => {
    ready = true;
    // hls.js positions its initial fragment itself. Waiting for decoded data
    // avoids racing that first append with a second native seek in WebKit.
  };
  const start = () => {
    seekTimer = undefined;
    if (target === null || disposed || failed || recovering || document.hidden)
      return;
    if (!owner) {
      const url = new URL(source);
      owner = new PlayerTranscodeSession(sceneId, crypto.randomUUID());
      url.searchParams.set("stream_session", owner.id!);
      url.searchParams.set("start", String(target));
      owner.selectSource(url.toString());
      if (Hls.isSupported()) {
        engine = new Hls({
          autoStartLoad: false,
          startPosition: target,
          backBufferLength: 15,
          maxBufferLength: 4,
          maxMaxBufferLength: 6,
          maxBufferSize: 1024 * 1024,
          // A preview error must not generate an unlimited retry stream.
          fragLoadingMaxRetry: 1,
          manifestLoadingMaxRetry: 1,
          levelLoadingMaxRetry: 1,
        });
        engine.on(Hls.Events.MANIFEST_PARSED, () => {
          if (target !== null) engine?.startLoad(target);
        });
        engine.on(Hls.Events.FRAG_BUFFERED, () => {
          // Finish the engine's append/position callbacks before we stop its
          // loader or the coordinator requests the next background sample.
          queueMicrotask(showFrame);
        });
        engine.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) {
            if (data.type === Hls.ErrorTypes.MEDIA_ERROR) mediaError();
            else fail();
          }
        });
        engine.attachMedia(video);
        engine.loadSource(url.toString());
      } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
        native = true;
        video.src = url.toString();
        video.load();
      } else {
        fail();
      }
    } else {
      engine?.startLoad(target);
      // Supersede a slow background seek when the user requests another area.
      // Waiting for its seeked event could otherwise starve the new target.
      seek(true);
    }
  };
  const hidden = () => {
    if (document.hidden) release();
  };
  video.addEventListener("loadedmetadata", metadata);
  video.addEventListener("loadeddata", showFrame);
  video.addEventListener("seeked", showFrame);
  video.addEventListener("error", mediaError);
  document.addEventListener("visibilitychange", hidden);

  const update = (time: number | null, active: boolean) => {
    target = time;
    foreground = active;
    clearTimeout(idleTimer);
    if (time === null) {
      clearTimeout(seekTimer);
      seekTimer = undefined;
      onFrame(false);
      engine?.stopLoad();
      if (native) release();
      else idleTimer = setTimeout(release, 30_000);
    } else if (ready && isTimeBuffered(time, readTimeRanges(video.buffered))) {
      clearTimeout(seekTimer);
      seekTimer = undefined;
      seek();
      showFrame();
    } else if (seekTimer === undefined) {
      // Keep the existing deadline as the finger moves. The callback reads
      // the newest target instead of waiting indefinitely for a quiet pointer.
      seekTimer = setTimeout(start, active ? 120 : 0);
    }
  };

  return {
    preview(time: number | null) {
      update(time, time !== null);
    },
    prepare(time: number) {
      update(time, false);
    },
    release() {
      target = null;
      release();
    },
    dispose() {
      disposed = true;
      target = null;
      video.removeEventListener("loadedmetadata", metadata);
      video.removeEventListener("loadeddata", showFrame);
      video.removeEventListener("seeked", showFrame);
      video.removeEventListener("error", mediaError);
      document.removeEventListener("visibilitychange", hidden);
      release();
    },
  };
}
