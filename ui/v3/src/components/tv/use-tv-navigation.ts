import { useCallback, useEffect, useRef, type RefObject } from "react";
import { flushSync } from "react-dom";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { motion } from "@/core/motion";

/** One functional slide transition commits once; drag and animation frames
 * update only the stable strip, never the feed's React state. */
export function useTvNavigation(
  strip: RefObject<HTMLDivElement | null>,
  select: (direction: -1 | 1) => void,
) {
  const latest = useCommittedRef(select);
  const animation = useRef<Animation | null>(null);
  const serial = useRef(0);
  const move = useCallback(
    (direction: -1 | 1) => {
      if (animation.current) return;
      const element = strip.current;
      if (
        !element ||
        window.matchMedia("(prefers-reduced-motion: reduce)").matches ||
        typeof element.animate !== "function"
      ) {
        latest.current(direction);
        return;
      }
      const token = ++serial.current;
      const running = element.animate(
        [
          { transform: element.style.transform || "translateY(0)" },
          { transform: `translateY(${-direction * 100}%)` },
        ],
        {
          duration: motion.duration.swipe,
          easing: motion.easing.fade,
          fill: "forwards",
        },
      );
      animation.current = running;
      void running.finished
        .then(() => {
          if (serial.current !== token) return;
          // Selection commits while the retained center is offscreen; resetting the
          // functional transform happens in the same task as the new React render.
          flushSync(() => latest.current(direction));
          element.style.transform = "";
          running.cancel();
          animation.current = null;
        })
        .catch(() => {});
    },
    [strip],
  );
  const drag = useCallback(
    (offset: number) => {
      if (!animation.current && strip.current)
        strip.current.style.transform = `translateY(${offset}px)`;
    },
    [strip],
  );
  const cancel = useCallback(() => {
    serial.current++;
    animation.current?.cancel();
    animation.current = null;
    if (strip.current) strip.current.style.transform = "";
  }, [strip]);
  useEffect(() => {
    document.addEventListener("visibilitychange", cancel);
    return () => {
      document.removeEventListener("visibilitychange", cancel);
      cancel();
    };
  }, [cancel]);
  return { move, drag, cancel };
}
