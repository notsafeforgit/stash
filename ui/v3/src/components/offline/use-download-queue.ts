import { joinPlatformURL } from "@/core/platform-url";
import { canCoordinateOfflineStorage, getOfflineScope } from "./offline-scope";
import { migrateLegacyDownloads } from "./offline-migration";
/** Offline download commands are coordinated with Web Locks. IndexedDB owns
 * the durable queue, BroadcastChannel publishes changes between tabs, and a
 * per-scene lock prevents deletion or replacement while its writer is active.
 * Every store, file, lock and channel belongs to the same deployment. */

import { useMemo, useSyncExternalStore } from "react";
import { useIntl } from "react-intl";
import { useToast } from "@/hooks/toast";
import {
  clearAll,
  deleteEntry,
  getEntry,
  listEntriesByStatus,
  listEntries,
  subscribeToEntries,
  patchEntry,
  putEntry,
} from "./offline-db";
import {
  clearAllScenes,
  existingSceneSize,
  opfsPathForScene,
  removeScene,
  storageEstimate,
  writeScene,
} from "./opfs-storage";
import { downloadQueryString, type DownloadMode } from "./pick-download-format";
import { StreamingResolutionEnum } from "src/core/generated-graphql";

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

type Listener = () => void;

export function canCoordinateDownloads(): boolean {
  return canCoordinateOfflineStorage();
}

export class OfflineQueueUnavailableError extends Error {
  constructor() {
    super(
      "Offline library changes are unavailable in this browser. Existing downloads can still be played.",
    );
  }
}

async function sceneLock<T>(
  sceneId: string,
  action: () => Promise<T>,
): Promise<T> {
  if (!canCoordinateDownloads())
    return Promise.reject(new OfflineQueueUnavailableError());
  return await navigator.locks.request(
    (await getOfflineScope()).sceneLock(sceneId),
    action,
  );
}

/** One instance per page; Web Locks own work across pages sharing the database.
 * Scene commands and the writer use the same lock. Cancellation is a durable
 * request outside that lock, followed by waiting for the writer to release it. */
export class DownloadQueueStore {
  private snapshot: QueueSnapshot = { queued: [], active: null };
  private listeners = new Set<Listener>();
  private worker: Promise<void> | undefined;
  private initialization: Promise<void> | undefined;
  private unsubscribe: (() => void) | undefined;
  private refreshVersion = 0;
  private task:
    | { sceneId: string; requestId?: string; abort: AbortController }
    | undefined;

  subscribe = (listener: Listener) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = (): QueueSnapshot => this.snapshot;

  private publish(next: QueueSnapshot) {
    this.snapshot = next;
    for (const listener of this.listeners) listener();
  }

  private updateActive(patch: Partial<ActiveDownload>) {
    if (this.snapshot.active)
      this.publish({
        ...this.snapshot,
        active: { ...this.snapshot.active, ...patch },
      });
  }

  private async refresh() {
    const version = ++this.refreshVersion;
    const entries = await listEntries();
    if (version !== this.refreshVersion) return;
    const taskEntry = entries.find(
      (entry) => entry.scene_id === this.task?.sceneId,
    );
    if (
      taskEntry?.cancel_requested &&
      taskEntry.request_id === this.task?.requestId
    )
      this.task?.abort.abort();
    const downloading = entries.find((entry) => entry.status === "downloading");
    const active = downloading
      ? {
          sceneId: downloading.scene_id,
          bytesDownloaded: downloading.bytes_downloaded ?? 0,
          bytesTotal: downloading.bytes || null,
        }
      : null;
    // The owner has finer progress than the periodically persisted checkpoint.
    const localActive =
      this.task && this.snapshot.active?.sceneId === downloading?.scene_id
        ? this.snapshot.active
        : active;
    this.publish({
      queued: entries
        .filter((entry) => entry.status === "queued")
        .sort((a, b) => (a.queued_at ?? 0) - (b.queued_at ?? 0))
        .map((entry) => entry.scene_id),
      active: localActive,
    });
  }

