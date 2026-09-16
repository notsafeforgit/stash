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
  let pausedPosition: { source: string; time: number } | null = null;
  let ownSeek: number | null = null;

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
  const clearFrames = () => {
    generation++;
    if (video && callback !== null) video.cancelVideoFrameCallback(callback);
    callback = null;
    latest = null;
    previous = null;
    schedule();
  };
  const reset = () => {
    pausedPosition = null;
    ownSeek = null;
    clearFrames();
  };
  const onSeeking = () => {
    // The pause correction itself seeks. Every other seek belongs to the
    // user/source controller and replaces the saved pause position.
    if (
      ownSeek === null ||
      !video ||
      Math.abs(video.currentTime - ownSeek) > 0.001
    )
      pausedPosition = null;
    ownSeek = null;
    clearFrames();
  };
  const onPlay = () => {
    // Native fullscreen/casting can resume outside our commands. A queued
    // play event from before a newer pause must not erase that pause's anchor.
    if (!video?.paused) {
      pausedPosition = null;
      ownSeek = null;
    }
  };
  const attach = (next: HTMLVideoElement | null) => {
    if (video === next) return;
    if (video) {
      if (callback !== null) video.cancelVideoFrameCallback(callback);
      video.removeEventListener("seeking", onSeeking);
      video.removeEventListener("emptied", reset);
      video.removeEventListener("loadstart", reset);
      video.removeEventListener("play", onPlay);
    }
    callback = null;
    video = next;
    reset();
    if (video) {
      video.addEventListener("seeking", onSeeking);
      video.addEventListener("emptied", reset);
      video.addEventListener("loadstart", reset);
      video.addEventListener("play", onPlay);
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
      const clock = media?.currentTime ?? Number.NaN;
      const source = media?.currentSrc;
      const canPreserve =
        media &&
        !media.paused &&
        !media.seeking &&
        !media.ended &&
        media.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA &&
        !document.hidden &&
        (!media.remote || media.remote.state === "disconnected") &&
        Number.isFinite(clock);

      // Keep the library's local/remote target and synchronous user-gesture
      // behavior. A toggle that starts playback must never become a seek.
      command();
      if (
        !canPreserve ||
        video !== media ||
        !media.paused ||
        media.seeking ||
        source !== media.currentSrc
      )
        return;

      const visibleFrame =
        frame &&
        frame.source === source &&
        Number.isFinite(frame.time) &&
        frame.displayAt <= now &&
        now - frame.displayAt <= 250
          ? frame.time + 0.000001
          : clock;
      // Stay just inside the frame's PTS, avoiding floating-point rounding to
      // the previous frame. The visible image can slightly lead the native
      // clock, so prefer its timestamp. A marker's start remains its floor.
      const target = Math.max(earliestTime, visibleFrame);
      if (!isTimeBuffered(target, readTimeRanges(media.buffered))) return;
      pausedPosition = { source: media.currentSrc, time: target };
      if (Math.abs(media.currentTime - target) < 0.000001) return;
      try {
        // A precise buffered seek repositions both audio and video while
        // paused. It does not reload the source, flush HLS, or defer play().
        ownSeek = target;
        media.currentTime = target;
      } catch {
        ownSeek = null;
        // A disappearing source must not prevent the original pause command.
      }
    },
    restoreBeforePlay() {
      const position = pausedPosition;
      pausedPosition = null;
      ownSeek = null;
      const media = video;
      if (
        !position ||
        !media?.paused ||
        media.seeking ||
        media.ended ||
        position.source !== media.currentSrc ||
        (media.remote && media.remote.state !== "disconnected") ||
        !isTimeBuffered(position.time, readTimeRanges(media.buffered)) ||
        Math.abs(media.currentTime - position.time) < 0.001
      )
        return;
      try {
        // Safari's audio pipeline can finish pausing after the synchronous
        // correction above and advance its clock again. The next user Play
        // restores the retained position before starting either track. Keep
        // both calls in the original gesture; never resume from a timer.
        media.currentTime = position.time;
      } catch {
        // Source recovery remains the source controller's responsibility.
      }
    },
  };
}
