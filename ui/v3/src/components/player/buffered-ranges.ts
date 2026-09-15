import type { PlaybackRange } from "@/core/marker-range";

export type MediaTimeRanges = readonly (readonly [number, number])[];

export function readTimeRanges(ranges: TimeRanges): MediaTimeRanges {
  return Array.from(
    { length: ranges.length },
    (_, index): readonly [number, number] => [
      ranges.start(index),
      ranges.end(index),
    ],
  );
}

export function isTimeBuffered(time: number, ranges: MediaTimeRanges): boolean {
  return ranges.some(([start, end]) => time >= start && time < end);
}

/** A stable scene-time snapshot for external-store subscribers. Equal native
 * range snapshots must not schedule renders or erase holes between ranges. */
export function createBufferedRangesReader() {
  let snapshot: readonly PlaybackRange[] = [];
  return (
    ranges: MediaTimeRanges,
    offset: number,
  ): readonly PlaybackRange[] => {
    if (
      snapshot.length !== ranges.length ||
      ranges.some(
        ([start, end], index) =>
          snapshot[index]?.start !== start + offset ||
          snapshot[index]?.end !== end + offset,
      )
    ) {
      snapshot = ranges.map(([start, end]) => ({
        start: start + offset,
        end: end + offset,
      }));
    }
    return snapshot;
  };
}
