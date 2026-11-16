// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createPreparedScrubPreview } from "./prepared-scrub-preview";
import { backgroundKeyframeBatches } from "./scrub-keyframes";
import { clearScrubTimings, summarizeScrubTimings } from "./scrub-timings";

const { streams } = vi.hoisted(() => ({
  streams: [] as {
    video: HTMLVideoElement;
    frame: (visible: boolean, time?: number) => void;
    preview: ReturnType<typeof vi.fn>;
    release: ReturnType<typeof vi.fn>;
    dispose: ReturnType<typeof vi.fn>;
  }[],
}));
vi.mock("./scrub-stream", () => ({
  createScrubStream: (
    video: HTMLVideoElement,
    _id: string,
    _source: string,
    frame: (visible: boolean, time?: number) => void,
  ) => {
    const stream = {
      video,
      frame,
      preview: vi.fn(),
      release: vi.fn(),
      dispose: vi.fn(),
    };
    streams.push(stream);
    return stream;
  },
}));

const KEYFRAMES = {
  index: "https://stash.test/scene/7/keyframes?apikey=key",
  still: "https://stash.test/scene/7/keyframe?apikey=key",
  batch: "https://stash.test/scene/7/keyframes/stills?apikey=key",
};
const everyTwoSeconds = (count: number) =>
  Array.from({ length: count }, (_, index) => index * 2);
/** Level 0 and level 1 of the 60-keyframe fixture index, as scene times. */
const LEVEL_0 = "0,6,14,22,30,36,44,52,60,66,74,82,90,96,104,112";
const LEVEL_1 = "2,10,18,26,32,40,48,56,62,70,78,86,92,100,108,116";

interface PendingFetch {
  url: URL;
  signal: AbortSignal;
  priority: RequestPriority | undefined;
  done: boolean;
  respond: (response: Partial<Response>) => Promise<void>;
}
let fetches: PendingFetch[] = [];
const flush = async () => {
  for (let index = 0; index < 10; index++) await Promise.resolve();
};
const open = (entry: PendingFetch) => !entry.done && !entry.signal.aborted;
const indexFetches = () =>
  fetches.filter((entry) => entry.url.pathname.endsWith("/keyframes"));
const stillFetches = () =>
  fetches.filter((entry) => entry.url.pathname.endsWith("/keyframe"));
const batchFetches = () =>
  fetches.filter((entry) => entry.url.pathname.endsWith("/keyframes/stills"));
const requestedTimes = () =>
  stillFetches().map((entry) => entry.url.searchParams.get("t"));
const batchTimes = () =>
  batchFetches().map((entry) => entry.url.searchParams.get("t"));
const openStills = () => stillFetches().filter(open);
const openBatches = () => batchFetches().filter(open);
const still = (time?: number): Partial<Response> => ({
  ok: true,
  status: 200,
  headers: new Headers({
    "Content-Type": "image/jpeg",
    ...(time === undefined ? {} : { "X-Keyframe-Time": String(time) }),
  }),
  blob: async () => new Blob(["jpeg-bytes"], { type: "image/jpeg" }),
});
/** A batch body with one four-byte still per slot; `failed` slots are empty. */
const batch = (entry: PendingFetch, failed: number[] = []) => {
  const times = entry.url.searchParams.get("t")!.split(",");
  const lengths = times.map((_, slot): number =>
    failed.includes(slot) ? 0 : 4,
  );
  return {
    ok: true,
    status: 200,
    headers: new Headers({
      "Content-Type": "image/jpeg",
      "X-Keyframe-Times": times
        .map((time, slot) => (failed.includes(slot) ? "" : time))
        .join(","),
      "X-Keyframe-Lengths": lengths.join(","),
    }),
    arrayBuffer: async () =>
      new Uint8Array(lengths.reduce((sum, length) => sum + length, 0)).buffer,
  } satisfies Partial<Response>;
};
const keyframeIndex = (times = everyTwoSeconds(60)): Partial<Response> => ({
  ok: true,
  status: 200,
  headers: new Headers({ "Content-Type": "application/json" }),
  json: async () => ({ version: "file-1", duration: 120, times }),
});
const status = (code: number, headers?: HeadersInit): Partial<Response> => ({
  ok: false,
  status: code,
  headers: new Headers(headers),
});
const respond = async (entry: PendingFetch, response: Partial<Response>) => {
  await entry.respond(response);
  vi.advanceTimersByTime(0);
  await flush();
};

