import type { PreviewImageData } from "@/components/shared/preview-image";
import "yet-another-react-lightbox/styles.css";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type ComponentType,
  type ReactNode,
} from "react";
import YARLightbox, {
  type GenericSlide,
  type RenderSlideProps,
  type FullscreenRef,
  type ZoomRef,
  type SlideshowRef,
  isImageSlide,
  useLightboxState,
} from "yet-another-react-lightbox";
import Fullscreen from "yet-another-react-lightbox/plugins/fullscreen";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import Slideshow from "yet-another-react-lightbox/plugins/slideshow";
import { loadSettings } from "./settings";
import { useQuery } from "@apollo/client/react";
import { FindImageDocument } from "@/core/generated-graphql";
import {
  type LightboxSlide,
  LIGHTBOX_ZOOM_TUNING,
  ImageEntityFooter,
  OriginalSizeButton,
  LightboxImageActionsButton,
  LightboxCloseButton,
  useAtOriginalSize,
} from "./lightbox";
import { Spinner } from "src/components/ui/spinner";
import { SceneSlideContent } from "./scene-slide-content";
import { PersistentSceneCarousel } from "./scene-carousel";
import { lightboxIconRenders } from "./lightbox-icons";
import type { OfflineEntry } from "src/components/offline/offline-db";
import { useIsTouch } from "@/utils/screen";
import { lightboxAnimation, useLightboxMotion } from "./use-lightbox-motion";
import { LightboxMotionSurface } from "./lightbox-motion-surface";
import { useImageLightboxChrome } from "./use-image-lightbox-chrome";
import { popupOwnsLightboxKeys } from "./lightbox-keyboard";
import { cn } from "@/lib/utils";

// ── Persistence keys ───────────────────────────────────────────────────────────

const LOOP_STORAGE_KEY = "stash-lightbox-loop";

// ── Slide type ─────────────────────────────────────────────────────────────────

export interface SceneSlideMarker {
  id: string;
  title: string;
  seconds: number;
  primaryTag: { id: string; name: string };
  tags: { id: string; name: string }[];
}

export interface SceneSlide extends GenericSlide {
  type: "scene";
  sceneId: string;
  title?: string;
  posterSrc?: string;
  posterImage?: PreviewImageData | null;
  /** Transient placeholder shown while the next/prev page is loading. */
  loading?: boolean;
  /** Keep the single media element while viewing an image, with no source. */
  suspended?: boolean;
  /** When set, the player starts playback at this marker's `seconds` and the
   *  overlay shows marker title + primary tag + tags instead of the scene's
   *  title + performers. */
  marker?: SceneSlideMarker;
  /** Offline-mode shortcut: when set, the slide skips the GraphQL
   *  `findScene` fetch and builds a fake scene directly from the
   *  IDB-snapshotted `OfflineEntry` plus an OPFS blob URL resolved
   *  inside the slide. The mutating overlay buttons are also
   *  suppressed so the offline view stays read-only. */
  offlineEntry?: OfflineEntry;
}

declare module "yet-another-react-lightbox" {
  interface SlideTypes {
    scene: SceneSlide;
  }
}

// ── Component ──────────────────────────────────────────────────────────────────

export interface SceneLightboxProps {
  open: boolean;
  onClose: () => void;
  slides: SceneSlide[];
  index?: number;
  onView?: (index: number) => void;
  /** Finite carousel (no wrap-around). Used when boundary sentinel slides exist. */
  finite?: boolean;
  /** Supply scoped playback without mounting the library's data providers. */
  slideContent?: ComponentType<SceneLightboxSlideProps>;
}

export interface MediaLightboxProps extends Omit<SceneLightboxProps, "slides"> {
  slides: (SceneSlide | LightboxSlide)[];
  totalCount?: number;
  pageStartIndex?: number;
  counterLabel?: string;
  slideshowAutoplay?: boolean;
  renderPending?: () => ReactNode;
}

function ImageFooter({ id, touch }: { id: string; touch: boolean }) {
  // Only mounted for the selected image. The normalized cache is shared with
  // the regular image lightbox; revisiting it needs no additional request.
  const { data } = useQuery(FindImageDocument, { variables: { id } });
  if (data?.findImage?.id !== id) return null;
  return <ImageEntityFooter imageId={id} mobile={touch} showRating={!touch} />;
}

function MediaCounter({
  total,
  start,
  label,
}: {
  total: number;
  start: number;
  label?: string;
}) {
  const { currentIndex, currentSlide } = useLightboxState();
  if (currentSlide?.loading || (total < 2 && !label)) return null;
  return (
    <div
      data-lightbox-counter
      className="pointer-events-none absolute inset-x-0 top-2 flex justify-center text-xs tabular-nums text-white/85"
    >
      <span className="rounded bg-black/45 px-2 py-1">
        {label && <span className="line-clamp-1 max-w-[70vw]">{label}</span>}
        {start + currentIndex + 1} / {total}
      </span>
    </div>
  );
}

