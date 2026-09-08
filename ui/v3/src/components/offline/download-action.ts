/**
 * `useSceneDownloadAction` — shared hook backing the "Download" /
 * "Re-download" entry in both the scene card context menu and the
 * scene detail actions menu. Returns a stable label + click handler
 * computed from current queue + IDB state, so the menu items in both
 * surfaces stay in lockstep.
 */

import { useCallback } from "react";
import { useToast } from "@/hooks/toast";
import { useOfflineEntry } from "./use-offline-entries";
import {
  sceneDownloadSnapshot,
  type DownloadableScene,
} from "./scene-download-input";
import { useIntl } from "react-intl";
import type { StreamingResolutionEnum } from "src/core/generated-graphql";
import {
  probeCodecsDecodableInMp4,
  useCodecsDecodableInMp4,
  useVideoCodecDecodableInMp4,
} from "src/components/player/player-utils";
import type { OfflineEntry } from "./offline-db";
import { pickDownloadFormat } from "./pick-download-format";
import {
  useDownloadQueue,
  getDownloadQueueStore,
  canCoordinateDownloads,
} from "./use-download-queue";
import {
  loadOfflineMaxResolution,
  // The settings UI writes via `saveOfflineMaxResolution`; reading is
  // enough here.
} from "./offline-settings";
import { useServerCapabilities } from "./use-server-capabilities";

export type DownloadStatus =
  | "idle"
  | "queued"
  | "downloading"
  | "complete"
  | "error";

export interface SceneDownloadAction {
  /** UI label appropriate for the current state. */
  label: string;
  /** Detailed status — for icon picking / disabled-state styling. */
  status: DownloadStatus;
  /** Live IDB row when one exists. */
  entry?: OfflineEntry;
  /** Disabled when an active download is in flight for this scene. */
  disabled: boolean;
  /** Triggers the right action for the current state. */
  onSelect: () => void;
}

interface UseSceneDownloadActionOpts {
  scene: DownloadableScene;
  /** Optional override of the user's "max resolution" setting — the
   *  settings UI uses this for the per-scene override on the detail
   *  actions menu (Phase 1: omitted; user setting wins). */
  maxResolutionOverride?: StreamingResolutionEnum;
}

export function useSceneDownloadAction(
  opts: UseSceneDownloadActionOpts,
): SceneDownloadAction {
  const intl = useIntl();
  const { scene } = opts;
  const sceneId = scene.id;
  const file = scene.files?.[0];
  const videoCodec = file?.video_codec ?? null;
  const audioCodec = file?.audio_codec ?? null;

  const videoAndAudioInMp4 = useCodecsDecodableInMp4(videoCodec, audioCodec);
  const videoInMp4 = useVideoCodecDecodableInMp4(videoCodec);
  // Browser-side HEVC / AV1 decode capability: probes `hvc1.*` /
  // `av01.*` MIME via MMS/MSE `isTypeSupported`. Reuses the same
  // probe cache the rest of the player uses, so no extra round-trip.
  const decodesHevc = useVideoCodecDecodableInMp4("hevc");
  const decodesAv1 = useVideoCodecDecodableInMp4("av1");
  // Server-side encoder availability — fetched once via the
  // `serverCapabilities` query and cached. Defaults to false until
  // the query lands so the first-render auto-pick falls through to
  // H.264 for any device that hasn't yet seen the capability.
  const serverCaps = useServerCapabilities();
  const serverHevcAvailable = serverCaps.downloadFormats.includes("hevc");
  const serverAv1Available = serverCaps.downloadFormats.includes("av1");

  const queue = useDownloadQueue();
  const {
    entry,
    loading: entryLoading,
    error: entryError,
  } = useOfflineEntry(sceneId);

  const isActive = queue.state.active?.sceneId === sceneId;
  const status: DownloadStatus = isActive
    ? "downloading"
    : (entry?.status ?? "idle");

  const labelId = (() => {
    switch (status) {
      case "downloading":
        return "offline.actions.downloading";
      case "queued":
        return "offline.actions.queued_for_download";
      case "complete":
        return "offline.actions.redownload";
      case "error":
        return "offline.actions.retry_download";
      default:
        return "offline.actions.download";
    }
  })();
  const label = intl.formatMessage({ id: labelId });

  const onSelect = () => {
    if (!file) return;
    const maxRes = opts.maxResolutionOverride ?? loadOfflineMaxResolution();
    const pick = pickDownloadFormat({
      source: { width: file.width ?? 0, height: file.height ?? 0 },
      device: {
        videoAndAudioInMp4,
        videoInMp4,
        decodesHevc,
        decodesAv1,
      },
      server: {
        hevcAvailable: serverHevcAvailable,
        av1Available: serverAv1Available,
      },
      maxResolution: maxRes,
    });

    if (status === "error") {
      void queue.retry(sceneId);
      return;
    }

    const snapshot = sceneDownloadSnapshot(scene);
    if (!snapshot) return;
    void queue.enqueue({
      snapshot,
      mode: pick.mode,
      resolution: pick.resolution,
    });
  };

  return {
    label,
    status,
    entry,
    disabled:
      !canCoordinateDownloads() ||
      !file ||
      entryLoading ||
      !!entryError ||
      status === "downloading" ||
      status === "queued",
    onSelect,
  };
}

