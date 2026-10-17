// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createScrubStream, scrubStreamURL } from "./scrub-stream";

const { instances } = vi.hoisted(() => ({
  instances: [] as {
    hasEnoughToStart: boolean;
    source: string;
    startLoad: ReturnType<typeof vi.fn>;
    stopLoad: ReturnType<typeof vi.fn>;
    recoverMediaError: ReturnType<typeof vi.fn>;
    destroy: ReturnType<typeof vi.fn>;
    events: Map<string, (...args: unknown[]) => void>;
  }[],
}));
vi.mock("@videojs/hlsjs-video", () => ({
  Hls: class {
    static isSupported = () => true;
    static Events = {
      MANIFEST_PARSED: "manifest",
      FRAG_BUFFERED: "buffered",
      ERROR: "error",
    };
    static ErrorTypes = { MEDIA_ERROR: "mediaError" };
    hasEnoughToStart = true;
    source = "";
    events = new Map<string, (...args: unknown[]) => void>();
    startLoad = vi.fn();
    stopLoad = vi.fn();
    recoverMediaError = vi.fn();
    destroy = vi.fn();
    constructor() {
      instances.push(this);
    }
    on(event: string, callback: (...args: unknown[]) => void) {
      this.events.set(event, callback);
    }
    attachMedia() {}
    loadSource(source: string) {
      this.source = source;
    }
  },
}));

const source =
  "https://stash.test/scene/example/stream.scrub.master.m3u8?resolution=LOW";
