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

it("bounds compressed bytes and evicts fine samples before broad coverage", () => {
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
  cache.add(30.01, blob, false);
  cache.add(40, new Blob(["12345678901"]), true);
  expect(cache.size).toBe(2);
  cache.clear();
  expect(revoke.mock.calls.flat()).toEqual(["blob:2", "blob:1", "blob:3"]);
});

it("also limits tiny frames by count and releases every object URL", () => {
  let id = 0;
  vi.spyOn(URL, "createObjectURL").mockImplementation(() => `blob:${++id}`);
  const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const cache = new ScrubFrameCache(100, 3);
  for (let time = 0; time < 10; time++) cache.add(time, new Blob(["x"]), true);
  expect(cache.size).toBe(3);
  cache.clear();
  expect(revoke).toHaveBeenCalledTimes(10);
});