  init(): Promise<void> {
    if (!this.initialization) {
      this.unsubscribe = subscribeToEntries(() => {
        void this.refresh()
          .then(() => {
            if (this.snapshot.queued.length) this.kickWorker();
          })
          .catch((error) => console.error("[offline] refresh failed", error));
      });
      this.initialization = this.refresh()
        .then(() => {
          this.kickWorker();
        })
        .catch((error) => {
          this.initialization = undefined;
          this.unsubscribe?.();
          this.unsubscribe = undefined;
          throw error;
        });
    }
    return this.initialization;
  }

  async enqueue({ snapshot, mode, resolution }: EnqueueArgs): Promise<void> {
    await this.init();
    const before = await getEntry(snapshot.scene_id);
    if (before?.status === "queued" || before?.status === "downloading") return;
    await sceneLock(snapshot.scene_id, async () => {
      const existing = await getEntry(snapshot.scene_id);
      if (existing?.status === "queued" || existing?.status === "downloading")
        return;
      await removeScene(snapshot.scene_id);
      await putEntry({
        ...snapshot,
        format: mode,
        resolution,
        width_actual: snapshot.width,
        height_actual: snapshot.height,
        bytes: 0,
        downloaded_at: 0,
        status: "queued",
        opfs_path: await opfsPathForScene(snapshot.scene_id),
        server_status: "unknown",
        request_id: crypto.randomUUID(),
        queued_at: Date.now(),
      });
    });
    await this.refresh();
    this.kickWorker();
  }

  async retry(sceneId: string): Promise<void> {
    await this.init();
    const before = await getEntry(sceneId);
    if (
      !before ||
      before.status === "queued" ||
      before.status === "downloading"
    )
      return;
    await sceneLock(sceneId, async () => {
      const entry = await getEntry(sceneId);
      if (!entry || entry.status === "queued" || entry.status === "downloading")
        return;
      if (entry.status === "complete") await removeScene(sceneId);
      await patchEntry(sceneId, {
        status: "queued",
        error: undefined,
        bytes_downloaded: undefined,
        cancel_requested: false,
        request_id: crypto.randomUUID(),
        queued_at: Date.now(),
      });
    });
    await this.refresh();
    this.kickWorker();
  }

  private async requestCancellation(sceneId: string) {
    const entry = await getEntry(sceneId);
    if (entry?.status === "queued" || entry?.status === "downloading") {
      if (
        this.task?.sceneId === sceneId &&
        this.task.requestId === entry.request_id
      )
        this.task.abort.abort();
      await patchEntry(sceneId, (current) =>
        current.request_id === entry.request_id
          ? { cancel_requested: true }
          : {},
      );
    }
    return entry;
  }

  async cancel(sceneId: string): Promise<void> {
    await this.init();
    const requested = await this.requestCancellation(sceneId);
    if (
      !requested ||
      (requested.status !== "queued" && requested.status !== "downloading")
    )
      return;
    await sceneLock(sceneId, async () => {
      const entry = await getEntry(sceneId);
      if (
        !entry ||
        entry.request_id !== requested.request_id ||
        entry.status === "complete"
      )
        return;
      await removeScene(sceneId);
      if (entry.status === "queued") await deleteEntry(sceneId);
      else await this.markError(sceneId, "Cancelled");
    });
    await this.refresh();
  }

  async remove(sceneId: string): Promise<void> {
    await this.init();
    const requested = await this.requestCancellation(sceneId);
    await sceneLock(sceneId, async () => {
      const entry = await getEntry(sceneId);
      // A retry queued after this removal request is a different operation.
      if (entry?.request_id !== requested?.request_id) return;
      await removeScene(sceneId);
      await deleteEntry(sceneId);
    });
    await this.refresh();
  }

  async removeAll(): Promise<void> {
    if (!canCoordinateDownloads()) throw new OfflineQueueUnavailableError();
    await this.init();
    await Promise.all(
      (await listEntries()).map((entry) =>
        this.requestCancellation(entry.scene_id),
      ),
    );
    // The worker owns this lock until its writers settle. Clearing the stores
    // under it also prevents an orphan file from surviving clear-all.
    await navigator.locks.request(
      (await getOfflineScope()).workerLock,
      async () => {
        await clearAllScenes();
        await clearAll();
      },
    );
    await this.refresh();
  }

