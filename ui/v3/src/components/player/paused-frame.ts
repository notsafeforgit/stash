import { isTimeBuffered, readTimeRanges } from "./buffered-ranges";

interface PresentedFrame {
  time: number;
  displayAt: number;
  source: string;
}

/** Preserve the visible frame for an explicit pause. The native media clock
 * can lead the compositor; resuming that clock can skip unseen frames. Keep
 * only two presentation timestamps, without copying pixels or updating React. */
export function createPausedFrame() {
  let video: HTMLVideoElement | null = null;
  let callback: number | null = null;
  let generation = 0;
  let latest: PresentedFrame | null = null;
  let previous: PresentedFrame | null = null;

  const schedule = () => {
    const media = video;
    if (!media?.requestVideoFrameCallback || callback !== null) return;
    const observing = generation;
    callback = media.requestVideoFrameCallback((_now, metadata) => {
      if (video !== media || generation !== observing) return;
      callback = null;
      if (!media.seeking) {
        previous = latest;
        latest = {
          time: metadata.mediaTime,
          displayAt: metadata.expectedDisplayTime,
          source: media.currentSrc,
        };
      }
      schedule();
    });
  };
  const reset = () => {
    generation++;
    if (video && callback !== null) video.cancelVideoFrameCallback(callback);
    callback = null;
    latest = null;
    previous = null;
    schedule();
  };
  const attach = (next: HTMLVideoElement | null) => {
    if (video === next) return;
    if (video) {
      if (callback !== null) video.cancelVideoFrameCallback(callback);
      video.removeEventListener("seeking", reset);
      video.removeEventListener("emptied", reset);
      video.removeEventListener("loadstart", reset);
    }
    callback = null;
    video = next;
    reset();
    if (video) {
      video.addEventListener("seeking", reset);
      video.addEventListener("emptied", reset);
      video.addEventListener("loadstart", reset);
    }
  };

  return {
    /** A React video ref: detaching cancels the observer, including on unmount. */
    attach,
    reset,
    preserveOnPause(command: () => void, earliestTime = 0) {
      const media = video;
      const now = performance.now();
      // A submitted frame may still be waiting for a future vsync. In that
      // case the preceding frame is the one the user actually saw.
      const frame = latest && latest.displayAt <= now ? latest : previous;
      const canPreserve =
        media &&
        !media.paused &&
        !media.seeking &&
        !media.ended &&
        media.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA &&
        !document.hidden &&
        (!media.remote || media.remote.state === "disconnected") &&
        frame &&
        frame.source === media.currentSrc &&
        Number.isFinite(frame.time) &&
        frame.displayAt <= now &&
        now - frame.displayAt <= 250;

      // Keep the library's local/remote target and synchronous user-gesture
      // behavior. A toggle that starts playback must never become a seek.
      command();
      if (
        !canPreserve ||
        video !== media ||
        !media.paused ||
        media.seeking ||
        frame.source !== media.currentSrc
      )
        return;

      // Stay just inside the frame's PTS, avoiding floating-point rounding to
      // the previous frame. A marker's fractional start remains its floor.
      const target = Math.max(earliestTime, frame.time + 0.000001);
      if (
        !isTimeBuffered(target, readTimeRanges(media.buffered)) ||
        Math.abs(media.currentTime - target) < 0.000001
      )
        return;
      try {
        // A precise buffered seek repositions both audio and video while
        // paused. It does not reload the source, flush HLS, or defer play().
        media.currentTime = target;
      } catch {
        // A disappearing source must not prevent the original pause command.
      }
    },
  };
}
