/**
 * IndexedDB wrapper for the offline-scenes metadata store.
 *
 * Pairs with `opfs-storage.ts` — OPFS holds the binary MP4 bytes,
 * IDB holds the metadata (snapshotted scene fields, download status,
 * resume position). Two stores instead of one because OPFS can't
 * carry rich metadata and IDB can't stream multi-GB files efficiently.
 *
 * Scene store `offline_scenes` keyed on `scene_id`. Indices on
 * `downloaded_at` (chronological list view ordering) and `status`
 * (resume queue scan on PWA reload). All access goes through this
 * module — UI code does not touch `indexedDB.open` directly. Each deployment
 * also keeps import receipts and migration settings in this database.
 */

import {
  canCoordinateOfflineStorage,
  getOfflineScope,
  LEGACY_OFFLINE_DATABASE,
} from "./offline-scope";
import { offlineEntrySchema } from "./offline-entry-schema";
import { legacyEntryDeployment } from "./offline-migration-policy";

const DB_VERSION = 1;
const STORE = "offline_scenes";
const IMPORTS = "imports";
const SETTINGS = "settings";

/** Snapshotted scene metadata + local download state. */
export interface OfflineEntry {
  /** Scene id (primary key). */
  scene_id: string;
  /** Optional for entries created before coordinated queue ownership. */
  request_id?: string;
  queued_at?: number;
  cancel_requested?: boolean;

  // ── Scene fields snapshotted at download time ──
  // Refreshed by the metadata-refresh pass on Offline-view mount when
  // the server is reachable. Cards render from these without a fetch
  // so the Offline view works fully offline.

  title: string;
  /** Long-form description. Optional because pre-existing entries
   *  written before this field was added carry no value; the metadata-
   *  refresh pass fills it in when the server is reachable. */
  details?: string | null;
  studio_name: string | null;
  studio_id: string | null;
  performers: { id: string; name: string }[];
  tags: { id: string; name: string }[];
  duration: number;
  width: number;
  height: number;
  date: string | null;
  paths: {
    screenshot: string | null;
    preview: string | null;
    sprite: string | null;
    vtt: string | null;
  };

  // ── Local-only playback state ──

  /** Last-played scene-time in seconds. Written on player unmount. */
  last_position_seconds?: number;

  // ── Download metadata ──

  format: "copy" | "copy-aac" | "hevc" | "h264" | "av1";
  source_video_codec: string;
  source_audio_codec: string;
  /** Snapshot of the source file's server-side path (e.g.
   *  "/library/scenes/My Scene.mp4"). Used as the title fallback for
   *  untitled scenes so the offline view shows the source filename
   *  rather than the scene id (whose stem is what `objectTitle`
   *  derives from `files[0].path` when `title` is empty). Optional —
   *  pre-existing entries downloaded before this field was added
   *  fall back to the OPFS path. */
  source_file_path?: string;
  /** `StreamingResolutionEnum` value the user requested at download time. */
  resolution: string;
  /** Post-scale dimensions of the file actually on disk. For copy
   *  modes these match the source; for transcode modes they reflect
   *  the resolution clamp. */
  width_actual: number;
  height_actual: number;
  /** Final file size in bytes once `status === "complete"`. Best-effort
   *  during `downloading` (we update from the response Content-Length
   *  if it arrives, else stays 0). */
  bytes: number;
  /** Unix ms. Set when status flips to `complete`. */
  downloaded_at: number;
  status: "queued" | "downloading" | "complete" | "error";
  /** Live during `downloading`. Cleared on transition to `complete`. */
  bytes_downloaded?: number;
  /** Surfaced to the user via the card; cleared on retry. */
  error?: string;
  /** Path under the OPFS root, including this deployment's directory. */
  opfs_path: string;

  // ── Server-state echo ──
  // Result of the most recent metadata refresh. "unknown" until the
  // first refresh after download; flips to "missing" if the scene was
  // deleted server-side (the local file remains playable but can't be
  // re-downloaded). Drives the "Removed from server" badge on cards.
  server_status: "present" | "missing" | "unknown";
}

/**
 * Human-friendly label for an offline entry. Used everywhere a
 * downloaded scene needs a name: download tray rows, save-to-Files
 * dialog, etc. Falls back through title → source filename stem →
 * scene id, so we never display a bare numeric id when the user
 * actually has a recognisable name available.
 */
