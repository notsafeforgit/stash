/**
 * OPFS (Origin Private File System) storage for offline scene MP4s.
 *
 * Why OPFS not IndexedDB Blobs: OPFS exposes
 * `FileSystemWritableFileStream`, which lets `fetch().body.pipeTo(...)`
 * stream bytes straight to disk with browser-managed backpressure.
 * IndexedDB Blob writes accumulate in V8 heap until the transaction
 * commits, which crashes the tab on multi-GB downloads (especially on
 * iOS where the JS heap ceiling is small).
 *
 * Why a sub-directory `scenes/`: future-proofing for other entity
 * types (gallery archives, etc.) — they'd live under sibling dirs.
 *
 * Layout: `stash-offline/<deployment-hash>/scenes/<scene_id>.mp4`.
 * Legacy files remain under `scenes/`, accessible to migration and read-only
 * playback when browser storage coordination is unavailable.
 */

import {
  getOfflineScope,
  sceneFilename,
  type OfflineScope,
} from "./offline-scope";
import { locateEntry } from "./offline-db";

const SCENES_DIR = "scenes";

async function rootDir(): Promise<FileSystemDirectoryHandle> {
  if (!("storage" in navigator) || !navigator.storage.getDirectory) {
    throw new Error(
      "OPFS not available in this browser. Offline downloads require " +
        "Origin Private File System support (Safari 15.2+, Chrome 86+, " +
        "Firefox 111+).",
    );
  }
  return navigator.storage.getDirectory();
}

async function deploymentDir(scope: OfflineScope | null, create = false) {
  let root = await rootDir();
  if (scope) {
    root = await root.getDirectoryHandle("stash-offline", { create });
    root = await root.getDirectoryHandle(scope.directory, { create });
  }
  return root;
}

async function scenesDir(
  create = false,
  scope = getOfflineScope(),
): Promise<FileSystemDirectoryHandle> {
  const root = await deploymentDir(await scope, create);
  return root.getDirectoryHandle(SCENES_DIR, { create });
}

export async function opfsPathForScene(sceneId: string): Promise<string> {
  const scope = await getOfflineScope();
  return `stash-offline/${scope.directory}/${SCENES_DIR}/${sceneFilename(sceneId)}`;
}

/**
 * Pipe a `Response.body` into this deployment's `scenes/<sceneId>.mp4`,
 * reporting bytes written via `onProgress`. Returns the final byte
 * count once the stream closes cleanly.
 *
 * The download worker calls this with the response from
 * `fetch('/scene/{id}/download.mp4')` and an AbortSignal tied to the
 * worker's cancel button. Aborting partway through closes the
 * underlying file (any partial write is left on disk; the worker
 * deletes it via `removeScene` on the error path).
 *
 * Backpressure: `pipeTo` honours the writable's queuing strategy, so
 * fetch downloads only as fast as OPFS can persist. JS heap stays
 * flat across multi-GB writes.
 *
 * Resume support: pass `startOffset > 0` to keep the existing bytes
 * 0..startOffset and append from there. The caller (download queue)
 * uses this with a `Range: bytes=<offset>-` HTTP request to resume
 * an interrupted download — `bytes` returned in this case is the
 * absolute file size (existing + appended), not just appended.
 */
export async function writeScene(
  sceneId: string,
  body: ReadableStream<Uint8Array>,
  signal: AbortSignal,
  onProgress?: (bytes: number) => void,
  startOffset = 0,
): Promise<number> {
  signal.throwIfAborted();
  const dir = await scenesDir(true);
  signal.throwIfAborted();
  const handle = await dir.getFileHandle(sceneFilename(sceneId), {
    create: true,
  });
  signal.throwIfAborted();
  const writable = await handle.createWritable({
    keepExistingData: startOffset > 0,
  });

  let bytes = startOffset;
  const progressTransform = new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller) {
      bytes += chunk.byteLength;
      onProgress?.(bytes);
      controller.enqueue(chunk);
    },
  });

  try {
    signal.throwIfAborted();
    if (startOffset > 0) await writable.seek(startOffset);
    signal.throwIfAborted();
    await body.pipeThrough(progressTransform, { signal }).pipeTo(writable, {
      signal,
    });
  } catch (err) {
    // Best-effort close; some browsers leave the file partially
    // written, which the caller cleans up via `removeScene`.
    try {
      await writable.abort();
    } catch {
      /* ignore */
    }
    throw err;
  }
  return bytes;
}

