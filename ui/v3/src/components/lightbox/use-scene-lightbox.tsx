import { useCallback, useEffect, useRef, useState } from "react";
import type * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import type { PageNavHandle } from "src/components/list";
import { SceneLightbox, type SceneSlide } from "./scene-lightbox";

type SceneItem = GQL.SlimSceneDataFragment;

const LOADING_SCENE_SLIDE: SceneSlide = {
  type: "scene",
  sceneId: "__loading__",
  loading: true,
};

function sceneToSlide(scene: SceneItem): SceneSlide {
  return {
    type: "scene",
    sceneId: scene.id,
    title: objectTitle(scene) || undefined,
    posterSrc: scene.paths.screenshot ?? undefined,
  };
}

function buildSceneSlides(
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
 * Paged video lightbox for scene list views. Mirrors useImageLightbox —
 * sentinel loading slides at boundaries trigger page loads via pageNavRef
 * when swiped or arrow-keyed to. Landing on a sentinel fires the adjacent
 * page load; onItemsChanged repopulates slides when that page arrives.
 */
export function useSceneLightbox() {
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
    (_item: SceneItem, allItems: SceneItem[], index: number) => {
      const nav = pageNavRef.current;
      const prevPage = nav ? nav.currentPage > 1 : false;
      const nextPage = nav ? nav.currentPage < nav.totalPages : false;
      setHasPrevPage(prevPage);
      setHasNextPage(nextPage);
      const slides = buildSceneSlides(
        allItems.map(sceneToSlide),
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

  const onItemsChanged = useCallback((items: SceneItem[]) => {
    const dir = pendingPageDirectionRef.current;
    if (!dir) return;
    pendingPageDirectionRef.current = null;
    const nav = pageNavRef.current;
    const prevPage = nav ? nav.currentPage > 1 : false;
    const nextPage = nav ? nav.currentPage < nav.totalPages : false;
    setHasPrevPage(prevPage);
    setHasNextPage(nextPage);
    const realSlides = items.map(sceneToSlide);
    const slides = buildSceneSlides(realSlides, prevPage, nextPage);
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
