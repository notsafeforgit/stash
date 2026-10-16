import { useEffect, useMemo, useRef, useState } from "react";
import {
  createModule,
  cssVar,
  MODULE_CAROUSEL,
  parseLengthPercentage,
  translateLabel,
  translateSlideCounter,
  useA11yContext,
  useController,
  useLightboxState,
  useForkRef,
  useRTL,
  ImageSlide,
  isImageSlide,
  useLightboxProps,
  type SlideImage,
  type ComponentProps,
  type Plugin,
} from "yet-another-react-lightbox";
import { cn } from "@/lib/utils";
import { SceneSlidePoster } from "./scene-slide-content";

/** Use the same public image renderer and Zoom wrappers as the image lightbox.
 * Only the current image mounts metadata controls; neighbors preload pixels. */
function ImageTrackSlide({
  slide,
  offset,
}: {
  slide: SlideImage;
  offset: number;
}) {
  const { render, carousel, on } = useLightboxProps();
  const { slideRect } = useController();
  const { currentIndex } = useLightboxState();
  const content = render.slide?.({ slide, offset, rect: slideRect }) ?? (
    <ImageSlide
      slide={slide}
      offset={offset}
      rect={slideRect}
      render={render}
      imageFit={carousel.imageFit}
      imageProps={carousel.imageProps}
      onClick={
        offset === 0 ? () => on.click?.({ index: currentIndex }) : undefined
      }
    />
  );
  return (
    <>
      {offset === 0 && render.slideHeader?.({ slide })}
      {render.slideContainer?.({ slide, children: content }) ?? content}
      {offset === 0 && render.slideFooter?.({ slide })}
    </>
  );
}

/**
 * YARL's default carousel keys slides by their position in the collection.
 * Keep keyed posters on the track and one permanent player above them. During
 * a swipe the player stays over its outgoing poster, retaining its decoded
 * frame and source until the track finishes moving. Only then do we center it
 * and change scenes. Rebuilding a media pipeline during that animation can stall
 * WebKit, and replacing the incoming poster can flash an undecoded image.
 *
 * The public module API, controller ref and carousel stylesheet retain
 * YARL's drag offsets, swipe animations, spacing and fullscreen behavior.
 * React remains the sole owner of the media DOM; no portals or reparenting.
 */