  private kickWorker() {
    if (this.worker || !canCoordinateDownloads()) return;
    let failed = false;
    this.worker = getOfflineScope()
      .then((scope) =>
        navigator.locks.request(scope.workerLock, async () => {
          // A failed migration leaves its source intact and must not prevent new
          // downloads. Its recoverable error is exposed by the migration control.
          await migrateLegacyDownloads();
          // The previous owner has released its lock; only now can these be orphans.
          for (const entry of await listEntriesByStatus("downloading")) {
            await sceneLock(entry.scene_id, async () => {
              const current = await getEntry(entry.scene_id);
              if (current?.status === "downloading")
                await this.markError(entry.scene_id, "Interrupted by reload");
            });
          }
          while (true) {
            const queued = (await listEntriesByStatus("queued")).sort(
              (a, b) => (a.queued_at ?? 0) - (b.queued_at ?? 0),
            );
            const next = queued[0];
            if (!next) break;
            await sceneLock(next.scene_id, () => this.runOne(next.scene_id));
          }
        }),
      )
      .then(() => undefined)
      .catch((error) => {
        failed = true;
        console.error("[offline] worker failed", error);
      })
      .finally(async () => {
        try {
          await this.refresh();
        } catch (error) {
          failed = true;
          console.error("[offline] refresh failed", error);
        }
        this.worker = undefined;
        // An enqueue may have landed between the final scan and lock release.
        if (!failed && this.snapshot.queued.length) this.kickWorker();
      });
  }

  private async runOne(sceneId: string): Promise<void> {
    const abort = new AbortController();
    const task: {
      sceneId: string;
      requestId?: string;
      abort: AbortController;
    } = { sceneId, abort };
    this.task = task;
    let checkpoint: Promise<unknown> = Promise.resolve();
    let discardPartial = false;
    try {
      const entry = await getEntry(sceneId);
      if (entry?.status !== "queued") return;
      task.requestId = entry.request_id;
      if (entry.cancel_requested) abort.abort();
      abort.signal.throwIfAborted();
      this.publish({
        queued: this.snapshot.queued.filter((id) => id !== sceneId),
        active: { sceneId, bytesDownloaded: 0, bytesTotal: null },
      });
      await patchEntry(sceneId, { status: "downloading", bytes_downloaded: 0 });
      abort.signal.throwIfAborted();
      const estimate = await storageEstimate();
      abort.signal.throwIfAborted();
      if (
        estimate.quota &&
        estimate.usage != null &&
        estimate.usage / estimate.quota >= 0.95
      )
        throw new Error("Out of storage. Free space and retry.");
      const resumeOffset = await existingSceneSize(sceneId);
      abort.signal.throwIfAborted();
      await patchEntry(sceneId, { bytes_downloaded: resumeOffset });
      this.updateActive({ bytesDownloaded: resumeOffset });
      abort.signal.throwIfAborted();
      const resolution = Object.values(StreamingResolutionEnum).find(
        (value) => value === entry.resolution,
      );
      if (!resolution)
        throw new Error(
          "Unknown download resolution; select a resolution and download again.",
        );
      const url = joinPlatformURL(
        (await getOfflineScope()).deploymentURL,
        `scene/${sceneId}/download.mp4?${downloadQueryString({ mode: entry.format, resolution, effectiveHeight: 0 })}`,
      ).href;
      const response = await fetch(url, {
        signal: abort.signal,
        headers: resumeOffset ? { Range: `bytes=${resumeOffset}-` } : {},
      });
      abort.signal.throwIfAborted();
      if (!response.ok || !response.body)
        throw new Error(
          `Server returned HTTP ${response.status}: ${response.statusText}`,
        );
      const startOffset = response.status === 206 ? resumeOffset : 0;
      discardPartial = startOffset === 0;
      let total: number | null = null;
      if (response.status === 206) {
        const range = /^bytes (\d+)-(\d+)\/(\d+|\*)$/.exec(
          response.headers.get("content-range") ?? "",
        );
        if (!range || Number(range[1]) !== resumeOffset)
          throw new Error("Server returned an inconsistent download range");
        if (range[3] !== "*") total = Number(range[3]);
      } else {
        const length = response.headers.get("content-length");
        if (length !== null) total = Number(length);
      }
      this.updateActive({ bytesDownloaded: startOffset, bytesTotal: total });
      await patchEntry(sceneId, { bytes_downloaded: startOffset });
      abort.signal.throwIfAborted();
      let lastCheckpoint = performance.now();
      const bytes = await writeScene(
        sceneId,
        response.body,
        abort.signal,
        (downloaded) => {
          this.updateActive({ bytesDownloaded: downloaded });
          if (performance.now() - lastCheckpoint >= 1000) {
            lastCheckpoint = performance.now();
            checkpoint = checkpoint
              .then(() => patchEntry(sceneId, { bytes_downloaded: downloaded }))
              .catch((error) => {
                console.error("[offline] progress checkpoint failed", error);
              });
          }
        },
        startOffset,
      );
      await checkpoint;
      abort.signal.throwIfAborted();
      await patchEntry(sceneId, {
        status: "complete",
        downloaded_at: Date.now(),
        bytes,
        bytes_downloaded: undefined,
        error: undefined,
        cancel_requested: false,
      });
    } catch (error) {
      await checkpoint;
      try {
        if (abort.signal.aborted || discardPartial) await removeScene(sceneId);
      } finally {
        await this.markError(
          sceneId,
          abort.signal.aborted ? "Cancelled" : errorMessage(error),
        );
      }
    } finally {
      abort.abort();
      this.task = undefined;
      this.publish({ ...this.snapshot, active: null });
    }
  }

