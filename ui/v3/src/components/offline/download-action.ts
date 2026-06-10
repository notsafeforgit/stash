/**
 * `useSceneDownloadAction` — shared hook backing the "Download" /
 * "Re-download" entry in both the scene card context menu and the
 * scene detail actions menu. Returns a stable label + click handler
 * computed from current queue + IDB state, so the menu items in both
 * surfaces stay in lockstep.
 */

import { useCallback, useEffect, useState } from "react";
import { useIntl } from "react-intl";
import type { StreamingResolutionEnum } from "src/core/generated-graphql";
import {
  probeCodecsDecodableInMp4,
  useCodecsDecodableInMp4,
  useVideoCodecDecodableInMp4,
} from "src/components/player/player-utils";
import type { SceneCardScene } from "src/components/cards/scene-card";
import { getEntry, subscribeToEntries, type OfflineEntry } from "./offline-db";
import { pickDownloadFormat } from "./pick-download-format";
import { useDownloadQueue, type SceneSnapshot } from "./use-download-queue";
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
  scene: Pick<
    SceneCardScene,
    | "id"
    | "title"
    | "details"
    | "files"
    | "studio"
    | "performers"
    | "tags"
    | "date"
    | "paths"
  >;
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
  const [entry, setEntry] = useState<OfflineEntry | undefined>(undefined);
  const [entryLoaded, setEntryLoaded] = useState(false);

  // IDB lookup is async; refetch on every IDB write so the menu label
  // ("Download" / "Re-download" / "Retry") and disabled state stay in
  // sync with deletes / completes / status flips initiated anywhere
  // in the app. Cheap (single key lookup); UI tolerates one-render lag
  // on stale data.
  useEffect(() => {
    let cancelled = false;
    const reload = () => {
      void getEntry(sceneId).then((row) => {
        if (cancelled) return;
        setEntry(row);
        setEntryLoaded(true);
      });
    };
    reload();
    const unsubscribe = subscribeToEntries(reload);
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, [sceneId]);

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

    const snapshot: SceneSnapshot = {
      scene_id: sceneId,
      title: scene.title ?? "",
      details: scene.details ?? null,
      studio_name: scene.studio?.name ?? null,
      studio_id: scene.studio?.id ?? null,
      performers:
        scene.performers?.map((p) => ({ id: p.id, name: p.name })) ?? [],
      tags: scene.tags?.map((t) => ({ id: t.id, name: t.name })) ?? [],
      duration: file.duration ?? 0,
      width: file.width ?? 0,
      height: file.height ?? 0,
      date: scene.date ?? null,
      paths: {
        screenshot: scene.paths?.screenshot ?? null,
        preview: scene.paths?.preview ?? null,
        sprite: null,
        vtt: scene.paths?.vtt ?? null,
      },
      source_video_codec: videoCodec ?? "",
      source_audio_codec: audioCodec ?? "",
      source_file_path: file.path,
    };
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
    disabled: !entryLoaded
      ? false
      : status === "downloading" || status === "queued",
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
  const queue = useDownloadQueue();
  // HEVC / AV1 device-level decode capability — invariant across
  // scenes (probe is on the codec spec, not the source). Hooked once
  // here so the returned function is a sync setup → async kicker.
  const decodesHevc = useVideoCodecDecodableInMp4("hevc");
  const decodesAv1 = useVideoCodecDecodableInMp4("av1");
  const serverCaps = useServerCapabilities();
  const serverHevcAvailable = serverCaps.downloadFormats.includes("hevc");
  const serverAv1Available = serverCaps.downloadFormats.includes("av1");

  return useCallback(
    async (scenes: readonly SceneCardScene[]) => {
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
        const snapshot: SceneSnapshot = {
          scene_id: scene.id,
          title: scene.title ?? "",
          details: scene.details ?? null,
          studio_name: scene.studio?.name ?? null,
          studio_id: scene.studio?.id ?? null,
          performers:
            scene.performers?.map((p) => ({ id: p.id, name: p.name })) ?? [],
          tags: scene.tags?.map((t) => ({ id: t.id, name: t.name })) ?? [],
          duration: file.duration ?? 0,
          width: file.width ?? 0,
          height: file.height ?? 0,
          date: scene.date ?? null,
          paths: {
            screenshot: scene.paths?.screenshot ?? null,
            preview: scene.paths?.preview ?? null,
            sprite: null,
            vtt: scene.paths?.vtt ?? null,
          },
          source_video_codec: videoCodec ?? "",
          source_audio_codec: audioCodec ?? "",
          source_file_path: file.path,
        };
        await queue.enqueue({
          snapshot,
          mode: pick.mode,
          resolution: pick.resolution,
        });
      }
    },
    [queue, decodesHevc, decodesAv1, serverHevcAvailable, serverAv1Available],
  );
}
