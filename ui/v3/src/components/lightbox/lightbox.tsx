import "yet-another-react-lightbox/styles.css";
import "yet-another-react-lightbox/plugins/thumbnails.css";

import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import YARLightbox, {
  type RenderSlideProps,
  type RenderSlideFooterProps,
  type RenderSlideHeaderProps,
  type SlideshowRef,
  type ZoomRef,
  useLightboxState,
} from "yet-another-react-lightbox";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import Thumbnails from "yet-another-react-lightbox/plugins/thumbnails";
import Slideshow from "yet-another-react-lightbox/plugins/slideshow";
import Fullscreen from "yet-another-react-lightbox/plugins/fullscreen";
import { useFragment, useMutation } from "@apollo/client/react";
import { useImageOCounter } from "src/hooks/use-image-o-counter";
import { useIsTruncated } from "src/hooks/use-is-truncated";
import {
  LightboxOverlay,
  LightboxDate,
  LightboxDetails,
} from "./lightbox-overlay";
import { lightboxIconRenders } from "./lightbox-icons";
import { useIntl } from "react-intl";
import {
  Settings2Icon,
  DropletsIcon,
  ExternalLinkIcon,
  Maximize2Icon,
  Minimize2Icon,
  RotateCwIcon,
  RotateCcwIcon,
  Trash2Icon,
} from "lucide-react";
import { Link } from "@tanstack/react-router";
import type { LinkProps } from "@tanstack/react-router";
import * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
import { galleryLabel } from "src/lib/gallery-utils";
import { cn } from "src/lib/utils";
import { Spinner } from "src/components/ui/spinner";
import { Button } from "src/components/ui/button";
import { Switch } from "src/components/ui/switch";
import { NumberInput } from "src/components/filters/number-input";
import { RatingSystem } from "src/components/ui/rating-system";
import {
  DeleteDialog,
  DeleteFilesList,
  type DeleteOptions,
} from "src/components/detail/delete-dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "src/components/ui/tooltip";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "src/components/ui/popover";
import { useLightboxHistory } from "./use-lightbox-history";

// ── Module augmentation ────────────────────────────────────────────────────────

declare module "yet-another-react-lightbox" {
  interface GenericSlide {
    imageId?: string;
    imageTitle?: string;
  }
}

// ── Types ──────────────────────────────────────────────────────────────────────

export type DisplayMode = "fitXY" | "fitX" | "original";

export interface LightboxSlide {
  src: string;
  alt?: string;
  width?: number;
  height?: number;
  imageId?: string;
  imageTitle?: string;
  filePaths?: string[];
  /** Transient placeholder shown while the next/prev page is loading. */
  loading?: boolean;
}

// ── Zoom plugin tuning ─────────────────────────────────────────────────────────
// Shared with the inline image viewer on the image detail page so the
// pinch / wheel zoom feel is identical between the list lightbox and the
// single-image detail viewer.

export const LIGHTBOX_ZOOM_TUNING = {
  maxZoomPixelRatio: 4,
  pinchZoomV4: true,
  wheelZoomDistanceFactor: 50,
} as const;

// ── Settings ───────────────────────────────────────────────────────────────────

interface LightboxSettings {
  scrollToZoom: boolean;
  displayMode: DisplayMode;
  slideshowDelay: number;
}

const SETTINGS_KEY = "stash_lightbox_settings";
const DEFAULT_SETTINGS: LightboxSettings = {
  scrollToZoom: false,
  displayMode: "fitXY",
  slideshowDelay: 5,
};

function loadSettings(): LightboxSettings {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    if (!raw) return DEFAULT_SETTINGS;
    return { ...DEFAULT_SETTINGS, ...JSON.parse(raw) };
  } catch {
    return DEFAULT_SETTINGS;
  }
}

function persistSettings(s: LightboxSettings) {
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
}

// ── Custom slide renderers ─────────────────────────────────────────────────────

function FitXSlide({ slide }: { slide: LightboxSlide }) {
  return (
    <div className="w-full h-full overflow-y-auto overflow-x-hidden flex justify-center items-start">
      <img
        src={slide.src}
        alt={slide.alt ?? slide.imageTitle ?? ""}
        className="w-full h-auto max-w-full select-none"
        draggable={false}
      />
    </div>
  );
}

