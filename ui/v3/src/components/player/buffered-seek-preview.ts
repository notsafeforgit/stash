import { isTimeBuffered, readTimeRanges } from "./buffered-ranges";
import { recordScrubTiming } from "./scrub-timings";

/** A drag previews decoded frames on the existing media element. Unbuffered
 * positions never trigger a seek, and newer positions replace queued work. */
export function createBufferedSeekPreview(onEnd?: () => void) {
  let session: {
    video: HTMLVideoElement;
    wasPaused: boolean;
    mediaTime: number;
    resumeBuffering: () => void;
  } | null = null;
  let pending: number | null = null;
  /** When the pending target was requested, for time-to-display timings. */
  let pendingAt = 0;
  let issued: { target: number; at: number } | null = null;
  let frame: number | null = null;

  const schedule = () => {
    if (frame !== null || pending === null || !session) return;
    frame = requestAnimationFrame(() => {
      frame = null;
      const video = session?.video;
      if (!video || pending === null || video.seeking) return;
      const target = pending;
      pending = null;
      // Re-read native ranges at the write boundary: Safari can evict data
      // during a drag, and separate ranges may have unbuffered holes.
      if (!isTimeBuffered(target, readTimeRanges(video.buffered))) return;
      if (Math.abs(video.currentTime - target) < 0.001) return;
      try {
        video.currentTime = target;
        issued = { target, at: pendingAt };
      } catch {
        // A source can become unavailable before the next frame. A preview
        // must never escalate to source recovery or start a transcode.
      }
    });
  };
  const seeked = () => {
    const video = session?.video;
    if (issued && video && Math.abs(video.currentTime - issued.target) < 0.01)
      recordScrubTiming("main-buffer", performance.now() - issued.at);
    issued = null;
    schedule();
  };

  const take = () => {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
    pending = null;
    issued = null;
    const previous = session;
    session = null;
    previous?.video.removeEventListener("seeked", seeked);
    if (previous) onEnd?.();
    return previous;
  };

  return {
    /** Native seek events from this preview belong to the held gesture. */
    isActive: () => session !== null,
    preview(
      video: HTMLVideoElement,
      mediaTime: number,
      suspendBuffering: () => () => void,
      external = false,
    ) {
      if (!Number.isFinite(mediaTime)) return;
      if (video.readyState === 0) return;
      if (!session) {
        session = {
          video,
          wasPaused: video.paused,
          mediaTime: video.currentTime,
          resumeBuffering: suspendBuffering(),
        };
        video.pause();
        video.addEventListener("seeked", seeked);
      }
      // An independent scrub stream previews remote frames without moving or
      // refilling the high-resolution player. The same gesture owns pause,
      // cancellation and the single final committed seek in either mode.
      const next = external ? null : mediaTime;
      if (next !== pending) pendingAt = performance.now();
      pending = next;
      schedule();
    },
    /** An explicit play/pause command during the drag replaces its old intent. */
    setPaused(paused: boolean) {
      if (session) session.wasPaused = paused;
    },
    /** The seek owner commits or restores the position before resuming loading. */
    take,
    dispose() {
      take()?.resumeBuffering();
    },
  };
}
