import { useMemo, useRef, useState } from "react";
import { MediaDetailLayout } from "@/components/detail/media-detail-layout";
import { ScenePlayer } from "@/components/player/scene-player";
import { offlineEntryToSceneData } from "@/components/offline/offline-scene-adapter";
import { ConfigurationProvider } from "@/hooks/config";
import { useMediaQuery } from "@/utils/screen";
import { playerConfiguration } from "./player-configuration";

export function SceneDetailFixture() {
  const [focused, setFocused] = useState(false);
  const viewerButtonRef = useRef<HTMLButtonElement>(null);
  const desktop = useMediaQuery("(min-width: 1024px)");
  const scene = useMemo(() => {
    const params = new URLSearchParams(location.search);
    const hls = params.has("hls");
    const landscape = hls || params.has("landscape");
    const scene = offlineEntryToSceneData(
      {
        scene_id: "detail",
        title: "Example scene",
        studio_name: null,
        studio_id: null,
        performers: [],
        tags: [],
        duration: hls && !params.has("short") ? 600 : 12,
        width: landscape ? 160 : 90,
        height: landscape ? 90 : 160,
        date: null,
        paths: { screenshot: null, preview: null, sprite: null, vtt: null },
        format: "h264",
        source_video_codec: "h264",
        source_audio_codec: hls ? "" : "aac",
        resolution: "ORIGINAL",
        width_actual: landscape ? 160 : 90,
        height_actual: landscape ? 90 : 160,
        bytes: 100000,
        downloaded_at: 1,
        status: "complete",
        opfs_path: "fixture",
        server_status: "present",
      },
      new URL("/scene/detail/stream", location.href).href,
    );
    if (hls) {
      scene.sceneStreams = [
        {
          url: new URL("/scene/detail/stream.master.m3u8", location.href).href,
          mime_type: "application/vnd.apple.mpegurl",
          label: "HLS",
        },
      ];
    }
    return scene;
  }, []);

  return (
    <ConfigurationProvider configuration={playerConfiguration}>
      <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
        <div className="h-12 shrink-0 border-b">App header</div>
        <MediaDetailLayout
          title={scene.title ?? undefined}
          mobilePageScroll
          primaryFocusMode={focused}
          onClosePrimaryFocus={() => setFocused(false)}
          primaryFocusReturnRef={viewerButtonRef}
          onBack={() => {}}
          primaryContent={
            <ScenePlayer
              scene={scene}
              autostartEnabled={false}
              fill={focused}
              enablePinchZoom={desktop || focused}
              viewerOpen={focused}
              onToggleViewer={() => setFocused(!focused)}
              viewerButtonRef={viewerButtonRef}
            />
          }
          tabs={[
            {
              id: "details",
              label: "Details",
              content: (
                <div className="h-300">
                  <p>Scene details below the player</p>
                </div>
              ),
            },
          ]}
        />
      </div>
    </ConfigurationProvider>
  );
}