const cleanup: (() => void)[] = [];
beforeEach(() => {
  vi.useFakeTimers();
  streams.length = 0;
  fetches = [];
  clearScrubTimings();
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  vi.spyOn(HTMLMediaElement.prototype, "readyState", "get").mockReturnValue(4);
  vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
  vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(
    160,
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (input: string, init?: RequestInit) =>
        new Promise<Response>((resolve, reject) => {
          const signal = init!.signal!;
          signal.addEventListener("abort", () =>
            reject(new DOMException("aborted", "AbortError")),
          );
          const entry: PendingFetch = {
            url: new URL(input),
            signal,
            priority: init?.priority,
            done: false,
            respond: async (response) => {
              entry.done = true;
              resolve(response as Response);
              await flush();
            },
          };
          fetches.push(entry);
        }),
    ),
  );
  let url = 0;
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = vi.fn(() => `blob:still-${++url}`);
      static revokeObjectURL = vi.fn();
    },
  );
});
afterEach(() => {
  for (const dispose of cleanup.splice(0)) dispose();
  document.body.replaceChildren();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, "connection");
});

function fixture({
  keyframes = KEYFRAMES as typeof KEYFRAMES | null,
  start = 0,
  end = 120,
}: {
  keyframes?: typeof KEYFRAMES | null;
  start?: number;
  end?: number;
} = {}) {
  const root = document.createElement("div");
  root.dataset.scenePlayer = "";
  root.dataset.playbackReady = "true";
  const main = document.createElement("video");
  main.dataset.playbackVideo = "";
  let ahead = 12;
  let paused = false;
  Object.defineProperties(main, {
    paused: { get: () => paused },
    buffered: { get: () => ({ length: 1, start: () => 0, end: () => ahead }) },
  });
  const surface = document.createElement("div");
  root.append(main, surface);
  document.body.append(root);
  const onReady = vi.fn();
  const onFailure = vi.fn();
  const controller = createPreparedScrubPreview({
    surface,
    sceneId: "7",
    source: "https://stash.test/scene/7/stream.scrub.master.m3u8",
    keyframes,
    start,
    end,
    offset: 0,
    onFailure,
    onReady,
  });
  cleanup.push(controller.dispose);
  const image = surface.querySelector<HTMLImageElement>(
    "img[data-scrub-still]",
  )!;
  return {
    ...controller,
    root,
    main,
    surface,
    image,
    onReady,
    buffer: (seconds: number) => {
      ahead = seconds;
    },
    pause: (value: boolean) => {
      paused = value;
    },
    /** Healthy playback for the 1.5 s preparation delay, then the index. */
    async prepare(index: Partial<Response> = keyframeIndex()) {
      vi.advanceTimersByTime(2000);
      expect(indexFetches()).toHaveLength(1);
      await respond(indexFetches()[0]!, index);
    },
  };
}

it("waits for healthy playback, loads the index once and prefetches deterministic batches one at a time", async () => {
  const f = fixture();
  f.buffer(2);
  vi.advanceTimersByTime(3000);
  expect(fetches).toHaveLength(0);
  f.buffer(12);
  vi.advanceTimersByTime(1000);
  expect(fetches).toHaveLength(0);
  vi.advanceTimersByTime(1000);
  expect(indexFetches()).toHaveLength(1);
  expect(indexFetches()[0]!.url.searchParams.get("apikey")).toBe("key");
  // Background work waits for canonical index times.
  expect(batchFetches()).toHaveLength(0);
  await respond(indexFetches()[0]!, keyframeIndex());
  expect(f.surface.dataset.keyframeIndex).toBe("ready");
  // Level 0 spans the whole file, whatever the playhead.
  expect(batchFetches()[0]!.url.toString()).toBe(
    `https://stash.test/scene/7/keyframes/stills?apikey=key&v=file-1&t=${LEVEL_0}`,
  );
  expect(batchFetches()[0]!.priority).toBe("low");
  expect(stillFetches()).toHaveLength(0);
  vi.advanceTimersByTime(1000);
  expect(batchFetches()).toHaveLength(1);
  // One separate still per slot; a failed (empty) slot is skipped.
  await respond(batchFetches()[0]!, batch(batchFetches()[0]!, [3]));
  expect(f.surface.dataset.previewFrames).toBe("15");
  expect(URL.createObjectURL).toHaveBeenCalledTimes(15);
  expect(batchTimes()).toEqual([LEVEL_0, LEVEL_1]);
  expect(openBatches()).toHaveLength(1);
  // Prepared stills stay hidden, and no HLS session is started for them.
  expect(f.surface.style.opacity).not.toBe("1");
  expect(f.surface.dataset.previewReady).not.toBe("true");
  expect(streams).toHaveLength(0);

  // Yield immediately when the main player's headroom disappears.
  f.buffer(2);
  vi.advanceTimersByTime(500);
  expect(openBatches()).toHaveLength(0);
  vi.advanceTimersByTime(5000);
  expect(batchFetches()).toHaveLength(2);
  expect(f.main.currentTime).toBe(0);
  // The cancelled batch is requested again, with the same canonical URL.
  f.buffer(12);
  vi.advanceTimersByTime(2000);
  expect(batchTimes()).toEqual([LEVEL_0, LEVEL_1, LEVEL_1]);
  await respond(batchFetches()[2]!, batch(batchFetches()[2]!));
  expect(f.surface.dataset.previewFrames).toBe("31");
  expect(indexFetches()).toHaveLength(1);
});

