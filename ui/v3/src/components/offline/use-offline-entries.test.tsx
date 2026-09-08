// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { OfflineEntry } from "./offline-db";

const mocks = vi.hoisted(() => ({
  init: vi.fn(),
  read: vi.fn(),
  listeners: new Set<() => void>(),
}));
vi.mock("./use-download-queue", () => ({
  ensureDownloadQueueInit: mocks.init,
}));
vi.mock("./offline-db", () => ({
  getEntry: mocks.read,
  listEntries: vi.fn(),
  subscribeToEntries: (listener: () => void) => {
    mocks.listeners.add(listener);
    return () => mocks.listeners.delete(listener);
  },
}));
import { useOfflineEntry } from "./use-offline-entries";

const entry: OfflineEntry = {
  scene_id: "1",
  title: "Scene",
  studio_name: null,
  studio_id: null,
  performers: [],
  tags: [],
  duration: 1,
  width: 1,
  height: 1,
  date: null,
  paths: { screenshot: null, preview: null, sprite: null, vtt: null },
  format: "copy",
  source_video_codec: "h264",
  source_audio_codec: "aac",
  resolution: "STANDARD",
  width_actual: 1,
  height_actual: 1,
  bytes: 0,
  downloaded_at: 0,
  status: "complete",
  opfs_path: "scenes/1.mp4",
  server_status: "unknown",
};
function deferred<T>() {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
let root: Root;
let host: HTMLDivElement;
function Probe({ id }: { id: string }) {
  const { entry, loading, error, refresh } = useOfflineEntry(id);
  return (
    <div>
      <output>
        {JSON.stringify({
          id: entry?.scene_id,
          loading,
          error: error?.message,
        })}
      </output>
      <button type="button" onClick={refresh}>
        Retry
      </button>
    </div>
  );
}
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.resetAllMocks();
  mocks.listeners.clear();
  mocks.init.mockResolvedValue(undefined);
  host = document.createElement("div");
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  expect(mocks.listeners.size).toBe(0);
  vi.unstubAllGlobals();
});

it("ignores a stale read after a newer notification under Strict Mode", async () => {
  const stale = deferred<OfflineEntry | undefined>();
  mocks.read.mockReturnValue(stale.promise);
  await act(async () =>
    root.render(
      <StrictMode>
        <Probe id="1" />
      </StrictMode>,
    ),
  );
  expect(mocks.listeners.size).toBe(1);
  mocks.read.mockResolvedValue(undefined);
  await act(async () => {
    for (const listener of mocks.listeners) listener();
  });
  expect(host.querySelector("output")?.textContent).toBe('{"loading":false}');
  await act(async () => stale.resolve(entry));
  expect(host.querySelector("output")?.textContent).toBe('{"loading":false}');
});

it("hides the previous scene immediately and recovers from a failed read", async () => {
  mocks.read.mockResolvedValue(entry);
  await act(async () => root.render(<Probe id="1" />));
  expect(host.querySelector("output")?.textContent).toContain('"id":"1"');
  const pending = deferred<OfflineEntry | undefined>();
  mocks.read.mockReturnValue(pending.promise);
  await act(async () => root.render(<Probe id="2" />));
  expect(host.querySelector("output")?.textContent).toBe('{"loading":true}');
  mocks.read.mockRejectedValue(new Error("Storage unavailable"));
  await act(async () => {
    for (const listener of mocks.listeners) listener();
  });
  expect(host.querySelector("output")?.textContent).toBe(
    '{"loading":false,"error":"Storage unavailable"}',
  );
  await act(async () => pending.resolve(entry));
  expect(host.querySelector("output")?.textContent).not.toContain('"id":"1"');
  mocks.read.mockResolvedValue({ ...entry, scene_id: "2" });
  await act(async () => host.querySelector("button")?.click());
  expect(host.querySelector("output")?.textContent).toBe(
    '{"id":"2","loading":false}',
  );
});
