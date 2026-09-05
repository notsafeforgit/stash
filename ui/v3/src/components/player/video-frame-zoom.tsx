/**
 * Pinch-to-zoom + pan wrapper for the video frame.
 *
 * The `<video>` lives inside a CSS-transformed inner div; the outer
 * wrapper is a sibling of `PlayerControls`, `PlayerPoster`, and the
 * reload spinner so those overlays stay fixed while only the frame
 * scales.
 *
 * Inputs handled
 * --------------
 * - **2-finger touch**         → pinch zoom anchored at the gesture
 *                                midpoint.
 * - **1-finger touch drag**    → pan when scale > 1; below an 8px
 *                                threshold it falls through so taps
 *                                still toggle play/pause and surface
 *                                the controls overlay.
 * - **Double-tap (touch)**     → toggle zoom: 1× → DOUBLE_TAP_ZOOM
 *                                anchored at the tap point, then back
 *                                to 1× on a subsequent double-tap.
 *                                Skipped when the tap lands on an
 *                                interactive element (button / link /
 *                                input) so the existing controls keep
 *                                their double-tap-as-double-click
 *                                semantics.
 * - **`ctrl + wheel`**         → wheel zoom anchored at the cursor
 *                                (Chrome / Firefox / Edge trackpad
 *                                pinch).
 * - **Plain wheel when zoomed**→ pan via deltaX/deltaY (two-finger
 *                                trackpad swipe).
 * - **Mouse drag**             → pan when scale > 1; below the same
 *                                8px threshold a click still fires.
 * - **Safari gesture events**  → desktop Safari trackpad pinches use
 *                                `gesturestart` / `gesturechange` /
 *                                `gestureend`, not ctrl+wheel.
 *
 * Persistence across source changes
 * ---------------------------------
 * Transform state is owned by the parent (`ScenePlayer`) and passed in
 * as a controlled prop. That way it survives quality switches and engine
 * swaps triggered by seek-past-buffer. If the state lived inside this
 * component, a direct↔HLS element swap would snap the user out of their
 * pinched view back to 1×.
 *
 * Why window-level capture-phase listeners
 * ----------------------------------------
 * `Controls.Root` (the YARL/Video.js controls overlay) is `absolute
 * inset-0` with `pointer-events: auto`, covering the entire player
 * area. Touch / wheel / pointer events land on the controls layer,
 * not on this wrapper. We register on `window` with `capture: true`
 * so we run before any element-level handler on the path, and route
 * by containment check (`closest("[data-scene-player]")`) so the
 * gesture zone covers the entire visible video including the
 * controls overlay.
 *
 * Tap vs. drag (the 8px threshold)
 * --------------------------------
 * `preventDefault` on `touchstart` / `pointerdown` cancels the
 * compatibility-mouse-event chain — including the synthesized
 * `click`. So intercepting at the start of every gesture would
 * suppress all the controls' onClick handlers (play/pause, big
 * skip buttons, tap-to-show-controls). Instead, 1-finger touch and
 * mouse drag stay in `pending` until movement crosses 8px; only
 * then are subsequent moves intercepted. Pinch (2 fingers) is
 * unambiguous and starts immediately. After a real mouse pan, the
 * synthesized click is swallowed at the window-capture click
 * listener so a pan-end doesn't toggle pause.
 *
 * Coordination with other layers
 * ------------------------------
 * - `installPagePinchZoomGuard` (in `src/lib/prevent-page-pinch-
 *   zoom.ts`) blocks page-level zoom app-wide; the outer wrapper
 *   marks itself `data-pinch-zoom-allowed=""` to opt back in.
 * - The inner transform target gets `touch-action: none` to suppress
 *   iOS Safari's native page zoom on the video frame. The local
 *   recognizer below owns double-tap zoom so the transform remains
 *   confined to the scene rather than scaling the browser viewport.
 * - On Safari, wheel events can fire alongside `gesturechange`
 *   during a trackpad pinch; the wheel handler bails when a Safari
 *   gesture is active so the zoom isn't double-applied.
 */
import type React from "react";
import { useEffect, useRef, useState } from "react";

