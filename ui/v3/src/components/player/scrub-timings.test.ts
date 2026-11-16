// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  clearScrubTimings,
  recordScrubTiming,
  scrubDebugEnabled,
  scrubTimingSnapshot,
  scrubTimings,
  subscribeScrubTimings,
  summarizeScrubTimings,
} from "./scrub-timings";

beforeEach(() => clearScrubTimings());
afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

it("keeps a bounded ring of the most recent measurements in order", () => {
  for (let index = 0; index < 300; index++)
    recordScrubTiming("still-cache", index);
  const entries = scrubTimings();
  expect(entries).toHaveLength(256);
  expect(entries[0]!.ms).toBe(44);
  expect(entries.at(-1)!.ms).toBe(299);
  recordScrubTiming("still-cache", Number.NaN);
  recordScrubTiming("still-cache", -1);
  expect(scrubTimings().at(-1)!.ms).toBe(299);
});

it("summarizes count, median and 90th percentile for each tier", () => {
  for (const ms of [40, 10, 30, 20, 50, 60, 70, 80, 90, 100])
    recordScrubTiming("still-network", ms);
  recordScrubTiming("exact-frame", 612.34);
  expect(summarizeScrubTimings()).toEqual({
    "still-cache": { count: 0, median: null, p90: null },
    "still-network": { count: 10, median: 50, p90: 90 },
    "exact-frame": { count: 1, median: 612.3, p90: 612.3 },
    "main-buffer": { count: 0, median: null, p90: null },
  });
});

it("exposes the measurements on window and notifies subscribers with stable snapshots", () => {
  const listener = vi.fn();
  const unsubscribe = subscribeScrubTimings(listener);
  const before = scrubTimingSnapshot();
  expect(scrubTimingSnapshot()).toBe(before);
  recordScrubTiming("main-buffer", 12);
  expect(listener).toHaveBeenCalledOnce();
  const after = scrubTimingSnapshot();
  expect(after).not.toBe(before);
  expect(after["main-buffer"].count).toBe(1);
  expect(window.__stashScrubTimings?.entries).toMatchObject([
    { tier: "main-buffer", ms: 12 },
  ]);
  expect(window.__stashScrubTimings?.summary()["main-buffer"].median).toBe(12);
  window.__stashScrubTimings?.clear();
  expect(scrubTimings()).toEqual([]);
  unsubscribe();
  recordScrubTiming("main-buffer", 1);
  expect(listener).toHaveBeenCalledTimes(2);
});

it("enables the readout only for the exact opt-in value and tolerates blocked storage", () => {
  expect(scrubDebugEnabled()).toBe(false);
  localStorage.setItem("stash.scrubDebug", "true");
  expect(scrubDebugEnabled()).toBe(false);
  localStorage.setItem("stash.scrubDebug", "1");
  expect(scrubDebugEnabled()).toBe(true);
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new DOMException("blocked", "SecurityError");
  });
  expect(scrubDebugEnabled()).toBe(false);
});
