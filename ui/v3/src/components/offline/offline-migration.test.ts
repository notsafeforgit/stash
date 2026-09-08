import { IDBFactory } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { OfflineEntry } from "./offline-db";
import type { OfflineScope } from "./offline-scope";

const mocks = vi.hoisted(() => ({
  base: "http://localhost/a/",
  target: "",
  files: new Map<string, File>(),
  held: new Set<string>(),
  write: vi.fn(),
  remove: vi.fn(),
}));
vi.mock("@/core/platform-url", () => ({
  getPlatformURL: () => new URL(mocks.base),
}));
vi.mock("./opfs-storage", () => ({
  opfsPathForScene: async (id: string) => `namespaced/scenes/${id}.mp4`,
  readSceneFromScope: async (id: string, scope: OfflineScope | null) =>
    mocks.files.get(`${scope?.databaseName ?? "stash-offline"}:${id}`) ?? null,
  writeScene: mocks.write,
  removeScene: mocks.remove,
}));

function entry(id = "1", base = "http://localhost/a/"): OfflineEntry {
  return {
    scene_id: id,
    title: `Saved ${id}`,
    studio_name: null,
    studio_id: null,
    performers: [],
    tags: [],
    duration: 1,
    width: 16,
    height: 9,
    date: null,
    paths: {
      screenshot: `${base}scene/${id}/screenshot?t=1`,
      preview: null,
      sprite: null,
      vtt: null,
    },
    last_position_seconds: 42,
    format: "copy",
    source_video_codec: "h264",
    source_audio_codec: "aac",
    source_file_path: "/library/original.mp4",
    resolution: "STANDARD",
    width_actual: 16,
    height_actual: 9,
    bytes: 4,
    downloaded_at: 10,
    status: "complete",
    opfs_path: `scenes/${id}.mp4`,
    server_status: "present",
  };
}

