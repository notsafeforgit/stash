import type { BackgroundFetchRegistration } from "./background-fetch-types";
import {
  currentBackgroundEntry,
  startBackgroundDownload,
} from "@/components/offline/background-downloads";
import {
  getEntry,
  listEntriesByStatus,
  patchEntry,
  subscribeToEntries,
} from "@/components/offline/offline-db";
import { getOfflineScope } from "@/components/offline/offline-scope";
import { checkDownloadProcessing } from "@/components/offline/download-processing";
import {
  removeScene,
  storageEstimate,
  writeScene,
} from "@/components/offline/opfs-storage";

/** Completion is browser-owned. Stream directly from its response into OPFS;
 * never turn a multi-GB response into an in-memory Blob or ArrayBuffer. */
export async function finishBackgroundDownload(
  transfer: BackgroundFetchRegistration,
  registration: Pick<ServiceWorkerRegistration, "backgroundFetch">,
) {
  const scope = await getOfflineScope();
  await navigator.locks.request(scope.workerLock, async () => {
    const entry = await currentBackgroundEntry(transfer.id);
    if (entry)
      await navigator.locks.request(
        scope.sceneLock(entry.scene_id),
        async () => {
          const current = await currentBackgroundEntry(transfer.id);
          if (current?.status !== "downloading") return;
          const abort = new AbortController();
          const unsubscribe = subscribeToEntries(() => {
            void getEntry(entry.scene_id)
              .then((latest) => {
                if (
                  latest?.cancel_requested ||
                  latest?.background_fetch_id !== transfer.id
                )
                  abort.abort();
              })
              .catch(() => abort.abort());
          });
          try {
            if (current.cancel_requested) throw new Error("Cancelled");
            if (transfer.result !== "success")
              throw new Error(
                transfer.failureReason || "Background download interrupted",
              );
            const records = await transfer.matchAll();
            if (records.length !== 1 || !records[0])
              throw new Error("Incomplete background download");
            const response = await records[0].responseReady;
            const type = response.headers.get("content-type")?.split(";", 1)[0];
            if (
              response.status !== 200 ||
              !response.body ||
              (type !== "video/mp4" && type !== "application/octet-stream")
            )
              throw new Error(
                `Invalid video download (HTTP ${response.status})`,
              );
            const estimate = await storageEstimate();
            const expected =
              Number(response.headers.get("content-length")) ||
              transfer.downloaded;
            // The browser's completed response occupies storage until this event
            // settles; reserve room for the OPFS copy as well.
            if (
              estimate.quota &&
              estimate.usage !== undefined &&
              expected > estimate.quota * 0.95 - estimate.usage
            )
              throw new Error("Out of storage. Free space and retry.");
            const bytes = await writeScene(
              entry.scene_id,
              response.body,
              abort.signal,
            );
            abort.signal.throwIfAborted();
            if (expected > 0 && bytes !== expected)
              throw new Error("Incomplete video download");
            await checkDownloadProcessing(current, abort.signal);
            let committed = false;
            await patchEntry(entry.scene_id, (latest) => {
              if (
                latest.background_fetch_id !== transfer.id ||
                latest.cancel_requested
              )
                return {};
              committed = true;
              return {
                status: "complete",
                bytes,
                downloaded_at: Date.now(),
                bytes_downloaded: undefined,
                background_fetch_id: undefined,
                error: undefined,
                cancel_requested: false,
              };
            });
            if (!committed) throw new Error("Cancelled");
          } catch (error) {
            await removeScene(entry.scene_id);
            await patchEntry(entry.scene_id, (latest) =>
              latest.background_fetch_id === transfer.id
                ? {
                    status: "error",
                    background_fetch_id: undefined,
                    bytes_downloaded: undefined,
                    error: abort.signal.aborted
                      ? "Cancelled"
                      : error instanceof Error
                        ? error.message
                        : "Background download failed",
                  }
                : {},
            );
          } finally {
            unsubscribe();
            abort.abort();
          }
        },
      );
    // Continue the durable queue even if every app window has closed.
    const manager = registration.backgroundFetch;
    if (!manager) return;
    if ((await listEntriesByStatus("downloading")).length) return;
    const queued = (await listEntriesByStatus("queued")).sort(
      (a, b) => (a.queued_at ?? 0) - (b.queued_at ?? 0),
    );
    for (const next of queued) {
      const started = await navigator.locks.request(
        scope.sceneLock(next.scene_id),
        async () => {
          const latest = await getEntry(next.scene_id);
          if (latest?.status !== "queued" || latest.cancel_requested)
            return undefined;
          return startBackgroundDownload(latest, manager);
        },
      );
      // Permission refusal leaves the queue for the foreground fallback.
      if (started !== undefined) break;
    }
  });
}