export function entryDisplayTitle(entry: OfflineEntry): string {
  if (entry.title) return entry.title;
  if (entry.source_file_path) {
    const base = entry.source_file_path.replace(/^.*[\\/]/, "");
    const dot = base.lastIndexOf(".");
    return dot > 0 ? base.slice(0, dot) : base;
  }
  return entry.scene_id;
}

let dbPromise: Promise<IDBDatabase> | null = null;

async function openDB(): Promise<IDBDatabase> {
  const scope = await getOfflineScope();
  if (dbPromise) return dbPromise;
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(scope.databaseName, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) {
        const store = db.createObjectStore(STORE, { keyPath: "scene_id" });
        store.createIndex("by_downloaded_at", "downloaded_at");
        store.createIndex("by_status", "status");
      }
      if (!db.objectStoreNames.contains(IMPORTS))
        db.createObjectStore(IMPORTS, { keyPath: ["source", "sceneId"] });
      if (!db.objectStoreNames.contains(SETTINGS))
        db.createObjectStore(SETTINGS);
    };
    req.onsuccess = () => {
      const db = req.result;
      // If something else (another tab, dev-tools) requests an upgrade,
      // close this connection so the upgrade can proceed; UI code will
      // re-open on next call.
      db.onversionchange = () => {
        db.close();
        dbPromise = null;
      };
      resolve(db);
    };
    req.onerror = () => {
      dbPromise = null;
      reject(req.error);
    };
  });
  return dbPromise;
}

function txPromise<T = void>(
  storeMode: IDBTransactionMode,
  fn: (store: IDBObjectStore) => IDBRequest<T> | undefined,
): Promise<T> {
  return openDB().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const tx = db.transaction(STORE, storeMode);
        const store = tx.objectStore(STORE);
        let result: T | undefined;
        const req = fn(store);
        if (req) {
          req.onsuccess = () => {
            result = req.result;
          };
          req.onerror = () => reject(req.error);
        }
        tx.oncomplete = () => resolve(result as T);
        tx.onerror = () => reject(tx.error);
        tx.onabort = () =>
          reject(tx.error ?? new Error("Offline database transaction aborted"));
      }),
  );
}

export async function getEntry(
  sceneId: string,
): Promise<OfflineEntry | undefined> {
  return (await locateEntry(sceneId))?.entry;
}

/** Read-only legacy playback is possible when migration is unsupported. A
 * current row or import receipt takes precedence, including after deletion. */
export async function locateEntry(
  sceneId: string,
): Promise<{ kind: "scoped" | "legacy"; entry: OfflineEntry } | undefined> {
  const current = await txPromise<OfflineEntry | undefined>(
    "readonly",
    (store) => store.get(sceneId) as IDBRequest<OfflineEntry | undefined>,
  );
  if (current) return { kind: "scoped", entry: current };
  if (
    canCoordinateOfflineStorage() ||
    !(await automaticMigrationEnabled()) ||
    (await hasImportReceipt(LEGACY_OFFLINE_DATABASE, sceneId))
  )
    return undefined;
  const entry = await readOfflineSourceEntry(LEGACY_OFFLINE_DATABASE, sceneId);
  if (
    entry &&
    legacyEntryDeployment(entry) === (await getOfflineScope()).deploymentURL
  )
    return { kind: "legacy", entry };
  return undefined;
}

/**
 * All entries newest-first via the `by_downloaded_at` index. Cursor
 * walk rather than `getAll` because `getAll` on an index doesn't accept
 * a direction — we want descending so the list view's natural order is
 * "most recently downloaded at top."
 */