  private async markError(sceneId: string, error: string) {
    await patchEntry(sceneId, {
      status: "error",
      error,
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

function ensureInit(): Promise<void> {
  return store.init();
}

// ── Hook ─────────────────────────────────────────────────────────────────────

export interface UseDownloadQueue {
  state: QueueSnapshot;
  enqueue: DownloadQueueStore["enqueue"];
  cancel: DownloadQueueStore["cancel"];
  retry: DownloadQueueStore["retry"];
  remove: DownloadQueueStore["remove"];
  removeAll: DownloadQueueStore["removeAll"];
  /** Returns the recovery promise. Mostly for tests. */
  ready: () => Promise<void>;
}

/** Commands attach error reporting even when event handlers discard the returned
 * promise. The original promise still rejects for callers coordinating a batch. */
export function useDownloadCommands() {
  const intl = useIntl();
  const toast = useToast();
  return useMemo(() => {
    function report<T>(promise: Promise<T>): Promise<T> {
      void promise.catch((error) =>
        toast.error(
          error instanceof OfflineQueueUnavailableError
            ? intl.formatMessage({
                id: "offline.errors.coordination_unavailable",
                defaultMessage:
                  "Offline library changes are unavailable in this browser. Existing downloads can still be played.",
              })
            : error,
        ),
      );
      return promise;
    }
    return {
      enqueue: (args: EnqueueArgs) => report(store.enqueue(args)),
      cancel: (sceneId: string) => report(store.cancel(sceneId)),
      retry: (sceneId: string) => report(store.retry(sceneId)),
      remove: (sceneId: string) => report(store.remove(sceneId)),
      removeAll: () => report(store.removeAll()),
      ready: ensureInit,
    };
  }, [intl, toast]);
}
export function useDownloadQueue(): UseDownloadQueue {
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot);
  const commands = useDownloadCommands();
  return useMemo(() => ({ state, ...commands }), [state, commands]);
}

/** Direct singleton access for non-component callers (e.g. boot code). */
export function getDownloadQueueStore(): DownloadQueueStore {
  return store;
}

export function ensureDownloadQueueInit(): Promise<void> {
  return ensureInit();
}
