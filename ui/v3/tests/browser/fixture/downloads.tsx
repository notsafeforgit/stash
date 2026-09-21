import { useEffect, useState } from "react";
import { DownloadTray } from "@/components/offline/download-tray";
import { DownloadProgressBar } from "@/components/offline/download-progress-bar";
import {
  putEntry,
  patchEntry,
  clearAll,
  type OfflineEntry,
} from "@/components/offline/offline-db";
import { getOfflineScope } from "@/components/offline/offline-scope";
import { offlineEntryToSceneData } from "@/components/offline/offline-scene-adapter";
import { SceneFileInfoTab } from "@/components/detail/scene-detail-tabs";
import { Button } from "@/components/ui/button";

const saved: OfflineEntry = {
  scene_id: "1",
  title: "Saved HDR video",
  studio_name: null,
  studio_id: null,
  performers: [],
  tags: [],
  duration: 60,
  width: 1920,
  height: 1080,
  date: null,
  paths: { screenshot: null, preview: null, sprite: null, vtt: null },
  format: "copy",
  source_video_codec: "hevc",
  source_audio_codec: "aac",
  source_file_path: "/library/example.mp4",
  source_file_metadata: {
    frame_rate: 29.97,
    bit_rate: 20_000_000,
    bit_depth: 10,
    color_range: "tv",
    color_space: "bt2020nc",
    color_transfer: "smpte2084",
    color_primaries: "bt2020",
  },
  resolution: "ORIGINAL",
  width_actual: 1920,
  height_actual: 1080,
  bytes: 90_000_000,
  status: "complete",
  downloaded_at: 1,
  opfs_path: "scenes/1.mp4",
  server_status: "present",
};

let preparation: Promise<void> | undefined;
async function prepareDownloads() {
  const scope = await getOfflineScope();
  // Model a worker in another tab. Hold its lock so this fixture can publish
  // durable progress without making network requests or writing media files.
  await new Promise<void>((resolve, reject) => {
    void navigator.locks
      .request(scope.workerLock, async () => {
        resolve();
        await new Promise(() => {});
      })
      .catch(reject);
  });
  const params = new URLSearchParams(location.search);
  if (params.has("idle")) return;
  await putEntry({
    ...saved,
    title: "Active download with a long title that should fit on a small phone",
    status: "downloading",
    bytes: params.has("unknown") ? 0 : 100 * 1024 * 1024,
    bytes_downloaded: 25 * 1024 * 1024,
  });
  await putEntry({
    ...saved,
    scene_id: "2",
    title: "Queued video",
    status: "queued",
  });
  await putEntry({
    ...saved,
    scene_id: "3",
    title: "Failed video",
    status: "error",
    error: "Interrupted",
  });
}

export function DownloadsFixture() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    let disposed = false;
    preparation ??= prepareDownloads();
    void preparation.then(() => {
      if (!disposed) setReady(true);
    });
    return () => {
      disposed = true;
    };
  }, []);
  if (!ready) return null;
  return (
    <>
      <div className="hidden md:flex">
        <DownloadTray />
      </div>
      <DownloadTray mobile />
      <DownloadProgressBar />
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3">
        <div className="flex flex-wrap gap-2">
          <Button
            onClick={() =>
              void patchEntry("1", { bytes_downloaded: 50 * 1024 * 1024 })
            }
          >
            Advance download
          </Button>
          <Button onClick={() => void clearAll()}>Clear fixture</Button>
        </div>
        <SceneFileInfoTab
          scene={offlineEntryToSceneData(saved, "blob:offline")}
        />
      </div>
    </>
  );
}
