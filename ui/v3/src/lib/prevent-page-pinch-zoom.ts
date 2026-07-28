/**
 * Disable page-level pinch-to-zoom across the app.
 *
 * The viewport meta in `index.html` (`maximum-scale=1, user-scalable=no`)
 * covers Android Chrome but is intentionally ignored by iOS Safari since
 * iOS 10 (an Apple accessibility decision), and is irrelevant on desktop
 * — where Ctrl+wheel and trackpad-pinch (also Ctrl+wheel) trigger the
 * browser's native page zoom regardless of viewport settings. CSS
 * `touch-action: pan-x pan-y` on body helps but is not enough on its
 * own — iOS Safari still allows pinch-to-zoom in many scenarios.
 *
 * To force-disable across all platforms we install window-capture
 * listeners that suppress the platform hooks page zoom rides on:
 *
 * - `touchstart` with 2+ touches (iOS Safari) — fired the moment a
 *   second finger lands. iOS latches the pinch gesture on this event,
 *   so a later `touchmove` `preventDefault` is ignored in some
 *   scenarios (notably standalone/page-level taps after the body has
 *   no scroll position to chain). Suppressing here cancels the gesture
 *   before iOS commits to it.
 *
 * - `touchmove` with 2+ touches (iOS Safari) — the canonical iOS pinch
 *   trigger. `preventDefault` here blocks page zoom even when
 *   `gesturestart` doesn't (e.g. inside scrollable regions iOS treats
 *   specially). Custom pinch-zoom inside the app uses pointer events
 *   on wrappers with `touch-action: none`, which fire independently
 *   of the underlying touchmove, so suppressing these is safe for our
 *   own zoom.
 *
 * - `gesturestart` / `gesturechange` / `gestureend` (iOS Safari) —
 *   non-standard multi-touch zoom events; belt-and-suspenders alongside
 *   the touchmove guard.
 *
 * - `wheel` with `ctrlKey` (desktop) — the canonical signal for both
 *   Ctrl+scroll and trackpad-pinch.
 *
 * Regions that *want* pinch-zoom (the video frame, yarl lightbox slides)
 * opt in with `data-pinch-zoom-allowed` on a wrapper element; events
 * inside such a region pass through to component-level handlers
 * untouched.
 *
 * Single-finger taps are deliberately not interpreted here. Browser-level
 * double-tap zoom is suppressed by the app's global
 * `touch-action: manipulation`; the image and scene lightboxes own the only
 * app-level double-tap gestures, both for local content zoom. Keeping that
 * recognition local means a rapid scroll can never be turned into a
 * synthetic click by a page-wide gesture listener.
 */

let installed = false;

export function installPagePinchZoomGuard() {
  if (installed) return;
  installed = true;

  function isInsideAllowed(target: EventTarget | null): boolean {
    if (!(target instanceof Element)) return false;
    return !!target.closest("[data-pinch-zoom-allowed]");
  }

  function onGesture(e: Event) {
    if (isInsideAllowed(e.target)) return;
    e.preventDefault();
  }

  function onWheel(e: WheelEvent) {
    if (!e.ctrlKey) return;
    if (isInsideAllowed(e.target)) return;
    e.preventDefault();
  }

  function onTouchStart(e: TouchEvent) {
    if (e.touches.length < 2) return;
    // Multi-touch — pinch territory; cancel before iOS commits.
    if (!isInsideAllowed(e.target)) e.preventDefault();
  }

  function onTouchMove(e: TouchEvent) {
    if (e.touches.length < 2) return;
    if (!isInsideAllowed(e.target)) e.preventDefault();
  }

  // `gesturestart` etc. aren't in TS's WindowEventMap (Safari-only);
  // bind via the string overload.
  window.addEventListener("gesturestart", onGesture, {
    capture: true,
    passive: false,
  });
  window.addEventListener("gesturechange", onGesture, {
    capture: true,
    passive: false,
  });
  window.addEventListener("gestureend", onGesture, {
    capture: true,
    passive: false,
  });
  window.addEventListener("touchstart", onTouchStart, {
    capture: true,
    passive: false,
  });
  window.addEventListener("touchmove", onTouchMove, {
    capture: true,
    passive: false,
  });
  window.addEventListener("wheel", onWheel, {
    capture: true,
    passive: false,
  });
}
