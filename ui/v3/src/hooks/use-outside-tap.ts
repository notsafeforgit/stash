import { useEffect, useRef } from "react";
import { useCommittedRef } from "./use-committed-ref";

/** Consume a touch outside a transient control, without blocking native scroll.
 * Wait for click before dismissing: blurring on touchstart moves the keyboard
 * and can retarget the same gesture onto the newly exposed content.
 */
export function useOutsideTap(options: {
  isActive: () => boolean;
  isInside: (target: Node) => boolean;
  onDismiss: () => void;
}) {
  const latest = useCommittedRef(options);
  useEffect(() => {
    let gesture: { x: number; y: number; moved: boolean } | null = null;

    function start(event: PointerEvent) {
      gesture = null;
      if (
        event.pointerType !== "touch" ||
        !latest.current.isActive() ||
        !(event.target instanceof Node) ||
        latest.current.isInside(event.target)
      )
        return;
      gesture = { x: event.clientX, y: event.clientY, moved: !event.isPrimary };
      // Stop list selection/long-press handlers and Base UI's early dismissal.
      // Do not preventDefault: the browser must still be able to start scrolling.
      event.stopImmediatePropagation();
    }

    function move(event: PointerEvent) {
      if (!gesture) return;
      if (Math.hypot(event.clientX - gesture.x, event.clientY - gesture.y) > 10)
        gesture.moved = true;
      event.stopImmediatePropagation();
    }

    function cancel(event: PointerEvent) {
      if (!gesture) return;
      gesture.moved = true;
      event.stopImmediatePropagation();
    }

    function touch(event: TouchEvent) {
      if (!gesture) return;
      if (event.touches.length > 1) gesture.moved = true;
      // Base UI otherwise treats an outside touchmove as dismissal. Native
      // scrolling does not depend on these events reaching JavaScript handlers.
      event.stopImmediatePropagation();
    }

    function mouse(event: MouseEvent) {
      if (!gesture) return;
      // Compatibility mouse events must not transfer focus before click.
      event.preventDefault();
      event.stopImmediatePropagation();
    }

    function click(event: MouseEvent) {
      const completed = gesture;
      gesture = null;
      if (!completed || event.detail === 0) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      if (!completed.moved && latest.current.isActive())
        latest.current.onDismiss();
    }

    // Window capture precedes both React's delegated handlers and Base UI's
    // document listeners, regardless of which component mounted first.
    window.addEventListener("pointerdown", start, true);
    window.addEventListener("pointermove", move, true);
    window.addEventListener("pointercancel", cancel, true);
    window.addEventListener("pointerup", move, true);
    const touchOptions = { capture: true, passive: true };
    window.addEventListener("touchstart", touch, touchOptions);
    window.addEventListener("touchmove", touch, touchOptions);
    window.addEventListener("touchend", touch, touchOptions);
    window.addEventListener("mousedown", mouse, true);
    window.addEventListener("mouseup", mouse, true);
    window.addEventListener("contextmenu", mouse, true);
    window.addEventListener("click", click, true);
    return () => {
      window.removeEventListener("pointerdown", start, true);
      window.removeEventListener("pointermove", move, true);
      window.removeEventListener("pointercancel", cancel, true);
      window.removeEventListener("pointerup", move, true);
      window.removeEventListener("touchstart", touch, true);
      window.removeEventListener("touchmove", touch, true);
      window.removeEventListener("touchend", touch, true);
      window.removeEventListener("mousedown", mouse, true);
      window.removeEventListener("mouseup", mouse, true);
      window.removeEventListener("contextmenu", mouse, true);
      window.removeEventListener("click", click, true);
    };
  }, [latest]);
}

/** Keep Base UI's keyboard/focus behavior, but consume outside touch activation. */
export function usePopoverOutsideTap(open: boolean, onDismiss: () => void) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popupRef = useRef<HTMLDivElement>(null);
  useOutsideTap({
    isActive: () => open,
    isInside: (target) =>
      !!triggerRef.current?.contains(target) ||
      !!popupRef.current?.contains(target),
    onDismiss,
  });
  return { triggerRef, popupRef };
}
