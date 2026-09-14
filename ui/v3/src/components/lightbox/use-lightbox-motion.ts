import { useCallback, useEffect, useRef } from "react";
import type {
  ControllerRef,
  LightboxExternalProps,
} from "yet-another-react-lightbox";
import { motion } from "@/core/motion";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { useLightboxHistory } from "./use-lightbox-history";

/** YARL owns gesture tracking, reduced motion and the media lifecycle. */
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
  const onCloseRef = useCommittedRef(onClose);
  useEffect(() => {
    if (open) closing.current = false;
  }, [open]);
  const requestClose = useCallback(() => {
    if (closing.current) return;
    closing.current = true;
    if (controllerRef.current) controllerRef.current.close();
    else onCloseRef.current();
  }, []);
  const onExiting = useCallback(() => {
    closing.current = true;
  }, []);
  const finishClose = useLightboxHistory(open, onClose, requestClose);
  return { controllerRef, requestClose, finishClose, onExiting };
}