it("stops background batches at 64 stills per scene", async () => {
  const f = fixture({ end: 1200 });
  await f.prepare(keyframeIndex(everyTwoSeconds(600)));
  for (let request = 0; request < 4; request++) {
    expect(openBatches()).toHaveLength(1);
    const entry = openBatches()[0]!;
    await respond(entry, batch(entry));
  }
  vi.advanceTimersByTime(10_000);
  expect(batchFetches()).toHaveLength(4);
  expect(f.surface.dataset.previewFrames).toBe("64");
  expect(stillFetches()).toHaveLength(0);
});

it("requests only whole-file batches with a slot inside a marker range", async () => {
  const f = fixture({ start: 40, end: 50 });
  await f.prepare();
  const expected = backgroundKeyframeBatches(60, 20, 24, 64);
  for (const positions of expected) {
    const entry = openBatches()[0]!;
    expect(entry.url.searchParams.get("t")).toBe(
      positions.map((position) => position * 2).join(","),
    );
    await respond(entry, batch(entry));
  }
  expect(batchFetches()).toHaveLength(expected.length);
  expect(batchTimes()[0]).toBe(LEVEL_0);
  for (const times of batchTimes())
    expect(
      times!
        .split(",")
        .map(Number)
        .some((time) => time >= 40 && time <= 48),
    ).toBe(true);
  vi.advanceTimersByTime(5000);
  expect(batchFetches()).toHaveLength(expected.length);
});

it("honours data saving, pauses during any drag and releases stills when hidden", async () => {
  Object.defineProperty(navigator, "connection", {
    configurable: true,
    value: { saveData: true },
  });
  const f = fixture();
  vi.advanceTimersByTime(5000);
  expect(fetches).toHaveLength(0);
  Reflect.deleteProperty(navigator, "connection");
  await f.prepare();
  await respond(batchFetches()[0]!, batch(batchFetches()[0]!));
  expect(f.surface.dataset.previewFrames).toBe("16");
  // A buffered main-player drag also takes priority over coverage.
  const scrubber = document.createElement("div");
  scrubber.dataset.positionScrubber = "";
  scrubber.dataset.dragging = "true";
  f.root.append(scrubber);
  vi.advanceTimersByTime(500);
  expect(openBatches()).toHaveLength(0);
  const requested = batchFetches().length;
  vi.advanceTimersByTime(5000);
  expect(batchFetches()).toHaveLength(requested);
  scrubber.remove();
  vi.advanceTimersByTime(2000);
  expect(openBatches()).toHaveLength(1);

  vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  document.dispatchEvent(new Event("visibilitychange"));
  expect(openBatches()).toHaveLength(0);
  expect(f.surface.dataset.previewFrames).toBe("0");
  expect(URL.revokeObjectURL).toHaveBeenCalledTimes(16);
  const afterHidden = batchFetches().length;
  vi.advanceTimersByTime(3000);
  expect(batchFetches()).toHaveLength(afterHidden);
  // The index is retained for the same file version.
  expect(indexFetches()).toHaveLength(1);
});

