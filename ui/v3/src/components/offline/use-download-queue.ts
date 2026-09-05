import { getPlatformURL } from "@/core/platform-url";
/**
 * Singleton download queue for offline scene downloads.
 *
 * Holds a serial worker (one in-flight download at a time) that pumps
 * `OfflineEntry` rows whose status is `queued`. Components subscribe
 * via `useDownloadQueue()` to render queue state and call
 * `enqueue` / `cancel` / `retry` / `remove`.
 *
 * Why a vanilla store + `useSyncExternalStore` instead of Context:
 * the queue outlives any single mounted component (downloads continue
 * across navigation between routes), and a Context provider would
 * have to live above the entire route tree. The vanilla store is
 * module-scoped, gets initialised once per page load, and any
 * subscriber on any route sees consistent state without prop-drilling.
 *
 * Persistence model: queue/active state is in-memory + IndexedDB.
 *   - IndexedDB row's `status` field is the source of truth across
 *     PWA reloads.
 *   - In-memory queue is rebuilt from IDB at module init by scanning
 *     for `status === "queued"`.
 *   - In-flight `status === "downloading"` rows on init mean a
 *     previous tab/PWA-session was interrupted; we mark them
 *     `error: "Interrupted"` and the user can retry. The retry path
 *     consults the existing OPFS file size and attempts a `Range:
 *     bytes=N-` request — if the server returns 206 the partial
 *     bytes are appended to, otherwise the file is rewritten from
 *     scratch. Range works against the static-file fast path
 *     (`encDownloadCopy` for MP4-family sources); ffmpeg-piped
 *     responses don't carry `Accept-Ranges`, so the server returns
 *     200 + full body and the worker restarts from byte 0
 *     transparently.
 *
 * Cancellation: each download gets a fresh AbortController whose
 * signal feeds the fetch + the OPFS writer's `pipeTo`. Cancelling
 * aborts the fetch (server-side ffmpeg gets killed via
 * connection-close), stops the OPFS write, and the worker's
 * try/catch flips status to `error` (or to a clean cancellation
 * state — see `cancel`).
 */

import { useSyncExternalStore } from "react";
import {
  deleteEntry,
  getEntry,
  listEntriesByStatus,
  patchEntry,
  putEntry,
  type OfflineEntry,
} from "./offline-db";
import {
  existingSceneSize,
  opfsPathForScene,
  removeScene,
  storageEstimate,
  writeScene,
} from "./opfs-storage";
import { downloadQueryString, type DownloadMode } from "./pick-download-format";
import type { StreamingResolutionEnum } from "src/core/generated-graphql";

export interface QueueSnapshot {
  /** Scene ids waiting their turn, in order. Excludes the active one. */
  queued: string[];
  /** Currently downloading scene + live progress. `null` when idle. */
  active: ActiveDownload | null;
}

export interface ActiveDownload {
  sceneId: string;
  bytesDownloaded: number;
  /** From `Content-Length` if the server sent it; else null. */
  bytesTotal: number | null;
}

/**
 * Snapshot fields the queue captures at enqueue time so the IDB entry
 * doesn't need to be rebuilt from scratch on the worker side. The
 * caller (download-button.tsx) projects from the live `Scene` object.
 */
export interface SceneSnapshot {
  scene_id: string;
  title: string;
  details: string | null;
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
  source_video_codec: string;
  source_audio_codec: string;
  /** Source file's server-side path. Used by the offline view as the
   *  title fallback for untitled scenes — see OfflineEntry. */
  source_file_path?: string;
}

export interface EnqueueArgs {
  snapshot: SceneSnapshot;
  mode: DownloadMode;
  resolution: StreamingResolutionEnum;
}

// ── Store ────────────────────────────────────────────────────────────────────

type Listener = () => void;

class DownloadQueueStore {
  private snapshot: QueueSnapshot = { queued: [], active: null };
  private listeners = new Set<Listener>();
  private workerRunning = false;
  private currentAbort: AbortController | null = null;
  /** Most recent debounced-progress write timestamp per scene. */
  private lastProgressWrite = new Map<string, number>();
  private readonly PROGRESS_WRITE_INTERVAL_MS = 1000;

  // ── External snapshot API ──

