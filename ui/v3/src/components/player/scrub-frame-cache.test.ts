import { afterEach, expect, it, vi } from "vitest";
import { ScrubFrameCache, scrubPreparationTimes } from "./scrub-frame-cache";

afterEach(() => vi.restoreAllMocks());

it("covers the entire selected interval progressively with a bounded sample count", () => {
  expect(scrubPreparationTimes(10, 42).slice(0, 7)).toEqual([
    26, 18, 34, 14, 22, 30, 38,
  ]);
  const long = scrubPreparationTimes(0, 36_000);
  expect(long).toHaveLength(63);
  expect(new Set(long).size).toBe(63);
  expect(Math.min(...long)).toBeGreaterThan(0);
  expect(Math.max(...long)).toBeLessThan(36_000);
  expect(scrubPreparationTimes(0, Infinity)).toEqual([]);
  expect(scrubPreparationTimes(5, 5)).toEqual([]);
});

it("bounds compressed bytes and evicts fine stills before broad coverage", () => {
  let id = 0;
  vi.spyOn(URL, "createObjectURL").mockImplementation(() => `blob:${++id}`);
  const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const cache = new ScrubFrameCache(10, 3);
  const blob = new Blob(["1234"]);
  cache.add(10, blob, true);
  cache.add(11, blob, false);
  cache.add(30, blob, true);
  expect(cache.size).toBe(2);
  expect(cache.has(10)).toBe(true);
  expect(cache.has(11)).toBe(false);
  expect(cache.nearest(28, 3)?.time).toBe(30);
  expect(cache.nearest(20, 3)).toBeUndefined();
  // Millisecond-rounded keyframe times identify the same still.
  expect(cache.add(30.001, blob, false)?.url).toBe("blob:3");
  expect(cache.size).toBe(2);
  cache.add(40, new Blob(["12345678901"]), true);
  expect(cache.size).toBe(2);
  cache.clear();
  expect(revoke.mock.calls.flat()).toEqual(["blob:2", "blob:1", "blob:3"]);
});

it("also limits tiny stills by count and releases every object URL", () => {
  let id = 0;
  vi.spyOn(URL, "createObjectURL").mockImplementation(() => `blob:${++id}`);
  const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const cache = new ScrubFrameCache(100, 3);
  for (let time = 0; time < 10; time++) cache.add(time, new Blob(["x"]), true);
  expect(cache.size).toBe(3);
  cache.clear();
  expect(revoke).toHaveBeenCalledTimes(10);
});

it("defaults to about 256 ten-kilobyte stills within 4 MiB", () => {
  vi.spyOn(URL, "createObjectURL").mockImplementation(() => "blob:still");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const cache = new ScrubFrameCache();
  const still = new Blob([new Uint8Array(10 * 1024)]);
  for (let time = 0; time < 300; time++) cache.add(time * 2, still, true);
  expect(cache.size).toBe(256);
  const bytes = new ScrubFrameCache();
  const large = new Blob([new Uint8Array(64 * 1024)]);
  for (let time = 0; time < 100; time++) bytes.add(time, large, true);
  expect(bytes.size).toBe(64);
});

it("resolves raw targets through the keyframe interval each response proved", () => {
  vi.spyOn(URL, "createObjectURL").mockImplementation(() => "blob:still");
  const cache = new ScrubFrameCache();
  // A request for 9.4 returned keyframe 8; a later request for 9.9 did too.
  cache.add(8, new Blob(["x"]), false, 9.4);
  expect(cache.covering(8)?.time).toBe(8);
  expect(cache.covering(9.4)?.time).toBe(8);
  expect(cache.covering(9.9)).toBeUndefined();
  expect(cache.covering(7.9)).toBeUndefined();
  cache.add(8, new Blob(["x"]), true, 9.9);
  expect(cache.size).toBe(1);
  expect(cache.covering(9.9)?.time).toBe(8);
});
