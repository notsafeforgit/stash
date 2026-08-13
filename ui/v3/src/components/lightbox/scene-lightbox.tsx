import "yet-another-react-lightbox/styles.css";

import { useCallback, useEffect, useRef, useState } from "react";
import YARLightbox, {
  type ControllerRef,
  type GenericSlide,
  type RenderSlideProps,
  type Slide,
  type FullscreenRef,
} from "yet-another-react-lightbox";
import Fullscreen from "yet-another-react-lightbox/plugins/fullscreen";
import { Spinner } from "src/components/ui/spinner";
import { SceneSlideContent } from "./scene-slide-content";
import { lightboxIconRenders } from "./lightbox-icons";
import type { OfflineEntry } from "src/components/offline/offline-db";
import { useLightboxHistory } from "./use-lightbox-history";

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
  /** Transient placeholder shown while the next/prev page is loading. */
  loading?: boolean;
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
}

export function SceneLightbox({
  open,
  onClose,
  slides,
  index = 0,
  onView,
  finite = false,
}: SceneLightboxProps) {
  const requestClose = useLightboxHistory(open, onClose);
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

  // Lightbox-scoped loop preference. Lifted out of ScenePlayer so
  // the user's toggle persists across the per-scene player remount that
  // a slide swipe triggers (key={slide.sceneId} on ActiveSceneSlide).
  // Also persisted to localStorage so it survives close-then-reopen,
  // matching how `ScenePlayer` already persists auto-advance — without
  // this the user's chosen playback mode resets to "normal" every time
  // the lightbox is reopened, even if localStorage retained auto-advance,
  // because cycling into "loop" writes auto-advance = false.
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
  const controllerRef = useRef<ControllerRef>(null);
  const handleNext = useCallback(() => {
    controllerRef.current?.next();
  }, []);

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

  // Document-level keyboard handler. YARL listens for keyboard navigation via React's
  // `onKeyDown` on its own container (`subscribeSensors(EVENT_ON_KEY_DOWN, …)`),
  // which only fires when focus is somewhere inside the lightbox tree. Some
  // interactions strand focus on `document.body` — most reproducibly,
  // switching the player's source via the controls menu remounts
  // `Player.Provider`, unmounting the menu trigger that Base UI restored
  // focus to when the menu closed; with no element to focus, the browser
  // falls back to body. Body sits outside YARL's portal tree, so escape
  // never reaches YARL until the user clicks back into the lightbox. The
  // embedded player can also leave focus on the media surface after a mouse
  // click, where arrow keys no longer reliably reach YARL's container. A
  // document listener bypasses that focus dependency entirely.
  //
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

      const active = document.activeElement as HTMLElement | null;
      if (
        active?.tagName === "INPUT" ||
        active?.tagName === "TEXTAREA" ||
        active?.isContentEditable ||
        active?.closest(
          '[role="menu"], [role="alertdialog"], [role="listbox"], [role="combobox"]',
        )
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
  }, [open, requestClose, isSingleSlide, finite]);

  const renderSlide = useCallback(
    ({ slide, offset }: RenderSlideProps) => {
      if (slide.type !== "scene") return undefined;
      return (
        <SceneSlideContent
          slide={slide}
          isActive={offset === 0}
          onToggleFullscreen={handleToggleLightboxFullscreen}
          loopEnabled={loopEnabled}
          onLoopToggle={handleLoopToggle}
          onNext={handleNext}
        />
      );
    },
    [handleToggleLightboxFullscreen, loopEnabled, handleLoopToggle, handleNext],
  );

  return (
    <YARLightbox
      open={open}
      close={requestClose}
      slides={slides as Slide[]}
      index={index}
      plugins={[Fullscreen]}
      fullscreen={{ ref: fullscreenRef }}
      carousel={finite ? { finite: true, preload: 1 } : { preload: 1 }}
      controller={{
        ref: controllerRef,
        disableSwipeNavigation: isSingleSlide && !finite,
      }}
      animation={{ zoom: 250 }}
      toolbar={{ buttons: ["fullscreen", "close"] }}
      on={{
        view: ({ index: newIndex }) => {
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
        iconLoading: () => <Spinner className="size-10 text-white/70" />,
        slide: renderSlide,
        ...(isSingleSlide &&
          !finite && {
            buttonPrev: () => null,
            buttonNext: () => null,
          }),
      }}
    />
  );
}