function SceneCarousel({ carousel, render, styles, labels }: ComponentProps) {
  const { slides, currentIndex, currentSlide, globalIndex, animation } =
    useLightboxState();
  const { setCarouselRef, slideRect, containerRect } = useController();
  const trackRef = useRef<HTMLDivElement>(null);
  const carouselRef = useForkRef(trackRef, setCarouselRef);
  const { autoPlaying, focusWithin } = useA11yContext();
  const rtl = useRTL();
  const spacing = parseLengthPercentage(carousel.spacing);
  const padding = parseLengthPercentage(carousel.padding);
  const step =
    containerRect.width +
    (spacing.pixel ?? (containerRect.width * (spacing.percent ?? 0)) / 100);
  const [settled, setSettled] = useState({
    slide: currentSlide,
    globalIndex,
    slides,
  });

  // The controller creates its WAAPI animation in a parent layout effect.
  // Observe it after that commit, and wait for actual completion rather than
  // a second guessed duration (or YARL's timer, which can precede the last
  // painted frame). Cleanup prevents an interrupted swipe loading its scene.
  // Reduced motion / missing WAAPI take the immediate, non-animated path.
  // biome-ignore lint/correctness/useExhaustiveDependencies: A replacement animation must invalidate the previous completion, including cancellation back to the same slide.
  useEffect(() => {
    let active = true;
    const commit = () => {
      if (active) {
        setSettled((previous) =>
          previous.slide === currentSlide &&
          previous.globalIndex === globalIndex &&
          previous.slides === slides
            ? previous
            : { slide: currentSlide, globalIndex, slides },
        );
      }
    };
    const animations = trackRef.current?.getAnimations?.() ?? [];
    if (animations.length) {
      void Promise.all(
        animations.map((running) => running.finished.catch(() => {})),
      ).then(commit);
    } else {
      commit();
    }
    return () => {
      active = false;
    };
  }, [currentSlide, globalIndex, slides, animation]);

  const current =
    settled.slide === currentSlide && settled.globalIndex === globalIndex;
  const [retainedScene, setRetainedScene] = useState(
    settled.slide?.type === "scene" ? settled.slide : undefined,
  );
  if (settled.slide?.type === "scene" && settled.slide !== retainedScene)
    setRetainedScene(settled.slide);
  // Preserve Safari's media element/grant through images, but release the old
  // source and transcode lease. The hidden player must never keep playing.
  const playerSlide = useMemo(
    () =>
      settled.slide?.type === "scene"
        ? settled.slide
        : retainedScene && { ...retainedScene, suspended: true },
    [settled.slide, retainedScene],
  );
  const playerOffset =
    (settled.globalIndex - globalIndex) * step * (rtl ? -1 : 1);
  // Gesture and animation context updates should not re-render the player.
  const player = useMemo(
    () =>
      playerSlide &&
      render.slide?.({ slide: playerSlide, offset: 0, rect: slideRect }),
    [playerSlide, render.slide, slideRect],
  );

  return (
    // biome-ignore lint/a11y/useSemanticElements: YARL requires an HTMLDivElement ref for its animated track.
    <div
      ref={carouselRef}
      className="yarl__carousel yarl__carousel_with_slides"
      style={{
        position: "relative",
        willChange: "transform",
        [cssVar("carousel_slides_count")]: 3,
        [cssVar("carousel_spacing_px")]: spacing.pixel ?? 0,
        [cssVar("carousel_spacing_percent")]: spacing.percent ?? 0,
        [cssVar("carousel_padding_px")]: padding.pixel ?? 0,
        [cssVar("carousel_padding_percent")]: padding.percent ?? 0,
      }}
      role="region"
      aria-live={autoPlaying && !focusWithin ? "off" : "polite"}
      aria-roledescription={translateLabel(labels, "Carousel")}
      aria-label={translateLabel(labels, "Photo gallery")}
    >
      {[-1, 0, 1].map((offset) => {
        const slideIndex = currentIndex + offset;
        const outside = slideIndex < 0 || slideIndex >= slides.length;
        const slide =
          slides.length === 0 || (carousel.finite && outside)
            ? undefined
            : slides[
                ((slideIndex % slides.length) + slides.length) % slides.length
              ];
        return (
          <div
            key={globalIndex + offset}
            className={cn(
              "yarl__slide yarl__flex_center",
              slide && isImageSlide(slide) && "image-lightbox",
              offset === 0 &&
                slide &&
                isImageSlide(slide) &&
                "yarl__slide_current",
            )}
            style={styles.slide}
            inert={offset !== 0 || slide?.type === "scene"}
            aria-hidden={offset !== 0 || slide?.type === "scene" || undefined}
          >
            {slide?.type === "scene" && <SceneSlidePoster slide={slide} />}
            {slide && isImageSlide(slide) && (
              <ImageTrackSlide slide={slide} offset={offset} />
            )}
          </div>
        );
      })}
      {/* This sibling never moves in the DOM, preserving Safari's media grant. */}
      {/* biome-ignore lint/a11y/useSemanticElements: A media slide is a group, not a form fieldset. */}
      <div
        className={cn(
          "yarl__slide yarl__flex_center lightbox-scene-slide",
          current && settled.slide?.type === "scene" && "yarl__slide_current",
        )}
        style={{
          ...styles.slide,
          position: "absolute",
          top: 0,
          bottom: 0,
          left: "50%",
          width: containerRect.width,
          marginLeft: -containerRect.width / 2,
          transform: playerOffset ? `translateX(${playerOffset}px)` : undefined,
          // A paginated list can replace the index space during a swipe.
          // Keep its player alive, but don't show unrelated retained media.
          visibility:
            settled.slide?.type !== "scene" ||
            (settled.slides !== slides && !current)
              ? "hidden"
              : undefined,
        }}
        inert={!current || settled.slide?.type !== "scene"}
        aria-hidden={!current || settled.slide?.type !== "scene" || undefined}
        role="group"
        aria-roledescription={translateLabel(labels, "Slide")}
        aria-label={translateSlideCounter(labels, slides, currentIndex)}
      >
        {player}
      </div>
    </div>
  );
}

export const PersistentSceneCarousel: Plugin = ({ replace }) => {
  replace(MODULE_CAROUSEL, createModule(MODULE_CAROUSEL, SceneCarousel));
};
