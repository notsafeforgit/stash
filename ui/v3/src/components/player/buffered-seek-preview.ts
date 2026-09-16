import { isTimeBuffered, readTimeRanges } from "./buffered-ranges";

/** A drag previews decoded frames on the existing media element. Unbuffered
 * positions never trigger a seek, and newer positions replace queued work. */
export function createBufferedSeekPreview() {
  let session: {
    video: HTMLVideoElement;
    wasPaused: boolean;
    mediaTime: number;
    resumeBuffering: () => void;
  } | null = null;
  let pending: number | null = null;
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
      } catch {
        // A source can become unavailable before the next frame. A preview
        // must never escalate to source recovery or start a transcode.
      }
    });
  };

  const take = () => {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
    pending = null;
    const previous = session;
    session = null;
    previous?.video.removeEventListener("seeked", schedule);
    return previous;
  };

  return {
    preview(
      video: HTMLVideoElement,
      mediaTime: number,
      suspendBuffering: () => () => void,
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
        video.addEventListener("seeked", schedule);
      }
      pending = mediaTime;
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
