import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { Download, ChevronLeft, ChevronRight } from "lucide-react";
import {
  ScenePlayer,
  type ScenePlaybackData,
} from "@/components/player/scene-player";
import { PlayerTranscodeSession } from "@/components/player/player-transcode-session";
import { ImageViewer } from "@/components/detail/image-viewer";
import { Button, buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  shareDetailSchema,
  shareRequest,
  type SharedDetail,
} from "./share-contract";

function SharedPlayback({ detail, base }: { detail: SharedDetail; base: URL }) {
  const [session] = useState(
    () =>
      new PlayerTranscodeSession(
        detail.media.key,
        crypto.randomUUID(),
        new URL(`media/${detail.media.key}/`, base),
      ),
  );
  useEffect(() => () => session.dispose(), [session]);
  const scene: ScenePlaybackData = {
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
  };
  return (
    <ScenePlayer
      scene={scene}
      transcodeSession={session}
      castingAllowed={false}
      activityScope={{ kind: "disabled" }}
      autoplay={false}
      autostartEnabled={false}
      enablePinchZoom
      fill
    />
  );
}

export function SharedMediaViewer({
  mediaKey,
  base,
  previous,
  next,
}: {
  mediaKey: string;
  base: URL;
  previous?: () => void;
  next?: () => void;
}) {
  const intl = useIntl();
  const [state, setState] = useState<
    | { kind: "loading" }
    | { kind: "ready"; detail: SharedDetail }
    | { kind: "error" }
  >({ kind: "loading" });
  useEffect(() => {
    const abort = new AbortController();
    void shareRequest(
      new URL(`media/${mediaKey}/`, base),
      shareDetailSchema,
      abort.signal,
    ).then(
      (detail) => {
        if (!abort.signal.aborted) setState({ kind: "ready", detail });
      },
      () => {
        if (!abort.signal.aborted) setState({ kind: "error" });
      },
    );
    return () => abort.abort();
  }, [mediaKey, base]);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex min-h-80 h-[65dvh] items-center justify-center overflow-hidden rounded-lg bg-black">
        {state.kind === "loading" && <Spinner />}
        {state.kind === "error" && (
          <Alert>
            <AlertDescription>
              {intl.formatMessage({
                id: "sharing.media_unavailable",
                defaultMessage: "This item is no longer available.",
              })}
            </AlertDescription>
          </Alert>
        )}
        {state.kind === "ready" &&
          (state.detail.media.video ? (
            <SharedPlayback detail={state.detail} base={base} />
          ) : (
            <ImageViewer
              actions={false}
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
          ))}
      </div>
      <div className="flex items-center justify-between gap-3">
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="icon"
            disabled={!previous}
            onClick={previous}
            aria-label={intl.formatMessage({
              id: "sharing.previous",
              defaultMessage: "Previous item",
            })}
          >
            <ChevronLeft />
          </Button>
          <Button
            variant="outline"
            size="icon"
            disabled={!next}
            onClick={next}
            aria-label={intl.formatMessage({
              id: "sharing.next",
              defaultMessage: "Next item",
            })}
          >
            <ChevronRight />
          </Button>
        </div>
        {state.kind === "ready" && (
          <p className="min-w-0 flex-1 truncate">{state.detail.media.title}</p>
        )}
        {state.kind === "ready" && state.detail.media.download && (
          <a
            className={buttonVariants({ variant: "outline" })}
            href={state.detail.media.download}
            download
          >
            <Download data-icon="inline-start" />
            {intl.formatMessage({
              id: "sharing.download_original",
              defaultMessage: "Download original",
            })}
          </a>
        )}
      </div>
    </div>
  );
}
