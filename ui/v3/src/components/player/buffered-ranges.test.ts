import { describe, expect, it } from "vitest";
import { createBufferedRangesReader, isTimeBuffered } from "./buffered-ranges";

describe("buffered ranges", () => {
  it("preserves gaps and a clip's scene-time origin", () => {
    const read = createBufferedRangesReader();
    expect(
      read(
        [
          [0, 2],
          [6, 10],
        ],
        100,
      ),
    ).toEqual([
      { start: 100, end: 102 },
      { start: 106, end: 110 },
    ]);
  });

  it("keeps equal snapshots stable and replaces them after eviction or a source change", () => {
    const read = createBufferedRangesReader();
    const first = read(
      [
        [0, 2],
        [6, 10],
      ],
      100,
    );
    expect(
      read(
        [
          [0, 2],
          [6, 10],
        ],
        100,
      ),
    ).toBe(first);
    const evicted = read([[7, 10]], 100);
    expect(evicted).toEqual([{ start: 107, end: 110 }]);
    expect(first).toEqual([
      { start: 100, end: 102 },
      { start: 106, end: 110 },
    ]);
    expect(read([[7, 10]], 0)).toEqual([{ start: 7, end: 10 }]);
    const empty = read([], 0);
    expect(empty).toEqual([]);
    expect(read([], 200)).toBe(empty);
  });

  it("does not treat holes or the exclusive buffered end as playable data", () => {
    const ranges = [
      [0, 2],
      [6, 10],
    ] as const;
    for (const time of [0, 1, 6, 9.99])
      expect(isTimeBuffered(time, ranges)).toBe(true);
    for (const time of [-1, 2, 5, 10])
      expect(isTimeBuffered(time, ranges)).toBe(false);
  });
});