const beacon = vi.fn();
const cleanup: (() => void)[] = [];
beforeEach(() => {
  vi.useFakeTimers();
  instances.length = 0;
  beacon.mockReset().mockReturnValue(true);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response()));
  Object.defineProperty(navigator, "sendBeacon", {
    configurable: true,
    value: beacon,
  });
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
});
afterEach(() => {
  for (const dispose of cleanup.splice(0)) dispose();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function fixture() {
  const video = document.createElement("video");
  const frames = vi.fn();
  const failure = vi.fn();
  const controller = createScrubStream(
    video,
    "example",
    source,
    frames,
    failure,
  );
  cleanup.push(controller.dispose);
  return { video, frames, failure, ...controller };
}

it("requires a library HLS endpoint and preserves authorization without inheriting the main session or clip bounds", () => {
  const url = new URL(
    scrubStreamURL([
      {
        url: "https://stash.test/base/scene/uuid/stream.hevc.master.m3u8?apikey=test-key&start=40&end=60&stream_session=main&resolution=ORIGINAL",
      },
    ])!,
  );
  expect(url.pathname).toBe("/base/scene/uuid/stream.scrub.master.m3u8");
  expect([...url.searchParams]).toEqual([
    ["apikey", "test-key"],
    ["resolution", "LOW"],
  ]);
  expect(
    scrubStreamURL([
      { url: "blob:https://stash.test/offline" },
      { url: "https://stash.test/share/token/media/scene/stream.master.m3u8" },
    ]),
  ).toBeNull();
});

it("does no work on a quick gesture and throttles unbuffered positions under an independent owner", () => {
  const f = fixture();
  f.preview(10);
  vi.advanceTimersByTime(100);
  f.preview(null);
  vi.advanceTimersByTime(200);
  expect(instances).toHaveLength(0);
  f.preview(20);
  vi.advanceTimersByTime(100);
  f.preview(40);
  vi.advanceTimersByTime(180);
  expect(instances).toHaveLength(1);
  const url = new URL(instances[0]!.source);
  expect(url.searchParams.get("start")).toBe("40");
  expect(url.searchParams.get("stream_session")).toMatch(/^[a-f\d-]{36}$/);
  instances[0]!.events.get("manifest")?.();
  expect(instances[0]!.startLoad).toHaveBeenLastCalledWith(40);
  f.preview(null);
  expect(instances[0]!.stopLoad).toHaveBeenCalled();
  f.dispose();
  const stopped = new URL(beacon.mock.calls.at(-1)![0] as string);
  expect(stopped.searchParams.get("stream_session")).toBe(
    url.searchParams.get("stream_session"),
  );
  expect(stopped.searchParams.get("release")).toBe("1");
});

it("reuses buffered frames without another download, then releases idle media and owns a fresh session on reuse", () => {
  const f = fixture();
  f.preview(40);
  vi.advanceTimersByTime(180);
  Object.defineProperties(f.video, {
    readyState: { get: () => 4 },
    buffered: { get: () => ({ length: 1, start: () => 38, end: () => 50 }) },
  });
  f.video.dispatchEvent(new Event("loadedmetadata"));
  f.preview(40.2);
  f.video.dispatchEvent(new Event("seeked"));
  expect(f.video.currentTime).toBe(40.2);
  expect(f.frames).toHaveBeenLastCalledWith(true, 40.2);
  expect(instances).toHaveLength(1);
  expect(instances[0]!.startLoad).not.toHaveBeenCalled();
  f.preview(null);
  vi.advanceTimersByTime(20_000);
  f.preview(41);
  expect(instances).toHaveLength(1);
  f.preview(null);
  vi.advanceTimersByTime(30_000);
  expect(instances[0]!.destroy).toHaveBeenCalledOnce();
  expect(f.video.hasAttribute("src")).toBe(false);
  f.preview(60);
  vi.advanceTimersByTime(180);
  expect(instances).toHaveLength(2);
  expect(instances[1]!.source).not.toBe(instances[0]!.source);
});

it("starts requests during continuous motion without waiting for a quiet pointer", () => {
  const f = fixture();
  for (const target of [10, 20, 30, 40, 50, 60]) {
    f.preview(target);
    vi.advanceTimersByTime(40);
  }
  expect(instances).toHaveLength(1);
  expect(new URL(instances[0]!.source).searchParams.get("start")).toBe("30");
  expect(instances[0]!.startLoad).toHaveBeenLastCalledWith(60);
});

it("prepares silent frames without showing them and stops background loading after delivery", () => {
  const f = fixture();
  f.prepare(40);
  vi.advanceTimersByTime(0);
  Object.defineProperties(f.video, {
    readyState: { get: () => 4 },
    buffered: { get: () => ({ length: 1, start: () => 38, end: () => 50 }) },
  });
  f.video.currentTime = 40;
  f.video.dispatchEvent(new Event("loadedmetadata"));
  f.video.dispatchEvent(new Event("loadeddata"));
  expect(f.frames).toHaveBeenLastCalledWith(false, 40);
  expect(instances[0]!.stopLoad).toHaveBeenCalled();
  f.preview(40.5);
  f.video.dispatchEvent(new Event("seeked"));
  expect(f.frames).toHaveBeenLastCalledWith(true, 40.5);
  f.release();
  expect(instances[0]!.destroy).toHaveBeenCalledOnce();
});

it("leaves first-fragment positioning with hls.js until its append is complete", async () => {
  const f = fixture();
  f.prepare(0.16);
  vi.advanceTimersByTime(0);
  instances[0]!.hasEnoughToStart = false;
  Object.defineProperty(f.video, "readyState", { get: () => 3 });
  f.video.dispatchEvent(new Event("loadedmetadata"));
  f.video.dispatchEvent(new Event("loadeddata"));
  expect(f.video.currentTime).toBe(0);
  expect(f.frames).not.toHaveBeenCalled();
  expect(instances[0]!.stopLoad).not.toHaveBeenCalled();
  instances[0]!.hasEnoughToStart = true;
  f.video.currentTime = 0.16;
  instances[0]!.events.get("buffered")?.();
  await Promise.resolve();
  expect(f.frames).toHaveBeenLastCalledWith(false, 0.16);
  f.release();
  f.video.dispatchEvent(new Event("error"));
  expect(f.failure).not.toHaveBeenCalled();
});

it("recovers one decoder failure under the same lease and stops after a repeated failure", () => {
  const f = fixture();
  f.prepare(0.086);
  vi.advanceTimersByTime(0);
  const engine = instances[0]!;
  f.video.dispatchEvent(new Event("error"));
  engine.events.get("error")?.("error", { fatal: true, type: "mediaError" });
  f.preview(9.12);
  vi.advanceTimersByTime(100);
  expect(engine.recoverMediaError).toHaveBeenCalledOnce();
  expect(engine.startLoad).toHaveBeenLastCalledWith(9.12);
  expect(instances).toHaveLength(1);
  expect(beacon).not.toHaveBeenCalled();
  expect(f.failure).not.toHaveBeenCalled();
  // Failure before the replacement even reaches metadata must also terminate.
  f.video.dispatchEvent(new Event("error"));
  vi.advanceTimersByTime(200);
  expect(engine.recoverMediaError).toHaveBeenCalledOnce();
  expect(engine.destroy).toHaveBeenCalledOnce();
  expect(f.failure).toHaveBeenCalledOnce();
});

it("cancels decoder recovery when the preview is released", () => {
  const f = fixture();
  f.prepare(0.086);
  vi.advanceTimersByTime(0);
  f.video.dispatchEvent(new Event("error"));
  f.release();
  vi.advanceTimersByTime(200);
  expect(instances[0]!.recoverMediaError).not.toHaveBeenCalled();
  expect(instances[0]!.destroy).toHaveBeenCalledOnce();
  expect(f.failure).not.toHaveBeenCalled();
});

it("disposes failed downloads instead of looping requests, and cancels pending work on backgrounding", () => {
  const f = fixture();
  f.preview(50);
  vi.advanceTimersByTime(180);
  instances[0]!.events.get("error")?.("error", { fatal: true });
  expect(f.failure).toHaveBeenCalledOnce();
  f.preview(80);
  vi.advanceTimersByTime(500);
  expect(instances).toHaveLength(1);
  const next = fixture();
  next.preview(10);
  vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  document.dispatchEvent(new Event("visibilitychange"));
  vi.advanceTimersByTime(500);
  expect(instances).toHaveLength(1);
});