export type SceneLightboxSlideProps = ComponentProps<typeof SceneSlideContent>;

export function MediaLightbox({
  open,
  onClose,
  slides,
  index = 0,
  onView,
  finite = false,
  slideContent: SlideContent = SceneSlideContent,
  totalCount,
  pageStartIndex = 0,
  counterLabel,
  slideshowAutoplay,
  renderPending,
}: MediaLightboxProps) {
  const {
    controllerRef,
    portal,
    requestClose,
    finishClose,
    onSurfaceReady,
    onExiting,
  } = useLightboxMotion(open, onClose);
  const touch = useIsTouch();
  const slideshowRef = useRef<SlideshowRef>(null);
  const resumeSlideshow = useRef(slideshowAutoplay ?? false);
  const [slideshowSettings] = useState(loadSettings);
  const [view, setView] = useState({ propIndex: index, currentIndex: index });
  if (view.propIndex !== index)
    setView({ propIndex: index, currentIndex: index });
  const selected = slides[view.currentIndex];
  const imageActive = !!selected && isImageSlide(selected);
  useEffect(() => {
    if (slideshowAutoplay === undefined) return;
    // Image timing pauses on scenes and loading slots. Video playback keeps
    // its own end/loop controls; it must never be cut off by an image timer.
    if (imageActive && !selected?.loading && resumeSlideshow.current)
      slideshowRef.current?.play();
    else slideshowRef.current?.pause();
  }, [imageActive, selected, slideshowAutoplay]);
  const { revealed: chromeRevealed, onClick: onImageClick } =
    useImageLightboxChrome(`${open}:${view.currentIndex}`, true);
  const zoomRef = useRef<ZoomRef>(null);
  const [atOriginal, zoomCallbacks] = useAtOriginalSize(zoomRef);
  // Lightbox fullscreen ref — populated by the YARL Fullscreen plugin via
  // its `fullscreen.ref` prop. Used so the embedded ScenePlayer can
  // delegate fullscreen requests (its hidden button + `f` hotkey) to the
  // lightbox-level fullscreen instead of fullscreening just the video
  // element. That way the slide-nav swipe gestures, title overlay, and
  // YARL's own toolbar all stay visible in fullscreen mode.
  const fullscreenRef = useRef<FullscreenRef>(null);
  const handleToggleLightboxFullscreen = useCallback((): boolean => {
    const fs = fullscreenRef.current;
    // Returning `false` lets the player fall back to its native
    // fullscreen path (`webkitEnterFullscreen` on the video element).
    //
    // 1. YARL's plugin is disabled (iOS Safari pre-16.4 — no
    //    Element.requestFullscreen at all).
    // 2. Coarse-pointer device (touch / mobile). Even on iOS 16.4+
    //    where Element.requestFullscreen exists, calling it on a
    //    `<div>` (YARL's container) silently rejects in many
    //    configurations — the promise resolves rejected, YARL's
    //    `.catch(() => {})` swallows it, and from outside there's
    //    no way to tell it didn't actually fullscreen. Routing
    //    touch through the player's native path instead reliably
    //    fullscreens the video element via `webkitEnterFullscreen`,
    //    which iOS has supported since iOS 5. We trade the
    //    desktop-only nicety of slide-chrome-visible-in-fullscreen
    //    (title overlay, swipe nav, YARL toolbar) for fullscreen
    //    that actually works on phones.
    if (!fs || fs.disabled) return false;
    if (window.matchMedia("(pointer: coarse)").matches) return false;
    if (fs.fullscreen) {
      fs.exit();
    } else {
      fs.enter();
    }
    return true;
  }, []);

  // Keep the loop preference across closing/reopening, alongside the player's
  // persisted auto-advance choice. Loop and auto-advance remain exclusive.
  const [loopEnabled, setLoopEnabled] = useState(() => {
    try {
      return localStorage.getItem(LOOP_STORAGE_KEY) === "true";
    } catch {
      return false;
    }
  });
  const handleLoopToggle = useCallback(() => {
    setLoopEnabled((v) => {
      const next = !v;
      try {
        localStorage.setItem(LOOP_STORAGE_KEY, String(next));
      } catch {
        /* localStorage unavailable */
      }
      return next;
    });
  }, []);

  // YARL controller — used to advance imperatively from the player's
  // auto-advance toggle (which fires on the video's `ended` event).
  const handleNext = useCallback(
    (slide: SceneSlide) => {
      const controller = controllerRef.current;
      // The outgoing video remains alive during a swipe. Its late EOF must not
      // advance again after the user has already selected a different slide.
      const state = controller?.getLightboxState();
      if (state?.currentSlide === slide && !state.animation?.increment)
        controller?.next();
    },
    [controllerRef],
  );

  // Per-gesture wheel lockout — pin one trackpad swipe to one slide
  // advance, with an early-release escape hatch for deliberate
  // consecutive swipes. YARL's internal wheel handler filters
  // subsequent events only by an inertia heuristic that macOS
  // trackpad momentum can defeat, so an aggressive two-finger swipe
  // can drag the next slide partway in again.
  //
  // Strategy: latch a 700 ms lock on every `view` change. While the
  // lock is held, swallow horizontal wheel events at the window
  // level — *unless* an event's magnitude clearly spikes above the
  // tracked momentum envelope (≥ 1.8× decayed peak), which signals
  // the user lifted and re-swiped. In that case release the lock
  // immediately and let the event through, so quick repeat swipes
  // still register without the tail of swipe N polluting swipe N+1.
  // Vertical scroll is untouched; pointer/touch swipes already
  // advance by exactly one and aren't affected.
  const wheelLockUntilRef = useRef(0);
  const wheelEnvelopeRef = useRef(0);
  const lastWheelAtRef = useRef(0);
  useEffect(() => {
    if (!open) return;
    function onWheelCapture(e: WheelEvent) {
      if (Math.abs(e.deltaX) <= Math.abs(e.deltaY)) return;

      const now = Date.now();
      const absDelta = Math.abs(e.deltaX);
      // Decay the envelope based on real wall-clock gap so paused
      // gestures relax the threshold even if we missed events.
      const dt = lastWheelAtRef.current
        ? Math.max(0, now - lastWheelAtRef.current)
        : 0;
      lastWheelAtRef.current = now;
      if (dt > 0) {
        // ~8% per 16 ms ≈ momentum fades to ~50% over ~130 ms quiet.
        wheelEnvelopeRef.current *= 0.92 ** (dt / 16);
      }

      if (now < wheelLockUntilRef.current) {
        const isNewGesture =
          absDelta > Math.max(40, wheelEnvelopeRef.current * 1.8);
        if (isNewGesture) {
          wheelLockUntilRef.current = 0;
          wheelEnvelopeRef.current = absDelta;
          return;
        }
        e.stopImmediatePropagation();
        e.preventDefault();
      }
      wheelEnvelopeRef.current = Math.max(wheelEnvelopeRef.current, absDelta);
    }
    window.addEventListener("wheel", onWheelCapture, {
      capture: true,
      passive: false,
    });
    return () => {
      window.removeEventListener("wheel", onWheelCapture, { capture: true });
    };
  }, [open]);

  const isSingleSlide = slides.length === 1;

  // Document-level navigation also handles focus restored to body after menus
  // or left on the media surface, outside YARL's keyboard sensor container.
  // Skip when `defaultPrevented` (a Base UI menu, dialog, or the player's
  // own escape consumer already handled it) or when focus is on an editable
  // input/menu surface that should own its keyboard interaction.
  useEffect(() => {
    if (!open) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented) return;
      const isEscape = e.key === "Escape";
      const isHorizontalArrow = e.key === "ArrowLeft" || e.key === "ArrowRight";
      if (!isEscape && !isHorizontalArrow) return;

      const active = document.activeElement;
      if (isHorizontalArrow && active?.closest('[role="slider"]')) return;
      if (
        active?.tagName === "INPUT" ||
        active?.tagName === "TEXTAREA" ||
        (active instanceof HTMLElement && active.isContentEditable) ||
        popupOwnsLightboxKeys()
      ) {
        return;
      }

      if (isEscape) {
        // Let the browser exit HTML5 fullscreen without also closing the
        // lightbox underneath it.
        if (document.fullscreenElement) return;
        e.preventDefault();
        requestClose();
        return;
      }

      if (e.altKey || e.ctrlKey || e.metaKey) return;
      if (isSingleSlide && !finite) return;
      e.preventDefault();
      e.stopPropagation();
      if (e.key === "ArrowLeft") {
        controllerRef.current?.prev();
      } else {
        controllerRef.current?.next();
      }
    }
    // Capture phase on `document` so we run before Base UI Tooltip's
    // useDismiss listener (also on `document`, bubble phase) — its
    // default `escapeKeyBubbles: false` calls `stopPropagation()` on
    // the open tooltip, which would otherwise swallow the first
    // Escape after focus lands on a chip (the second press worked
    // because the tooltip had closed and Base UI's listener was gone).
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [open, requestClose, isSingleSlide, finite, controllerRef]);

  const renderSlide = useCallback(
    ({ slide, offset }: RenderSlideProps) => {
      if (isImageSlide(slide)) {
        if (slide.loading)
          return (
            renderPending?.() ?? <Spinner className="size-10 text-white/70" />
          );
        return undefined;
      }
      if (slide.type !== "scene") return undefined;
      return (
        <SlideContent
          slide={slide}
          isActive={offset === 0}
          onToggleFullscreen={handleToggleLightboxFullscreen}
          loopEnabled={loopEnabled}
          onLoopToggle={handleLoopToggle}
          onNext={() => handleNext(slide)}
          onClose={touch ? requestClose : undefined}
        />
      );
    },
    [
      handleToggleLightboxFullscreen,
      loopEnabled,
      handleLoopToggle,
      handleNext,
      touch,
      requestClose,
      SlideContent,
      renderPending,
    ],
  );

  return (
    <YARLightbox
      open={open}
      portal={portal}
      close={finishClose}
      animation={lightboxAnimation}
      slides={slides}
      index={index}
      plugins={[
        Fullscreen,
        Zoom,
        PersistentSceneCarousel,
        ...(slideshowAutoplay !== undefined ? [Slideshow] : []),
      ]}
      slideshow={{
        ref: slideshowRef,
        delay: slideshowSettings.slideshowDelay * 1000,
      }}
      fullscreen={{ ref: fullscreenRef }}
      zoom={{ ...LIGHTBOX_ZOOM_TUNING, ref: zoomRef }}
      className={cn(
        "media-lightbox",
        imageActive && "image-lightbox lightbox-mobile-toolbar-bottom",
        imageActive && (chromeRevealed ? "chrome-revealed" : "chrome-hidden"),
      )}
      carousel={{ finite, preload: 1, ...(touch && { padding: 0 }) }}
      controller={{
        ref: controllerRef,
        disableSwipeNavigation: isSingleSlide && !finite,
      }}
      toolbar={{
        buttons: [
          ...(imageActive && !selected?.loading
            ? [
                "zoom" as const,
                <OriginalSizeButton
                  key="original"
                  zoomRef={zoomRef}
                  atOriginal={atOriginal}
                />,
                <LightboxImageActionsButton
                  key="actions"
                  zoomRef={zoomRef}
                  atOriginal={atOriginal}
                  zoomEnabled
                />,
                ...(slideshowAutoplay !== undefined
                  ? ["slideshow" as const]
                  : []),
              ]
            : []),
          "fullscreen",
          ...(imageActive
            ? [<LightboxCloseButton key="close" />]
            : ["close" as const]),
        ],
      }}
      on={{
        slideshowStart: () => {
          resumeSlideshow.current = true;
        },
        slideshowStop: () => {
          // An automatic pause at a sentinel/scene retains slideshow intent.
          const current =
            controllerRef.current?.getLightboxState().currentSlide;
          if (current && isImageSlide(current) && !current.loading)
            resumeSlideshow.current = false;
        },
        exiting: onExiting,
        click: ({ index: clickedIndex }) => {
          const slide = slides[clickedIndex];
          if (slide && isImageSlide(slide) && !slide.loading) onImageClick();
        },
        zoom: zoomCallbacks.zoom,
        view: ({ index: newIndex }) => {
          setView({ propIndex: index, currentIndex: newIndex });
          zoomCallbacks.view();
          // Latch the wheel lockout on every slide change so the
          // trackpad momentum tail can't drag the next slide partway
          // in. The lock has a hard 700 ms ceiling and an early-
          // release escape hatch when a fresh-gesture spike is
          // detected — see the listener above for details.
          wheelLockUntilRef.current = Date.now() + 700;
          onView?.(newIndex);
        },
      }}
      render={{
        ...lightboxIconRenders,
        controls: () => (
          <>
            <LightboxMotionSurface onReady={onSurfaceReady} />
            {totalCount !== undefined && (
              <MediaCounter
                total={totalCount}
                start={pageStartIndex}
                label={counterLabel}
              />
            )}
          </>
        ),
        // Touch users close from the player's control bar. Pending/error
        // slides expose the same action at the bottom until a player exists.
        ...(touch &&
          !imageActive && {
            buttonClose: () => null,
            buttonFullscreen: () => null,
          }),
        iconLoading: () => <Spinner className="size-10 text-white/70" />,
        slide: renderSlide,
        slideFooter: ({ slide }) =>
          isImageSlide(slide) && !slide.loading && slide.imageId ? (
            <ImageFooter id={slide.imageId} touch={touch} />
          ) : null,
        ...(isSingleSlide &&
          !finite && {
            buttonPrev: () => null,
            buttonNext: () => null,
          }),
      }}
    />
  );
}

// Scene lists, markers and offline playback share this same carousel/player.
export function SceneLightbox(props: SceneLightboxProps) {
  return <MediaLightbox {...props} />;
}
