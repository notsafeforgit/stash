import { useCallback, useEffect, useRef, useState } from "react";
import type * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import type { PageNavHandle } from "src/components/list";
import { SceneLightbox, type SceneSlide } from "./scene-lightbox";

type MarkerItem = GQL.SceneMarkerDataFragment;

const LOADING_SCENE_SLIDE: SceneSlide = {
  type: "scene",
  sceneId: "__loading__",
  loading: true,
};

function markerTitle(m: MarkerItem): string {
  if (m.title) return m.title;
  const sceneTitle = objectTitle(m.scene);
  return sceneTitle
    ? `${sceneTitle} — ${m.primary_tag.name}`
    : m.primary_tag.name;
}

function markerToSlide(m: MarkerItem): SceneSlide {
  return {
    type: "scene",
    sceneId: m.scene.id,
    title: markerTitle(m),
    posterSrc: m.screenshot ?? undefined,
    marker: {
      id: m.id,
      title: markerTitle(m),
      seconds: m.seconds,
      primaryTag: { id: m.primary_tag.id, name: m.primary_tag.name },
      tags: m.tags.map((t) => ({ id: t.id, name: t.name })),
    },
  };
}

function buildMarkerSlides(
  real: SceneSlide[],
  hasPrev: boolean,
  hasNext: boolean,
): SceneSlide[] {
  return [
    ...(hasPrev ? [LOADING_SCENE_SLIDE] : []),
    ...real,
    ...(hasNext ? [LOADING_SCENE_SLIDE] : []),
  ];
}

/**
 * Paged video lightbox for marker list views. Mirrors useSceneLightbox: each
 * slide is a scene-type lightbox slide carrying the marker context, so the
 * embedded ScenePlayer starts at the marker timestamp and the overlay shows
 * the marker's title + primary tag + tags. Sentinel slides at boundaries
 * trigger adjacent page loads via pageNavRef when swiped to.
 */
export function useMarkerLightbox() {
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [lightboxSlides, setLightboxSlides] = useState<SceneSlide[]>([]);
  const [lightboxIndex, setLightboxIndex] = useState(0);
  const [hasPrevPage, setHasPrevPage] = useState(false);
  const [hasNextPage, setHasNextPage] = useState(false);
  const pageNavRef = useRef<PageNavHandle | null>(null);
  const pendingPageDirectionRef = useRef<"forward" | "backward" | null>(null);

  const lightboxSlidesRef = useRef(lightboxSlides);
  const hasPrevPageRef = useRef(hasPrevPage);
  const hasNextPageRef = useRef(hasNextPage);
  useEffect(() => {
    lightboxSlidesRef.current = lightboxSlides;
  }, [lightboxSlides]);
  useEffect(() => {
    hasPrevPageRef.current = hasPrevPage;
  }, [hasPrevPage]);
  useEffect(() => {
    hasNextPageRef.current = hasNextPage;
  }, [hasNextPage]);

  const onCardPreviewClick = useCallback(
    (_item: MarkerItem, allItems: MarkerItem[], index: number) => {
      const nav = pageNavRef.current;
      const prevPage = nav ? nav.currentPage > 1 : false;
      const nextPage = nav ? nav.currentPage < nav.totalPages : false;
      setHasPrevPage(prevPage);
      setHasNextPage(nextPage);
      const slides = buildMarkerSlides(
        allItems.map(markerToSlide),
        prevPage,
        nextPage,
      );
      setLightboxSlides(slides);
      setLightboxIndex(index + (prevPage ? 1 : 0));
      setLightboxOpen(true);
    },
    [],
  );

  const onLightboxView = useCallback((index: number) => {
    const slides = lightboxSlidesRef.current;
    const nav = pageNavRef.current;
    if (!nav || pendingPageDirectionRef.current) return;

    const atEndSentinel =
      hasNextPageRef.current &&
      index === slides.length - 1 &&
      slides[index]?.loading;
    const atStartSentinel =
      hasPrevPageRef.current && index === 0 && slides[0]?.loading;

    if (atEndSentinel) {
      pendingPageDirectionRef.current = "forward";
      setHasNextPage(false);
      nav.nextPage();
    } else if (atStartSentinel) {
      pendingPageDirectionRef.current = "backward";
      setHasPrevPage(false);
      nav.prevPage();
    }
  }, []);

  const onItemsChanged = useCallback((items: MarkerItem[]) => {
    const dir = pendingPageDirectionRef.current;
    if (!dir) return;
    pendingPageDirectionRef.current = null;
    const nav = pageNavRef.current;
    const prevPage = nav ? nav.currentPage > 1 : false;
    const nextPage = nav ? nav.currentPage < nav.totalPages : false;
    setHasPrevPage(prevPage);
    setHasNextPage(nextPage);
    const realSlides = items.map(markerToSlide);
    const slides = buildMarkerSlides(realSlides, prevPage, nextPage);
    setLightboxSlides(slides);
    setLightboxIndex(
      dir === "forward"
        ? prevPage
          ? 1
          : 0
        : slides.length - 1 - (nextPage ? 1 : 0),
    );
  }, []);

  const lightboxElement = lightboxOpen ? (
    <SceneLightbox
      open
      onClose={() => setLightboxOpen(false)}
      slides={lightboxSlides}
      index={lightboxIndex}
      onView={onLightboxView}
      finite={lightboxSlides.some((s) => s.loading)}
    />
  ) : null;

  return {
    onCardPreviewClick,
    onItemsChanged,
    pageNavRef,
    lightboxElement,
    lightboxOpen,
  };
}
