// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { OfflineEntry } from "./offline-db";
import { FileMissingError, saveToFiles } from "./save-to-files";

const { readScene } = vi.hoisted(() => ({ readScene: vi.fn() }));
vi.mock("./opfs-storage", () => ({ readScene }));

const entry: OfflineEntry = {
  scene_id: "1",
  title: "A / video",
  details: null,
  studio_name: null,
  studio_id: null,
  performers: [],
  tags: [],
  duration: 1,
  width: 10,
  height: 10,
  date: null,
  paths: { screenshot: null, preview: null, sprite: null, vtt: null },
  source_video_codec: "h264",
  source_audio_codec: "aac",
  format: "copy",
  resolution: "STANDARD",
  width_actual: 10,
  height_actual: 10,
  bytes: 5,
  downloaded_at: 1,
  status: "complete",
  opfs_path: "scenes/1.mp4",
  server_status: "unknown",
};

function videoFile() {
  return Object.assign(new File(["video"], "1.mp4", { type: "video/mp4" }), {
    stream: () =>
      new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new TextEncoder().encode("video"));
          controller.close();
        },
      }),
  });
}

beforeEach(() => {
  readScene.mockReset();
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  delete window.showSaveFilePicker;
});

it("opens the native picker before storage lookup and streams to the selected file", async () => {
  const chunks: Uint8Array[] = [];
  const close = vi.fn();
  const destination = {
    createWritable: vi.fn(
      async () =>
        new WritableStream<Uint8Array>({
          write(chunk) {
            chunks.push(chunk);
          },
          close,
        }),
    ),
  };
  window.showSaveFilePicker = vi.fn(async function (this: Window) {
    expect(this).toBe(window);
    expect(readScene).not.toHaveBeenCalled();
    return destination;
  });
  readScene.mockResolvedValue(videoFile());
  const pending = saveToFiles(entry);
  expect(window.showSaveFilePicker).toHaveBeenCalledWith(
    expect.objectContaining({
      suggestedName: "A video.mp4",
    }),
  );
  await pending;
  expect(new TextDecoder().decode(chunks[0])).toBe("video");
  expect(close).toHaveBeenCalledOnce();
});

it("treats cancelling the native picker as a normal exit without reading or writing", async () => {
  window.showSaveFilePicker = vi
    .fn()
    .mockRejectedValue(new DOMException("Cancelled", "AbortError"));
  await expect(saveToFiles(entry)).resolves.toBeUndefined();
  expect(readScene).not.toHaveBeenCalled();
});

it("reports missing source files before opening a writable destination", async () => {
  const createWritable = vi.fn();
  window.showSaveFilePicker = vi.fn().mockResolvedValue({ createWritable });
  readScene.mockResolvedValue(null);
  await expect(saveToFiles(entry)).rejects.toBeInstanceOf(FileMissingError);
  expect(createWritable).not.toHaveBeenCalled();
});

it("keeps the fallback URL alive beyond the click task and releases it later", async () => {
  vi.useFakeTimers();
  const createObjectURL = vi.fn(() => "blob:offline-export");
  const revokeObjectURL = vi.fn();
  vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.download).toBe("A video.mp4");
      expect(this.href).toBe("blob:offline-export");
      expect(this.isConnected).toBe(true);
    });
  readScene.mockResolvedValue(videoFile());
  await saveToFiles(entry);
  expect(click).toHaveBeenCalledOnce();
  expect(document.querySelector("a[download]")).toBeNull();
  vi.advanceTimersByTime(1_000);
  expect(revokeObjectURL).not.toHaveBeenCalled();
  vi.runAllTimers();
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:offline-export");
});