export async function listEntries(): Promise<OfflineEntry[]> {
  const db = await openDB();
  const entries = await new Promise<OfflineEntry[]>((resolve, reject) => {
    const tx = db.transaction(STORE, "readonly");
    const store = tx.objectStore(STORE);
    const idx = store.index("by_downloaded_at");
    const out: OfflineEntry[] = [];
    const cursorReq = idx.openCursor(null, "prev");
    cursorReq.onsuccess = () => {
      const cursor = cursorReq.result;
      if (cursor) {
        out.push(cursor.value as OfflineEntry);
        cursor.continue();
      }
    };
    cursorReq.onerror = () => reject(cursorReq.error);
    tx.oncomplete = () => resolve(out);
    tx.onerror = () => reject(tx.error);
    tx.onabort = () =>
      reject(tx.error ?? new Error("Offline database transaction aborted"));
  });
  if (!canCoordinateOfflineStorage() && (await automaticMigrationEnabled())) {
    const currentIds = new Set(entries.map((entry) => entry.scene_id));
    const scope = await getOfflineScope();
    for (const entry of (await readOfflineSource(LEGACY_OFFLINE_DATABASE))
      .entries) {
      if (
        !currentIds.has(entry.scene_id) &&
        legacyEntryDeployment(entry) === scope.deploymentURL &&
        !(await hasImportReceipt(LEGACY_OFFLINE_DATABASE, entry.scene_id))
      )
        entries.push(entry);
    }
    entries.sort((a, b) => b.downloaded_at - a.downloaded_at);
  }
  return entries;
}

export async function listEntriesByStatus(
  status: OfflineEntry["status"],
): Promise<OfflineEntry[]> {
  const db = await openDB();
  return new Promise<OfflineEntry[]>((resolve, reject) => {
    const tx = db.transaction(STORE, "readonly");
    const store = tx.objectStore(STORE);
    const idx = store.index("by_status");
    const req = idx.getAll(status);
    req.onsuccess = () => resolve(req.result as OfflineEntry[]);
    req.onerror = () => reject(req.error);
  });
}

export async function putEntry(entry: OfflineEntry): Promise<void> {
  await txPromise("readwrite", (store) => store.put(entry));
  notifyChange();
}

/**
 * Patch a subset of fields on an existing entry. Used by the download
 * worker for status transitions and the player for resume-position
 * writes — both want a non-destructive update that doesn't clobber the
 * scene snapshot or other in-flight changes from a parallel write.
 */
export async function patchEntry(
  sceneId: string,
  patch:
    | Partial<OfflineEntry>
    | ((current: OfflineEntry) => Partial<OfflineEntry>),
): Promise<OfflineEntry | undefined> {
  const db = await openDB();
  const result = await new Promise<OfflineEntry | undefined>(
    (resolve, reject) => {
      const tx = db.transaction(STORE, "readwrite");
      const store = tx.objectStore(STORE);
      const getReq = store.get(sceneId);
      let merged: OfflineEntry | undefined;
      getReq.onsuccess = () => {
        const existing = getReq.result as OfflineEntry | undefined;
        if (!existing) {
          // Patch on missing row is a no-op (entry deleted under us).
          return;
        }
        merged = {
          ...existing,
          ...(typeof patch === "function" ? patch(existing) : patch),
        };
        const putReq = store.put(merged);
        putReq.onerror = () => reject(putReq.error);
      };
      getReq.onerror = () => reject(getReq.error);
      tx.oncomplete = () => resolve(merged);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () =>
        reject(tx.error ?? new Error("Offline database transaction aborted"));
    },
  );
  if (result) notifyChange();
  return result;
}

export async function deleteEntry(sceneId: string): Promise<void> {
  await txPromise("readwrite", (store) => store.delete(sceneId));
  notifyChange();
}

export async function clearAll(): Promise<void> {
  const db = await openDB();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction([STORE, SETTINGS], "readwrite");
    tx.objectStore(STORE).clear();
    // Retained legacy backups must not silently repopulate a cleared library.
    tx.objectStore(SETTINGS).put(true, "automaticMigrationDisabled");
    tx.oncomplete = () => resolve();
    tx.onabort = tx.onerror = () => reject(tx.error);
  });
  notifyChange();
}

export async function automaticMigrationEnabled(): Promise<boolean> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const req = db
      .transaction(SETTINGS)
      .objectStore(SETTINGS)
      .get("automaticMigrationDisabled");
    req.onsuccess = () => resolve(req.result !== true);
    req.onerror = () => reject(req.error);
  });
}

export async function hasImportReceipt(
  source: string,
  sceneId: string,
): Promise<boolean> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const req = db
      .transaction(IMPORTS)
      .objectStore(IMPORTS)
      .count([source, sceneId]);
    req.onsuccess = () => resolve(req.result > 0);
    req.onerror = () => reject(req.error);
  });
}