/**
 * Returns the size of the existing OPFS scene file in bytes, or 0 if
 * the file doesn't exist yet. Used by the download queue to compute
 * the resume offset for an interrupted download.
 */
export async function existingSceneSize(sceneId: string): Promise<number> {
  try {
    const dir = await scenesDir(false);
    const handle = await dir.getFileHandle(sceneFilename(sceneId), {
      create: false,
    });
    const file = await handle.getFile();
    return file.size;
  } catch (err) {
    if (err instanceof DOMException && err.name === "NotFoundError") return 0;
    throw err;
  }
}

/**
 * Read a downloaded scene as a `File` (which subclasses `Blob`). The
 * caller wraps it in a `URL.createObjectURL(file)` for `<video>`
 * playback and remembers to revoke when the player unmounts.
 *
 * Returns `null` if the file is missing — a hint that the browser
 * evicted the OPFS entry under storage pressure (the IDB metadata row
 * is still there). The list view uses this to render a "File missing"
 * state with a re-download affordance.
 */
export async function readScene(sceneId: string): Promise<File | null> {
  const location = await locateEntry(sceneId);
  return readSceneFromScope(
    sceneId,
    location?.kind === "legacy" ? null : await getOfflineScope(),
  );
}

/** Source reader for migration and legacy playback. No operation can write or
 * delete another deployment's files or the legacy directory through the normal
 * storage interface. */
export async function readSceneFromScope(
  sceneId: string,
  scope: OfflineScope | null,
): Promise<File | null> {
  try {
    const root = await deploymentDir(scope);
    const dir = await root.getDirectoryHandle(SCENES_DIR);
    const handle = await dir.getFileHandle(sceneFilename(sceneId));
    return await handle.getFile();
  } catch (err) {
    if (err instanceof DOMException && err.name === "NotFoundError") {
      return null;
    }
    throw err;
  }
}

export async function removeScene(sceneId: string): Promise<void> {
  try {
    const dir = await scenesDir(false);
    await dir.removeEntry(sceneFilename(sceneId));
  } catch (err) {
    // Idempotent: missing is success.
    if (err instanceof DOMException && err.name === "NotFoundError") return;
    throw err;
  }
}

export async function clearAllScenes(): Promise<void> {
  try {
    const root = await deploymentDir(await getOfflineScope());
    await root.removeEntry(SCENES_DIR, { recursive: true });
  } catch (err) {
    if (err instanceof DOMException && err.name === "NotFoundError") return;
    throw err;
  }
}

/**
 * Storage estimate exposed in settings + on Offline view header.
 * Browsers may return values in different units; spec says bytes.
 * Both fields are best-effort and may be undefined on older engines.
 */
export interface StorageEstimate {
  usage: number | undefined;
  quota: number | undefined;
}

export async function storageEstimate(): Promise<StorageEstimate> {
  if (!("storage" in navigator) || !navigator.storage.estimate) {
    return { usage: undefined, quota: undefined };
  }
  const e = await navigator.storage.estimate();
  return { usage: e.usage, quota: e.quota };
}

/**
 * Ask the browser for "persistent" storage so OPFS files aren't first
 * in line for eviction under storage pressure. On iOS Safari this only
 * succeeds for PWAs added to Home Screen — desktop browsers grant it
 * more liberally. Idempotent: calling repeatedly is cheap and a
 * granted state never reverts to unsought.
 */
export async function requestPersistent(): Promise<boolean> {
  if (!("storage" in navigator) || !navigator.storage.persist) {
    return false;
  }
  return navigator.storage.persist();
}

export async function isPersisted(): Promise<boolean> {
  if (!("storage" in navigator) || !navigator.storage.persisted) {
    return false;
  }
  return navigator.storage.persisted();
}