function OriginalSlide({ slide }: { slide: LightboxSlide }) {
  const ref = useRef<HTMLDivElement>(null);

  // YARL's `.yarl__container` sets `touch-action: none` so its swipe-to-navigate
  // controller can claim every pointer/wheel event — but that means our
  // overflow-auto container never gets a chance to actually pan around the
  // larger-than-viewport image. We need to:
  //   1. restore touch-action so touch gestures scroll the container natively;
  //   2. set overscroll-behavior: contain so a touch swipe past the edge
  //      doesn't chain back into YARL's swipe controller;
  //   3. intercept wheel events (trackpad two-finger pans) and stop
  //      propagation when the container can still scroll in the gesture's
  //      direction. Once the container is at its edge, we let the wheel
  //      event reach YARL so its `useWheelSwipe` can navigate to the next
  //      slide as usual.
  function handleWheel(e: React.WheelEvent<HTMLDivElement>) {
    const el = ref.current;
    if (!el) return;
    const { deltaX, deltaY } = e;
    // Slack to swallow sub-pixel reports from trackpads.
    const eps = 0.5;
    const horizontal = Math.abs(deltaX) > Math.abs(deltaY);
    const canConsume = horizontal
      ? (deltaX > 0 && el.scrollLeft + el.clientWidth < el.scrollWidth - eps) ||
        (deltaX < 0 && el.scrollLeft > eps)
      : (deltaY > 0 &&
          el.scrollTop + el.clientHeight < el.scrollHeight - eps) ||
        (deltaY < 0 && el.scrollTop > eps);
    if (canConsume) e.stopPropagation();
  }

  return (
    <div
      ref={ref}
      onWheel={handleWheel}
      className="w-full h-full overflow-auto flex justify-start items-start touch-pan-x touch-pan-y overscroll-contain"
    >
      <img
        src={slide.src}
        alt={slide.alt ?? slide.imageTitle ?? ""}
        style={{
          width: "auto",
          height: "auto",
          maxWidth: "none",
          maxHeight: "none",
        }}
        className="select-none"
        draggable={false}
      />
    </div>
  );
}

// ── Image entity footer ────────────────────────────────────────────────────────

/** Link that shows a tooltip with its full text when its rendered width is truncated. */
function TruncatingLink({
  text,
  className,
  children,
  ...linkProps
}: LinkProps & {
  text: string;
  className?: string;
  children: React.ReactNode;
}) {
  const [ref, truncated] = useIsTruncated<HTMLAnchorElement>();
  return (
    <Tooltip disabled={!truncated}>
      <TooltipTrigger
        render={
          <Link
            {...linkProps}
            ref={ref}
            className={className}
            target="_blank"
            rel="noreferrer"
          >
            {children}
          </Link>
        }
      />
      <TooltipContent>{text}</TooltipContent>
    </Tooltip>
  );
}

interface DeleteTarget {
  imageId: string;
  title?: string;
  filePaths: string[];
}

function isRealSlide(slide: LightboxSlide | undefined): boolean {
  return !!slide && !slide.loading;
}

function LightboxCounter({
  pageStartIndex,
  totalCount,
}: {
  pageStartIndex: number;
  totalCount?: number;
}) {
  const { slides, currentIndex } = useLightboxState();
  const typedSlides = slides as LightboxSlide[];
  const currentSlide = typedSlides[currentIndex];
  if (!isRealSlide(currentSlide)) return null;

  const leadingSentinel = typedSlides[0]?.loading ? 1 : 0;
  const localRealIndex = currentIndex - leadingSentinel;
  const realCount = typedSlides.filter(isRealSlide).length;
  const displayTotal = totalCount ?? realCount;
  if (localRealIndex < 0 || localRealIndex >= realCount || displayTotal <= 1) {
    return null;
  }

  return (
    <LightboxOverlay
      position="top"
      className="items-start gap-0 pt-3 pb-8 pointer-events-none"
    >
      <span className="rounded bg-black/45 px-2 py-1 text-xs font-medium tabular-nums text-white/85 backdrop-blur-sm">
        {pageStartIndex + localRealIndex + 1} / {displayTotal}
      </span>
    </LightboxOverlay>
  );
}

