import { useState } from "react";
import { useQuery } from "@apollo/client/react";
import { FindImageDocument, FindSceneDocument } from "@/core/generated-graphql";
import { useMsg } from "@/hooks/message";
import { useConfigurationContextOptional } from "@/hooks/config";
import { ImageViewer } from "@/components/detail/image-viewer";
import { ScenePlayer } from "@/components/player/scene-player";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

function MediaUnavailable({ retry }: { retry: () => void }) {
  const msg = useMsg();
  return (
    <Alert className="max-w-md">
      <AlertTitle>
        {msg("album_playback.unavailable", "This media is unavailable")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {msg(
            "album_playback.unavailable_help",
            "It may have been removed or its file may be offline. The source position is retained.",
          )}
        </p>
        <Button variant="outline" onClick={retry}>
          {msg("actions.retry", "Retry")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

export function AlbumImage({ id }: { id: string }) {
  const result = useQuery(FindImageDocument, {
    variables: { id },
    fetchPolicy: "no-cache",
  });
  const image =
    result.data?.findImage?.id === id ? result.data.findImage : undefined;
  if (result.loading) return <Spinner />;
  if (
    result.error ||
    !image ||
    !image.visual_files.length ||
    (!image.paths.image && !image.paths.preview)
  )
    return (
      <MediaUnavailable retry={() => void result.refetch().catch(() => {})} />
    );
  return <ImageViewer image={image} actions={false} inline />;
}

export function AlbumVideo({
  id,
  playbackKey,
  onNext,
}: {
  id: string;
  playbackKey: string;
  onNext?: () => void;
}) {
  const configuration = useConfigurationContextOptional()?.configuration;
  const result = useQuery(FindSceneDocument, {
    variables: { id },
    fetchPolicy: "no-cache",
  });
  const scene =
    !result.error && result.data?.findScene?.id === id
      ? result.data.findScene
      : undefined;
  // Keep one player through adjacent videos and suspend it immediately while
  // another entity is loading. No old response may play under the next slot.
  const [retained, setRetained] = useState(scene);
  if (scene && scene !== retained) setRetained(scene);
  const ready =
    !!scene?.files.length && !!scene.sceneStreams.length && !result.loading;
  const current = scene ?? retained;
  return (
    <div className="relative size-full">
      {current && (
        <ScenePlayer
          scene={current}
          playbackKey={playbackKey}
          suspended={!ready}
          activityScope={{
            kind: "online-scene",
            sceneId: id,
            visitKey: playbackKey,
          }}
          fill
          autoplay
          autostartEnabled={configuration?.interface.autostartVideo ?? true}
          enablePinchZoom
          onNext={onNext}
        />
      )}
      {!ready && (
        <div className="absolute inset-0 flex items-center justify-center bg-background p-4">
          {result.loading ? (
            <Spinner />
          ) : (
            <MediaUnavailable
              retry={() => void result.refetch().catch(() => {})}
            />
          )}
        </div>
      )}
    </div>
  );
}
