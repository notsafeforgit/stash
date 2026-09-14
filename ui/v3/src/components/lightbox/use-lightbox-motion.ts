import { useCallback, useEffect, useRef } from "react";
import type {
  ControllerRef,
  LightboxExternalProps,
} from "yet-another-react-lightbox";
import { motion } from "@/core/motion";
import { createPaintAnimation } from "@/core/paint-animation";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { useLightboxHistory } from "./use-lightbox-history";

/** YARL owns gestures and exit completion. The entrance waits for a paint. */
export const lightboxAnimation = {
  fade: motion.duration.lightbox,
  swipe: motion.duration.swipe,
  navigation: motion.duration.lightbox,
  easing: {
    fade: motion.easing.fade,
    swipe: "ease-out",
    navigation: motion.easing.fade,
  },
} satisfies LightboxExternalProps["animation"];

/** Mobile Close, keyboard dismissal and browser Back all finish YARL's exit
 * before unmounting its media. History is consumed exactly once. */
export function useLightboxMotion(open: boolean, onClose: () => void) {
  const controllerRef = useRef<ControllerRef>(null);
  const closing = useRef(false);
  const surface = useRef<HTMLElement | null>(null);
  const visual = useRef<ReturnType<typeof createPaintAnimation> | null>(null);
  const onCloseRef = useCommittedRef(onClose);
  useEffect(() => {
    if (open) closing.current = false;
    else visual.current?.cancel();
  }, [open]);
  useEffect(
    () => () => {
      visual.current?.cancel();
    },
    [],
  );
  const onSurfaceReady = useCallback((element: HTMLElement) => {
    surface.current = element;
    visual.current ??= createPaintAnimation();
    // An empty layer reveals opaque media. Fading or scaling the media subtree
    // itself can drop almost every frame in WebKit at phone pixel densities.
    visual.current.play(element, [{ opacity: 0.99 }, { opacity: 0 }], {
      duration: motion.duration.lightboxEnter,
      easing: motion.easing.reveal,
      afterPaint: true,
      id: "lightbox-enter",
    });
    return () => {
      visual.current?.cancel();
      surface.current = null;
    };
  }, []);
  const requestClose = useCallback(() => {
    if (closing.current) return;
    closing.current = true;
    if (controllerRef.current) controllerRef.current.close();
    else onCloseRef.current();
  }, []);
  const onExiting = useCallback(() => {
    closing.current = true;
    const element = surface.current;
    if (!element || !visual.current) return;
    const opacity = getComputedStyle(element).opacity;
    visual.current.play(element, [{ opacity }, { opacity: 1 }], {
      duration: motion.duration.lightbox,
      easing: motion.easing.reveal,
      id: "lightbox-exit",
      hold: true,
    });
  }, []);
  const finishClose = useLightboxHistory(open, onClose, requestClose);
  const portal = {
    container: {
      // YARL still owns its close timer. Its CSS opacity transition must not
      // composite the entire media tree alongside our smaller paint surface.
      style:
        typeof Element !== "undefined" &&
        typeof Element.prototype.animate === "function"
          ? { opacity: 1, transition: "none" }
          : undefined,
    },
  } satisfies LightboxExternalProps["portal"];
  return {
    controllerRef,
    portal,
    requestClose,
    finishClose,
    onSurfaceReady,
    onExiting,
  };
}
