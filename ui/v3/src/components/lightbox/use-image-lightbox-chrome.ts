import { useCallback, useLayoutEffect, useRef, useState } from "react";

/** Share image tap-to-reveal with the mixed viewer. YARL owns the gesture:
 * swipes/pinches don't click, and a double tap must zoom without revealing
 * controls that would intercept its second tap. */
export function useImageLightboxChrome(
  selection: string,
  initiallyRevealed = false,
) {
  const [revealed, setRevealed] = useState(initiallyRevealed);
  const revealedRef = useRef(initiallyRevealed);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastClickAt = useRef(0);
  const cancel = useCallback(() => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
    lastClickAt.current = 0;
  }, []);
  // A delayed tap belongs to its image, never the scene or image after it.
  // biome-ignore lint/correctness/useExhaustiveDependencies: Selection owns the pending gesture's lifetime.
  useLayoutEffect(() => cancel, [selection, cancel]);
  const onClick = useCallback(() => {
    const now = Date.now();
    const doubleTap = now - lastClickAt.current < 300;
    lastClickAt.current = now;
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
    if (doubleTap) return;
    if (revealedRef.current) {
      revealedRef.current = false;
      setRevealed(false);
    } else {
      timer.current = setTimeout(() => {
        timer.current = null;
        revealedRef.current = true;
        setRevealed(true);
      }, 250);
    }
  }, []);
  return { revealed, onClick };
}
