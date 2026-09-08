// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { StreamingResolutionEnum } from "@/core/generated-graphql";
import type { OfflineEntry } from "./offline-db";
const mocks = vi.hoisted(() => ({
  rows: new Map<string, OfflineEntry>(),
  listeners: new Set<() => void>(),
  storage: vi.fn(),
  write: vi.fn(),
  remove: vi.fn(),
  fetch: vi.fn(),
  size: vi.fn(),
}));
vi.mock("./offline-migration", () => ({
  migrateLegacyDownloads: async () => {},
}));
vi.mock("./offline-scope", () => ({
  canCoordinateOfflineStorage: () =>
    !!navigator.locks && typeof BroadcastChannel !== "undefined",
  getOfflineScope: async () => ({
    deploymentURL: "http://localhost/stash/",
    workerLock: "test-offline-worker",
    sceneLock: (id: string) => `test-offline-scene:${id}`,
  }),
}));
vi.mock("./offline-db", () => {
  const changed = () => {
    for (const listener of mocks.listeners) listener();
  };
  return {
    clearAll: async () => {
      mocks.rows.clear();
      changed();
    },
    getEntry: async (id: string) => mocks.rows.get(id),
    listEntries: async () => [...mocks.rows.values()],
    listEntriesByStatus: async (status: string) =>
      [...mocks.rows.values()].filter((entry) => entry.status === status),
    putEntry: async (entry: OfflineEntry) => {
      mocks.rows.set(entry.scene_id, entry);
      changed();
    },
    deleteEntry: async (id: string) => {
      mocks.rows.delete(id);
      changed();
    },
    patchEntry: async (
      id: string,
      patch:
        | Partial<OfflineEntry>
        | ((entry: OfflineEntry) => Partial<OfflineEntry>),
    ) => {
      const entry = mocks.rows.get(id);
      if (!entry) return;
      const next = {
        ...entry,
        ...(typeof patch === "function" ? patch(entry) : patch),
      };
      mocks.rows.set(id, next);
      changed();
      return next;
    },
    subscribeToEntries: (listener: () => void) => {
      mocks.listeners.add(listener);
      return () => {
        mocks.listeners.delete(listener);
      };
    },
  };
});
vi.mock("./opfs-storage", () => ({
  clearAllScenes: mocks.remove,
  existingSceneSize: mocks.size,
  opfsPathForScene: (id: string) => `scenes/${id}.mp4`,
  storageEstimate: mocks.storage,
  writeScene: mocks.write,
  removeScene: mocks.remove,
}));
import {
  DownloadQueueStore,
  OfflineQueueUnavailableError,
  type EnqueueArgs,
} from "./use-download-queue";

const args: EnqueueArgs = {
  snapshot: {
    scene_id: "1",
    title: "Scene",
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
  },
  mode: "copy",
  resolution: StreamingResolutionEnum.Standard,
};

beforeEach(() => {
  mocks.rows.clear();
  mocks.listeners.clear();
  vi.clearAllMocks();
  mocks.storage.mockResolvedValue({});
  mocks.size.mockResolvedValue(0);
  mocks.remove.mockResolvedValue(undefined);
  mocks.write.mockResolvedValue(10);
  mocks.fetch.mockImplementation(async () => new Response("test"));
  vi.stubGlobal("fetch", mocks.fetch);
  vi.stubGlobal("BroadcastChannel", class {});
  const locks = new Map<string, Promise<unknown>>();
  vi.stubGlobal("navigator", {
    locks: {
      request: <T>(name: string, action: () => Promise<T>) => {
        const next = (locks.get(name) ?? Promise.resolve())
          .catch(() => undefined)
          .then(action);
        locks.set(name, next);
        return next;
      },
    },
  });
});
afterEach(() => vi.unstubAllGlobals());

