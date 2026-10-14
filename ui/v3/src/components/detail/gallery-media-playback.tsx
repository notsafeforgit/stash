import { useCallback, useMemo, useState } from "react";
import { useMsg } from "@/hooks/message";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { MediaLightbox } from "@/components/lightbox/scene-lightbox";
import type { LightboxSlide, SceneSlide } from "@/components/lightbox";
import type { AlbumReadPage } from "@/components/archive/albums/read";
import { NativeArchiveError } from "@/core/native-archive/client";
import {
  GalleryMediaReadError,
  type GalleryMediaItem,
  type GalleryMediaPage,
} from "./gallery-media";

const PENDING: LightboxSlide = { src: "", loading: true };

export default function GalleryMediaPlayback({
  data,
  index: initialIndex,
  signature,
  busy,
  error,
  onMore,
  onClose,
}: {
  data?: AlbumReadPage<GalleryMediaItem, number, GalleryMediaPage>;
  index: number;
  signature: string;
  busy: boolean;
  error: unknown;
  onMore: () => Promise<void>;
  onClose: () => void;
}) {
  const msg = useMsg();
  const [index, setIndex] = useState(initialIndex);
  const changed =
    (data !== undefined && data.signature !== signature) ||
    (error instanceof NativeArchiveError && error.code === "album_changed");
  const slides = useMemo<(LightboxSlide | SceneSlide)[]>(() => {
    if (changed) return [PENDING];
    const items = (data?.items ?? []).map(
      (item): LightboxSlide | SceneSlide => {
        if (item.scene)
          return {
            type: "scene",
            sceneId: item.scene.id,
            title: item.scene.title ?? undefined,
            posterSrc: item.scene.paths.screenshot ?? undefined,
          };
        const image = item.image;
        return {
          src: image?.paths.image ?? image?.paths.preview ?? "",
          thumbnail: image?.paths.thumbnail ?? undefined,
          imageId: image?.id,
          imageTitle: image?.title ?? undefined,
          alt: image?.title ?? undefined,
          width: image?.visual_files[0]?.width,
          height: image?.visual_files[0]?.height,
        };
      },
    );
    return data?.next != null ? [...items, PENDING] : items;
  }, [data?.items, data?.next, changed]);
  const onView = useCallback(
    (next: number) => {
      if (changed) return;
      setIndex(next);
      // Reaching the boundary loads one page. Failed reads stay on that same
      // slot until Retry; swiping back never discards the already loaded list.
      if (
        next === data?.items.length &&
        data.next != null &&
        !busy &&
        error === undefined
      )
        void onMore();
    },
    [changed, data, busy, error, onMore],
  );
  const renderPending = useCallback(
    () => (
      <div className="max-w-lg p-4">
        {changed ? (
          <Alert>
            <AlertTitle>
              {msg("source_albums.changed", "This album changed")}
            </AlertTitle>
            <AlertDescription>
              {msg(
                "gallery_media.changed",
                "Close the viewer and reload the gallery before continuing.",
              )}
            </AlertDescription>
          </Alert>
        ) : error !== undefined ? (
          <GalleryMediaReadError error={error} retry={() => void onMore()} />
        ) : (
          <Spinner className="size-10 text-white/70" />
        )}
      </div>
    ),
    [changed, error, onMore, msg],
  );
  return (
    <MediaLightbox
      open
      finite
      slides={slides}
      index={changed ? 0 : index}
      totalCount={changed ? 0 : data?.header.count}
      onView={onView}
      onClose={onClose}
      renderPending={renderPending}
    />
  );
}
