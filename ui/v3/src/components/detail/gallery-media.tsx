import { useCallback, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { Link } from "@tanstack/react-router";
import { Images, Play, MoreHorizontal } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  FindGalleryMediaDocument,
  type FindGalleryMediaQuery,
} from "@/core/generated-graphql";
import { Button } from "@/components/ui/button";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Spinner } from "@/components/ui/spinner";
import { PostEmpty } from "@/components/archive/posts/shared";
import { AlbumReadError } from "@/components/archive/albums/ordered";
import { useAlbumPages } from "@/components/archive/albums/read";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import { useGalleryCover } from "./use-gallery-cover";
import { deferredOverlay } from "@/components/shared/deferred-overlay";
import { LightboxPending } from "@/components/lightbox/lightbox-pending";

const GalleryMediaPlayback = deferredOverlay(
  () => import("./gallery-media-playback"),
  (props) => ({ open: true, onClose: props.onClose }),
  LightboxPending,
);
export type GalleryMediaPage = NonNullable<
  FindGalleryMediaQuery["findGallery"]
>["media"];
export type GalleryMediaItem = GalleryMediaPage["items"][number];

export function galleryMediaKey(item: GalleryMediaItem) {
  return item.scene ? `scene:${item.scene.id}` : `image:${item.image?.id}`;
}

export function GalleryMediaReadError({
  error,
  retry,
}: {
  error: unknown;
  retry: () => void;
}) {
  const msg = useMsg();
  if (error instanceof NativeArchiveError && error.code === "album_changed")
    return <AlbumReadError error={error} retry={retry} />;
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {msg("gallery_media.error", "Could not load gallery media")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {msg(
            "source_posts.failed_help",
            "Check your connection and access to Stash, then retry.",
          )}
        </p>
        <Button variant="outline" onClick={retry}>
          {msg("actions.retry", "Retry")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

export function GalleryMedia({ id }: { id: string }) {
  const msg = useMsg();
  const client = useApolloClient();
  const { setCover, pending: coverPending } = useGalleryCover(id);
  const load = useCallback(
    async (offset: number | undefined, signal: AbortSignal) => {
      const response = await client.query({
        query: FindGalleryMediaDocument,
        variables: { id, offset: offset ?? 0 },
        fetchPolicy: "no-cache",
        // Each page owns its cancellation. A StrictMode remount must not reuse
        // the already-aborted request from the previous effect.
        context: { queryDeduplication: false, fetchOptions: { signal } },
      });
      const page = response.data?.findGallery?.media;
      if (!page) throw new Error("Gallery media unavailable");
      return {
        items: page.items,
        signature: page.signature,
        next: page.next_offset ?? null,
        header: page,
      };
    },
    [client, id],
  );
  const result = useAlbumPages(load);
  const [playback, setPlayback] = useState<{
    index: number;
    signature: string;
  } | null>(null);
  const items = result.data?.items ?? [];

  return (
    <div className="flex min-h-0 flex-col gap-4 p-4 md:flex-1 md:overflow-y-auto">
      <p className="text-sm text-muted-foreground">
        {msg(
          "gallery_media.order",
          "Images and scenes appear together in source order where known, followed by other gallery items in file order.",
        )}
      </p>
      {result.error !== undefined && (
        <GalleryMediaReadError error={result.error} retry={result.reload} />
      )}
      {result.busy && (
        <Spinner
          aria-label={msg("gallery_media.loading", "Loading gallery media")}
        />
      )}
      {!result.busy && result.error === undefined && items.length === 0 && (
        <PostEmpty
          title={msg(
            "gallery_media.empty",
            "No images or scenes in this gallery",
          )}
        />
      )}
      {items.length > 0 && (
        <Button
          className="self-start"
          variant="outline"
          onClick={() =>
            setPlayback({ index: 0, signature: result.data!.signature })
          }
        >
          <Play data-icon="inline-start" />
          {msg("gallery_media.view", "View gallery")}
        </Button>
      )}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-4">
        {items.map((item, index) => {
          const media = item.scene ?? item.image;
          if (!media) return null;
          const title =
            media.title ||
            `${item.scene ? msg("scene", "Scene") : msg("image", "Image")} #${media.id}`;
          const thumbnail =
            item.scene?.paths.screenshot ?? item.image?.paths.thumbnail;
          return (
            <div
              key={galleryMediaKey(item)}
              className="min-w-0"
              data-gallery-media={galleryMediaKey(item)}
            >
              <Button
                variant="ghost"
                className="relative h-auto w-full overflow-hidden p-0"
                aria-label={title}
                onClick={() =>
                  setPlayback({ index, signature: result.data!.signature })
                }
              >
                {thumbnail ? (
                  <img
                    src={thumbnail}
                    alt=""
                    loading="lazy"
                    decoding="async"
                    draggable={false}
                    className="aspect-square w-full object-contain"
                  />
                ) : (
                  <span className="flex aspect-square w-full items-center justify-center">
                    <Images />
                  </span>
                )}
                {item.scene && (
                  <span
                    className="pointer-events-none absolute bottom-2 left-2 rounded-full bg-background/80 p-1.5 text-foreground"
                    aria-hidden="true"
                  >
                    <Play className="size-4" />
                  </span>
                )}
              </Button>
              <div className="flex items-start gap-1 pt-1">
                <div className="min-w-0 flex-1 text-sm">
                  {item.scene ? (
                    <Link
                      className="line-clamp-2 break-words"
                      to="/scenes/$sceneId"
                      params={{ sceneId: media.id }}
                    >
                      {title}
                    </Link>
                  ) : (
                    <Link
                      className="line-clamp-2 break-words"
                      to="/images/$imageId"
                      params={{ imageId: media.id }}
                    >
                      {title}
                    </Link>
                  )}
                </div>
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={msg(
                          "gallery_media.actions",
                          "Media actions",
                        )}
                      />
                    }
                  >
                    <MoreHorizontal />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent>
                    <DropdownMenuGroup>
                      <DropdownMenuItem
                        disabled={coverPending}
                        onClick={() =>
                          void setCover(
                            item.scene ? "scene" : "image",
                            media.id,
                          )
                        }
                      >
                        {msg(
                          "actions.set_as_gallery_cover",
                          "Set as gallery cover",
                        )}
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>
          );
        })}
      </div>
      {result.data?.next != null && (
        <Button
          variant="outline"
          disabled={result.busy || result.error !== undefined}
          onClick={() => void result.more()}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
      {playback && (
        <GalleryMediaPlayback
          data={result.data}
          {...playback}
          busy={result.busy}
          error={result.error}
          onMore={result.more}
          onClose={() => setPlayback(null)}
        />
      )}
    </div>
  );
}