it("expires prepared stills after five minutes of paused inactivity", async () => {
  const f = fixture();
  f.pause(true);
  await f.prepare();
  await respond(batchFetches()[0]!, batch(batchFetches()[0]!));
  expect(f.surface.dataset.previewFrames).toBe("16");
  vi.advanceTimersByTime(5 * 60_000);
  expect(f.surface.dataset.previewFrames).toBe("0");
  expect(URL.revokeObjectURL).toHaveBeenCalled();
});

it("backs off background batches while the server is saturated", async () => {
  const f = fixture();
  await f.prepare();
  await respond(batchFetches()[0]!, status(503, { "Retry-After": "2" }));
  vi.advanceTimersByTime(1500);
  expect(batchFetches()).toHaveLength(1);
  vi.advanceTimersByTime(1000);
  expect(batchTimes()).toEqual([LEVEL_0, LEVEL_0]);
  expect(f.surface.dataset.previewFrames).toBe("0");
});

it("falls back to canonical single stills when the batch endpoint is missing", async () => {
  const f = fixture();
  await f.prepare();
  await respond(batchFetches()[0]!, status(404));
  vi.advanceTimersByTime(500);
  expect(batchFetches()).toHaveLength(1);
  // The playhead keyframe first, then the middle; never more than two.
  expect(requestedTimes()).toEqual(["0", "60"]);
  expect(stillFetches()[1]!.url.toString()).toBe(
    "https://stash.test/scene/7/keyframe?apikey=key&t=60&v=file-1",
  );
  expect(stillFetches()[1]!.priority).toBe("low");
  await respond(stillFetches()[1]!, still(60));
  expect(requestedTimes()).toEqual(["0", "60", "30"]);
  // Dense prefetch near the finger is unavailable too.
  f.preview(90.5);
  vi.advanceTimersByTime(1000);
  expect(batchFetches()).toHaveLength(1);
});

it("falls back to raw times without an index and caches by the returned keyframe time", async () => {
  const f = fixture();
  await f.prepare(status(404));
  expect(f.surface.dataset.keyframeIndex).toBe("unavailable");
  expect(requestedTimes()).toEqual(["0", "60"]);
  expect(stillFetches()[1]!.url.searchParams.has("v")).toBe(false);
  await respond(stillFetches()[1]!, still(59.5));
  // Within [returned keyframe, requested time]: an exact cache hit.
  f.preview(59.9);
  expect(f.surface.dataset.previewKind).toBe("still");
  expect(f.image.dataset.scrubTime).toBe("59.5");
  expect(requestedTimes()).not.toContain("59.9");
  // Beyond it the nearest still covers the movement while the target loads.
  f.preview(61);
  expect(f.image.dataset.scrubTime).toBe("59.5");
  expect(requestedTimes()).toContain("61");
  await respond(stillFetches().at(-1)!, still(60.8));
  expect(f.image.dataset.scrubTime).toBe("60.8");
  expect(summarizeScrubTimings()).toMatchObject({
    "still-cache": { count: 1 },
    "still-network": { count: 1 },
  });
  // No index means no batches, and a missing index is not requested again.
  vi.advanceTimersByTime(1000);
  expect(batchFetches()).toHaveLength(0);
  f.preview(null);
  vi.advanceTimersByTime(120_000);
  expect(indexFetches()).toHaveLength(1);
});