  subscribe = (listener: Listener) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): QueueSnapshot => this.snapshot;

  private setSnapshot(next: QueueSnapshot) {
    this.snapshot = next;
    this.listeners.forEach((l) => {
      l();
    });
  }

  private updateActive(patch: Partial<ActiveDownload>) {
    if (!this.snapshot.active) return;
    this.setSnapshot({
      ...this.snapshot,
      active: { ...this.snapshot.active, ...patch },
    });
  }

  // ── Recovery ──

  /**
   * Initial sweep — call once at module init. Rebuilds the queue
   * from IDB and marks any orphan `downloading` entries as errored.
   *
   * Partial OPFS files are NOT removed here — the retry path will
   * inspect the file's size and attempt a range-resume against it.
   * Worst case the server doesn't honour the range and we restart
   * from byte 0; either way the partial isn't wasted by being
   * eagerly deleted.
   */
  async init(): Promise<void> {
    const downloading = await listEntriesByStatus("downloading");
    for (const e of downloading) {
      await patchEntry(e.scene_id, {
        status: "error",
        error: "Interrupted by reload",
        bytes_downloaded: undefined,
      });
    }

    const queued = await listEntriesByStatus("queued");
    if (queued.length > 0) {
      this.setSnapshot({
        queued: queued.map((e) => e.scene_id),
        active: null,
      });
      this.kickWorker();
    }
  }

  // ── Public actions ──

  /**
   * Insert a scene into the queue. If the scene already has a
   * `complete` entry it's wiped (OPFS file + IDB row) before queuing
   * so a re-download replaces in place. Pre-existing `queued` /
   * `downloading` entries for the same scene short-circuit (no-op).
   */
  async enqueue({ snapshot, mode, resolution }: EnqueueArgs): Promise<void> {
    const existing = await getEntry(snapshot.scene_id);
    if (existing) {
      if (existing.status === "queued" || existing.status === "downloading") {
        return; // Already in the queue.
      }
      // Replace in place: clear OPFS + IDB before re-queuing. Do not
      // delete the IDB row entirely — we re-write it below with fresh
      // metadata so the row persists across the brief gap.
      try {
        await removeScene(snapshot.scene_id);
      } catch {
        /* ignore — IDB write below is the source of truth */
      }
    }

    const entry: OfflineEntry = {
      scene_id: snapshot.scene_id,
      title: snapshot.title,
      details: snapshot.details,
      studio_name: snapshot.studio_name,
      studio_id: snapshot.studio_id,
      performers: snapshot.performers,
      tags: snapshot.tags,
      duration: snapshot.duration,
      width: snapshot.width,
      height: snapshot.height,
      date: snapshot.date,
      paths: snapshot.paths,
      format: mode,
      source_video_codec: snapshot.source_video_codec,
      source_audio_codec: snapshot.source_audio_codec,
      source_file_path: snapshot.source_file_path,
      resolution,
      width_actual: snapshot.width, // tightened on completion when known
      height_actual: snapshot.height,
      bytes: 0,
      downloaded_at: 0,
      status: "queued",
      opfs_path: opfsPathForScene(snapshot.scene_id),
      server_status: "unknown",
    };
    await putEntry(entry);
    this.setSnapshot({
      ...this.snapshot,
      queued: [...this.snapshot.queued, snapshot.scene_id],
    });
    this.kickWorker();
  }

  /**
   * Stop a queued or in-flight download. For queued: drops from the
   * queue + deletes the IDB row. For in-flight: aborts the fetch,
   * the worker's catch path flips status to `error` with
   * "Cancelled" — same surface as a network failure so the user can
   * still retry.
   */
  async cancel(sceneId: string): Promise<void> {
    if (this.snapshot.active?.sceneId === sceneId && this.currentAbort) {
      this.currentAbort.abort(new DOMException("Cancelled", "AbortError"));
      // Status flip + queue advance happens in the worker's catch.
      return;
    }
    if (this.snapshot.queued.includes(sceneId)) {
      this.setSnapshot({
        ...this.snapshot,
        queued: this.snapshot.queued.filter((id) => id !== sceneId),
      });
      await deleteEntry(sceneId);
      try {
        await removeScene(sceneId);
      } catch {
        /* ignore */
      }
    }
  }

  /**
   * Re-queue a scene whose previous attempt errored. The IDB row is
   * patched back to `queued` (preserving metadata snapshot); the
   * worker picks it up on next idle cycle.
   */
  async retry(sceneId: string): Promise<void> {
    const existing = await getEntry(sceneId);
    if (!existing) return;
    if (existing.status === "queued" || existing.status === "downloading") {
      return;
    }
    await patchEntry(sceneId, {
      status: "queued",
      error: undefined,
      bytes_downloaded: undefined,
    });
    this.setSnapshot({
      ...this.snapshot,
      queued: [...this.snapshot.queued, sceneId],
    });
    this.kickWorker();
  }

  /**
   * Permanently remove a downloaded entry — IDB row + OPFS file. Used
   * by the list view's per-card delete button.
   */
  async remove(sceneId: string): Promise<void> {
    if (this.snapshot.active?.sceneId === sceneId) {
      // In-flight delete: cancel first, then fall through.
      await this.cancel(sceneId);
    }
    if (this.snapshot.queued.includes(sceneId)) {
      this.setSnapshot({
        ...this.snapshot,
        queued: this.snapshot.queued.filter((id) => id !== sceneId),
      });
    }
    try {
      await removeScene(sceneId);
    } catch {
      /* ignore */
    }
    await deleteEntry(sceneId);
  }

  // ── Worker ──

  private kickWorker() {
    if (this.workerRunning) return;
    this.workerRunning = true;
    void this.runWorker().finally(() => {
      this.workerRunning = false;
    });
  }

  private async runWorker(): Promise<void> {
    while (this.snapshot.queued.length > 0) {
      const sceneId = this.snapshot.queued[0];
      this.setSnapshot({
        queued: this.snapshot.queued.slice(1),
        active: { sceneId, bytesDownloaded: 0, bytesTotal: null },
      });
      try {
        await this.runOne(sceneId);
      } catch (err) {
        // runOne handles its own status writes; this catch is just a
        // safety net so the worker loop survives unexpected throws.
        console.error("[offline] worker error for scene", sceneId, err);
      } finally {
        this.lastProgressWrite.delete(sceneId);
      }
      this.setSnapshot({ ...this.snapshot, active: null });
    }
  }

  private async runOne(sceneId: string): Promise<void> {
    const entry = await getEntry(sceneId);
    if (!entry) return;

    // Quota guard. Refuse if we'd exceed 95% of available — leaves
    // headroom for the browser's internal metadata. We don't know
    // the final size up-front, so the check is a coarse "are we
    // basically full" gate, not a precise reservation.
    const est = await storageEstimate();
    if (est.usage != null && est.quota != null && est.quota > 0) {
      const ratio = est.usage / est.quota;
      if (ratio >= 0.95) {
        await patchEntry(sceneId, {
          status: "error",
          error: "Out of storage. Free space and retry.",
        });
        return;
      }
    }

    // Resume offset: the size of any partial OPFS file from a
    // previous attempt. The Range header below asks the server to
    // skip those bytes; if the server can't honour ranges (ffmpeg
    // pipe — no Accept-Ranges) it returns 200 with the full body and
    // we rewrite from byte 0.
    let resumeOffset = 0;
    try {
      resumeOffset = await existingSceneSize(sceneId);
    } catch {
      /* ignore — fall through to fresh download */
    }

    await patchEntry(sceneId, {
      status: "downloading",
      bytes_downloaded: resumeOffset || 0,
    });
    if (resumeOffset > 0) {
      // Surface the resume baseline immediately so the progress bar
      // doesn't briefly snap back to 0 before the first chunk lands.
      this.updateActive({ bytesDownloaded: resumeOffset });
    }

    const abort = new AbortController();
    this.currentAbort = abort;

    const url = getPlatformURL(
      `scene/${sceneId}/download.mp4?${downloadQueryString({
        mode: entry.format,
        resolution: entry.resolution as StreamingResolutionEnum,
        effectiveHeight: 0, // unused by querystring builder
      })}`,
    ).href;

    const headers: HeadersInit = {};
    if (resumeOffset > 0) {
      headers.Range = `bytes=${resumeOffset}-`;
    }

    let response: Response;
    try {
      response = await fetch(url, { signal: abort.signal, headers });
    } catch (err) {
      this.currentAbort = null;
      await this.markError(sceneId, errorMessage(err));
      return;
    }

    if (!response.ok || !response.body) {
      this.currentAbort = null;
      await this.markError(
        sceneId,
        `Server returned HTTP ${response.status}: ${response.statusText}`,
      );
      return;
    }

    // 206 Partial Content → server honoured the range. Append from
    // `resumeOffset`. 200 OK → server ignored the range (transcode
    // pipe, stale partial mismatch, or no range support). Either
    // way, rewrite from scratch — the partial bytes can't be trusted
    // to byte-match the new full-body response.
    const startOffset = response.status === 206 ? resumeOffset : 0;
    if (startOffset === 0 && resumeOffset > 0) {
      // Reset the in-memory progress to 0 so the bar visibly
      // restarts; the subsequent writes will repaint it.
      this.updateActive({ bytesDownloaded: 0 });
    }

    // Total size: for 206 the server sends Content-Range
    // (`bytes <start>-<end>/<total>`); for 200 it's just
    // Content-Length. Fall back to null when either is missing.
    let total: number | null = null;
    if (response.status === 206) {
      const range = response.headers.get("content-range");
      if (range) {
        const match = /\/(\d+)$/.exec(range);
        if (match) total = parseInt(match[1], 10) || null;
      }
    } else {
      const cl = response.headers.get("content-length");
      if (cl) total = parseInt(cl, 10) || null;
    }
    if (total != null) {
      this.updateActive({ bytesTotal: total });
    }

    const onProgress = (bytes: number) => {
      this.updateActive({ bytesDownloaded: bytes });
      // Debounced IDB write — keep the bar lively in-memory but only
      // touch IDB ~once a second so we don't trash the transaction
      // log on multi-GB downloads.
      const last = this.lastProgressWrite.get(sceneId) ?? 0;
      const now = performance.now();
      if (now - last >= this.PROGRESS_WRITE_INTERVAL_MS) {
        this.lastProgressWrite.set(sceneId, now);
        void patchEntry(sceneId, { bytes_downloaded: bytes });
      }
    };

    let bytes: number;
    try {
      bytes = await writeScene(
        sceneId,
        response.body,
        abort.signal,
        onProgress,
        startOffset,
      );
    } catch (err) {
      this.currentAbort = null;
      // Best-effort cleanup of partial file ONLY when the partial is
      // unrecoverable — i.e. the user explicitly cancelled, or we
      // got a 200 response and the bytes we'd written are now half
      // a fresh download. Network blips on a 206 leave the bytes in
      // place so the next retry can resume from where we stopped.
      const isCancel = err instanceof DOMException && err.name === "AbortError";
      if (isCancel || startOffset === 0) {
        try {
          await removeScene(sceneId);
        } catch {
          /* ignore */
        }
      }
      await this.markError(sceneId, errorMessage(err));
      return;
    }

    this.currentAbort = null;
    await patchEntry(sceneId, {
      status: "complete",
      downloaded_at: Date.now(),
      bytes,
      bytes_downloaded: undefined,
      error: undefined,
    });
  }

  private async markError(sceneId: string, message: string): Promise<void> {
    await patchEntry(sceneId, {
      status: "error",
      error: message,
      bytes_downloaded: undefined,
    });
  }
}

