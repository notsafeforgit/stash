import { MockedProvider } from "@apollo/client/testing/react";
import { useState } from "react";
import { Container, createPlayer } from "@videojs/react";
import { Video, videoFeatures } from "@videojs/react/video";
import { Button } from "@/components/ui/button";
import { PlayerControls } from "@/components/player/player-controls";
import type { PlayerSource } from "@/components/player/player-utils";
import { SceneLightbox } from "@/components/lightbox/scene-lightbox";
import { useIsTouch } from "@/utils/screen";
import "@/components/player/player.css";

const Player = createPlayer({ features: videoFeatures });
const sources: PlayerSource[] = [
  { src: "/player.mp4", type: "video/mp4", label: "Direct stream" },
  { src: "/player.mp4?quality=720", type: "video/mp4", label: "MP4 HD (720p)" },
];
const noop = () => {};

// A tiny synthetic gray MP4 exercises real media state without library data,
// GraphQL requests, activity tracking, or remote media dependencies.
export function PlayerFixture() {
  const [open, setOpen] = useState(false);
  const [source, setSource] = useState(sources[0] ?? null);
  const [mode, setMode] = useState<"normal" | "advance" | "loop">("normal");
  const touch = useIsTouch();
  const loading = new URLSearchParams(location.search).has("loading");
  const close = () => setOpen(false);
  return (
    <>
      <Button onClick={() => setOpen(true)}>Open player</Button>
      {loading ? (
        <MockedProvider>
          <SceneLightbox
            open={open}
            onClose={close}
            slides={[{ type: "scene", sceneId: "pending", loading: true }]}
          />
        </MockedProvider>
      ) : open ? (
        <div
          data-testid="player"
          data-scene-player
          className="fixed inset-0 bg-black"
        >
          <Player.Player>
            <Container className="absolute inset-0">
              <Video
                src={source?.src}
                autoPlay
                muted
                loop
                playsInline
                className="size-full object-contain"
              />
              <PlayerControls
                Player={Player}
                sources={sources}
                activeSource={source}
                onSourceChange={setSource}
                markers={[]}
                fileDuration={3661}
                offsetStart={0}
                onSeek={noop}
                playbackMode={mode}
                canAdvance
                onCyclePlaybackMode={() =>
                  setMode(
                    mode === "normal"
                      ? "advance"
                      : mode === "advance"
                        ? "loop"
                        : "normal",
                  )
                }
                onClose={touch ? close : undefined}
                onTemporaryPlaybackRateChange={noop}
              />
            </Container>
          </Player.Player>
        </div>
      ) : null}
    </>
  );
}
