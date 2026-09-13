import { getDownloadRegistration, registerOfflineWorker } from "@/pwa/register";
import type {
  BackgroundFetchManager,
  BackgroundFetchRegistration,
} from "@/pwa/background-fetch-types";
import { joinPlatformURL } from "@/core/platform-url";
import { StreamingResolutionEnum } from "@/core/generated-graphql";
import { downloadQueryString } from "./pick-download-format";
import { getEntry, patchEntry, type OfflineEntry } from "./offline-db";
import { getOfflineScope } from "./offline-scope";

export async function downloadURL(entry: OfflineEntry) {
  const resolution = Object.values(StreamingResolutionEnum).find(
    (value) => value === entry.resolution,
  );
  if (!resolution)
    throw new Error(
      "Unknown download resolution; select a resolution and download again.",
    );
  return joinPlatformURL(
    (await getOfflineScope()).deploymentURL,
    `scene/${entry.scene_id}/download.mp4?${downloadQueryString({ mode: entry.format, resolution, effectiveHeight: 0 })}`,
  );
}

/** Called under the deployment and scene locks. Persist ownership before the
 * browser request so closing the page during permission UI is recoverable. */
export async function startBackgroundDownload(
  entry: OfflineEntry,
  manager: BackgroundFetchManager,
  signal?: AbortSignal,
): Promise<boolean> {
  signal?.throwIfAborted();
  const id = `scene-${entry.scene_id}-${entry.request_id ?? crypto.randomUUID()}`;
  const url = await downloadURL(entry);
  await patchEntry(entry.scene_id, {
    background_fetch_id: id,
    status: "downloading",
    bytes_downloaded: 0,
  });
  try {
    signal?.throwIfAborted();
    const transfer = await manager.fetch(
      id,
      [new Request(url, { credentials: "include" })],
      { title: "Stash" },
    );
    if (signal?.aborted) {
      await transfer.abort();
      signal.throwIfAborted();
    }
    watchBackgroundProgress(entry.scene_id, transfer);
    return true;
  } catch {
    await patchEntry(entry.scene_id, {
      background_fetch_id: undefined,
      status: "queued",
    });
    return false;
  }
}

export async function tryBackgroundDownload(
  entry: OfflineEntry,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  const registration = await registerOfflineWorker();
  signal.throwIfAborted();
  const manager = registration?.backgroundFetch;
  return manager ? startBackgroundDownload(entry, manager, signal) : false;
}

export async function resumeBackgroundDownload(entry: OfflineEntry) {
  if (!entry.background_fetch_id) return false;
  const manager = (await getDownloadRegistration())?.backgroundFetch;
  const transfer = await manager?.get(entry.background_fetch_id);
  if (!transfer) return false;
  watchBackgroundProgress(entry.scene_id, transfer);
  return true;
}

export async function abortBackgroundDownload(entry: OfflineEntry) {
  if (!entry.background_fetch_id) return;
  const manager = (await getDownloadRegistration())?.backgroundFetch;
  await (await manager?.get(entry.background_fetch_id))?.abort();
}

const watched = new Set<string>();
function watchBackgroundProgress(
  sceneId: string,
  transfer: BackgroundFetchRegistration,
) {
  // Worker completion owns the final write. Pages only publish progress.
  if (typeof window === "undefined" || watched.has(transfer.id)) return;
  watched.add(transfer.id);
  let checkpoint = 0;
  const progress = () => {
    if (transfer.result) {
      transfer.removeEventListener("progress", progress);
      watched.delete(transfer.id);
      return;
    }
    if (Date.now() - checkpoint < 1000) return;
    checkpoint = Date.now();
    void patchEntry(sceneId, (current) =>
      current.background_fetch_id === transfer.id &&
      current.status === "downloading"
        ? {
            bytes_downloaded: transfer.downloaded,
            bytes: transfer.downloadTotal,
          }
        : {},
    ).catch(() => {});
  };
  transfer.addEventListener("progress", progress);
  progress();
}

export async function currentBackgroundEntry(id: string) {
  const sceneId = /^scene-([1-9]\d*)-/.exec(id)?.[1];
  if (!sceneId) return undefined;
  const entry = await getEntry(sceneId);
  return entry?.background_fetch_id === id ? entry : undefined;
}
