import { afterEach, expect, it, vi } from "vitest";
import {
  fetchKeyframeIndex,
  fetchKeyframeStill,
  keyframeAt,
  keyframePosition,
  keyframePreparationTimes,
  keyframeStillURL,
  parseKeyframeIndex,
} from "./scrub-keyframes";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("finds the greatest keyframe at or before a target with the server's tolerance", () => {
  const times = [0, 2.002, 4.004, 6.006, 8.008, 10.01];
  expect(keyframeAt(times, 0)).toBe(0);
  expect(keyframeAt(times, 1.99)).toBe(0);
  expect(keyframeAt(times, 1.995)).toBe(2.002);
  expect(keyframeAt(times, 9.9)).toBe(8.008);
  expect(keyframeAt(times, 10.01)).toBe(10.01);
  expect(keyframeAt(times, 500)).toBe(10.01);
  // Before the first keyframe resolves to it, as a raw request would.
  expect(keyframeAt([0.5, 2], 0.1)).toBe(0.5);
  expect(keyframePosition([], 3)).toBe(-1);
  expect(keyframeAt([], 3)).toBeUndefined();
  expect(keyframePosition(times, Number.NaN)).toBe(-1);
  const many = Array.from({ length: 10_000 }, (_, index) => index * 1.5);
  expect(keyframeAt(many, 7_777.7)).toBe(7_777.5);
});

it("validates an optional index and orders unsorted times", () => {
  expect(
    parseKeyframeIndex({ version: "f1-g2", duration: 12, times: [0, 2, 4] }),
  ).toEqual({ version: "f1-g2", times: [0, 2, 4] });
  expect(
    parseKeyframeIndex({ version: "v", times: [4, "x", 0, -1, 2] }),
  ).toEqual({ version: "v", times: [0, 2, 4] });
  expect(parseKeyframeIndex({ version: "v", times: [] })).toBeNull();
  expect(parseKeyframeIndex({ times: [0] })).toBeNull();
  expect(parseKeyframeIndex(null)).toBeNull();
  expect(parseKeyframeIndex("index")).toBeNull();
});

it("prepares keyframes breadth-first inside the selected range without repeats", () => {
  const times = Array.from({ length: 61 }, (_, index) => index * 2);
  const order = keyframePreparationTimes(times, 0, 120, 128, 33);
  expect(order.slice(0, 6)).toEqual([32, 60, 30, 90, 14, 44]);
  expect(order).toHaveLength(60);
  expect(new Set(order).size).toBe(60);
  expect(Math.max(...order)).toBe(118);
  expect(keyframePreparationTimes(times, 0, 120, 10)).toHaveLength(10);
  // A marker range starts with the keyframe its first frame decodes from.
  const marker = keyframePreparationTimes(times, 41, 47, 128);
  expect([...marker].sort((a, b) => a - b)).toEqual([40, 42, 44, 46]);
  // Clustered keyframes that fractions cannot reach are filled in order.
  const clustered = keyframePreparationTimes([0, 0.1, 0.2, 50], 0, 60, 128);
  expect([...clustered].sort((a, b) => a - b)).toEqual([0, 0.1, 0.2, 50]);
  expect(keyframePreparationTimes([], 0, 10, 8)).toEqual([]);
  expect(keyframePreparationTimes(times, 10, 10, 8)).toEqual([]);
});

it("uses one canonical URL per keyframe while retaining authorization", () => {
  const base = "https://stash.test/scene/7/keyframe?apikey=key";
  expect(keyframeStillURL(base, 8.008, "f1-g2")).toBe(
    "https://stash.test/scene/7/keyframe?apikey=key&t=8.008&v=f1-g2",
  );
  expect(keyframeStillURL(base, 9.25)).toBe(
    "https://stash.test/scene/7/keyframe?apikey=key&t=9.25",
  );
});

it("classifies index responses as available, missing or retryable", async () => {
  const signal = new AbortController().signal;
  const fetch = vi.fn<typeof globalThis.fetch>();
  vi.stubGlobal("fetch", fetch);
  fetch.mockResolvedValueOnce(
    Response.json({ version: "v", duration: 4, times: [0, 2] }),
  );
  expect(await fetchKeyframeIndex("https://stash.test/k", signal)).toEqual({
    status: "ok",
    index: { version: "v", times: [0, 2] },
  });
  fetch.mockResolvedValueOnce(new Response(null, { status: 404 }));
  expect(await fetchKeyframeIndex("https://stash.test/k", signal)).toEqual({
    status: "missing",
  });
  fetch.mockResolvedValueOnce(
    new Response(null, { status: 503, headers: { "Retry-After": "12" } }),
  );
  expect(await fetchKeyframeIndex("https://stash.test/k", signal)).toEqual({
    status: "retry",
    after: 12,
  });
  fetch.mockResolvedValueOnce(Response.json({ version: "v", times: [] }));
  expect(await fetchKeyframeIndex("https://stash.test/k", signal)).toEqual({
    status: "missing",
  });
  fetch.mockRejectedValueOnce(new DOMException("timeout", "AbortError"));
  expect(await fetchKeyframeIndex("https://stash.test/k", signal)).toEqual({
    status: "retry",
    after: 30,
  });
});

it("reads the returned keyframe time and classifies still failures", async () => {
  const fetch = vi.fn<typeof globalThis.fetch>();
  vi.stubGlobal("fetch", fetch);
  const signal = new AbortController().signal;
  fetch.mockResolvedValueOnce(
    new Response(new Blob(["jpeg"]), {
      headers: { "Content-Type": "image/jpeg", "X-Keyframe-Time": "8.008" },
    }),
  );
  const ok = await fetchKeyframeStill("https://stash.test/s", signal, true);
  expect(ok).toMatchObject({ status: "ok", time: 8.008 });
  expect(fetch).toHaveBeenLastCalledWith("https://stash.test/s", {
    signal,
    priority: "low",
  });
  fetch.mockResolvedValueOnce(
    new Response(new Blob(["jpeg"]), {
      headers: { "Content-Type": "image/jpeg" },
    }),
  );
  expect(
    await fetchKeyframeStill("https://stash.test/s", signal, false),
  ).toMatchObject({ status: "ok", time: null });
  fetch.mockResolvedValueOnce(new Response(null, { status: 404 }));
  expect(
    await fetchKeyframeStill("https://stash.test/s", signal, false),
  ).toEqual({ status: "missing" });
  fetch.mockResolvedValueOnce(
    new Response(null, { status: 503, headers: { "Retry-After": "60" } }),
  );
  expect(
    await fetchKeyframeStill("https://stash.test/s", signal, false),
  ).toEqual({ status: "busy", after: 10 });
  fetch.mockResolvedValueOnce(new Response("html", { status: 200 }));
  expect(
    await fetchKeyframeStill("https://stash.test/s", signal, false),
  ).toEqual({ status: "error" });
  const aborted = new AbortController();
  aborted.abort();
  fetch.mockRejectedValueOnce(new DOMException("aborted", "AbortError"));
  expect(
    await fetchKeyframeStill("https://stash.test/s", aborted.signal, false),
  ).toEqual({ status: "aborted" });
});
