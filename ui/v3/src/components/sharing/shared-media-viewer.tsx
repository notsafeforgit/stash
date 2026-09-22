import { useEffect, useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { Download, Maximize } from "lucide-react";
import {
  ScenePlayer,
  type ScenePlaybackData,
} from "@/components/player/scene-player";
import { PlayerTranscodeSession } from "@/components/player/player-transcode-session";
import { LightboxScenePlayer } from "@/components/lightbox/lightbox-scene-player";
import type { SceneLightboxSlideProps } from "@/components/lightbox/scene-lightbox";
import { ImageViewer } from "@/components/detail/image-viewer";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import type { SharedDetail, SharedMedia } from "./share-contract";
import { useSharedMedia } from "./use-shared-media";

export function SharedPlayback({
  detail,
  base,
  suspended = false,
  lightbox,
  onControlsVisibilityChange,
}: {
  detail: SharedDetail;
  base: URL;
  suspended?: boolean;
  lightbox?: Omit<SceneLightboxSlideProps, "slide" | "isActive">;
  onControlsVisibilityChange?: (visible: boolean) => void;
}) {
  const endpoint = new URL(`media/${detail.media.key}/`, base).href;
  const [lease, setLease] = useState(() => ({
    endpoint,
    session: new PlayerTranscodeSession(
      detail.media.key,
      crypto.randomUUID(),
      new URL(endpoint),
    ),
  }));
  if (lease.endpoint !== endpoint) {
    setLease({
      endpoint,
      session: new PlayerTranscodeSession(
        detail.media.key,
        crypto.randomUUID(),
        new URL(endpoint),
      ),
    });
  }
  useEffect(() => () => lease.session.dispose(), [lease]);
  useEffect(() => {
    if (suspended) lease.session.selectSource(undefined);
  }, [lease, suspended]);
  const scene = useMemo<ScenePlaybackData>(
    () => ({
      id: detail.media.key,
      title: detail.media.title,
      resume_time: 0,
      files: [
        {
          path: "",
          width: detail.media.width,
          height: detail.media.height,
          duration: detail.media.duration,
          frame_rate: detail.frame_rate,
          video_codec: detail.video_codec,
          audio_codec: detail.audio_codec,
          updated_at: "",
        },
      ],
      paths: { screenshot: detail.media.thumbnail, caption: null },
      sceneStreams: detail.streams.map((stream) => ({
        __typename: "SceneStreamEndpoint",
        ...stream,
        url: new URL(stream.url, base).href,
      })),
      scene_markers: [],
      captions: [],
      performers: [],
      studio: null,
      preview_image: null,
    }),
    [detail, base],
  );
  const common = {
    scene,
    transcodeSession: lease.session,
    castingAllowed: false,
    activityScope: { kind: "disabled" },
    suspended,
    playbackKey: detail.media.key,
  } as const;
  return lightbox ? (
    <LightboxScenePlayer
      {...common}
      {...lightbox}
      onControlsVisibilityChange={onControlsVisibilityChange}
      autostartEnabled
    />
  ) : (
    <ScenePlayer
      {...common}
      autoplay={false}
      autostartEnabled={false}
      enablePinchZoom
      fill
    />
  );
}

export function ShareDownload({ media }: { media: SharedMedia }) {
  const intl = useIntl();
  if (!media.download) return null;
  return (
    <Button
      variant="outline"
      nativeButton={false}
      role="link"
      render={<a href={media.download} download />}
    >
      <Download data-icon="inline-start" />
      {intl.formatMessage({
        id: "sharing.download_original",
        defaultMessage: "Download original",
      })}
    </Button>
  );
}

export function SharedMediaStatus({ loading }: { loading: boolean }) {
  const intl = useIntl();
  return loading ? (
    <Spinner />
  ) : (
    <Alert className="max-w-sm">
      <AlertDescription>
        {intl.formatMessage({
          id: "sharing.media_unavailable",
          defaultMessage: "This item is no longer available.",
        })}
      </AlertDescription>
    </Alert>
  );
}

/** A leaf detail view contains only the media and its grant's actions. */
export function SharedMediaViewer({
  media,
  base,
  suspended,
  onOpenViewer,
}: {
  media: SharedMedia;
  base: URL;
  suspended: boolean;
  onOpenViewer: () => void;
}) {
  const intl = useIntl();
  const state = useSharedMedia(media.key, base);
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <div className="relative flex h-[65dvh] min-h-64 items-center justify-center overflow-hidden rounded-lg bg-black">
        {state.kind !== "ready" ? (
          <SharedMediaStatus loading={state.kind === "loading"} />
        ) : state.detail.media.video ? (
          <SharedPlayback
            detail={state.detail}
            base={base}
            suspended={suspended}
          />
        ) : (
          <ImageViewer
            actions={false}
            onOpenViewer={onOpenViewer}
            image={{
              title: state.detail.media.title,
              paths: { image: state.detail.media.image, preview: null },
              visual_files: [
                {
                  path: "",
                  width: state.detail.media.width,
                  height: state.detail.media.height,
                },
              ],
            }}
          />
        )}
      </div>
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="outline" onClick={onOpenViewer}>
          <Maximize data-icon="inline-start" />
          {intl.formatMessage({
            id: "sharing.open_viewer",
            defaultMessage: "Open viewer",
          })}
        </Button>
        <ShareDownload media={media} />
      </div>
    </section>
  );
}