function LightboxDeleteShortcut({
  disabled,
  onRequestDelete,
}: {
  disabled: boolean;
  onRequestDelete: (target: DeleteTarget) => void;
}) {
  const { slides, currentIndex } = useLightboxState();
  const lastDKeyTime = useRef(0);

  useEffect(() => {
    if (disabled) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key.toLocaleLowerCase() !== "d") return;
      if (e.defaultPrevented) return;
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable)
      ) {
        return;
      }

      const now = Date.now();
      const slide = (slides as LightboxSlide[])[currentIndex];
      if (now - lastDKeyTime.current < 1000 && slide?.imageId) {
        e.preventDefault();
        onRequestDelete({
          imageId: slide.imageId,
          title: slide.imageTitle || slide.alt,
          filePaths: slide.filePaths ?? [],
        });
      }
      lastDKeyTime.current = now;
    }

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [currentIndex, disabled, onRequestDelete, slides]);

  return null;
}

function ImageEntityFooter({
  imageId,
  onRequestDelete,
}: {
  imageId: string;
  onRequestDelete?: (target: DeleteTarget) => void;
}) {
  const intl = useIntl();

  const { data, complete } = useFragment({
    fragment: GQL.SlimImageDataFragmentDoc,
    fragmentName: "SlimImageData",
    from: { __typename: "Image", id: imageId },
  });

  const { incrementO } = useImageOCounter(imageId);

  const [updateImage] = useMutation(GQL.ImageUpdateDocument);

  if (!complete) return null;

  const image = data as GQL.SlimImageDataFragment;
  const oCounter = image.o_counter ?? 0;
  const rating100 = image.rating100 ?? null;
  const galleries = image.galleries ?? [];
  const performers = image.performers ?? [];
  const title = imageTitle(image);
  const details = image.details ?? null;
  const filePaths = image.visual_files.map((f) => f.path);

  return (
    <LightboxOverlay position="bottom">
      {/* Title — `pointer-events-auto` on the link itself (not the
          flex row) so gaps past the truncated link text fall through
          to YARL's tap handler instead of being absorbed by the row. */}
      {title && (
        <div className="flex items-center gap-1.5 min-w-0">
          <TruncatingLink
            to="/images/$imageId"
            params={{ imageId }}
            className="text-sm font-medium hover:underline truncate pointer-events-auto"
            text={title}
          >
            {title}
          </TruncatingLink>
          <ExternalLinkIcon className="size-3 shrink-0 opacity-60" />
          {onRequestDelete && (
            <Button
              variant="ghost"
              size="icon-xs"
              className="pointer-events-auto h-6 w-6 shrink-0 text-red-300 hover:bg-red-500/15 hover:text-red-200"
              title={intl.formatMessage({
                id: "actions.delete",
                defaultMessage: "Delete",
              })}
              onClick={() =>
                onRequestDelete({
                  imageId,
                  title,
                  filePaths,
                })
              }
            >
              <Trash2Icon className="size-3.5" />
            </Button>
          )}
        </div>
      )}

      <LightboxDate date={image.date} />

      {/* Galleries — see Title note. */}
      {galleries.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {galleries.map((g) => {
            const label = galleryLabel(g);
            return (
              <TruncatingLink
                key={g.id}
                to="/galleries/$galleryId"
                params={{ galleryId: g.id }}
                className="text-xs bg-white/15 hover:bg-white/25 rounded px-1.5 py-0.5 truncate max-w-[200px] pointer-events-auto"
                text={label}
              >
                {label}
              </TruncatingLink>
            );
          })}
        </div>
      )}

      {/* Performers — see Title note. */}
      {performers.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {performers.map((p) => (
            <TruncatingLink
              key={p.id}
              to="/performers/$performerId"
              params={{ performerId: p.id }}
              className="text-xs bg-white/15 hover:bg-white/25 rounded px-1.5 py-0.5 truncate max-w-[200px] pointer-events-auto"
              text={p.name}
            >
              {p.name}
            </TruncatingLink>
          ))}
        </div>
      )}

      {/* Details — sits between performers and the rating/O-counter row
          so the action buttons remain physically last (tab order +
          mobile YARL toolbar clearance, see note below). */}
      {details && <LightboxDetails text={details} position="bottom" />}

      {/* Rating + O-counter — stacked on mobile so the O button (last
          in tab order) clears the YARL toolbar that sits at bottom-right
          on small screens. Inline on >= md where the toolbar is back at
          its default top-right. `pointer-events-auto` lives on the
          interactive children (RatingSystem stars + the O button), not
          the row container, so taps in the gap between them fall
          through to YARL's tap handler. */}
      <div className="flex flex-col items-start gap-2 md:flex-row md:flex-wrap md:items-center md:gap-4">
        <div className="pointer-events-auto">
          <RatingSystem
            value={rating100}
            onSetRating={(v) =>
              updateImage({
                variables: { input: { id: imageId, rating100: v } },
              })
            }
          />
        </div>

        <Button
          variant="outline"
          className="h-auto bg-transparent px-2 py-1 text-[0.8125rem] gap-1 text-white/80 hover:text-white border-white/20 hover:bg-white/10 pointer-events-auto"
          onClick={() => incrementO()}
          title={intl.formatMessage({
            id: "actions.increment_o",
            defaultMessage: "Add O",
          })}
        >
          <DropletsIcon size={14} />
          {oCounter}
        </Button>
      </div>
    </LightboxOverlay>
  );
}