async function seed(name: string, entries: unknown[]) {
  const db = await new Promise<IDBDatabase>((resolve, reject) => {
    const req = indexedDB.open(name, 1);
    req.onupgradeneeded = () => {
      const store = req.result.createObjectStore("offline_scenes", {
        keyPath: "scene_id",
      });
      store.createIndex("by_downloaded_at", "downloaded_at");
      store.createIndex("by_status", "status");
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction("offline_scenes", "readwrite");
      for (const value of entries) tx.objectStore("offline_scenes").put(value);
      tx.oncomplete = () => resolve();
      tx.onabort = tx.onerror = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}
function file(source: string, id: string, contents = "test") {
  mocks.files.set(
    `${source}:${id}`,
    new File([contents], `${id}.mp4`, { lastModified: 10 }),
  );
}
async function modules() {
  const scope = await import("./offline-scope");
  mocks.target = (await scope.getOfflineScope()).databaseName;
  return {
    scope,
    db: await import("./offline-db"),
    migration: await import("./offline-migration"),
    policy: await import("./offline-migration-policy"),
  };
}
beforeEach(() => {
  vi.resetModules();
  vi.resetAllMocks();
  mocks.base = "http://localhost/a/";
  mocks.files.clear();
  mocks.held.clear();
  vi.stubGlobal("indexedDB", new IDBFactory());
  vi.stubGlobal("window", { location: { origin: "http://localhost" } });
  vi.stubGlobal(
    "BroadcastChannel",
    class {
      postMessage() {}
    },
  );
  const pending = new Map<string, Promise<unknown>>();
  type Callback<T> = (
    lock: { name: string; mode: "exclusive" } | null,
  ) => Promise<T>;
  vi.stubGlobal("navigator", {
    locks: {
      request<T>(
        name: string,
        options: LockOptions | Callback<T>,
        callback?: Callback<T>,
      ) {
        const settings = typeof options === "function" ? {} : options;
        const action = typeof options === "function" ? options : callback;
        if (!action) throw new Error("Missing lock callback");
        if (settings.ifAvailable && mocks.held.has(name)) return action(null);
        const run = (pending.get(name) ?? Promise.resolve()).then(async () => {
          settings.signal?.throwIfAborted();
          mocks.held.add(name);
          try {
            return await action({ name, mode: "exclusive" });
          } finally {
            mocks.held.delete(name);
          }
        });
        pending.set(
          name,
          run.catch(() => undefined),
        );
        return run;
      },
    },
  });
  mocks.write.mockImplementation(
    async (
      id: string,
      body: ReadableStream<Uint8Array>,
      signal: AbortSignal,
    ) => {
      signal.throwIfAborted();
      const bytes = await new Response(body).arrayBuffer();
      signal.throwIfAborted();
      mocks.files.set(`${mocks.target}:${id}`, new File([bytes], `${id}.mp4`));
      return bytes.byteLength;
    },
  );
  mocks.remove.mockImplementation(async (id: string) => {
    mocks.files.delete(`${mocks.target}:${id}`);
  });
});
afterEach(() => vi.unstubAllGlobals());

it("uses one normalized deployment identity for databases, files, locks and channels", async () => {
  const { scope } = await modules();
  const a = await scope.createOfflineScope(
    "http://user:secret@LOCALHOST:80/a///?apikey=secret#route",
  );
  const same = await scope.createOfflineScope("http://localhost/a/");
  const b = await scope.createOfflineScope("http://localhost/b/");
  expect(a.deploymentURL).toBe(same.deploymentURL);
  for (const key of [
    "databaseName",
    "directory",
    "channelName",
    "workerLock",
  ] as const) {
    expect(a[key]).toBe(same[key]);
    expect(a[key]).not.toBe(b[key]);
    expect(a[key]).not.toContain("secret");
  }
  expect(a.sceneLock("1")).not.toBe(b.sceneLock("1"));
  expect(a.directory).toMatch(/^[a-f0-9]{64}$/);
  expect(scope.deploymentFromDatabase(a.databaseName)).toBe(a.deploymentURL);
  expect(scope.deploymentFromDatabase("unrelated")).toBeUndefined();
  expect(() => scope.sceneFilename("../1")).toThrow();
});

it("requires consistent recognized artwork URLs before assigning legacy ownership", async () => {
  const { policy } = await modules();
  expect(policy.legacyEntryDeployment(entry())).toBe("http://localhost/a/");
  expect(
    policy.legacyEntryDeployment({
      ...entry(),
      paths: {
        ...entry().paths,
        preview: "http://localhost/b/scene/1/preview",
      },
    }),
  ).toBeUndefined();
  expect(
    policy.legacyEntryDeployment({
      ...entry(),
      paths: { screenshot: null, preview: null, sprite: null, vtt: null },
    }),
  ).toBeUndefined();
  expect(
    policy.legacyEntryDeployment({
      ...entry(),
      paths: {
        ...entry().paths,
        screenshot: "http://localhost/a/scene/2/screenshot",
      },
    }),
  ).toBeUndefined();
});

it("migrates only proven legacy owners and leaves ambiguous, foreign and malformed entries intact", async () => {
  const { db, migration } = await modules();
  const ambiguous = {
    ...entry("3"),
    paths: { screenshot: null, preview: null, sprite: null, vtt: null },
  };
  await seed("stash-offline", [
    entry(),
    entry("2", "http://localhost/b/"),
    ambiguous,
    { scene_id: "4", status: "corrupt" },
  ]);
  for (const id of ["1", "2", "3"]) file("stash-offline", id);
  await migration.migrateLegacyDownloads();
  expect((await db.listEntries()).map((row) => row.scene_id)).toEqual(["1"]);
  expect(await db.getEntry("1")).toMatchObject({
    status: "complete",
    last_position_seconds: 42,
    source_file_path: "/library/original.mp4",
  });
  expect(await mocks.files.get(`${mocks.target}:1`)?.text()).toBe("test");
  expect(await mocks.files.get("stash-offline:1")?.text()).toBe("test");
  expect(await db.readOfflineSource("stash-offline")).toMatchObject({
    invalid: 1,
    entries: expect.arrayContaining([ambiguous]),
  });
  expect(migration.getMigrationProgress().error).toBeUndefined();
});

it("ignores unknown extension fields for ownership and retains them in the restored metadata", async () => {
  const { db, migration } = await modules();
  const saved = {
    ...entry(),
    extension: { enabled: true },
    performers: [{ id: "7", name: "Saved performer", extension: "value" }],
    paths: { ...entry().paths, extension: { enabled: true } },
  };
  await seed("stash-offline", [saved]);
  file("stash-offline", "1");
  await migration.migrateLegacyDownloads();
  expect(await db.getEntry("1")).toMatchObject({
    status: "complete",
    extension: saved.extension,
    performers: saved.performers,
    paths: saved.paths,
  });
  expect((await db.readOfflineSource("stash-offline")).entries).toEqual([
    saved,
  ]);
});

it("commits import receipts with metadata and does not resurrect deleted downloads", async () => {
  const { db, migration } = await modules();
  await seed("stash-offline", [entry()]);
  file("stash-offline", "1");
  await migration.migrateLegacyDownloads();
  expect(await db.hasImportReceipt("stash-offline", "1")).toBe(true);
  await db.deleteEntry("1");
  await mocks.remove("1");
  // A new page gets a new automatic-migration lifecycle and the same database.
  vi.resetModules();
  const next = await modules();
  await next.migration.migrateLegacyDownloads();
  expect(await next.db.listEntries()).toEqual([]);
  expect(mocks.write).toHaveBeenCalledOnce();
});

it("does not replace an existing namespaced download with a legacy ID collision", async () => {
  const { db, migration } = await modules();
  await db.putEntry({ ...entry(), title: "New download" });
  file(mocks.target, "1", "new!");
  await seed("stash-offline", [entry()]);
  file("stash-offline", "1");
  await migration.migrateLegacyDownloads();
  expect((await db.getEntry("1"))?.title).toBe("New download");
  expect(await mocks.files.get(`${mocks.target}:1`)?.text()).toBe("new!");
  expect(mocks.write).not.toHaveBeenCalled();
  expect(await db.hasImportReceipt("stash-offline", "1")).toBe(true);
});

it("preserves sources and permits retry after an interrupted or quota-failed copy", async () => {
  const { db, migration } = await modules();
  await seed("stash-offline", [entry()]);
  file("stash-offline", "1");
  mocks.write.mockImplementationOnce(async (id: string) => {
    file(mocks.target, id, "par");
    throw new DOMException("Storage full", "QuotaExceededError");
  });
  await migration.migrateLegacyDownloads();
  expect(await db.listEntries()).toEqual([]);
  expect(await db.hasImportReceipt("stash-offline", "1")).toBe(false);
  expect(mocks.files.has(`${mocks.target}:1`)).toBe(false);
  expect(await mocks.files.get("stash-offline:1")?.text()).toBe("test");
  const source = (await migration.listRecoverySources())[0];
  expect(source).toBeDefined();
  if (!source) throw new Error("Missing recovery source");
  await migration.restoreDownloads(
    source,
    new Set(["1"]),
    new AbortController().signal,
  );
  expect((await db.getEntry("1"))?.status).toBe("complete");
});

it("requires explicit recovery for another prefix and retains both libraries", async () => {
  const { db, migration, scope } = await modules();
  const old = await scope.createOfflineScope("http://localhost/previous/");
  const saved = entry("1", old.deploymentURL);
  saved.paths.preview = "http://localhost/previous/custom-preview";
  saved.paths.sprite = "https://cdn.example.test/scene/hash_sprite.jpg";
  saved.paths.vtt = "/previous/scene/hash_thumbs.vtt?t=1#thumbnail";
  await seed(old.databaseName, [saved]);
  file(old.databaseName, "1");
  await migration.migrateLegacyDownloads();
  expect(await db.listEntries()).toEqual([]);
  const source = (await migration.listRecoverySources()).find(
    (candidate) => candidate.databaseName === old.databaseName,
  );
  if (!source) throw new Error("Previous prefix was not discovered");
  await Promise.all([
    migration.restoreDownloads(
      source,
      new Set(["1"]),
      new AbortController().signal,
    ),
    migration.restoreDownloads(
      source,
      new Set(["1"]),
      new AbortController().signal,
    ),
  ]);
  expect(mocks.write).toHaveBeenCalledOnce();
  expect((await db.getEntry("1"))?.paths).toEqual({
    screenshot: "http://localhost/a/scene/1/screenshot?t=1",
    preview: saved.paths.preview,
    sprite: saved.paths.sprite,
    vtt: "http://localhost/a/scene/hash_thumbs.vtt?t=1#thumbnail",
  });
  await db.clearAll();
  expect(await db.automaticMigrationEnabled()).toBe(false);
  expect((await db.readOfflineSource(old.databaseName)).entries).toHaveLength(
    1,
  );
  expect(await mocks.files.get(`${old.databaseName}:1`)?.text()).toBe("test");
});

it("does not wait on a busy source or read another worker's file", async () => {
  const { db, migration } = await modules();
  await seed("stash-offline", [entry()]);
  file("stash-offline", "1");
  mocks.held.add("stash-offline-worker");
  await migration.migrateLegacyDownloads();
  expect(migration.getMigrationProgress().error).toBeInstanceOf(
    migration.OfflineSourceBusyError,
  );
  expect(mocks.write).not.toHaveBeenCalled();
  expect(await db.listEntries()).toEqual([]);
});

it("retains incomplete legacy work without automatically fetching from the current server", async () => {
  const { db, migration } = await modules();
  await seed("stash-offline", [
    { ...entry(), status: "queued" },
    { ...entry("2"), status: "downloading" },
  ]);
  await migration.migrateLegacyDownloads();
  expect((await db.getEntry("1"))?.status).toBe("error");
  expect(await db.getEntry("2")).toBeUndefined();
  expect(
    (await db.readOfflineSource("stash-offline")).entries.map(
      (row) => row.status,
    ),
  ).toEqual(["queued", "downloading"]);
});

it("does not leave a phantom database when legacy storage is absent", async () => {
  const { db } = await modules();
  expect(await db.readOfflineSource("stash-offline")).toEqual({
    entries: [],
    invalid: 0,
  });
  expect(await indexedDB.databases()).toEqual([]);
});

it("cancels a copy without publishing a receipt or keeping a partial destination", async () => {
  const { db, migration } = await modules();
  await seed("stash-offline", [entry()]);
  file("stash-offline", "1");
  const source = (await migration.listRecoverySources())[0];
  if (!source) throw new Error("Missing source");
  mocks.write.mockImplementationOnce(
    (id: string, _body: unknown, signal: AbortSignal) => {
      file(mocks.target, id, "par");
      return new Promise((_resolve, reject) => {
        signal.addEventListener(
          "abort",
          () => reject(new DOMException("Cancelled", "AbortError")),
          { once: true },
        );
      });
    },
  );
  const abort = new AbortController();
  const restore = migration.restoreDownloads(
    source,
    new Set(["1"]),
    abort.signal,
  );
  const rejected = expect(restore).rejects.toMatchObject({
    name: "AbortError",
  });
  await vi.waitFor(() => expect(mocks.write).toHaveBeenCalledOnce());
  abort.abort();
  await rejected;
  expect(await db.getEntry("1")).toBeUndefined();
  expect(await db.hasImportReceipt("stash-offline", "1")).toBe(false);
  expect(mocks.files.has(`${mocks.target}:1`)).toBe(false);
  expect(await mocks.files.get("stash-offline:1")?.text()).toBe("test");
  expect(migration.getMigrationProgress().running).toBe(false);
});

it("rejects a source changed by an older writer that does not participate in locks", async () => {
  const { db, migration } = await modules();
  await seed("stash-offline", [entry()]);
  file("stash-offline", "1");
  mocks.write.mockImplementationOnce(async (id: string) => {
    file(mocks.target, id);
    await seed("stash-offline", [{ ...entry(), downloaded_at: 11 }]);
    return 4;
  });
  await migration.migrateLegacyDownloads();
  expect(migration.getMigrationProgress().error?.message).toContain(
    "changed while restoring",
  );
  expect(await db.listEntries()).toEqual([]);
  expect(await db.hasImportReceipt("stash-offline", "1")).toBe(false);
  expect(mocks.files.has(`${mocks.target}:1`)).toBe(false);
  expect(
    (await db.readOfflineSource("stash-offline")).entries[0]?.downloaded_at,
  ).toBe(11);
});

it("keeps identifiable legacy downloads readable without coordination and never assigns foreign data", async () => {
  const { db } = await modules();
  await seed("stash-offline", [entry(), entry("2", "http://localhost/b/")]);
  vi.stubGlobal("navigator", {});
  expect(await db.locateEntry("1")).toMatchObject({
    kind: "legacy",
    entry: { last_position_seconds: 42 },
  });
  expect((await db.listEntries()).map((row) => row.scene_id)).toEqual(["1"]);
  expect(await db.getEntry("2")).toBeUndefined();
  // Resume updates cannot mutate the legacy source through the normal adapter.
  expect(
    await db.patchEntry("1", { last_position_seconds: 80 }),
  ).toBeUndefined();
  expect(
    (await db.readOfflineSource("stash-offline")).entries[0]
      ?.last_position_seconds,
  ).toBe(42);
  await db.commitImportedEntry("stash-offline", entry());
  await db.deleteEntry("1");
  expect(await db.locateEntry("1")).toBeUndefined();
  expect(await db.listEntries()).toEqual([]);
});

it("inspects a previous address locally when database enumeration is unavailable", async () => {
  const { migration, scope } = await modules();
  const previous = await scope.createOfflineScope("http://localhost/previous/");
  await seed(previous.databaseName, [entry("1", previous.deploymentURL)]);
  Object.defineProperty(indexedDB, "databases", { value: undefined });
  expect(
    (await migration.inspectRecoverySource(previous.deploymentURL)).entries,
  ).toHaveLength(1);
  expect(
    (await migration.inspectRecoverySource("http://localhost/missing/"))
      .entries,
  ).toEqual([]);
  await expect(migration.inspectRecoverySource(mocks.base)).rejects.toThrow(
    "into itself",
  );
});
