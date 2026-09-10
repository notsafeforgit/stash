import { useEffect, useMemo, useState } from "react";
import { MockedProvider } from "@apollo/client/testing/react";
import type { MockedResponse } from "@apollo/client/testing";
import {
  SceneLightbox,
  type SceneSlide,
} from "@/components/lightbox/scene-lightbox";
import { offlineEntryToSceneData } from "@/components/offline/offline-scene-adapter";
import type { OfflineEntry } from "@/components/offline/offline-db";
import { ConfigurationProvider } from "@/hooks/config";
import { Button } from "@/components/ui/button";
import * as GQL from "@/core/generated-graphql";
import { playerConfiguration } from "./player-configuration";

const entry: OfflineEntry = {
  scene_id: "1",
  title: "Scene 1",
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
};
const url = (path: string) => new URL(path, location.href).href;
const scenes = ["1", "2", "3", "slow"].map((id): GQL.SceneDataFragment => {
  const scene = offlineEntryToSceneData(
    { ...entry, scene_id: id, title: `Scene ${id}` },
    url(`/scene/${id}/stream`),
  );
  return {
    ...scene,
    files: scene.files.map((file) => ({
      ...file,
      frame_rate: 30,
      video_stream_duration: 12,
      frame_count: 360,
      bit_depth: 8,
      color_range: null,
      color_space: null,
      color_transfer: null,
      color_primaries: null,
    })),
    sceneStreams: [
      ...(id === "2"
        ? []
        : [
            {
              url: url(`/scene/${id}/stream`),
              mime_type: "video/mp4",
              label: "Direct stream",
            },
          ]),
      {
        url: url(`/scene/${id}/stream.master.m3u8?resolution=LOW`),
        mime_type: "application/vnd.apple.mpegurl",
        label: "HLS (240p)",
      },
    ],
    scene_markers: [6, 8, 2].map((seconds, index) => ({
      __typename: "SceneMarker",
      id: `marker-${index}`,
      title: `Marker ${index + 1}`,
      seconds,
      end_seconds: index === 2 ? 10 : seconds + 2,
      scene,
      primary_tag: { id: "tag", name: "Example" },
      tags: [],
      screenshot: "",
      stream: "",
      preview: "",
      created_at: "",
      updated_at: "",
    })),
    captions: [{ language_code: "en", caption_type: "srt" }],
    paths: { ...scene.paths, caption: url(`/scene/${id}/caption`) },
  };
});
const mocks: MockedResponse<GQL.FindSceneQuery>[] = scenes.map((scene) => ({
  request: { query: GQL.FindSceneDocument, variables: { id: scene.id } },
  result: { data: { findScene: scene } },
  delay: scene.id === "slow" ? 1200 : 0,
  maxUsageCount: Number.POSITIVE_INFINITY,
}));
mocks.push({
  request: { query: GQL.FindSceneDocument, variables: { id: "missing" } },
  result: { data: { findScene: null } },
  delay: 0,
  maxUsageCount: Number.POSITIVE_INFINITY,
});

export function SceneLightboxFixture() {
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const [resolved, setResolved] = useState(false);
  const params = new URLSearchParams(location.search);
  const mode = params.get("mode");
  const autostart = !params.has("paused");
  const configuration = useMemo(
    () => ({
      ...playerConfiguration,
      interface: {
        ...playerConfiguration.interface,
        autostartVideo: autostart,
      },
    }),
    [autostart],
  );
  useEffect(() => {
    if (mode !== "pending" || index !== 1) return;
    const timer = setTimeout(() => setResolved(true), 800);
    return () => clearTimeout(timer);
  }, [mode, index]);
  const slides = useMemo<SceneSlide[]>(() => {
    if (mode === "long-marker")
      return [
        {
          type: "scene",
          sceneId: "2",
          marker: {
            id: "marker-2",
            title: "Long marker",
            seconds: 2,
            primaryTag: { id: "tag", name: "Example" },
            tags: [],
          },
        },
      ];
    if (mode === "markers")
      return [6, 8].map((seconds, i) => ({
        type: "scene",
        sceneId: "2",
        marker: {
          id: `marker-${i}`,
          title: `Marker ${i + 1}`,
          seconds,
          primaryTag: { id: "tag", name: "Example" },
          tags: [],
        },
      }));
    const ids =
      mode === "slow" ? ["1", "slow", "3", "missing"] : ["1", "2", "3"];
    return ids.map((sceneId, i) => ({
      type: "scene",
      sceneId,
      title: `Scene ${sceneId}`,
      loading: mode === "pending" && i === 1 && !resolved,
    }));
  }, [mode, resolved]);
  return (
    <MockedProvider mocks={mocks}>
      <ConfigurationProvider configuration={configuration}>
        <Button
          onClick={() => {
            setIndex(0);
            setOpen(true);
          }}
        >
          Open scenes
        </Button>
        <output data-testid="view">{index}</output>
        <SceneLightbox
          open={open}
          onClose={() => setOpen(false)}
          slides={slides}
          index={index}
          onView={setIndex}
          finite={mode === "pending" || mode === "slow"}
        />
      </ConfigurationProvider>
    </MockedProvider>
  );
}
