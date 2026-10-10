import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  AlbumImage,
  AlbumVideo,
} from "@/components/archive/albums/playback-media";
import type { AlbumReadPage } from "@/components/archive/albums/read";
import { NativeArchiveError } from "@/core/native-archive/client";
import {
  galleryMediaKey,
  GalleryMediaReadError,
  type GalleryMediaItem,
  type GalleryMediaPage,
} from "./gallery-media";

export default function GalleryMediaPlayback({
  gallery,
  data,
  index: initialIndex,
  signature,
  busy,
  error,
  onMore,
  onClose,
}: {
  gallery: string;
  data?: AlbumReadPage<GalleryMediaItem, number, GalleryMediaPage>;
  index: number;
  signature: string;
  busy: boolean;
  error: unknown;
  onMore: () => Promise<void>;
  onClose: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [index, setIndex] = useState(initialIndex);
  const latestIndex = useCommittedRef(index);
  const item = data?.items[index];
  const changed =
    (data !== undefined && data.signature !== signature) ||
    (error instanceof NativeArchiveError && error.code === "album_changed");
  const hasNext =
    !!item && (index + 1 < (data?.items.length ?? 0) || data?.next != null);
  function next() {
    if (latestIndex.current !== index || changed || busy || !hasNext) return;
    setIndex(index + 1);
    if (!data?.items[index + 1]) void onMore();
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="flex h-[90dvh] max-w-[calc(100%-1rem)] flex-col sm:max-w-6xl">
        <DialogHeader className="shrink-0 pr-8">
          <DialogTitle>
            {msg("gallery_media.viewer", "Gallery viewer")}
          </DialogTitle>
          <DialogDescription>
            {msg(
              "gallery_media.viewer_help",
              "Browse images and play scenes in this gallery's order.",
            )}
          </DialogDescription>
        </DialogHeader>
        {changed ? (
          <Alert>
            <AlertTitle>
              {msg("source_albums.changed", "This album changed")}
            </AlertTitle>
            <AlertDescription>
              <p>
                {msg(
                  "gallery_media.changed",
                  "Close the viewer and reload the gallery before continuing.",
                )}
              </p>
              <Button variant="outline" onClick={onClose}>
                {msg("actions.close", "Close")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <div
              className="flex min-h-0 flex-1 items-center justify-center overflow-hidden rounded-lg bg-muted"
              data-gallery-viewer-index={index}
            >
              {item?.scene ? (
                <AlbumVideo
                  id={item.scene.id}
                  playbackKey={`gallery:${gallery}:${signature}:${index}:${galleryMediaKey(item)}`}
                  onNext={hasNext ? next : undefined}
                />
              ) : item?.image ? (
                <AlbumImage key={item.image.id} id={item.image.id} />
              ) : busy ? (
                <Spinner />
              ) : error !== undefined ? (
                <GalleryMediaReadError
                  error={error}
                  retry={() => void onMore()}
                />
              ) : (
                <p>
                  {msg(
                    "album_playback.unavailable",
                    "This media is unavailable",
                  )}
                </p>
              )}
            </div>
            <div className="flex shrink-0 flex-col gap-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p
                  className="min-w-0 flex-1 wrap-anywhere text-sm"
                  aria-live="polite"
                >
                  {intl.formatMessage(
                    {
                      id: "gallery_media.position",
                      defaultMessage: "{position, number} of {count, number}",
                    },
                    { position: index + 1, count: data?.header.count ?? 0 },
                  )}
                  {(item?.scene?.title || item?.image?.title) &&
                    ` · ${item?.scene?.title || item?.image?.title}`}
                </p>
                {item?.scene ? (
                  <Link
                    to="/scenes/$sceneId"
                    params={{ sceneId: item.scene.id }}
                  >
                    {msg("source_albums.open_media", "Open media")}
                  </Link>
                ) : (
                  item?.image && (
                    <Link
                      to="/images/$imageId"
                      params={{ imageId: item.image.id }}
                    >
                      {msg("source_albums.open_media", "Open media")}
                    </Link>
                  )
                )}
              </div>
              <div className="flex items-center justify-between gap-2">
                <Button
                  variant="outline"
                  disabled={index === 0 || !data}
                  onClick={() => setIndex(index - 1)}
                >
                  <ChevronLeft data-icon="inline-start" />
                  {msg("actions.previous", "Previous")}
                </Button>
                <Button
                  variant="outline"
                  disabled={!hasNext || busy}
                  onClick={next}
                >
                  {msg("actions.next", "Next")}
                  <ChevronRight data-icon="inline-end" />
                </Button>
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
