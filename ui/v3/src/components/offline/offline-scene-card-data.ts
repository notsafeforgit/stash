/**
 * Adapter from `OfflineEntry` (the IDB row written at download time)
 * to `SceneCardScene` (the shape `<SceneCard>` consumes). Reuses the
 * existing card UI without forking it for the offline view.
 *
 * Fields the card consumes that the entry doesn't snapshot get
 * filled with safe defaults:
 *   - rating100, details: null/undefined (won't render rating overlay)
 *   - tags / performers / groups / scene_markers / galleries:
 *     snapshotted lists for what we care about, empty for what we
 *     don't (groups / scene_markers / galleries aren't shown on cards
 *     by default and aren't worth carrying through OPFS metadata for
 *     phase 1)
 *   - paths.preview / paths.vtt / paths.interactive_heatmap:
 *     snapshot's URLs (remote — fall back to placeholder when the
 *     PWA is offline; SceneCard handles a missing screenshot
 *     gracefully).
 *   - files: a single synthetic entry with the snapshotted source
 *     dimensions / duration / codecs. The card uses this for the
 *     resolution badge and aspect-ratio hint.
 */

import type { SceneCardScene } from "src/components/cards/scene-card";
import type { OfflineEntry } from "./offline-db";

export function offlineEntryToSceneCardScene(
  entry: OfflineEntry,
): SceneCardScene {
  return {
    __typename: "Scene",
    id: entry.scene_id,
    title: entry.title,
    date: entry.date ?? null,
    details: null,
    rating100: null,
    resume_time: entry.last_position_seconds ?? 0,
    paths: {
      screenshot: entry.paths.screenshot,
      preview: entry.paths.preview,
      vtt: entry.paths.vtt,
      interactive_heatmap: null,
    },
    studio: entry.studio_id
      ? {
          __typename: "Studio",
          id: entry.studio_id,
          name: entry.studio_name ?? "",
          image_path: null,
        }
      : null,
    files: [
      {
        id: `${entry.scene_id}-offline`,
        // Prefer the snapshot of the source file's server path so
        // `objectTitle` falls back to the source filename for
        // untitled scenes (matches the streaming view). Old entries
        // without that snapshot fall back to the OPFS path, whose
        // stem is the scene id.
        path: entry.source_file_path ?? entry.opfs_path,
        duration: entry.duration,
        width: entry.width_actual || entry.width,
        height: entry.height_actual || entry.height,
        size: entry.bytes,
        video_codec: entry.source_video_codec,
        audio_codec: entry.source_audio_codec,
      },
    ],
    performers: entry.performers.map((p) => ({
      __typename: "Performer",
      id: p.id,
      name: p.name,
      gender: null,
      favorite: false,
      image_path: null,
    })) as SceneCardScene["performers"],
    tags: entry.tags.map((t) => ({
      __typename: "Tag",
      id: t.id,
      name: t.name,
    })) as SceneCardScene["tags"],
  };
}
