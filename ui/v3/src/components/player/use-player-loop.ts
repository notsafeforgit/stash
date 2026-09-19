import { useEffect, type RefObject } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";

/** Restart just before EOF while the decoder is still playing. Native ended
 * handling remains the fallback for delayed timers and background playback. */
export function usePlayerLoop({
  enabled,
  rootRef,
  source,
  start,
  end,
  offsetStart,
  frameRate,
  onLoop,
}: {
  enabled: boolean;
  rootRef: RefObject<HTMLElement | null>;
  source: string | undefined;
  start: number;
  end: number | undefined;
  offsetStart: number;
  frameRate: number | undefined;
  onLoop: (start: number) => void;
}) {
  const onLoopRef = useCommittedRef(onLoop);
  useEffect(() => {
    if (!enabled || !source) return;
    const video = rootRef.current?.querySelector("video");
    if (!video) return;
    const document = video.ownerDocument;
    let timer: ReturnType<typeof setTimeout> | undefined;
    // Keep the final frame: trim at most a quarter frame, capped at 5 ms.
    // Waiting for EOF drains the decoder and adds a pause/resume at every lap.
    const fps = frameRate && frameRate > 0 ? frameRate : 30;
    const margin = Math.min(0.005, 0.25 / fps);
    const cancel = () => {
      clearTimeout(timer);
      timer = undefined;
    };
    const schedule = () => {
      cancel();
      if (
        document.hidden ||
        video.paused ||
        video.seeking ||
        video.ended ||
        video.readyState < 2 ||
        video.playbackRate <= 0 ||
        !Number.isFinite(video.duration)
      )
        return;
      const boundary = Math.min(
        video.duration,
        end === undefined ? Infinity : end - offsetStart,
      );
      if (boundary - (start - offsetStart) <= margin * 2) return;
      const remaining = boundary - video.currentTime;
      if (remaining <= margin) {
        onLoopRef.current(start);
        return;
      }
      // Re-read the media clock when the timer fires. Buffering, a rate
      // change or a seek must never loop early based on elapsed wall time.
      timer = setTimeout(
        schedule,
        Math.max(4, ((remaining - margin) / video.playbackRate) * 1000),
      );
    };
    const rescheduleEvents = [
      "playing",
      "timeupdate",
      "seeked",
      "ratechange",
      "durationchange",
    ];
    const cancelEvents = ["pause", "seeking", "waiting", "emptied", "ended"];
    for (const event of rescheduleEvents)
      video.addEventListener(event, schedule);
    for (const event of cancelEvents) video.addEventListener(event, cancel);
    document.addEventListener("visibilitychange", schedule);
    schedule();
    return () => {
      cancel();
      for (const event of rescheduleEvents)
        video.removeEventListener(event, schedule);
      for (const event of cancelEvents)
        video.removeEventListener(event, cancel);
      document.removeEventListener("visibilitychange", schedule);
    };
  }, [enabled, rootRef, source, start, end, offsetStart, frameRate]);
}
