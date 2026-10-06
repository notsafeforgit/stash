import type {
  ImageDataFragment,
  SceneDataFragment,
} from "../../src/core/generated-graphql";
import { offlineEntryToSceneData } from "../../src/components/offline/offline-scene-adapter";

export function playbackImage(id = "7"): ImageDataFragment {
  const src = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="900" height="1200"><rect width="900" height="1200" fill="#264653"/><circle cx="450" cy="400" r="250" fill="#2a9d8f"/></svg>')}`;
  return {
    __typename: "Image",
    id,
    title: "First image",
    code: null,
    date: null,
    urls: [],
    details: null,
    photographer: null,
    rating100: null,
    organized: false,
    o_counter: 0,
    preview_image: null,
    paths: {
      __typename: "ImagePathsType",
      thumbnail: src,
      preview: src,
      image: src,
    },
    galleries: [],
    studio: null,
    tags: [],
    performers: [],
    created_at: "2026-10-06T00:00:00Z",
    updated_at: "2026-10-06T00:00:00Z",
    custom_fields: {},
    visual_files: [
      {
        __typename: "ImageFile",
        id: "17",
        path: "/fixture/image.svg",
        size: 1000,
        mod_time: "2026-10-06T00:00:00Z",
        width: 900,
        height: 1200,
        bit_depth: 8,
        color_range: null,
        color_space: null,
        color_transfer: null,
        color_primaries: null,
        fingerprints: [],
      },
    ],
  };
}
export function playbackScene(base: string, id = "8"): SceneDataFragment {
  const scene = offlineEntryToSceneData(
    {
      scene_id: id,
      title: "Second video",
      studio_name: null,
      studio_id: null,
      performers: [],
      tags: [],
      duration: 12,
      width: 160,
      height: 90,
      date: null,
      paths: { screenshot: null, preview: null, sprite: null, vtt: null },
      format: "h264",
      source_video_codec: "h264",
      source_audio_codec: "aac",
      resolution: "ORIGINAL",
      width_actual: 160,
      height_actual: 90,
      bytes: 100000,
      downloaded_at: 1,
      status: "complete",
      opfs_path: "fixture",
      server_status: "present",
    },
    new URL(`/scene/${id}/stream`, base).href,
  );
  return {
    ...scene,
    files: scene.files.map((file) => ({
      ...file,
      frame_rate: 30,
      video_stream_duration: 12,
      frame_count: 360,
    })),
  };
}