function errorMessage(err: unknown): string {
  if (err instanceof DOMException && err.name === "AbortError") {
    return "Cancelled";
  }
  if (err instanceof Error) return err.message;
  return String(err);
}

// ── Module-scope singleton ───────────────────────────────────────────────────

const store = new DownloadQueueStore();

// Kick off the recovery sweep on first import. Failures are
// surfaced via `init`'s patch writes — no UI surface needed at
// boot since the user hasn't opened the Offline view yet.
let initPromise: Promise<void> | null = null;
function ensureInit(): Promise<void> {
  if (!initPromise) {
    initPromise = store.init().catch((err) => {
      console.error("[offline] init failed:", err);
    });
  }
  return initPromise;
}

// ── Hook ─────────────────────────────────────────────────────────────────────

export interface UseDownloadQueue {
  state: QueueSnapshot;
  enqueue: DownloadQueueStore["enqueue"];
  cancel: DownloadQueueStore["cancel"];
  retry: DownloadQueueStore["retry"];
  remove: DownloadQueueStore["remove"];
  /** Returns the recovery promise. Mostly for tests. */
  ready: () => Promise<void>;
}

export function useDownloadQueue(): UseDownloadQueue {
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot);
  return {
    state,
    enqueue: store.enqueue.bind(store),
    cancel: store.cancel.bind(store),
    retry: store.retry.bind(store),
    remove: store.remove.bind(store),
    ready: ensureInit,
  };
}

/** Direct singleton access for non-component callers (e.g. boot code). */
export function getDownloadQueueStore(): DownloadQueueStore {
  return store;
}

export function ensureDownloadQueueInit(): Promise<void> {
  return ensureInit();
}
