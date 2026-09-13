// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { OfflineEntry } from "@/components/offline/offline-db";
import type { BackgroundFetchRegistration } from "./background-fetch-types";

const mocks = vi.hoisted(() => ({
  rows: new Map<string, OfflineEntry>(),
  listeners: new Set<() => void>(),
  write: vi.fn(),
  remove: vi.fn(),
  estimate: vi.fn(),
  fetch: vi.fn(),
}));
vi.mock("@/components/offline/offline-scope", () => ({
  getOfflineScope: async () => ({
    deploymentURL: "http://localhost/stash/",
    workerLock: "worker",
    sceneLock: (id: string) => `scene-${id}`,
  }),
}));
vi.mock("@/components/offline/offline-db", () => ({
  getEntry: async (id: string) => mocks.rows.get(id),
  listEntriesByStatus: async (status: string) =>
    [...mocks.rows.values()].filter((entry) => entry.status === status),
  patchEntry: async (
    id: string,
    patch:
      | Partial<OfflineEntry>
      | ((entry: OfflineEntry) => Partial<OfflineEntry>),
  ) => {
    const entry = mocks.rows.get(id);
    if (!entry) return;
    mocks.rows.set(id, {
      ...entry,
      ...(typeof patch === "function" ? patch(entry) : patch),
    });
    for (const listener of mocks.listeners) listener();
  },
  subscribeToEntries: (listener: () => void) => {
    mocks.listeners.add(listener);
    return () => {
      mocks.listeners.delete(listener);
    };
  },
}));
vi.mock("@/components/offline/opfs-storage", () => ({
  writeScene: mocks.write,
  removeScene: mocks.remove,
  storageEstimate: mocks.estimate,
}));
import { finishBackgroundDownload } from "./background-fetch-handler";
import { startBackgroundDownload } from "@/components/offline/background-downloads";

function entry(id: string): OfflineEntry {
  return {
    scene_id: id,
    request_id: "request",
    title: "Video",
    studio_name: null,
    studio_id: null,
    performers: [],
    tags: [],
    duration: 1,
    width: 10,
    height: 10,
    date: null,
    paths: { screenshot: null, preview: null, sprite: null, vtt: null },
    format: "copy",
    source_video_codec: "h264",
    source_audio_codec: "aac",
    resolution: "STANDARD",
    width_actual: 10,
    height_actual: 10,
    bytes: 0,
    downloaded_at: 0,
    status: "queued",
    opfs_path: `scenes/${id}.mp4`,
    server_status: "unknown",
  };
}
class Transfer extends EventTarget implements BackgroundFetchRegistration {
  id = "scene-1-request";
  downloaded = 5;
  downloadTotal = 5;
  result = "success" as const;
  failureReason = "";
  recordsAvailable = true;
  abort = vi.fn(async () => true);
  response = new Response("video", {
    headers: { "content-type": "video/mp4", "content-length": "5" },
  });
  matchAll = vi.fn(async () => [
    {
      request: new Request("http://localhost/stash/scene/1/download.mp4"),
      responseReady: Promise.resolve(this.response),
    },
  ]);
}
const manager = { fetch: mocks.fetch, get: async () => undefined };
beforeEach(() => {
  mocks.rows.clear();
  mocks.listeners.clear();
  vi.clearAllMocks();
  mocks.estimate.mockResolvedValue({});
  mocks.remove.mockResolvedValue(undefined);
  mocks.write.mockImplementation(
    async (
      _id: string,
      body: ReadableStream<Uint8Array>,
      signal: AbortSignal,
    ) => {
      signal.throwIfAborted();
      return (await new Response(body).arrayBuffer()).byteLength;
    },
  );
  mocks.fetch.mockResolvedValue(new Transfer());
  const locks = new Map<string, Promise<unknown>>();
  vi.stubGlobal("navigator", {
    locks: {
      request: <T>(name: string, action: () => Promise<T>) => {
        const next = (locks.get(name) ?? Promise.resolve()).then(action);
        locks.set(
          name,
          next.catch(() => {}),
        );
        return next;
      },
    },
  });
  mocks.rows.set("1", {
    ...entry("1"),
    status: "downloading",
    background_fetch_id: "scene-1-request",
  });
});
afterEach(() => vi.unstubAllGlobals());