// ── Bulk enqueue ─────────────────────────────────────────────────────────────

/**
 * Hook returning a bulk-enqueue function for an array of `SceneCardScene`s.
 * Used by the scene-card bulk context menu when the user has multiple
 * scenes selected and picks "Download N scenes".
 *
 * Per-scene format pick uses the same async `probeCodecsDecodableInMp4`
 * the single-scene hook reads from synchronously. The serial loop is
 * fine UX-wise — `queue.enqueue` returns as soon as the IDB row +
 * in-memory queue are updated (the actual download still serialises
 * inside the queue worker), and all probes after the first per codec
 * are cache hits.
 */
export function useBulkSceneDownload() {
  const toast = useToast();
  const queue = getDownloadQueueStore();
  // HEVC / AV1 device-level decode capability — invariant across
  // scenes (probe is on the codec spec, not the source). Hooked once
  // here so the returned function is a sync setup → async kicker.
  const decodesHevc = useVideoCodecDecodableInMp4("hevc");
  const decodesAv1 = useVideoCodecDecodableInMp4("av1");
  const serverCaps = useServerCapabilities();
  const serverHevcAvailable = serverCaps.downloadFormats.includes("hevc");
  const serverAv1Available = serverCaps.downloadFormats.includes("av1");

  return useCallback(
    async (scenes: readonly DownloadableScene[]) => {
      try {
        const maxRes = loadOfflineMaxResolution();
        for (const scene of scenes) {
          const file = scene.files?.[0];
          if (!file) continue;
          const videoCodec = file.video_codec ?? null;
          const audioCodec = file.audio_codec ?? null;
          const [videoAndAudioInMp4, videoInMp4] = await Promise.all([
            probeCodecsDecodableInMp4(videoCodec, audioCodec),
            probeCodecsDecodableInMp4(videoCodec, null),
          ]);
          const pick = pickDownloadFormat({
            source: { width: file.width ?? 0, height: file.height ?? 0 },
            device: {
              videoAndAudioInMp4,
              videoInMp4,
              decodesHevc,
              decodesAv1,
            },
            server: {
              hevcAvailable: serverHevcAvailable,
              av1Available: serverAv1Available,
            },
            maxResolution: maxRes,
          });
          const snapshot = sceneDownloadSnapshot(scene);
          if (!snapshot) continue;
          await queue.enqueue({
            snapshot,
            mode: pick.mode,
            resolution: pick.resolution,
          });
        }
      } catch (error) {
        toast.error(error);
      }
    },
    [
      queue,
      decodesHevc,
      decodesAv1,
      serverHevcAvailable,
      serverAv1Available,
      toast,
    ],
  );
}