const MIN_SCALE = 1;
const MAX_SCALE = 5;
const PAN_THRESHOLD_PX = 8;
/**
 * Max time between the two taps of a double-tap-zoom. Also the defer
 * window that `PlayerControls` uses for its single-tap controls toggle —
 * the two must match exactly, otherwise a slow double-tap (e.g. ~250 ms
 * apart) would commit a controls flip before this recognizer cancels it,
 * causing the controls/lightbox-overlay flicker the deferred toggle was
 * meant to avoid. Exported so the controls layer pulls the same value.
 */
export const DOUBLE_TAP_MAX_MS = 200;
const DOUBLE_TAP_MAX_PX = 40;
const DOUBLE_TAP_ZOOM = 2.5;
const DOUBLE_TAP_ANIM_MS = 200;

export interface ZoomTransform {
  scale: number;
  x: number;
  y: number;
}

export const IDENTITY_TRANSFORM: ZoomTransform = { scale: 1, x: 0, y: 0 };

// Shared idle / pending / active state machine used for both
// 1-finger touch pan and mouse drag pan. Helpers are pure and
// mutate the tracker in place; the handlers below decide when to
// apply the resulting delta to the video transform.
type PanTracker = {
  state: "idle" | "pending" | "active";
  start: { x: number; y: number };
  last: { x: number; y: number };
  id: number | null;
};

function newPanTracker(): PanTracker {
  return {
    state: "idle",
    start: { x: 0, y: 0 },
    last: { x: 0, y: 0 },
    id: null,
  };
}

function panBegin(t: PanTracker, x: number, y: number, id: number) {
  t.state = "pending";
  t.start = { x, y };
  t.last = { x, y };
  t.id = id;
}

/**
 * Advance a pan tracker. Returns the delta to apply, or `null` if
 * the tracker is idle or still pending below the activation
 * threshold. The first activated move emits a delta of (newPos −
 * startPos), which can be up to ~threshold px — small enough to
 * feel like a continuous start.
 */
function panAdvance(
  t: PanTracker,
  x: number,
  y: number,
): { dx: number; dy: number } | null {
  if (t.state === "idle") return null;
  if (t.state === "pending") {
    if (Math.hypot(x - t.start.x, y - t.start.y) < PAN_THRESHOLD_PX)
      return null;
    t.state = "active";
  }
  const dx = x - t.last.x;
  const dy = y - t.last.y;
  t.last = { x, y };
  return { dx, dy };
}

/** Reset a tracker. Returns true if it was actively panning. */
function panEnd(t: PanTracker): boolean {
  const wasActive = t.state === "active";
  t.state = "idle";
  t.id = null;
  return wasActive;
}

