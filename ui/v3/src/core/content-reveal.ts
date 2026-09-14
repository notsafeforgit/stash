import { motion } from "./motion";

export type ContentRevealKind =
  | "route-forward"
  | "route-back"
  | "route-replace"
  | "list-view"
  | "detail-tab"
  | "focused-view";

/** Animate an empty paint surface, keeping images, players and scrollers still.
 * Owns one effect at a time, including pending frames and overlay exit holds. */
export function createContentReveal() {
  const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
  let animation: Animation | undefined;
  let surface: HTMLElement | undefined;
  let pending: { id: ContentRevealKind; duration: number } | undefined;
  let frame: number | undefined;
  const holds = new Set<symbol>();
  const hide = () => {
    if (surface) {
      surface.hidden = true;
      surface.style.removeProperty("opacity");
    }
    surface = undefined;
  };
  const cancel = () => {
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
    pending = undefined;
    animation?.cancel();
    animation = undefined;
    hide();
  };
  const start = () => {
    frame = undefined;
    if (holds.size || !pending || !surface) return;
    if (preference.matches || document.hidden || !surface.isConnected) {
      cancel();
      return;
    }
    const running = surface.animate(
      [{ opacity: motion.contentCoverOpacity }, { opacity: 0 }],
      {
        ...pending,
        easing: motion.easing.reveal,
      },
    );
    pending = undefined;
    animation = running;
    const finish = () => {
      if (animation !== running) return;
      running.cancel();
      animation = undefined;
      hide();
    };
    void running.finished.then(finish, finish);
  };
  const schedule = () => {
    if (holds.size || !pending || frame !== undefined) return;
    // Layout must get a paint before the short animation's clock starts.
    frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(start);
    });
  };
  preference.addEventListener("change", cancel);
  document.addEventListener("visibilitychange", cancel);
  return {
    cancel,
    play(target: HTMLElement | null, id: ContentRevealKind, duration: number) {
      cancel();
      if (!target?.animate || preference.matches || document.hidden) return;
      surface = target;
      surface.hidden = false;
      surface.style.opacity = String(motion.contentCoverOpacity);
      pending = { id, duration };
      schedule();
    },
    hold() {
      const token = Symbol();
      holds.add(token);
      cancel();
      return () => {
        if (holds.delete(token)) schedule();
      };
    },
    dispose() {
      preference.removeEventListener("change", cancel);
      document.removeEventListener("visibilitychange", cancel);
      cancel();
      holds.clear();
    },
  };
}