it("cancels during initialization before fetching and releases the scene lock", async () => {
  let release: (value: object) => void = () => {};
  mocks.storage.mockImplementation(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  const queue = new DownloadQueueStore();
  await queue.enqueue(args);
  await vi.waitFor(() => expect(mocks.storage).toHaveBeenCalledOnce());
  const cancelled = queue.cancel("1");
  await vi.waitFor(() =>
    expect(mocks.rows.get("1")?.cancel_requested).toBe(true),
  );
  release({});
  await cancelled;
  expect(mocks.fetch).not.toHaveBeenCalled();
  expect(queue.getSnapshot().active).toBeNull();
  expect(mocks.rows.get("1")?.status).toBe("error");
});

it("a second page cannot recover a live owner's download or enqueue it twice", async () => {
  let release: (value: Response) => void = () => {};
  mocks.fetch.mockImplementation(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  const first = new DownloadQueueStore();
  await Promise.all([first.enqueue(args), first.enqueue(args)]);
  await vi.waitFor(() => expect(mocks.fetch).toHaveBeenCalledOnce());
  const second = new DownloadQueueStore();
  await second.init();
  await second.enqueue(args);
  expect(mocks.rows.get("1")?.status).toBe("downloading");
  expect(second.getSnapshot().active?.sceneId).toBe("1");
  release(new Response("test"));
  await vi.waitFor(() => expect(mocks.rows.get("1")?.status).toBe("complete"));
  expect(mocks.fetch).toHaveBeenCalledOnce();
  await vi.waitFor(() => expect(second.getSnapshot().active).toBeNull());
});

it("remote removal waits for the aborted writer to finish before deleting", async () => {
  let release: () => void = () => {};
  let signal: AbortSignal | undefined;
  mocks.write.mockImplementation(
    (_id: string, _body: unknown, incoming: AbortSignal) => {
      signal = incoming;
      return new Promise((_resolve, reject) => {
        release = () => reject(new DOMException("Cancelled", "AbortError"));
      });
    },
  );
  const first = new DownloadQueueStore();
  const second = new DownloadQueueStore();
  await first.enqueue(args);
  await vi.waitFor(() => expect(mocks.write).toHaveBeenCalledOnce());
  mocks.remove.mockClear();
  const removed = second.remove("1");
  await vi.waitFor(() => expect(signal?.aborted).toBe(true));
  expect(mocks.remove).not.toHaveBeenCalled();
  release();
  await removed;
  expect(mocks.rows.has("1")).toBe(false);
});

it("rejects unsafe mutation when coordination is unavailable without clearing downloads", async () => {
  vi.stubGlobal("navigator", {});
  const queue = new DownloadQueueStore();
  await expect(queue.enqueue(args)).rejects.toBeInstanceOf(
    OfflineQueueUnavailableError,
  );
  expect(mocks.remove).not.toHaveBeenCalled();
  expect(mocks.fetch).not.toHaveBeenCalled();
});

it("recovers an orphan only after acquiring ownership and rejects mismatched ranges", async () => {
  const queue = new DownloadQueueStore();
  mocks.size.mockResolvedValue(20);
  mocks.fetch.mockResolvedValue(
    new Response("test", {
      status: 206,
      headers: { "content-range": "bytes 10-29/30" },
    }),
  );
  await queue.enqueue(args);
  await vi.waitFor(() => expect(mocks.rows.get("1")?.status).toBe("error"));
  expect(mocks.fetch).toHaveBeenCalledOnce();
  expect(mocks.rows.get("1")?.error).toContain("inconsistent download range");
  expect(mocks.write).not.toHaveBeenCalled();
  mocks.rows.set("1", {
    ...mocks.rows.get("1"),
    ...args.snapshot,
    status: "downloading",
    format: "copy",
    resolution: "STANDARD",
    width_actual: 10,
    height_actual: 10,
    bytes: 0,
    downloaded_at: 0,
    opfs_path: "scenes/1.mp4",
    server_status: "unknown",
  });
  const recovery = new DownloadQueueStore();
  await recovery.init();
  await vi.waitFor(() =>
    expect(mocks.rows.get("1")?.error).toBe("Interrupted by reload"),
  );
});

it("reports storage exhaustion without fetching and can retry after space is freed", async () => {
  mocks.storage.mockResolvedValue({ usage: 99, quota: 100 });
  const queue = new DownloadQueueStore();
  await queue.enqueue(args);
  await vi.waitFor(() => expect(mocks.rows.get("1")?.status).toBe("error"));
  expect(mocks.rows.get("1")?.error).toContain("Out of storage");
  expect(mocks.fetch).not.toHaveBeenCalled();
  mocks.storage.mockResolvedValue({ usage: 1, quota: 100 });
  await Promise.all([queue.retry("1"), queue.retry("1")]);
  await vi.waitFor(() => expect(mocks.rows.get("1")?.status).toBe("complete"));
  expect(mocks.fetch).toHaveBeenCalledOnce();
});

it.each([
  200, 206,
])("uses the correct offset when a resume receives HTTP %s", async (status) => {
  mocks.size.mockResolvedValue(20);
  mocks.fetch.mockResolvedValue(
    new Response("bytes", {
      status,
      headers: status === 206 ? { "content-range": "bytes 20-24/25" } : {},
    }),
  );
  const queue = new DownloadQueueStore();
  await queue.enqueue(args);
  await vi.waitFor(() => expect(mocks.rows.get("1")?.status).toBe("complete"));
  expect(mocks.fetch.mock.calls[0]?.[1].headers).toEqual({
    Range: "bytes=20-",
  });
  expect(mocks.write.mock.calls[0]?.[4]).toBe(status === 206 ? 20 : 0);
});

it("cancels a queued item while another scene owns the writer", async () => {
  let release: (response: Response) => void = () => {};
  mocks.fetch.mockImplementation(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  const queue = new DownloadQueueStore();
  await queue.enqueue(args);
  await vi.waitFor(() => expect(mocks.fetch).toHaveBeenCalledOnce());
  await queue.enqueue({
    ...args,
    snapshot: { ...args.snapshot, scene_id: "2" },
  });
  await queue.cancel("2");
  expect(mocks.rows.has("2")).toBe(false);
  release(new Response("test"));
  await vi.waitFor(() => expect(mocks.rows.get("1")?.status).toBe("complete"));
  expect(mocks.fetch).toHaveBeenCalledOnce();
});

it("clear-all waits for a live writer and prevents recovery from recreating removed rows", async () => {
  let release: () => void = () => {};
  let signal: AbortSignal | undefined;
  mocks.write.mockImplementation(
    (_id: string, _body: unknown, incoming: AbortSignal) => {
      signal = incoming;
      return new Promise((_resolve, reject) => {
        release = () => reject(new DOMException("Cancelled", "AbortError"));
      });
    },
  );
  const first = new DownloadQueueStore(),
    second = new DownloadQueueStore();
  await first.enqueue(args);
  await vi.waitFor(() => expect(mocks.write).toHaveBeenCalledOnce());
  mocks.remove.mockClear();
  const cleared = second.removeAll();
  await vi.waitFor(() => expect(signal?.aborted).toBe(true));
  expect(mocks.remove).not.toHaveBeenCalled();
  release();
  await cleared;
  expect(mocks.rows.size).toBe(0);
  await vi.waitFor(() => expect(first.getSnapshot().active).toBeNull());
});
