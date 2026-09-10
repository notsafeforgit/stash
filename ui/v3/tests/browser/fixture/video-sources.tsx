import { useCallback, useRef, useState, type RefObject } from "react";
import { Container, createPlayer } from "@videojs/react";
import { videoFeatures } from "@videojs/react/video";
import { HlsJsAdapter } from "@videojs/hlsjs-video";
import { SceneVideo } from "@/components/player/scene-video";
import { CanPlayEffect } from "@/components/player/player-overlays";
import {
  isClippedHls,
  isHlsPlaylist,
  parseStartPosition,
} from "@/components/player/hls";
import { Button } from "@/components/ui/button";

const Player = createPlayer({ features: videoFeatures });
const sourceURL = (path: string) => new URL(path, location.href).href;

function ResumeWhenReady({
  src,
  rootRef,
}: {
  src: string;
  rootRef: RefObject<HTMLDivElement | null>;
}) {
  const store = Player.usePlayer();
  const resume = useCallback(() => {
    void store.play();
  }, [store]);
  return <CanPlayEffect srcKey={src} rootRef={rootRef} onCanPlay={resume} />;
}

function PlaybackState() {
  const media = Player.useMedia();
  const state = Player.usePlayer((s) => ({
    time: s.currentTime,
    paused: s.paused,
    muted: s.muted,
  }));
  return (
    <output
      data-testid="playback-state"
      data-engine={
        media instanceof HlsJsAdapter && media.engine ? "hlsjs" : "native"
      }
      data-source={media instanceof HlsJsAdapter ? media.src : undefined}
    >
      {JSON.stringify(state)}
    </output>
  );
}

/** A real Video.js player with a tiny synthetic AVC/AAC file and fMP4 HLS. */
export function VideoSourcesFixture() {
  const videoRef = useRef<HTMLVideoElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const [src, setSrc] = useState(() => sourceURL("/media/audio.mp4"));
  const [advance, setAdvance] = useState(false);
  const [sequence, setSequence] = useState(0);
  const [loads, setLoads] = useState(0);
  const [mounted, setMounted] = useState(true);

  return (
    <>
      <Button
        onClick={() => {
          const video = videoRef.current;
          if (video) {
            video.muted = !video.muted;
            void video.play();
          }
        }}
      >
        Toggle sound
      </Button>
      <Button
        onClick={() => setSrc(sourceURL("/media/hls/stream.m3u8?start=4"))}
      >
        HLS at 4
      </Button>
      <Button
        onClick={() =>
          setSrc(sourceURL("/media/hls/stream.m3u8?quality=720&start=6"))
        }
      >
        HLS at 6
      </Button>
      <Button
        onClick={() => setSrc(sourceURL("/media/hls/stream.m3u8?reload=1"))}
      >
        Reload HLS at zero
      </Button>
      <Button onClick={() => setSrc(sourceURL("/media/audio.mp4?next=1"))}>
        Direct file
      </Button>
      <Button
        onClick={() =>
          setSrc(sourceURL("/media/clip/stream.m3u8?start=6&end=10"))
        }
      >
        Marker clip
      </Button>
      <Button onClick={() => setAdvance(true)}>Enable auto advance</Button>
      <Button onClick={() => setMounted(false)}>Unmount</Button>
      <output data-testid="sequence">{sequence}</output>
      <output data-testid="loads">{loads}</output>
      {mounted && (
        <Player.Player>
          <Container ref={rootRef}>
            <ResumeWhenReady src={src} rootRef={rootRef} />
            <SceneVideo
              ref={videoRef}
              src={src}
              sourceType="video/mp4"
              autoPlay
              muted
              playsInline
              preload="auto"
              onLoadedMetadata={(event) => {
                // Match ScenePlayer's pending-resume pre-positioning. WebKit
                // cannot reach canplay at time zero if only later HLS segments
                // were requested. Clip sources use media-relative time zero.
                if (isHlsPlaylist(src)) {
                  event.currentTarget.currentTime = isClippedHls(src)
                    ? 0
                    : Math.max(0, parseStartPosition(src));
                }
                setLoads((count) => count + 1);
              }}
              onEnded={() => {
                if (advance) {
                  setSequence(sequence + 1);
                  const path =
                    sequence % 2 === 0
                      ? "/media/hls/stream.m3u8"
                      : "/media/audio.mp4";
                  setSrc(sourceURL(`${path}?sequence=${sequence + 1}`));
                }
              }}
            />
            <PlaybackState />
          </Container>
        </Player.Player>
      )}
    </>
  );
}
