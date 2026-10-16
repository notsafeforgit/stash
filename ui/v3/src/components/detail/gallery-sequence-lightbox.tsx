import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { useMsg } from "@/hooks/message";
import { MediaLightbox } from "@/components/lightbox/scene-lightbox";
import type { LightboxSlide, SceneSlide } from "@/components/lightbox";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { NativeArchiveError } from "@/core/native-archive/client";
import { PostEmpty } from "@/components/archive/posts/shared";
import { GalleryMediaReadError } from "./gallery-media";
import {
  GallerySequence,
  type GallerySequenceStart,
  type GallerySequencePage,
  type GalleryDirection,
} from "./gallery-sequence";

const pending: LightboxSlide = { src: "", loading: true };

export default function GallerySequenceLightbox({
  start,
  onClose,
}: {
  start: GallerySequenceStart;
  onClose: () => void;
}) {
  const client = useApolloClient();
  const msg = useMsg();
  const [view, setView] = useState<{
    page: GallerySequencePage;
    index: number;
  }>();
  const [error, setError] = useState<unknown>();
  const session = useRef<{
    reader: GallerySequence;
    controller: AbortController;
  } | null>(null);
  const loading = useRef(false);
  const navigation = useRef(0);
  const moving = useRef(false);
  const retry = useRef<() => void>(() => {});

  useEffect(() => {
    const controller = new AbortController();
    const reader = new GallerySequence(client, start, controller.signal);
    session.current = { reader, controller };
    const load = () => {
      loading.current = true;
      setError(undefined);
      void reader
        .first()
        .then(({ page, index }) => {
          if (!controller.signal.aborted) setView({ page, index: index + 1 });
        })
        .catch((error: unknown) => {
          if (!controller.signal.aborted) setError(error);
        })
        .finally(() => {
          if (!controller.signal.aborted) loading.current = false;
        });
    };
    retry.current = load;
    load();
    return () => {
      controller.abort();
      session.current = null;
    };
  }, [client, start]);

  const move = useCallback(
    (page: GallerySequencePage, direction: GalleryDirection) => {
      const active = session.current;
      if (!active || loading.current) return;
      const load = () => {
        const request = ++navigation.current;
        moving.current = true;
        loading.current = true;
        setError(undefined);
        void active.reader
          .adjacent(page, direction)
          .then((next) => {
            if (
              !active.controller.signal.aborted &&
              request === navigation.current
            )
              setView({
                page: next,
                index: direction === "next" ? 1 : next.media.items.length,
              });
          })
          .catch((error: unknown) => {
            if (
              !active.controller.signal.aborted &&
              request === navigation.current
            )
              setError(error);
          })
          .finally(() => {
            if (
              !active.controller.signal.aborted &&
              request === navigation.current
            ) {
              loading.current = false;
              moving.current = false;
            }
          });
      };
      retry.current = load;
      load();
    },
    [],
  );

  const page = view?.page;
  const slides = useMemo<(LightboxSlide | SceneSlide)[]>(
    () =>
      page?.media.items.length
        ? [
            pending,
            ...page.media.items.map((item): LightboxSlide | SceneSlide =>
              item.scene
                ? {
                    type: "scene",
                    sceneId: item.scene.id,
                    title: item.scene.title ?? undefined,
                    posterSrc: item.scene.paths.screenshot ?? undefined,
                  }
                : {
                    src:
                      item.image?.paths.image ??
                      item.image?.paths.preview ??
                      "",
                    imageId: item.image?.id,
                    imageTitle: item.image?.title ?? undefined,
                    alt: item.image?.title ?? undefined,
                    thumbnail: item.image?.paths.thumbnail ?? undefined,
                    width: item.image?.visual_files[0]?.width,
                    height: item.image?.visual_files[0]?.height,
                  },
            ),
            pending,
          ]
        : [pending],
    [page],
  );

  const onView = useCallback(
    (index: number) => {
      if (!page?.media.items.length) return;
      setView((value) => (value?.page === page ? { ...value, index } : value));
      if (index === 0 || index === slides.length - 1) {
        if (error === undefined) move(page, index === 0 ? "previous" : "next");
      } else {
        // A swipe back into the current page cancels navigation intent. Keep
        // the request cached, but its late result must not move the viewer.
        if (moving.current) {
          navigation.current++;
          moving.current = false;
          loading.current = false;
        }
        if (error !== undefined) setError(undefined);
        if (index >= slides.length - 3) {
          // Warm only the adjacent page, never preload the full gallery/library.
          void session.current?.reader.adjacent(page, "next").catch(() => {});
        }
      }
    },
    [page, slides.length, error, move],
  );

  const renderPending = useCallback(
    () => (
      <div className="max-w-lg p-4">
        {error instanceof NativeArchiveError &&
        error.code === "album_changed" ? (
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
          <GalleryMediaReadError error={error} retry={() => retry.current()} />
        ) : page?.media.count === 0 ? (
          <PostEmpty
            title={msg(
              "gallery_media.empty",
              "No images or scenes in this gallery",
            )}
          />
        ) : (
          <Spinner className="size-10 text-white/70" />
        )}
      </div>
    ),
    [error, page?.media.count, msg],
  );

  return (
    <MediaLightbox
      open
      finite
      slides={slides}
      index={view?.index ?? 0}
      totalCount={page?.media.count}
      pageStartIndex={(page?.offset ?? 0) - 1}
      counterLabel={page?.gallery.title ?? undefined}
      slideshowAutoplay={start.autoplay}
      onView={onView}
      onClose={onClose}
      renderPending={renderPending}
    />
  );
}