/** The caller holds the destination scene lock until the file and this
 * transaction settle. Receipt and metadata commit together; current rows win. */
export async function commitImportedEntry(
  source: string,
  entry: OfflineEntry,
): Promise<void> {
  const db = await openDB();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction([STORE, IMPORTS], "readwrite");
    const store = tx.objectStore(STORE);
    const req = store.getKey(entry.scene_id);
    req.onsuccess = () => {
      if (req.result === undefined) store.add(entry);
      tx.objectStore(IMPORTS).put({ source, sceneId: entry.scene_id });
    };
    tx.oncomplete = () => resolve();
    tx.onabort = tx.onerror = () => reject(tx.error);
  });
  notifyChange();
}

/** Opens an existing source read-only. Aborting first creation avoids leaving
 * phantom databases when a source was removed after database enumeration. */
async function openExistingSourceDB(
  name: string,
): Promise<IDBDatabase | undefined> {
  return new Promise<IDBDatabase | undefined>((resolve, reject) => {
    const req = indexedDB.open(name);
    let missing = false;
    req.onupgradeneeded = () => {
      missing = true;
      req.transaction?.abort();
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => (missing ? resolve(undefined) : reject(req.error));
  });
}

export async function readOfflineSource(
  name: string,
): Promise<{ entries: OfflineEntry[]; invalid: number }> {
  const db = await openExistingSourceDB(name);
  if (!db) return { entries: [], invalid: 0 };
  try {
    if (!db.objectStoreNames.contains(STORE))
      return { entries: [], invalid: 0 };
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, "readonly");
      const req: IDBRequest<unknown[]> = tx.objectStore(STORE).getAll();
      const entries: OfflineEntry[] = [];
      let invalid = 0;
      req.onsuccess = () => {
        for (const value of req.result) {
          const parsed = offlineEntrySchema.safeParse(value);
          if (parsed.success) entries.push(parsed.data);
          else invalid++;
        }
      };
      tx.oncomplete = () => resolve({ entries, invalid });
      tx.onabort = tx.onerror = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}

export async function readOfflineSourceEntry(
  name: string,
  sceneId: string,
): Promise<OfflineEntry | undefined> {
  const db = await openExistingSourceDB(name);
  if (!db) return undefined;
  try {
    if (!db.objectStoreNames.contains(STORE)) return undefined;
    return await new Promise((resolve, reject) => {
      const req: IDBRequest<unknown> = db
        .transaction(STORE)
        .objectStore(STORE)
        .get(sceneId);
      req.onsuccess = () => {
        const parsed = offlineEntrySchema.safeParse(req.result);
        resolve(parsed.success ? parsed.data : undefined);
      };
      req.onerror = () => reject(req.error);
    });
  } finally {
    db.close();
  }
}

// ── Change notifications ─────────────────────────────────────────────────────
//
// IndexedDB has no native cross-tab / cross-component event API, so we
// run a tiny in-process pub-sub: every mutating function (`putEntry`,
// `patchEntry`, `deleteEntry`, `clearAll`) calls `notifyChange()` after
// the transaction commits, and `useOfflineEntries` subscribes to those
// notifications so the list view re-reads IDB without waiting for the
// next render-triggering event.
//
const dataListeners = new Set<() => void>();

let channel: BroadcastChannel | undefined;
async function getChannel() {
  const scope = await getOfflineScope();
  if (!channel && typeof BroadcastChannel !== "undefined") {
    channel = new BroadcastChannel(scope.channelName);
    channel.onmessage = () => notifyLocalListeners();
  }
  return channel;
}

function notifyChange(): void {
  notifyLocalListeners();
  void getChannel()
    .then((channel) => channel?.postMessage("changed"))
    .catch(reportChannelError);
}

function notifyLocalListeners(): void {
  dataListeners.forEach((l) => {
    try {
      l();
    } catch (err) {
      // A buggy listener shouldn't break the chain for everyone else.
      console.error("[offline-db] listener threw:", err);
    }
  });
}

export function subscribeToEntries(listener: () => void): () => void {
  void getChannel().catch(reportChannelError);
  dataListeners.add(listener);
  return () => {
    dataListeners.delete(listener);
  };
}

function reportChannelError(error: unknown) {
  console.error("[offline-db] change notification failed:", error);
}