it("shows the cached keyframe at or before the target, and only the decoded exact frame reports readiness", async () => {
  const f = fixture();
  await f.prepare();
  await respond(batchFetches()[0]!, batch(batchFetches()[0]!));
  const requested = fetches.length;
  f.preview(61.5);
  expect(f.surface.dataset.previewKind).toBe("still");
  expect(f.surface.dataset.previewReady).toBe("true");
  expect(f.surface.style.opacity).toBe("1");
  expect(f.image.dataset.scrubTime).toBe("60");
  // A hit needs neither a single still nor a dense batch yet.
  expect(fetches).toHaveLength(requested);
  // The background level 1 batch has no slot for this target.
  expect(openBatches()).toHaveLength(0);
  const stream = streams[0]!;
  expect(stream.preview).toHaveBeenLastCalledWith(61.5);
  // A keyframe still never counts toward the precision dwell.
  expect(f.onReady.mock.calls.every(([time]) => time === null)).toBe(true);
  stream.frame(true, 60);
  expect(f.surface.dataset.previewKind).toBe("still");
  stream.frame(true, 61.5);
  expect(f.surface.dataset.previewKind).toBe("video");
  expect(f.onReady).toHaveBeenLastCalledWith(61.5);
  // Nearby decoded video stays on screen instead of flashing back to a still.
  Object.defineProperty(stream.video, "buffered", {
    configurable: true,
    get: () => ({ length: 1, start: () => 58, end: () => 66 }),
  });
  f.preview(62);
  expect(f.surface.dataset.previewKind).toBe("video");
  expect(f.onReady).toHaveBeenLastCalledWith(null);
  expect(stillFetches()).toHaveLength(0);
  stream.frame(true, 62);
  expect(f.onReady).toHaveBeenLastCalledWith(62);
  // A distant target shows its cached keyframe immediately.
  f.preview(1.2);
  expect(f.surface.dataset.previewKind).toBe("still");
  expect(f.image.dataset.scrubTime).toBe("0");
  f.preview(null);
  expect(f.surface.style.opacity).toBe("0");
  expect(f.onReady).toHaveBeenLastCalledWith(null);
  stream.frame(true, 1.2);
  expect(f.onReady).toHaveBeenLastCalledWith(null);
  expect(f.main.currentTime).toBe(0);
  expect(summarizeScrubTimings()).toMatchObject({
    "still-cache": { count: 2 },
    "still-network": { count: 0 },
    "exact-frame": { count: 2 },
  });
});

it("fetches uncached stills latest-wins, with at most two foreground requests", async () => {
  const f = fixture();
  // The first drag requests the index at once without waiting for it.
  f.preview(10);
  expect(indexFetches()).toHaveLength(1);
  expect(requestedTimes()).toEqual(["10"]);
  expect(stillFetches()[0]!.priority).toBe("high");
  await respond(indexFetches()[0]!, keyframeIndex());
  // The miss also wants the dense batch around the finger.
  expect(batchTimes()).toEqual([everyTwoSeconds(16).join(",")]);
  expect(batchFetches()[0]!.priority).toBe("auto");
  f.preview(20.5);
  f.preview(30.5);
  f.preview(40.5);
  expect(requestedTimes()).toEqual(["10", "20"]);
  expect(stillFetches()[1]!.url.searchParams.get("v")).toBe("file-1");
  // At most one dense batch is in flight.
  expect(openBatches()).toHaveLength(1);
  // A superseded response populates the cache without being displayed.
  await respond(stillFetches()[0]!, still(10));
  expect(f.surface.dataset.previewReady).not.toBe("true");
  // The freed slot goes to the latest target, not the intermediate one.
  expect(requestedTimes()).toEqual(["10", "20", "40"]);
  await respond(stillFetches()[1]!, still(20));
  expect(f.surface.dataset.previewReady).not.toBe("true");
  await respond(stillFetches()[2]!, still(40));
  expect(f.surface.dataset.previewKind).toBe("still");
  expect(f.image.dataset.scrubTime).toBe("40");
  expect(f.surface.dataset.previewFrames).toBe("3");
  f.preview(21);
  expect(f.image.dataset.scrubTime).toBe("20");
  expect(requestedTimes()).toHaveLength(3);
  expect(summarizeScrubTimings()).toMatchObject({
    "still-cache": { count: 1 },
    "still-network": { count: 1 },
  });
  expect(f.onReady.mock.calls.every(([time]) => time === null)).toBe(true);
  // Release stops foreground fetching, including the dense batch.
  f.preview(80.2);
  expect(openStills()).toHaveLength(1);
  expect(openBatches()).toHaveLength(1);
  f.preview(null);
  expect(openStills()).toHaveLength(0);
  expect(openBatches()).toHaveLength(0);
  expect(f.surface.style.opacity).toBe("0");
});

it("lets a dense batch that lands first satisfy the target and cancel its single request", async () => {
  const f = fixture();
  f.preview(10);
  await respond(indexFetches()[0]!, keyframeIndex());
  f.preview(20.5);
  const single = stillFetches()[1]!;
  expect(single.url.searchParams.get("t")).toBe("20");
  const dense = batchFetches()[0]!;
  await respond(dense, batch(dense));
  expect(f.surface.dataset.previewKind).toBe("still");
  expect(f.image.dataset.scrubTime).toBe("20");
  expect(single.signal.aborted).toBe(true);
  expect(f.surface.dataset.previewFrames).toBe("16");
  expect(summarizeScrubTimings()["still-network"].count).toBe(1);
});

