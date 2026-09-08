import { expect, it, vi } from "vitest";
import { Virtualizer, type VirtualItem } from "@tanstack/react-virtual";
import {
  ListMeasurementsCache,
  matchesListMeasurements,
} from "./list-virtualizer-measurements";

function createVirtualizer(rows?: VirtualItem[]) {
  return new Virtualizer<HTMLElement, HTMLElement>({
    count: 4,
    getScrollElement: () => null,
    estimateSize: () => 100,
    initialRect: { width: 800, height: 100 },
    initialMeasurementsCache: rows,
    observeElementRect: vi.fn(),
    observeElementOffset: vi.fn(),
    scrollToFn: vi.fn(),
  });
}

it("restores the same row positions when measured heights differ from estimates", () => {
  const original = createVirtualizer();
  original.getTotalSize();
  original.resizeItem(0, 160);
  original.resizeItem(1, 80);
  expect(original.getTotalSize()).toBe(440);

  const restored = createVirtualizer(original.measurementsCache.slice());
  expect(restored.getTotalSize()).toBe(original.getTotalSize());
  expect(restored.measurementsCache[2].start).toBe(240);
  const estimatesOnly = createVirtualizer();
  estimatesOnly.getTotalSize();
  expect(estimatesOnly.measurementsCache[2].start).toBe(200);
});

it("rejects measurements after the width or ordered items change", () => {
  const saved = { width: 800, itemIds: ["a", "b"], rows: [] };
  expect(matchesListMeasurements(saved, 800, ["a", "b"])).toBe(true);
  // Data may still be loading; width can already be validated.
  expect(matchesListMeasurements(saved, 800)).toBe(true);
  expect(matchesListMeasurements(saved, 600, ["a", "b"])).toBe(false);
  expect(matchesListMeasurements(saved, 800, ["b", "a"])).toBe(false);
  expect(matchesListMeasurements(saved, 800, ["a"])).toBe(false);
  expect(matchesListMeasurements(saved, 800, ["a", "c"])).toBe(false);
});

it("bounds retained layouts and keeps the most recently updated pages", () => {
  const cache = new ListMeasurementsCache(2);
  const snapshot = { width: 800, itemIds: ["a"], rows: [] };
  cache.set("first-page", snapshot);
  cache.set("second-page", snapshot);
  const updated = { ...snapshot, width: 600 };
  cache.set("first-page", updated);
  cache.set("third-page", snapshot);
  expect(cache.get("first-page")).toEqual(updated);
  expect(cache.get("second-page")).toBeUndefined();
  expect(cache.get("third-page")).toEqual(snapshot);
});
