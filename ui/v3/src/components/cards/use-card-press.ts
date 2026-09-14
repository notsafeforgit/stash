import {
  useCallback,
  useEffect,
  useRef,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { createPaintAnimation } from "@/core/paint-animation";
import { motion } from "@/core/motion";

/** A small thumbnail response, starting on finger-down without rerendering the
 * card or delaying its action. Scrolling, selection and nested actions cancel it. */
export function useCardPress() {
  const feedback = useRef<ReturnType<typeof createPaintAnimation> | null>(null);
  const removeGestureListeners = useRef<(() => void) | undefined>(undefined);
  const cancel = useCallback(() => {
    removeGestureListeners.current?.();
    removeGestureListeners.current = undefined;
    feedback.current?.cancel();
  }, []);
  useEffect(() => cancel, [cancel]);

  function onPointerDownCapture(event: ReactPointerEvent<HTMLElement>) {
    cancel();
    if (
      !event.isPrimary ||
      event.button !== 0 ||
      event.ctrlKey ||
      event.metaKey ||
      event.currentTarget.closest("[data-selecting]") ||
      !(event.target instanceof Element) ||
      event.target.closest(
        "button:not([data-card-preview-button]), a[href]:not([data-card-link]), input, [role=checkbox]",
      )
    )
      return;
    const target = event.currentTarget.querySelector<HTMLElement>(
      "[data-entity-card-preview]",
    );
    const preference = matchMedia("(prefers-reduced-motion: reduce)");
    if (!target || typeof target.animate !== "function" || preference.matches)
      return;
    const { pointerId, clientX, clientY } = event;
    feedback.current ??= createPaintAnimation();
    const controller = feedback.current;
    controller.play(target, [{ scale: 1 }, { scale: 1.035 }], {
      id: "card-press",
      duration: motion.duration.press,
      easing: motion.easing.reveal,
      hold: true,
    });
    const up = (released: PointerEvent) => {
      if (released.pointerId !== pointerId) return;
      const scale = getComputedStyle(target).scale;
      removeGestureListeners.current?.();
      removeGestureListeners.current = undefined;
      controller.play(
        target,
        [
          { scale: scale === "none" ? "1" : scale },
          { scale: 1.025, offset: 0.3 },
          { scale: 1 },
        ],
        {
          id: "card-release",
          duration: motion.duration.release,
          easing: motion.easing.reveal,
        },
      );
    };
    const move = (moved: PointerEvent) => {
      if (
        moved.pointerId === pointerId &&
        Math.hypot(moved.clientX - clientX, moved.clientY - clientY) > 10
      )
        cancel();
    };
    window.addEventListener("pointerup", up, { passive: true });
    window.addEventListener("pointermove", move, { passive: true });
    window.addEventListener("pointercancel", cancel, { passive: true });
    window.addEventListener("blur", cancel);
    document.addEventListener("visibilitychange", cancel);
    preference.addEventListener("change", cancel);
    removeGestureListeners.current = () => {
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointercancel", cancel);
      window.removeEventListener("blur", cancel);
      document.removeEventListener("visibilitychange", cancel);
      preference.removeEventListener("change", cancel);
    };
  }
  return { onPointerDownCapture, cancel };
}
