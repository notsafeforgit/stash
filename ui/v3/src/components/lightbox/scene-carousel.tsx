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
  type ComponentProps,
  type Plugin,
} from "yet-another-react-lightbox";
import { cn } from "@/lib/utils";

/**
 * YARL's default carousel keys slides by their position in the collection.
 * This three-slot carousel instead owns one permanent center slot, with
 * posters on either side. Changing the current scene updates the center's
 * props without replacing its player or native video element.
 *
 * The public module API, controller ref and carousel stylesheet retain
 * YARL's drag offsets, swipe animations, spacing and fullscreen behavior.
 * React remains the sole owner of the media DOM; no portals or reparenting.
 */
function SceneCarousel({ carousel, render, styles, labels }: ComponentProps) {
  const { slides, currentIndex } = useLightboxState();
  const { setCarouselRef, slideRect } = useController();
  const { autoPlaying, focusWithin } = useA11yContext();
  const spacing = parseLengthPercentage(carousel.spacing);
  const padding = parseLengthPercentage(carousel.padding);

  return (
    // biome-ignore lint/a11y/useSemanticElements: YARL requires an HTMLDivElement ref for its animated track.
    <div
      ref={setCarouselRef}
      className="yarl__carousel yarl__carousel_with_slides"
      style={{
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
          // biome-ignore lint/a11y/useSemanticElements: A media slide is a group, not a form fieldset.
          <div
            key={offset}
            className={cn(
              "yarl__slide yarl__flex_center",
              offset === 0 && "yarl__slide_current",
            )}
            style={styles.slide}
            inert={offset !== 0}
            aria-hidden={offset !== 0 || undefined}
            role="group"
            aria-roledescription={translateLabel(labels, "Slide")}
            aria-label={translateSlideCounter(labels, slides, slideIndex)}
          >
            {slide && render.slide?.({ slide, offset, rect: slideRect })}
          </div>
        );
      })}
    </div>
  );
}

export const PersistentSceneCarousel: Plugin = ({ replace }) => {
  replace(MODULE_CAROUSEL, createModule(MODULE_CAROUSEL, SceneCarousel));
};
