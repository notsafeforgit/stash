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
 * - Second `touchstart` within ~350 ms of the previous one (iOS
 *   Safari + Android Chrome) — `touch-action: manipulation` on every
 *   element only suppresses double-tap-zoom *most* of the time on
 *   iOS, with empirical exceptions around portaled drawer content
 *   and re-rendering buttons. This app's contract is "PWA-like — no
 *   browser-built-in double-tap-zoom anywhere; only our explicit
 *   lightbox and video-frame handlers do zoom." So every rapid
 *   second tap outside an allow-listed gesture region gets its
 *   touchstart `preventDefault`'d to cancel the browser's zoom
 *   default. `preventDefault` on touchstart also suppresses the
 *   synthesized click chain, so the handler manually dispatches a
 *   `click` on the touched element to keep button / link onClicks
 *   firing on every tap (otherwise rapid `+` / `−` zoom-button
 *   taps would only register every other tap). No spatial check —
 *   two genuinely-far-apart rapid taps shouldn't happen by mistake,
 *   and the dispatched click still goes to the correct (second-tap)
 *   target regardless.
 *
 * Regions that *want* pinch-zoom (the video frame, yarl lightbox slides)
 * opt in with `data-pinch-zoom-allowed` on a wrapper element; events
 * inside such a region pass through to component-level handlers
 * untouched.
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

  // Tracks the most recent single-finger touchstart so a follow-up
  // touchstart can be identified as the second tap of a double-tap
  // pair. Reset (zero) whenever the gesture isn't a candidate for
  // pairing (multi-touch, or inside an allowed gesture region).
  let lastSingleTouchStartAt = 0;
  const DOUBLE_TAP_WINDOW_MS = 350;

  function onTouchStart(e: TouchEvent) {
    if (e.touches.length >= 2) {
      // Multi-touch — pinch territory; cancel before iOS commits.
      if (!isInsideAllowed(e.target)) e.preventDefault();
      lastSingleTouchStartAt = 0;
      return;
    }
    if (isInsideAllowed(e.target)) {
      // Custom gesture surface owns its own double-tap semantics
      // (e.g. the video-frame zoom).
      lastSingleTouchStartAt = 0;
      return;
    }
    const t = e.touches[0];
    if (!t) return;
    const now = Date.now();
    const dt = now - lastSingleTouchStartAt;
    if (dt > 0 && dt < DOUBLE_TAP_WINDOW_MS && e.cancelable) {
      // Second tap of a rapid pair — the browser would zoom the
      // viewport. preventDefault cancels both the zoom default and
      // the synthesized mouse-event chain (mousedown → mouseup →
      // click); manually dispatch a click so the touched element's
      // onClick still fires for the second tap (so e.g. the View
      // Options drawer's `+`/`−` zoom controls advance one step
      // per rapid tap, every tap, instead of every other one).
      e.preventDefault();
      const target = e.target;
      if (target instanceof HTMLElement) {
        // Microtask defer so the dispatched click runs after
        // touchstart propagation finishes; firing inline can leave
        // listeners that read `event.touches` confused.
        queueMicrotask(() => {
          target.dispatchEvent(
            new MouseEvent("click", {
              bubbles: true,
              cancelable: true,
              view: window,
              button: 0,
              clientX: t.clientX,
              clientY: t.clientY,
            }),
          );
        });
      }
      lastSingleTouchStartAt = 0;
      return;
    }
    lastSingleTouchStartAt = now;
  }

  function onTouchMove(e: TouchEvent) {
    // Single-touch movements are scrolling — leave them alone. Only
    // multi-touch is a pinch on iOS Safari.
    if (e.touches.length < 2) return;
    if (isInsideAllowed(e.target)) return;
    e.preventDefault();
  }

  // Belt-and-suspenders: also preventDefault on the second touchend
  // of a rapid pair. Some Safari versions decide double-tap-zoom at
  // gesture-recognition time (after touchend), which our touchstart
  // preventDefault doesn't always reach in time.
  let lastSingleTouchEndAt = 0;
  function onTouchEnd(e: TouchEvent) {
    if (e.changedTouches.length !== 1 || e.touches.length !== 0) {
      lastSingleTouchEndAt = 0;
      return;
    }
    if (isInsideAllowed(e.target)) {
      lastSingleTouchEndAt = 0;
      return;
    }
    const now = Date.now();
    const dt = now - lastSingleTouchEndAt;
    if (dt > 0 && dt < DOUBLE_TAP_WINDOW_MS && e.cancelable) {
      e.preventDefault();
      lastSingleTouchEndAt = 0;
      return;
    }
    lastSingleTouchEndAt = now;
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
  window.addEventListener("touchend", onTouchEnd, {
    capture: true,
    passive: false,
  });
  window.addEventListener("wheel", onWheel, {
    capture: true,
    passive: false,
  });
}
