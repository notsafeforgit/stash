import type { SceneSnapshot } from "./use-download-queue";

/** Capabilities read by downloads. Both mobile/list and detail query projections
 * satisfy this contract without pretending they fetched each other's fields. */
export interface DownloadableScene {
  id: string;
  title?: string | null;
  details?: string | null;
  date?: string | null;
  studio?: { id: string; name: string } | null;
  performers?: readonly { id: string; name: string }[];
  tags?: readonly { id: string; name: string }[];
  paths: {
    screenshot?: string | null;
    preview?: string | null;
    vtt?: string | null;
  };
  files: readonly {
    path: string;
    duration?: number | null;
    width?: number | null;
    height?: number | null;
    video_codec?: string | null;
    audio_codec?: string | null;
  }[];
}

export function sceneDownloadSnapshot(
  scene: DownloadableScene,
): SceneSnapshot | undefined {
  const file = scene.files[0];
  if (!file) return undefined;
  return {
    scene_id: scene.id,
    title: scene.title ?? "",
    details: scene.details ?? null,
    studio_name: scene.studio?.name ?? null,
    studio_id: scene.studio?.id ?? null,
    performers: (scene.performers ?? []).map(({ id, name }) => ({ id, name })),
    tags: (scene.tags ?? []).map(({ id, name }) => ({ id, name })),
    duration: file.duration ?? 0,
    width: file.width ?? 0,
    height: file.height ?? 0,
    date: scene.date ?? null,
    paths: {
      screenshot: scene.paths.screenshot ?? null,
      preview: scene.paths.preview ?? null,
      sprite: null,
      vtt: scene.paths.vtt ?? null,
    },
    source_video_codec: file.video_codec ?? "",
    source_audio_codec: file.audio_codec ?? "",
    source_file_path: file.path,
  };
}