it("streams completion to local storage and advances the queue without a page worker", async () => {
  mocks.rows.set("2", entry("2"));
  await finishBackgroundDownload(new Transfer(), { backgroundFetch: manager });
  expect(mocks.rows.get("1")).toMatchObject({
    status: "complete",
    bytes: 5,
    background_fetch_id: undefined,
  });
  expect(mocks.write).toHaveBeenCalledOnce();
  expect(mocks.rows.get("2")).toMatchObject({
    status: "downloading",
    background_fetch_id: "scene-2-request",
  });
  const requests: Request[] = mocks.fetch.mock.calls[0]?.[1];
  expect(requests[0]?.url).toContain(
    "http://localhost/stash/scene/2/download.mp4?",
  );
  expect(requests[0]?.credentials).toBe("include");
});
it("preserves a newer retry when an old completion arrives", async () => {
  mocks.rows.set("1", {
    ...entry("1"),
    status: "downloading",
    background_fetch_id: "scene-1-new",
  });
  await finishBackgroundDownload(new Transfer(), { backgroundFetch: manager });
  expect(mocks.write).not.toHaveBeenCalled();
  expect(mocks.remove).not.toHaveBeenCalled();
  expect(mocks.rows.get("1")?.background_fetch_id).toBe("scene-1-new");
});
it("rejects login HTML and quota exhaustion without publishing a complete file", async () => {
  const transfer = new Transfer();
  transfer.response = new Response("login", {
    headers: { "content-type": "text/html" },
  });
  await finishBackgroundDownload(transfer, {});
  expect(mocks.rows.get("1")?.status).toBe("error");
  expect(mocks.write).not.toHaveBeenCalled();
  mocks.rows.set("1", {
    ...entry("1"),
    status: "downloading",
    background_fetch_id: transfer.id,
  });
  mocks.estimate.mockResolvedValue({ usage: 94, quota: 100 });
  await finishBackgroundDownload(new Transfer(), {});
  expect(mocks.rows.get("1")?.error).toContain("Out of storage");
  expect(mocks.write).not.toHaveBeenCalled();
});
it("honours a durable cancellation even after network completion", async () => {
  mocks.rows.set("1", {
    ...entry("1"),
    status: "downloading",
    background_fetch_id: "scene-1-request",
    cancel_requested: true,
  });
  await finishBackgroundDownload(new Transfer(), {});
  expect(mocks.rows.get("1")).toMatchObject({
    status: "error",
    error: "Cancelled",
  });
  expect(mocks.write).not.toHaveBeenCalled();
});
it("returns refused browser transfers to the foreground queue", async () => {
  mocks.fetch.mockRejectedValueOnce(new Error("Permission denied"));
  const next = entry("2");
  mocks.rows.set("2", next);
  expect(await startBackgroundDownload(next, manager)).toBe(false);
  expect(mocks.rows.get("2")).toMatchObject({
    status: "queued",
    background_fetch_id: undefined,
  });
});

it("aborts a transfer created after cancellation during browser handoff", async () => {
  const pending = Promise.withResolvers<BackgroundFetchRegistration>();
  mocks.fetch.mockReturnValueOnce(pending.promise);
  const next = entry("2");
  mocks.rows.set("2", next);
  const abort = new AbortController();
  const started = startBackgroundDownload(next, manager, abort.signal);
  await vi.waitFor(() => expect(mocks.fetch).toHaveBeenCalledOnce());
  abort.abort();
  const transfer = new Transfer();
  pending.resolve(transfer);
  expect(await started).toBe(false);
  expect(transfer.abort).toHaveBeenCalledOnce();
  expect(mocks.rows.get("2")?.background_fetch_id).toBeUndefined();
});
