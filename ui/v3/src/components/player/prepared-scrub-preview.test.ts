// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createPreparedScrubPreview } from "./prepared-scrub-preview";

const { streams } = vi.hoisted(() => ({
  streams: [] as {
    frame: (visible: boolean, time?: number) => void;
    prepare: ReturnType<typeof vi.fn>;
    preview: ReturnType<typeof vi.fn>;
    release: ReturnType<typeof vi.fn>;
    dispose: ReturnType<typeof vi.fn>;
  }[],
}));
vi.mock("./scrub-stream", () => ({
  createScrubStream: (
    _video: HTMLVideoElement,
    _id: string,
    _source: string,
    frame: (visible: boolean, time?: number) => void,
  ) => {
    const stream = {
      frame,
      prepare: vi.fn(),
      preview: vi.fn(),
      release: vi.fn(),
      dispose: vi.fn(),
    };
    streams.push(stream);
    return stream;
  },
}));
const cleanup: (() => void)[] = [];
beforeEach(() => {
  vi.useFakeTimers();
  streams.length = 0;
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  vi.spyOn(HTMLMediaElement.prototype, "readyState", "get").mockReturnValue(4);
  vi.spyOn(HTMLVideoElement.prototype, "videoWidth", "get").mockReturnValue(
    160,
  );
  vi.spyOn(HTMLVideoElement.prototype, "videoHeight", "get").mockReturnValue(
    90,
  );
  vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
  vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(
    160,
  );
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
    drawImage: vi.fn(),
  } as unknown as CanvasRenderingContext2D);
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(
    (callback) => callback(new Blob(["jpeg"])),
  );
  let url = 0;
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = vi.fn(() => `blob:preview-${++url}`);
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
});
function fixture() {
  const root = document.createElement("div");
  root.dataset.scenePlayer = "";
  root.dataset.playbackReady = "true";
  const main = document.createElement("video");
  main.dataset.playbackVideo = "";
  let ahead = 12;
  Object.defineProperties(main, {
    paused: { get: () => false },
    buffered: { get: () => ({ length: 1, start: () => 0, end: () => ahead }) },
  });
  const surface = document.createElement("div");
  root.append(main, surface);
  document.body.append(root);
  const controller = createPreparedScrubPreview({
    surface,
    sceneId: "scene",
    source: "http://localhost/preview",
    start: 0,
    end: 120,
    offset: 0,
    onFailure: vi.fn(),
  });
  cleanup.push(controller.dispose);
  return {
    ...controller,
    main,
    surface,
    buffer: (seconds: number) => {
      ahead = seconds;
    },
  };
}

it("waits for stable playback headroom and immediately yields when that headroom disappears", () => {
  const f = fixture();
  f.buffer(2);
  vi.advanceTimersByTime(3000);
  expect(streams).toHaveLength(0);
  f.buffer(12);
  vi.advanceTimersByTime(2000);
  expect(streams).toHaveLength(1);
  expect(streams[0]!.prepare).toHaveBeenCalledWith(0);
  const mainTime = f.main.currentTime;
  f.buffer(2);
  vi.advanceTimersByTime(500);
  expect(streams[0]!.release).toHaveBeenCalled();
  expect(f.main.currentTime).toBe(mainTime);
});

it("shows a prepared frame immediately while the exact seek is still pending", () => {
  const f = fixture();
  vi.advanceTimersByTime(2000);
  const stream = streams[0]!;
  stream.frame(false, 0);
  vi.advanceTimersByTime(500);
  expect(stream.prepare).toHaveBeenLastCalledWith(60);
  stream.frame(false, 60);
  expect(f.surface.style.opacity).toBe("0");
  f.preview(61);
  expect(f.surface.dataset.previewKind).toBe("still");
  expect(f.surface.dataset.previewReady).toBe("true");
  expect(stream.preview).toHaveBeenLastCalledWith(61);
  stream.frame(true, 61);
  expect(f.surface.dataset.previewKind).toBe("video");
  f.preview(null);
  expect(f.surface.style.opacity).toBe("0");
  expect(f.main.currentTime).toBe(0);
});

it("recycles background sessions and clears all previews when hidden", () => {
  const f = fixture();
  vi.advanceTimersByTime(2000);
  const stream = streams[0]!;
  for (let sample = 0; sample < 8; sample++) {
    stream.frame(false, stream.prepare.mock.calls.at(-1)![0] as number);
    vi.advanceTimersByTime(500);
  }
  expect(stream.release).toHaveBeenCalled();
  expect(Number(f.surface.dataset.previewFrames)).toBe(8);
  vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  document.dispatchEvent(new Event("visibilitychange"));
  expect(f.surface.dataset.previewFrames).toBe("0");
  expect(URL.revokeObjectURL).toHaveBeenCalledTimes(8);
  const prepared = stream.prepare.mock.calls.length;
  vi.advanceTimersByTime(3000);
  expect(stream.prepare).toHaveBeenCalledTimes(prepared);
});

it("does not retain a late capture after the player is disposed", () => {
  let pending: BlobCallback | undefined;
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(
    (callback) => {
      pending = callback;
    },
  );
  const f = fixture();
  vi.advanceTimersByTime(2000);
  streams[0]!.frame(false, 0);
  f.dispose();
  pending?.(new Blob(["late"]));
  expect(URL.createObjectURL).not.toHaveBeenCalled();
});