// ── Settings button ────────────────────────────────────────────────────────────

interface SettingsButtonProps {
  settings: LightboxSettings;
  onSettingsChange: (s: LightboxSettings) => void;
}

function SettingsButton({ settings, onSettingsChange }: SettingsButtonProps) {
  const intl = useIntl();

  function update(partial: Partial<LightboxSettings>) {
    const next = { ...settings, ...partial };
    onSettingsChange(next);
    persistSettings(next);
  }

  return (
    <Popover>
      <PopoverTrigger
        className="yarl__button"
        aria-label={intl.formatMessage({
          id: "actions.settings",
          defaultMessage: "Settings",
        })}
      >
        <Settings2Icon className="yarl__icon" />
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        align="end"
        className="w-72"
        positionerClassName="z-[10001]"
      >
        <div className="flex flex-col gap-3 text-sm">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {intl.formatMessage({
              id: "lightbox.settings",
              defaultMessage: "Lightbox settings",
            })}
          </p>

          {/* Scroll to zoom */}
          <label className="flex items-center justify-between gap-2 cursor-pointer">
            <span>
              {intl.formatMessage({
                id: "lightbox.scroll_to_zoom",
                defaultMessage: "Scroll to zoom",
              })}
            </span>
            <Switch
              checked={settings.scrollToZoom}
              onCheckedChange={(v) => update({ scrollToZoom: v })}
              disabled={settings.displayMode !== "fitXY"}
            />
          </label>

          {/* Display mode */}
          <div className="flex flex-col gap-1.5">
            <span className="text-xs text-muted-foreground">
              {intl.formatMessage({
                id: "dialogs.lightbox.display_mode.label",
                defaultMessage: "Display mode",
              })}
            </span>
            <div className="flex rounded-md border border-input overflow-hidden">
              {(["fitXY", "fitX", "original"] as DisplayMode[]).map((mode) => (
                <Button
                  key={mode}
                  variant="ghost"
                  size="xs"
                  className={cn(
                    "h-auto rounded-none px-3 py-1 text-xs whitespace-nowrap font-normal",
                    settings.displayMode === mode
                      ? "bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground"
                      : "bg-background text-foreground hover:bg-muted",
                  )}
                  onClick={() => update({ displayMode: mode })}
                >
                  {mode === "fitXY"
                    ? intl.formatMessage({
                        id: "dialogs.lightbox.display_mode.fit_to_screen",
                        defaultMessage: "Fit to screen",
                      })
                    : mode === "fitX"
                      ? intl.formatMessage({
                          id: "dialogs.lightbox.display_mode.fit_horizontally",
                          defaultMessage: "Fit horizontally",
                        })
                      : intl.formatMessage({
                          id: "dialogs.lightbox.display_mode.original",
                          defaultMessage: "Original",
                        })}
                </Button>
              ))}
            </div>
          </div>

          {/* Slideshow delay */}
          <div className="flex flex-col gap-1.5">
            <span className="text-xs text-muted-foreground">
              {intl.formatMessage({
                id: "lightbox.slideshow_delay",
                defaultMessage: "Slideshow (s)",
              })}
            </span>
            <NumberInput
              value={settings.slideshowDelay}
              onChange={(v) => update({ slideshowDelay: Math.max(1, v) })}
              min={1}
            />
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

// ── Original size button ───────────────────────────────────────────────────────

// ── Rotate button ──────────────────────────────────────────────────────────────
//
// Lives in the toolbar; reads the *currently displayed* slide via YARL's
// `useLightboxState` hook so a single button instance services every slide.
// Hidden when the active slide has no `imageId` (sentinels, single-slide
// detail viewers without DB-backed images).

function LightboxRotateButton({
  direction,
  onRotate,
}: {
  direction: GQL.ImageRotateDirection;
  onRotate: (imageId: string, direction: GQL.ImageRotateDirection) => void;
}) {
  const intl = useIntl();
  const { slides, currentIndex } = useLightboxState();
  const slide = slides[currentIndex] as LightboxSlide | undefined;
  const imageId = slide?.imageId;
  if (!imageId) return null;

  const isCW = direction === GQL.ImageRotateDirection.Cw;
  const label = intl.formatMessage(
    isCW
      ? { id: "actions.rotate_cw", defaultMessage: "Rotate clockwise" }
      : {
          id: "actions.rotate_ccw",
          defaultMessage: "Rotate counter-clockwise",
        },
  );
  const Icon = isCW ? RotateCwIcon : RotateCcwIcon;

  return (
    <button
      type="button"
      className="yarl__button"
      title={label}
      aria-label={label}
      onClick={() => onRotate(imageId, direction)}
    >
      <Icon className="yarl__icon" />
    </button>
  );
}

/**
 * Tracks whether the bound zoom ref is currently at its 1:1 "original size"
 * level. Returns `[atOriginal, callbacks]` — spread `callbacks` into the
 * YARLightbox `on` prop so the boolean updates from `on.zoom` / resets on
 * slide change. Lets `OriginalSizeButton` swap its icon/label without
 * polling.
 */
export function useAtOriginalSize(zoomRef: React.RefObject<ZoomRef | null>) {
  const [atOriginal, setAtOriginal] = useState(false);
  const onZoom = useCallback(
    ({ zoom }: { zoom: number }) => {
      const ref = zoomRef.current;
      if (!ref) return;
      const target = ref.maxZoom / LIGHTBOX_ZOOM_TUNING.maxZoomPixelRatio;
      setAtOriginal(Math.abs(zoom - target) < 0.01);
    },
    [zoomRef],
  );
  const onView = useCallback(() => setAtOriginal(false), []);
  return [atOriginal, { zoom: onZoom, view: onView }] as const;
}

export function OriginalSizeButton({
  zoomRef,
  atOriginal,
}: {
  zoomRef: React.RefObject<ZoomRef | null>;
  atOriginal: boolean;
}) {
  const intl = useIntl();
  const label = atOriginal
    ? intl.formatMessage({
        id: "actions.fit_to_screen",
        defaultMessage: "Fit to screen",
      })
    : intl.formatMessage({
        id: "actions.original_size",
        defaultMessage: "Original size",
      });
  const Icon = atOriginal ? Minimize2Icon : Maximize2Icon;
  return (
    <button
      type="button"
      className="yarl__button"
      title={label}
      aria-label={label}
      onClick={() => {
        const ref = zoomRef.current;
        if (!ref || ref.disabled) return;
        // maxZoom is computed as natural-pixel-ratio × maxZoomPixelRatio,
        // so dividing by maxZoomPixelRatio yields true 1:1 display.
        const originalTarget =
          ref.maxZoom / LIGHTBOX_ZOOM_TUNING.maxZoomPixelRatio;
        ref.changeZoom(atOriginal ? ref.minZoom : originalTarget);
      }}
    >
      <Icon className="yarl__icon" />
    </button>
  );
}

// ── Lightbox component ─────────────────────────────────────────────────────────

export interface LightboxProps {
  open: boolean;
  onClose: () => void;
  slides: LightboxSlide[];
  /** Current slide index. Changing this prop navigates the lightbox to that slide. */
  index?: number;
  onView?: (index: number) => void;
  /** Zero-based index of the first real slide within the full result set. */
  pageStartIndex?: number;
  /** Total real slides across the full result set. Falls back to local slides. */
  totalCount?: number;
  /** Optional delete action for the current image. Enables delete UI + shortcut. */
  onDeleteImage?: (imageId: string, opts: DeleteOptions) => Promise<void>;
  /** Start the slideshow immediately when the lightbox opens. */
  slideshowAutoplay?: boolean;
  /**
   * When true, the carousel is finite (no wrap-around). Use when boundary sentinel
   * slides are present so the user can swipe to them but not loop around.
   */
  finite?: boolean;
}

export function Lightbox({
  open,
  onClose,
  slides,
  index = 0,
  onView,
  pageStartIndex = 0,
  totalCount,
  onDeleteImage,
  slideshowAutoplay = false,
  finite = false,
}: LightboxProps) {
  const intl = useIntl();
  const requestClose = useLightboxHistory(open, onClose);
  const [settings, setSettings] = useState<LightboxSettings>(loadSettings);
  const slideshowPlayingRef = useRef(false);
  const resumeSlideshowRef = useRef(false);
  const slideshowRef = useRef<SlideshowRef>(null);
  const zoomRef = useRef<ZoomRef>(null);
  const [atOriginalSize, zoomTrackerCallbacks] = useAtOriginalSize(zoomRef);
  // Touch-only tap-to-reveal: hide the slide overlays (title, galleries,
  // performers, rating, o-counter) until the user taps the slide once.
  // Defaults to hidden so the image isn't cluttered on first load. The
  // visibility class is consumed by `globals.css` under `(hover: none)`,
  // so hover-capable devices ignore it and always show the overlays.
  const [chromeRevealed, setChromeRevealed] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  // Defer the reveal so a second tap (YARL's double-tap-to-zoom) can
  // cancel it. Without this, a double-tap that lands in the overlay
  // area would reveal the chrome on tap 1, the overlay's children
  // would re-acquire `pointer-events: auto`, and tap 2 would be
  // absorbed by the overlay before YARL's zoom toggle could fire. The
  // hide path stays immediate — once chrome is visible, hiding it on
  // a subsequent tap is fine because the chrome doesn't intercept
  // anything we'd want a follow-on tap to hit.
  const chromeRevealTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const lastClickAtRef = useRef(0);
  const handleSlideClick = useCallback(() => {
    // YARL's `on.click` fires for both taps of a double-tap (it doesn't
    // suppress on its own). Any tap that arrives inside the double-tap
    // window is treated as the second half of a potential zoom toggle:
    // cancel any pending chrome reveal and stay out of YARL's way.
    const now = Date.now();
    const delta = now - lastClickAtRef.current;
    lastClickAtRef.current = now;
    if (delta < 300) {
      if (chromeRevealTimerRef.current) {
        clearTimeout(chromeRevealTimerRef.current);
        chromeRevealTimerRef.current = null;
      }
      return;
    }
    setChromeRevealed((revealed) => {
      // Hiding: immediate, no need to wait for double-tap detection
      // (the chrome itself doesn't have anything we'd want a follow-on
      // tap to hit while still visible).
      if (revealed) return false;
      // Revealing: defer so a follow-up tap can cancel and let YARL's
      // double-tap zoom fire instead.
      if (chromeRevealTimerRef.current) {
        clearTimeout(chromeRevealTimerRef.current);
      }
      chromeRevealTimerRef.current = setTimeout(() => {
        chromeRevealTimerRef.current = null;
        setChromeRevealed(true);
      }, 250);
      return revealed;
    });
  }, []);
  useEffect(
    () => () => {
      if (chromeRevealTimerRef.current) {
        clearTimeout(chromeRevealTimerRef.current);
      }
    },
    [],
  );

  useEffect(() => {
    if (!open || !slideshowAutoplay) return;
    const timer = window.setTimeout(() => {
      slideshowRef.current?.play?.();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, slideshowAutoplay]);

  // Resume slideshow after a page load replaces the sentinel slides.
  const prevSlidesRef = useRef(slides);
  useEffect(() => {
    if (slides === prevSlidesRef.current) return;
    prevSlidesRef.current = slides;
    if (resumeSlideshowRef.current) {
      resumeSlideshowRef.current = false;
      slideshowRef.current?.play?.();
    }
  }, [slides]);

  // ── Image rotation ────────────────────────────────────────────────────────
  //
  // The rotate buttons rewrite the file's EXIF Orientation tag in place via
  // `imageRotate`. The mutation bumps `image.updated_at` server-side and
  // returns SlimImageData with a fresh `paths.image?t=<unix>` URL — Apollo
  // writes that to the normalized cache, so list cards and detail pages
  // pick up the new URL on their next render. The lightbox slides are a
  // local snapshot taken when the lightbox opened (see useImageLightbox),
  // so cache updates don't reach them. We track per-imageId src overrides
  // locally and apply the fresh URL from the mutation response — same end
  // result as relying on cache propagation, but reactive to the snapshot.
  const [imageRotate] = useMutation(GQL.ImageRotateDocument);
  const [slideSrcOverrides, setSlideSrcOverrides] = useState<
    Record<string, string>
  >({});
  const handleRotate = useCallback(
    (imageId: string, direction: GQL.ImageRotateDirection) => {
      void imageRotate({ variables: { id: imageId, direction } })
        .then(({ data }) => {
          const newSrc = data?.imageRotate?.paths?.image;
          if (!newSrc) return;
          setSlideSrcOverrides((prev) => ({ ...prev, [imageId]: newSrc }));
        })
        .catch(() => {
          /* surface via Apollo error link / toast elsewhere */
        });
    },
    [imageRotate],
  );

  const decoratedSlides = useMemo(() => {
    if (Object.keys(slideSrcOverrides).length === 0) return slides;
    return slides.map((s) => {
      if (!s.imageId) return s;
      const override = slideSrcOverrides[s.imageId];
      return override ? { ...s, src: override } : s;
    });
  }, [slides, slideSrcOverrides]);

  // Window-level escape handler. YARL listens for Escape via its own
  // container's onKeyDown, which only fires while focus is inside the
  // lightbox tree. After a sentinel-triggered page transition the
  // slides array changes, the previously-focused slide element is
  // unmounted, and the browser drops focus back to `document.body` —
  // outside YARL's portal — so Escape stops closing the lightbox until
  // the user clicks back inside. A window listener bypasses the focus
  // dependency entirely. Mirrors the SceneLightbox handler.
  //
  // Skip when `defaultPrevented` (something else already handled the
  // key) or when HTML5 fullscreen is active (the browser owns that
  // exit and we don't want to close the lightbox underneath it).
  useEffect(() => {
    if (!open) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      if (e.defaultPrevented) return;
      if (document.fullscreenElement) return;
      // Don't close the lightbox if the user is dismissing an open
      // popup that's meant to consume Escape (menu, listbox, combobox,
      // alertdialog). `role="dialog"` is intentionally NOT in this set:
      // YARL's own container carries `role="dialog"`, so including it
      // would make every Escape skip — the closest dialog ancestor is
      // always the lightbox itself. Tooltip triggers and dropdown
      // triggers don't carry these roles, so a tooltip showing on a
      // focused chip in the slide footer doesn't trip this check.
      const active = document.activeElement as HTMLElement | null;
      if (
        active?.closest(
          '[role="menu"], [role="alertdialog"], [role="listbox"], [role="combobox"]',
        )
      ) {
        return;
      }
      e.preventDefault();
      requestClose();
    }
    // Capture phase on `document` so we run before Base UI Tooltip's
    // useDismiss listener (also on `document`, bubble phase) — its
    // default `escapeKeyBubbles: false` calls `stopPropagation()` on
    // the open tooltip, which would otherwise swallow our Escape until
    // the user pressed it twice.
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [open, requestClose]);

  const isSingleSlide = slides.length === 1;
  const isSingleSlideMode = isSingleSlide && !finite;

  // Settings + slideshow + thumbnails are meaningless with one slide and
  // no boundary sentinels (single-image detail viewer); skip the plugin
  // wiring there to keep the toolbar tidy.
  const plugins = [
    Fullscreen,
    ...(settings.displayMode === "fitXY" ? [Zoom] : []),
    ...(isSingleSlideMode ? [] : [Thumbnails, Slideshow]),
  ];

  const renderSlide = useCallback(
    ({ slide }: RenderSlideProps) => {
      const s = slide as LightboxSlide;
      if (s.loading)
        return (
          <div className="flex items-center justify-center w-full h-full">
            <Spinner className="size-10 text-white/70" />
          </div>
        );
      if (settings.displayMode === "fitX") return <FitXSlide slide={s} />;
      if (settings.displayMode === "original")
        return <OriginalSlide slide={s} />;
      return undefined;
    },
    [settings.displayMode],
  );

  const renderSlideHeader = useCallback(
    (_props: RenderSlideHeaderProps) => (
      <LightboxCounter
        pageStartIndex={pageStartIndex}
        totalCount={totalCount}
      />
    ),
    [pageStartIndex, totalCount],
  );

  const renderSlideFooter = useCallback(
    ({ slide }: RenderSlideFooterProps) => {
      const s = slide as LightboxSlide;
      if (!s.imageId) return null;
      return (
        <ImageEntityFooter
          imageId={s.imageId}
          onRequestDelete={onDeleteImage ? setDeleteTarget : undefined}
        />
      );
    },
    [onDeleteImage],
  );

  const renderControls = useCallback(
    () =>
      onDeleteImage ? (
        <LightboxDeleteShortcut
          disabled={!!deleteTarget}
          onRequestDelete={setDeleteTarget}
        />
      ) : null,
    [deleteTarget, onDeleteImage],
  );

  return (
    <>
      <YARLightbox
        open={open}
        close={requestClose}
        slides={decoratedSlides as never}
        index={index}
        plugins={plugins}
        carousel={finite ? { finite: true } : undefined}
        controller={{
          disableSwipeNavigation: isSingleSlideMode,
          // YARL otherwise calls preventDefault() on horizontal wheel events
          // (via a non-passive native listener) so its swipe controller can
          // claim them. In "original" display mode that blocks the browser
          // from scrolling the OriginalSlide container horizontally; we want
          // pan-first / swipe-at-edge instead, gated by our React onWheel
          // handler that stopPropagation's while there's still room to pan.
          preventDefaultWheelX: settings.displayMode !== "original",
        }}
        animation={{ zoom: 250 }}
        className={cn(
          "image-lightbox lightbox-mobile-toolbar-bottom",
          chromeRevealed ? "chrome-revealed" : "chrome-hidden",
        )}
        zoom={{
          ...LIGHTBOX_ZOOM_TUNING,
          scrollToZoom: settings.scrollToZoom,
          ref: zoomRef,
        }}
        slideshow={{
          autoplay: false,
          delay: settings.slideshowDelay * 1000,
          ref: slideshowRef,
        }}
        thumbnails={{
          position: "bottom",
          width: 48,
          height: 36,
          gap: 4,
          padding: 2,
          border: 1,
        }}
        toolbar={{
          buttons: [
            ...(isSingleSlideMode
              ? []
              : [
                  <SettingsButton
                    key="settings"
                    settings={settings}
                    onSettingsChange={setSettings}
                  />,
                  "slideshow" as const,
                ]),
            ...(settings.displayMode === "fitXY"
              ? [
                  "zoom" as const,
                  <OriginalSizeButton
                    key="original-size"
                    zoomRef={zoomRef}
                    atOriginal={atOriginalSize}
                  />,
                ]
              : []),
            <LightboxRotateButton
              key="rotate-ccw"
              direction={GQL.ImageRotateDirection.Ccw}
              onRotate={handleRotate}
            />,
            <LightboxRotateButton
              key="rotate-cw"
              direction={GQL.ImageRotateDirection.Cw}
              onRotate={handleRotate}
            />,
            "fullscreen",
            "close",
          ],
        }}
        on={{
          click: handleSlideClick,
          view: ({ index: newIndex }) => {
            const s = (slides as LightboxSlide[])[newIndex];
            if (s?.loading && slideshowPlayingRef.current) {
              resumeSlideshowRef.current = true;
            }
            zoomTrackerCallbacks.view();
            onView?.(newIndex);
          },
          zoom: zoomTrackerCallbacks.zoom,
          slideshowStart: () => {
            slideshowPlayingRef.current = true;
          },
          slideshowStop: () => {
            slideshowPlayingRef.current = false;
          },
        }}
        render={{
          ...lightboxIconRenders,
          iconLoading: () => <Spinner className="size-10 text-white/70" />,
          slide: renderSlide,
          slideHeader: renderSlideHeader,
          slideFooter: renderSlideFooter,
          controls: renderControls,
          ...(isSingleSlideMode && {
            buttonPrev: () => null,
            buttonNext: () => null,
          }),
        }}
      />
      {onDeleteImage && deleteTarget && (
        <DeleteDialog
          open
          onOpenChange={(o) => {
            if (!o) setDeleteTarget(null);
          }}
          entityName={deleteTarget.title}
          showFileOptions
          details={
            deleteTarget.filePaths.length > 0 ? (
              <DeleteFilesList paths={deleteTarget.filePaths} />
            ) : undefined
          }
          detailsLabel={intl.formatMessage(
            {
              id: "dialogs.delete_show_files_count",
              defaultMessage:
                "Show {count, plural, one {# file} other {# files}}",
            },
            { count: deleteTarget.filePaths.length },
          )}
          onConfirm={(opts) => onDeleteImage(deleteTarget.imageId, opts)}
        />
      )}
    </>
  );
}
