import { definePlayerFeature } from "@videojs/core/dom";
import {
  isMediaBufferCapable,
  isMediaSourceCapable,
  type MediaBufferState,
  type TimeRangeLike,
} from "@videojs/media";

/** Retain the store snapshot when polling finds no change. */
function readRanges(
  previous: [number, number][],
  ranges: TimeRangeLike,
): [number, number][] {
  if (
    previous.length === ranges.length &&
    previous.every(
      ([start, end], index) =>
        start === ranges.start(index) && end === ranges.end(index),
    )
  )
    return previous;
  return Array.from({ length: ranges.length }, (_, index) => [
    ranges.start(index),
    ranges.end(index),
  ]);
}

/**
 * Video.js rc.2's buffer feature only samples on download/metadata events.
 * WebKit's MSE buffers can grow or be evicted without those notifications,
 * including while paused. Sample the public media ranges at most four times
 * a second between events, only while a visible player has a loaded source.
 * This feeds both standard controls and the semantic scene-player contract;
 * it neither drives playback nor depends on a particular streaming engine.
 */
export const sceneBufferFeature = definePlayerFeature<MediaBufferState>({
  name: "buffer",
  state: () => ({ buffered: [], seekable: [] }),
  attach({ target, signal, get, set }) {
    const { media } = target;
    if (!(media instanceof EventTarget) || !isMediaBufferCapable(media)) return;
    const events: EventTarget = media;
    const document = target.container?.ownerDocument;
    let timer: ReturnType<typeof setInterval> | undefined;
    const stop = () => {
      clearInterval(timer);
      timer = undefined;
    };
    const sync = () => {
      const previous = get();
      const buffered = readRanges(previous.buffered, media.buffered);
      const seekable = readRanges(previous.seekable, media.seekable);
      if (buffered !== previous.buffered || seekable !== previous.seekable)
        set({ buffered, seekable });
    };
    const refresh = () => {
      sync();
      if (
        document?.hidden ||
        (isMediaSourceCapable(media) && media.readyState === 0)
      ) {
        stop();
      } else {
        timer ??= setInterval(sync, 250);
      }
    };
    for (const event of [
      "loadedmetadata",
      "durationchange",
      "progress",
      "emptied",
      "canplay",
      "seeked",
    ] as const)
      events.addEventListener(event, refresh, { signal });
    document?.addEventListener("visibilitychange", refresh, { signal });
    signal.addEventListener("abort", stop, { once: true });
    refresh();
  },
});