export function VideoFrameZoom({
  children,
  enabled = true,
  transform,
  onTransformChange,
  onActiveGesture,
  isTemporarySpeedActive,
}: {
  children: React.ReactNode;
  /**
   * Enables local zoom gesture ownership without changing the wrapper DOM.
   * Keeping this component mounted while disabled lets an inline player enter
   * and leave a focused viewer without reparenting its `<video>` element.
   */
  enabled?: boolean;
  transform: ZoomTransform;
  onTransformChange: (t: ZoomTransform) => void;
  /**
   * Fired the moment we recognize the user is doing something other than a
   * still single-finger tap — double-tap, pinch start, or pan crossing its
   * activation threshold. Used by the controls layer to cancel any in-
   * flight tap-style action (deferred tap-to-toggle, long-press 2×-speed
   * timer) so the gesture doesn't accidentally trigger them. Fires once
   * per gesture recognition; subsequent moves in the same gesture don't
   * re-fire.
   */
  onActiveGesture?: () => void;
  /**
   * Reports whether a long-press speed boost currently owns the single-finger
   * gesture. Movement is consumed before it can become a zoom pan or lightbox
   * slide swipe, while release and a second-finger pinch still end the boost.
   */
  isTemporarySpeedActive?: () => boolean;
}) {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const transformRef = useRef(transform);
  transformRef.current = transform;
  const onTransformChangeRef = useRef(onTransformChange);
  onTransformChangeRef.current = onTransformChange;
  const onActiveGestureRef = useRef(onActiveGesture);
  onActiveGestureRef.current = onActiveGesture;
  const isTemporarySpeedActiveRef = useRef(isTemporarySpeedActive);
  isTemporarySpeedActiveRef.current = isTemporarySpeedActive;
  // Discrete actions (double-tap) drive a CSS transition; pinch / pan /
  // wheel must stay real-time, so the transition rule is on only for
  // the duration of an animated change. Stored in state so the inline
  // style updates trigger a re-render the browser can pick up.
  const [transitionMs, setTransitionMs] = useState(0);
  const setTransitionMsRef = useRef(setTransitionMs);
  setTransitionMsRef.current = setTransitionMs;

  useEffect(() => {
    if (!enabled) {
      setTransitionMs(0);
      return;
    }

    // The wrapper stays mounted across enable/disable transitions; only the
    // window listeners are rebound. Callback refs are read lazily so ordinary
    // prop updates don't churn the gesture listeners.
    let animationFrameId: number | undefined;
    let transitionTimerId: number | undefined;
    function setTransform(t: ZoomTransform) {
      onTransformChangeRef.current(t);
    }
    // Animated variant for discrete state changes (double-tap). Splits
    // the transition rule and the transform change across two paint
    // frames: first render commits a non-zero `transition` rule with
    // the *old* transform value, then rAF fires the actual transform
    // update — that ordering is what lets the browser interpolate. If
    // both flipped in one render, the spec lets the browser skip the
    // animation (the new `transition` describes *future* changes; the
    // simultaneous transform change has no prior baseline to animate
    // from). The setTimeout clears the rule a hair past the animation
    // length so any subsequent real-time gesture (pinch / wheel) sees
    // `transition: none` again.
    function setTransformAnimated(t: ZoomTransform, ms: number) {
      setTransitionMsRef.current(ms);
      animationFrameId = requestAnimationFrame(() => {
        animationFrameId = undefined;
        onTransformChangeRef.current(t);
      });
      transitionTimerId = window.setTimeout(() => {
        transitionTimerId = undefined;
        setTransitionMsRef.current(0);
      }, ms + 100);
    }
    // The "gesture zone" is the entire scene-player area (matching
    // `data-scene-player` on the player root), not just this
    // wrapper. PlayerControls / PlayerPoster / reload-spinner are
    // siblings of the wrapper, so a pinch landing on the controls
    // layer wouldn't pass `wrapper.contains` even though it's
    // visually on the video. Walking up to the player root covers
    // all of those.
    function isInside(target: EventTarget | null): boolean {
      const el = wrapperRef.current;
      if (!el || !target || !(target instanceof Node)) return false;
      const playerEl = el.closest("[data-scene-player]");
      if (!playerEl) return false;
      return playerEl.contains(target);
    }

    function elementCoords(clientX: number, clientY: number) {
      const el = wrapperRef.current;
      if (!el) return { x: 0, y: 0 };
      const rect = el.getBoundingClientRect();
      return {
        x: clientX - rect.left - rect.width / 2,
        y: clientY - rect.top - rect.height / 2,
      };
    }

    // Clamp pan so the scaled frame's edges never reveal letterbox
    // margin against the wrapper. At scale = 1 the only valid pan is
    // (0, 0), which forces a clean reset whenever the user pinches
    // back to 1×.
    function constrain(scale: number, x: number, y: number) {
      const el = wrapperRef.current;
      if (!el) return { scale, x, y };
      const rect = el.getBoundingClientRect();
      const maxX = (rect.width * (scale - 1)) / 2;
      const maxY = (rect.height * (scale - 1)) / 2;
      return {
        scale,
        x: Math.max(-maxX, Math.min(maxX, x)),
        y: Math.max(-maxY, Math.min(maxY, y)),
      };
    }

    // Compute (scale, pan) such that `anchor` (in element-local
    // coords, origin at element center) maps to the same on-screen
    // position before and after the zoom. world = (a − basePan) /
    // baseScale; we want world * newScale + newPan = a, so
    // newPan = a − (a − basePan) · (newScale / baseScale).
    function zoomAroundAnchor(
      newScale: number,
      anchor: { x: number; y: number },
      basePan: { x: number; y: number },
      baseScale: number,
    ) {
      const ratio = newScale / baseScale;
      const newX = anchor.x - (anchor.x - basePan.x) * ratio;
      const newY = anchor.y - (anchor.y - basePan.y) * ratio;
      return constrain(newScale, newX, newY);
    }

    function applyPanDelta(dx: number, dy: number) {
      const cur = transformRef.current;
      setTransform(constrain(cur.scale, cur.x + dx, cur.y + dy));
    }

    const isScaled = () => transformRef.current.scale > 1.001;

    // ── Pinch (2-finger touch) ────────────────────────────────────
    let pinching = false;
    let pinchInitialDist = 0;
    let pinchInitialScale = 1;
    let pinchInitialPan = { x: 0, y: 0 };
    let pinchAnchor = { x: 0, y: 0 };

    function startPinch(t1: Touch, t2: Touch) {
      pinching = true;
      pinchInitialDist = Math.hypot(
        t2.clientX - t1.clientX,
        t2.clientY - t1.clientY,
      );
      pinchInitialScale = transformRef.current.scale;
      pinchInitialPan = {
        x: transformRef.current.x,
        y: transformRef.current.y,
      };
      pinchAnchor = elementCoords(
        (t1.clientX + t2.clientX) / 2,
        (t1.clientY + t2.clientY) / 2,
      );
      // Cancel any in-flight 1-finger pan candidacy.
      panEnd(touchPan);
      onActiveGestureRef.current?.();
    }

    // ── Touch / mouse pan (shared state machine) ──────────────────
    const touchPan = newPanTracker();
    const mousePan = newPanTracker();
    // Set when `touchend` resets a previously-active touch pan to idle —
    // the synthesized `pointerup` fires right after, but by then the
    // state-machine check below reads "idle" and would let vjs's
    // container `pointerup` listener run `setActive` (the controls
    // feature's catch-all branch for non-tap pointerups), surfacing
    // controls at the *end* of every pan. The flag carries the
    // "was-panning" signal across the touchend→pointerup gap so the
    // pan-ending pointerup gets the same stopImmediatePropagation
    // treatment as the in-flight pan moves.
    let touchPanJustEnded = false;

    // ── Safari trackpad pinch (gesturestart/change/end) ───────────
    let gesturePinching = false;
    let gestureInitialScale = 1;
    let gestureInitialPan = { x: 0, y: 0 };
    let gestureAnchor = { x: 0, y: 0 };

    type SafariGestureEvent = Event & {
      scale: number;
      clientX: number;
      clientY: number;
    };
    const isSafariGestureEvent = (e: Event): e is SafariGestureEvent =>
      "scale" in e && typeof (e as { scale: unknown }).scale === "number";

    // Set true on a mouse pointerup that ends an active pan, so the
    // click event the browser synthesizes immediately afterward gets
    // swallowed before reaching the controls' onClick (otherwise
    // every drag would toggle play/pause).
    let suppressNextClick = false;

    // ── Double-tap (touch) ────────────────────────────────────────
    // Track the previous tap's wall-clock time + position so a
    // second tap close in time and space toggles zoom. The first
    // tap of a pair flows through unintercepted — its synthesized
    // click reaches the player's mute-toggle / no-op handler — so
    // the only quirk is that on a muted player a double-tap also
    // unmutes; that's a rare overlap and the fix would cost a 300ms
    // click delay which feels worse.
    let lastTapAt = 0;
    let lastTapPos = { x: 0, y: 0 };

    function isInteractive(target: EventTarget | null): boolean {
      if (!(target instanceof Element)) return false;
      const control = target.closest(
        'button, a, [role="button"], [role="link"], input, select, textarea',
      );
      // The full-frame play surface is a native button for keyboard access,
      // but still belongs to the video's gesture area. Toolbar controls do not.
      return !!control && !control.hasAttribute("data-video-gesture-surface");
    }

    /** Returns true if the tap completed a zoom toggle. */
    function tryDoubleTap(e: TouchEvent, t: Touch): boolean {
      // Skip when the tap landed on a player control (skip-back,
      // play/pause, skip-forward, the position slider, etc.) so
      // those keep their double-tap-as-double-click semantics.
      if (isInteractive(e.target)) {
        lastTapAt = 0;
        return false;
      }
      const now = Date.now();
      const close =
        Math.hypot(t.clientX - lastTapPos.x, t.clientY - lastTapPos.y) <
        DOUBLE_TAP_MAX_PX;
      if (now - lastTapAt < DOUBLE_TAP_MAX_MS && close) {
        const cur = transformRef.current;
        const target =
          cur.scale > 1.001
            ? IDENTITY_TRANSFORM
            : zoomAroundAnchor(
                DOUBLE_TAP_ZOOM,
                elementCoords(t.clientX, t.clientY),
                { x: 0, y: 0 },
                1,
              );
        setTransformAnimated(target, DOUBLE_TAP_ANIM_MS);
        // Reset so a third tap starts a fresh pair instead of
        // immediately re-triggering against this same timestamp.
        lastTapAt = 0;
        return true;
      }
      lastTapAt = now;
      lastTapPos = { x: t.clientX, y: t.clientY };
      return false;
    }

    // ── Touch handlers ────────────────────────────────────────────
    function onTouchStart(e: TouchEvent) {
      if (!isInside(e.target)) return;
      if (e.touches.length === 2) {
        startPinch(e.touches[0], e.touches[1]);
        e.stopImmediatePropagation();
        e.preventDefault();
        return;
      }
      if (e.touches.length === 1 && !pinching) {
        const t = e.touches[0];
        if (tryDoubleTap(e, t)) {
          // Suppress the synthesized click of the second tap so the
          // double-tap doesn't also toggle mute / hit any underlying
          // handler. The first tap of the pair already flowed through.
          // The first tap's `click` may have scheduled a deferred
          // controls toggle (see `PlayerControls`); fire onActiveGesture
          // so it can cancel that (and any in-flight long-press timer)
          // before they commit.
          onActiveGestureRef.current?.();
          panEnd(touchPan);
          e.stopImmediatePropagation();
          e.preventDefault();
          return;
        }
        if (isScaled()) {
          panBegin(touchPan, t.clientX, t.clientY, t.identifier);
          // No stopImmediatePropagation / preventDefault — let
          // touchstart through so a tap synthesizes a click.
        }
      }
    }

    function onTouchMove(e: TouchEvent) {
      if (!isInside(e.target)) return;
      if (e.touches.length === 1 && isTemporarySpeedActiveRef.current?.()) {
        e.stopImmediatePropagation();
        e.preventDefault();
        return;
      }
      if (pinching && e.touches.length >= 2) {
        const [t1, t2] = [e.touches[0], e.touches[1]];
        const dist = Math.hypot(
          t2.clientX - t1.clientX,
          t2.clientY - t1.clientY,
        );
        const ratio = dist / pinchInitialDist;
        const newScale = Math.max(
          MIN_SCALE,
          Math.min(MAX_SCALE, pinchInitialScale * ratio),
        );
        setTransform(
          zoomAroundAnchor(
            newScale,
            pinchAnchor,
            pinchInitialPan,
            pinchInitialScale,
          ),
        );
        e.stopImmediatePropagation();
        e.preventDefault();
        return;
      }
      if (e.touches.length === 1 && touchPan.state !== "idle") {
        const t = e.touches[0];
        if (t.identifier !== touchPan.id) return;
        const wasPending = touchPan.state === "pending";
        const delta = panAdvance(touchPan, t.clientX, t.clientY);
        if (!delta) return;
        if (wasPending) {
          // Just promoted to active. Steal pointer capture from
          // any slide-swipe handler that was tracking the first
          // finger so it gets `lostpointercapture` and aborts.
          const wrapper = wrapperRef.current;
          if (wrapper) {
            try {
              wrapper.setPointerCapture(t.identifier);
            } catch {
              /* touch identifier may not match a pointerId on every
                 browser; the stopImmediatePropagation chain below is
                 the primary defence regardless. */
            }
          }
          onActiveGestureRef.current?.();
        }
        applyPanDelta(delta.dx, delta.dy);
        e.stopImmediatePropagation();
        e.preventDefault();
      }
    }

    function onTouchEnd(e: TouchEvent) {
      if (pinching && e.touches.length < 2) {
        pinching = false;
        pinchInitialDist = 0;
        // If exactly one finger remains and we're zoomed, hand off
        // to a pending 1-finger pan with that finger so the user
        // can keep panning without lifting and re-tapping.
        if (e.touches.length === 1 && isScaled()) {
          const t = e.touches[0];
          panBegin(touchPan, t.clientX, t.clientY, t.identifier);
        }
      }
      if (e.touches.length === 0) {
        if (panEnd(touchPan)) touchPanJustEnded = true;
      }
    }

    // ── Wheel ─────────────────────────────────────────────────────
    function onWheel(e: WheelEvent) {
      if (!isInside(e.target)) return;
      // On Safari, gesture events take precedence — wheel events
      // can fire alongside the gesture stream during a pinch and
      // would double-apply the zoom.
      if (gesturePinching) {
        e.preventDefault();
        e.stopImmediatePropagation();
        return;
      }
      // Trackpad pinch and Ctrl+scroll surface as wheel events with
      // `ctrlKey: true` — always zoom, regardless of current scale.
      if (e.ctrlKey) {
        const cur = transformRef.current;
        const factor = Math.exp(-e.deltaY * 0.01);
        const newScale = Math.max(
          MIN_SCALE,
          Math.min(MAX_SCALE, cur.scale * factor),
        );
        const anchor = elementCoords(e.clientX, e.clientY);
        setTransform(
          zoomAroundAnchor(newScale, anchor, { x: cur.x, y: cur.y }, cur.scale),
        );
        e.preventDefault();
        e.stopImmediatePropagation();
        return;
      }
      // Plain wheel when zoomed: two-finger trackpad swipe / mouse
      // wheel pans the frame. Subtract the deltas so a rightward /
      // downward swipe reveals content to the right / below
      // (translates the inner content left / up). At scale = 1
      // this falls through so the lightbox's wheel-lock + YARL's
      // wheel-driven slide-nav still work.
      if (isScaled()) {
        applyPanDelta(-e.deltaX, -e.deltaY);
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    }

    // ── Safari gesture handlers ───────────────────────────────────
    function onGestureStart(e: Event) {
      if (!isInside(e.target)) return;
      if (!isSafariGestureEvent(e)) return;
      gesturePinching = true;
      gestureInitialScale = transformRef.current.scale;
      gestureInitialPan = {
        x: transformRef.current.x,
        y: transformRef.current.y,
      };
      gestureAnchor = elementCoords(e.clientX, e.clientY);
      e.preventDefault();
      e.stopImmediatePropagation();
    }

    function onGestureChange(e: Event) {
      if (!gesturePinching || !isSafariGestureEvent(e)) return;
      const newScale = Math.max(
        MIN_SCALE,
        Math.min(MAX_SCALE, gestureInitialScale * e.scale),
      );
      setTransform(
        zoomAroundAnchor(
          newScale,
          gestureAnchor,
          gestureInitialPan,
          gestureInitialScale,
        ),
      );
      e.preventDefault();
      e.stopImmediatePropagation();
    }

    function onGestureEnd(e: Event) {
      if (!gesturePinching) return;
      gesturePinching = false;
      e.preventDefault();
      e.stopImmediatePropagation();
    }

    // ── Pointer (mouse pan + touch pointer interception) ──────────
    function onPointerDown(e: PointerEvent) {
      if (!isInside(e.target)) return;
      if (e.pointerType === "mouse") {
        if (!isScaled()) return;
        // Record start position for threshold check on subsequent
        // moves. Don't intercept yet — a quick click should still
        // toggle pause (intercepting pointerdown suppresses click).
        panBegin(mousePan, e.clientX, e.clientY, e.pointerId);
        return;
      }
      // Touch pointers fire alongside touch events. Only intercept
      // when we're already in an active gesture; the pending state
      // must let pointerdown propagate so YARL/controls observe the
      // initial touch normally and a tap synthesizes a click.
      if (e.pointerType === "touch") {
        if (pinching || touchPan.state === "active") {
          e.stopImmediatePropagation();
          e.preventDefault();
        }
      }
    }

    function onPointerMove(e: PointerEvent) {
      if (e.pointerType === "mouse") {
        if (mousePan.state === "idle" || e.pointerId !== mousePan.id) return;
        const wasPending = mousePan.state === "pending";
        const delta = panAdvance(mousePan, e.clientX, e.clientY);
        if (!delta) return;
        if (wasPending) {
          const wrapper = wrapperRef.current;
          if (wrapper) {
            try {
              wrapper.setPointerCapture(e.pointerId);
            } catch {
              /* ignore */
            }
          }
        }
        applyPanDelta(delta.dx, delta.dy);
        e.stopImmediatePropagation();
        e.preventDefault();
        return;
      }
      if (e.pointerType === "touch") {
        // Includes `pending` (pre-threshold pan candidacy): keep the
        // lightbox carousel from beginning a slide swipe while this
        // scaled frame is deciding whether the gesture is a local pan.
        // Video.js itself ignores touch pointermove for controls
        // activity; mouse pointermove still reveals controls normally.
        if (pinching || touchPan.state !== "idle") {
          e.stopImmediatePropagation();
          e.preventDefault();
        }
      }
    }

    function onPointerUp(e: PointerEvent) {
      if (e.pointerType === "mouse") {
        if (e.pointerId !== mousePan.id) return;
        const wasPanning = panEnd(mousePan);
        const wrapper = wrapperRef.current;
        if (wrapper) {
          try {
            wrapper.releasePointerCapture(e.pointerId);
          } catch {
            /* ignore */
          }
        }
        if (wasPanning) {
          suppressNextClick = true;
          e.stopImmediatePropagation();
          e.preventDefault();
        }
        return;
      }
      if (e.pointerType === "touch") {
        // `touchend` resets the tracker to idle before this fires, so
        // we also honour the one-shot `touchPanJustEnded` flag set by
        // `onTouchEnd` — without it the pan-completing pointerup falls
        // through to vjs's `else setActive()` branch and re-surfaces
        // the controls right when the user releases their finger.
        if (pinching || touchPan.state === "active" || touchPanJustEnded) {
          e.stopImmediatePropagation();
        }
        touchPanJustEnded = false;
      }
    }

    function onClick(e: MouseEvent) {
      if (!suppressNextClick) return;
      if (!isInside(e.target)) return;
      suppressNextClick = false;
      e.stopImmediatePropagation();
      e.preventDefault();
    }

    // ── Listener registration ─────────────────────────────────────
    const cleanup: Array<() => void> = [];
    function bind<K extends keyof WindowEventMap>(
      name: K,
      handler: (e: WindowEventMap[K]) => void,
      options?: AddEventListenerOptions,
    ): void;
    function bind(
      name: string,
      handler: EventListener,
      options?: AddEventListenerOptions,
    ): void;
    function bind(
      name: string,
      handler: EventListener,
      options: AddEventListenerOptions = { capture: true },
    ) {
      window.addEventListener(name, handler, options);
      cleanup.push(() => window.removeEventListener(name, handler, options));
    }

    const passive = { capture: true, passive: false };
    const capture = { capture: true };

    bind("touchstart", onTouchStart, passive);
    bind("touchmove", onTouchMove, passive);
    bind("touchend", onTouchEnd, capture);
    bind("touchcancel", onTouchEnd, capture);
    bind("wheel", onWheel, passive);
    bind("pointerdown", onPointerDown, passive);
    bind("pointermove", onPointerMove, passive);
    bind("pointerup", onPointerUp, capture);
    bind("pointercancel", onPointerUp, capture);
    bind("click", onClick, capture);
    // Safari-only multi-touch trackpad gestures.
    bind("gesturestart", onGestureStart, passive);
    bind("gesturechange", onGestureChange, passive);
    bind("gestureend", onGestureEnd, passive);

    return () => {
      for (const fn of cleanup) fn();
      if (animationFrameId !== undefined) {
        cancelAnimationFrame(animationFrameId);
      }
      if (transitionTimerId !== undefined) {
        window.clearTimeout(transitionTimerId);
      }
    };
  }, [enabled]);

  const isZoomed = transform.scale > 1.001;

  return (
    <div
      ref={wrapperRef}
      className="absolute inset-0 overflow-hidden"
      // Opt this region out of the global page-pinch-zoom guard
      // (`installPagePinchZoomGuard`) so iOS gesturestart and
      // desktop ctrl+wheel reach the component-level handlers below
      // instead of being preventDefault'd at window level.
      data-pinch-zoom-allowed={enabled ? "" : undefined}
      data-video-frame-zoom=""
      data-zoom-enabled={enabled ? "" : undefined}
      style={{ cursor: enabled && isZoomed ? "grab" : undefined }}
    >
      <div
        className="w-full h-full"
        style={{
          transform: enabled
            ? `translate3d(${transform.x}px, ${transform.y}px, 0) scale(${transform.scale})`
            : undefined,
          transformOrigin: enabled ? "center center" : undefined,
          willChange: enabled ? "transform" : undefined,
          // Off by default so pinch / pan / wheel track fingers in
          // real-time; double-tap flips this to a non-zero duration
          // for one paint cycle so its discrete jump animates.
          transition: enabled
            ? transitionMs > 0
              ? `transform ${transitionMs}ms ease`
              : "none"
            : undefined,
          // Suppress native browser gestures on the transform target.
          // Pinch and double-tap zoom are implemented locally above so
          // they scale only the scene frame. The outer wrapper keeps its
          // default touch-action so controls remain interactive.
          touchAction: enabled ? "none" : undefined,
        }}
      >
        {children}
      </div>
    </div>
  );
}
