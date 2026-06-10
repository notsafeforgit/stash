/**
 * IndexedDB wrapper for the offline-scenes metadata store.
 *
 * Pairs with `opfs-storage.ts` — OPFS holds the binary MP4 bytes,
 * IDB holds the metadata (snapshotted scene fields, download status,
 * resume position). Two stores instead of one because OPFS can't
 * carry rich metadata and IDB can't stream multi-GB files efficiently.
 *
 * Single object store `offline_scenes` keyed on `scene_id`. Indices on
 * `downloaded_at` (chronological list view ordering) and `status`
 * (resume queue scan on PWA reload). All access goes through this
 * module — UI code does not touch `indexedDB.open` directly.
 */

const DB_NAME = "stash-offline";
const DB_VERSION = 1;
const STORE = "offline_scenes";

/** Snapshotted scene metadata + local download state. */
export interface OfflineEntry {
  /** Scene id (primary key). */
  scene_id: string;

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
  /** Path under the OPFS root (e.g. "scenes/308785.mp4"). */
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

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) {
        const store = db.createObjectStore(STORE, { keyPath: "scene_id" });
        store.createIndex("by_downloaded_at", "downloaded_at");
        store.createIndex("by_status", "status");
      }
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
    req.onerror = () => reject(req.error);
    req.onblocked = () => reject(new Error("IndexedDB upgrade blocked"));
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
        tx.onabort = () => reject(tx.error);
      }),
  );
}

export async function getEntry(
  sceneId: string,
): Promise<OfflineEntry | undefined> {
  return txPromise<OfflineEntry | undefined>(
    "readonly",
    (store) => store.get(sceneId) as IDBRequest<OfflineEntry | undefined>,
  );
}

/**
 * All entries newest-first via the `by_downloaded_at` index. Cursor
 * walk rather than `getAll` because `getAll` on an index doesn't accept
 * a direction — we want descending so the list view's natural order is
 * "most recently downloaded at top."
 */
export async function listEntries(): Promise<OfflineEntry[]> {
  const db = await openDB();
  return new Promise<OfflineEntry[]>((resolve, reject) => {
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
  });
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
  patch: Partial<OfflineEntry>,
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
        merged = { ...existing, ...patch };
        const putReq = store.put(merged);
        putReq.onerror = () => reject(putReq.error);
      };
      getReq.onerror = () => reject(getReq.error);
      tx.oncomplete = () => resolve(merged);
      tx.onerror = () => reject(tx.error);
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
  await txPromise("readwrite", (store) => store.clear());
  notifyChange();
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
// Cross-tab is not handled — if the user has Stash open in two tabs and
// deletes an entry in one, the other won't refresh until something else
// triggers a re-render. A `BroadcastChannel("stash-offline")` could
// close that gap, but the use case is rare enough to defer.

const dataListeners = new Set<() => void>();

function notifyChange(): void {
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
  dataListeners.add(listener);
  return () => {
    dataListeners.delete(listener);
  };
}