it("prefetches the dense neighbourhood when a drag slows or dwells, in its direction", async () => {
  const f = fixture();
  f.preview(5);
  await respond(indexFetches()[0]!, keyframeIndex());
  await respond(batchFetches()[0]!, batch(batchFetches()[0]!));
  // A fast pass over cached stills does not prefetch.
  f.preview(31);
  expect(batchFetches()).toHaveLength(1);
  // Dwelling prefetches the next dense batch in the drag direction.
  vi.advanceTimersByTime(250);
  expect(batchTimes()[1]).toBe(everyTwoSeconds(32).slice(16).join(","));
  await respond(batchFetches()[1]!, batch(batchFetches()[1]!));
  // Slowing to one keyframe in 200 ms over cached stills, before any dwell,
  // prefetches ahead of the finger.
  f.preview(58.5);
  expect(batchFetches()).toHaveLength(2);
  vi.advanceTimersByTime(200);
  f.preview(60.5);
  expect(batchTimes()[2]).toBe(everyTwoSeconds(48).slice(32).join(","));
  expect(batchFetches()[2]!.priority).toBe("auto");
  expect(stillFetches()).toHaveLength(1);
});

it("prioritises the drag over background batches it does not need", async () => {
  const unrelated = fixture();
  await unrelated.prepare();
  const levelZero = batchFetches()[0]!;
  unrelated.preview(3);
  expect(levelZero.signal.aborted).toBe(true);
  expect(requestedTimes()).toEqual(["2"]);

  fetches = [];
  const wanted = fixture();
  await wanted.prepare();
  const kept = batchFetches()[0]!;
  // Level 0 includes the keyframe at 60 s, so the drag keeps it.
  wanted.preview(61);
  expect(kept.signal.aborted).toBe(false);
  await respond(kept, batch(kept));
  expect(wanted.image.dataset.scrubTime).toBe("60");
  expect(summarizeScrubTimings()["still-network"].count).toBe(1);
});

it("backs off while saturated and turns stills off when the endpoint is missing", async () => {
  const f = fixture();
  f.preview(10);
  await respond(indexFetches()[0]!, status(404));
  await respond(stillFetches()[0]!, status(503, { "Retry-After": "2" }));
  f.preview(30);
  expect(requestedTimes()).toEqual(["10"]);
  vi.advanceTimersByTime(1900);
  expect(requestedTimes()).toEqual(["10"]);
  vi.advanceTimersByTime(200);
  expect(requestedTimes()).toEqual(["10", "30"]);
  await respond(stillFetches()[1]!, status(404));
  f.preview(50);
  f.preview(null);
  vi.advanceTimersByTime(10_000);
  f.preview(70);
  expect(requestedTimes()).toEqual(["10", "30"]);
  // The exact-frame stream is unaffected.
  expect(streams[0]!.preview).toHaveBeenLastCalledWith(70);
});

it("treats timeouts as failures and stops after repeated extraction errors", async () => {
  const f = fixture();
  f.preview(10);
  await respond(indexFetches()[0]!, status(404));
  vi.advanceTimersByTime(5000);
  await flush();
  expect(stillFetches()[0]!.signal.aborted).toBe(true);
  f.preview(20);
  await respond(stillFetches()[1]!, status(500));
  f.preview(40);
  await respond(stillFetches()[2]!, status(500));
  f.preview(60);
  f.preview(80);
  expect(requestedTimes()).toEqual(["10", "20", "40"]);
});

it("leaves the keyframe tier off without library endpoints", () => {
  const f = fixture({ keyframes: null });
  vi.advanceTimersByTime(5000);
  f.preview(10);
  expect(fetches).toHaveLength(0);
  expect(streams[0]!.preview).toHaveBeenLastCalledWith(10);
});

it("does not retain a late still after the player is disposed", async () => {
  const f = fixture();
  f.preview(10);
  const pending = stillFetches()[0]!;
  f.dispose();
  expect(pending.signal.aborted).toBe(true);
  expect(indexFetches()[0]!.signal.aborted).toBe(true);
  await pending.respond(still(10));
  expect(URL.createObjectURL).not.toHaveBeenCalled();
});
