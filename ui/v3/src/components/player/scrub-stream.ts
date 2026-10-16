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

/** Inert until a drag settles. Buffered seeks reuse decoded low-resolution
 * media; distant targets are coalesced so pointer moves do not spawn encoders.
 * This video never joins the main Video.js store or browser media session. */
export function createScrubStream(
  video: HTMLVideoElement,
  sceneId: string,
  source: string,
  onFrame: (visible: boolean) => void,
  onFailure: () => void,
) {
  let engine: Hls | null = null;
  let owner: PlayerTranscodeSession | null = null;
  let target: number | null = null;
  let seekTimer: ReturnType<typeof setTimeout> | undefined;
  let idleTimer: ReturnType<typeof setTimeout> | undefined;
  let ready = false;
  let native = false;
  let disposed = false;
  let failed = false;

  const release = () => {
    clearTimeout(seekTimer);
    clearTimeout(idleTimer);
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
    if (disposed || failed) return;
    failed = true;
    release();
    onFailure();
  };
  const seek = () => {
    if (!ready || target === null || disposed || document.hidden) return;
    if (video.seeking) return;
    try {
      video.currentTime = target;
    } catch {
      /* Metadata is still loading; loadeddata retries. */
    }
  };
  const showFrame = () => {
    if (target === null || video.seeking || video.readyState < 2) return;
    video.pause();
    if (Math.abs(video.currentTime - target) < 0.05) {
      video.dataset.scrubTime = String(video.currentTime);
      onFrame(true);
    } else seek();
  };
  const metadata = () => {
    ready = true;
    // hls.js positions its initial fragment itself. Waiting for decoded data
    // avoids racing that first append with a second native seek in WebKit.
  };
  const start = () => {
    if (target === null || disposed || failed || document.hidden) return;
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
          maxBufferLength: 15,
          maxMaxBufferLength: 30,
          maxBufferSize: 4 * 1024 * 1024,
          // A preview error must not generate an unlimited retry stream.
          fragLoadingMaxRetry: 1,
          manifestLoadingMaxRetry: 1,
          levelLoadingMaxRetry: 1,
        });
        engine.on(Hls.Events.MANIFEST_PARSED, () => {
          if (target !== null) engine?.startLoad(target);
        });
        engine.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) fail();
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
      seek();
    }
  };
  const hidden = () => {
    if (document.hidden) release();
  };
  video.addEventListener("loadedmetadata", metadata);
  video.addEventListener("loadeddata", showFrame);
  video.addEventListener("seeked", showFrame);
  video.addEventListener("error", fail);
  document.addEventListener("visibilitychange", hidden);

  return {
    preview(time: number | null) {
      target = time;
      clearTimeout(seekTimer);
      clearTimeout(idleTimer);
      if (time === null) {
        onFrame(false);
        engine?.stopLoad();
        // Native HLS exposes no stopLoad; release it immediately. MSE buffers
        // can be reused during repeated fine adjustments for thirty seconds.
        if (native) release();
        else idleTimer = setTimeout(release, 30_000);
      } else if (
        ready &&
        isTimeBuffered(time, readTimeRanges(video.buffered))
      ) {
        seek();
        showFrame();
      } else {
        seekTimer = setTimeout(start, 180);
      }
    },
    dispose() {
      disposed = true;
      target = null;
      video.removeEventListener("loadedmetadata", metadata);
      video.removeEventListener("loadeddata", showFrame);
      video.removeEventListener("seeked", showFrame);
      video.removeEventListener("error", fail);
      document.removeEventListener("visibilitychange", hidden);
      release();
    },
  };
}
