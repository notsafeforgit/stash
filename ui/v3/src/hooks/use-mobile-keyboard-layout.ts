import { useLayoutEffect, useRef, useState } from "react";

interface VisualViewportGeometry {
  height: number;
  offsetTop: number;
}

export function calculateVisualViewportBottomInset(
  layoutHeight: number,
  viewport: VisualViewportGeometry,
) {
  return Math.max(
    0,
    Math.round(layoutHeight - viewport.height - viewport.offsetTop),
  );
}

function acceptsTextInput(element: Element | null) {
  return (
    element instanceof HTMLInputElement ||
    element instanceof HTMLTextAreaElement ||
    (element instanceof HTMLElement && element.isContentEditable)
  );
}

/**
 * Reserves keyboard space below an in-flow mobile footer. Only this hook owns
 * --mobile-keyboard-inset; CSS uses it for margin and dependent size limits.
 * Write it during viewport events so Safari's native pan and our layout change
 * reach the same paint, without waiting for a React render or an extra frame.
 */
export function useMobileKeyboardLayout<T extends HTMLElement>() {
  const [element, setElement] = useState<T | null>(null);
  const updateRef = useRef<() => void>(() => {});

  useLayoutEffect(() => {
    if (!element) return;

    const viewport = window.visualViewport;
    if (!viewport) return;
    const appViewport = element.closest<HTMLElement>("[data-app-viewport]");

    const update = () => {
      const activeElement = document.activeElement;
      const hasFocusedInput =
        element.contains(activeElement) && acceptsTextInput(activeElement);

      const bottomInset = hasFocusedInput
        ? calculateVisualViewportBottomInset(
            appViewport?.getBoundingClientRect().height ??
              document.documentElement.clientHeight,
            viewport,
          )
        : 0;
      element.style.setProperty("--mobile-keyboard-inset", `${bottomInset}px`);
    };

    updateRef.current = update;

    element.addEventListener("focusin", update);
    element.addEventListener("focusout", update);
    viewport.addEventListener("resize", update);
    viewport.addEventListener("scroll", update);
    window.addEventListener("resize", update);
    // With interactive-widget=resizes-content, Safari shrinks 100dvh for the
    // keyboard after its viewport events, so measure again when the shell does.
    const shellResize =
      appViewport && typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(update)
        : null;
    if (appViewport) shellResize?.observe(appViewport);
    update();

    return () => {
      updateRef.current = () => {};
      element.removeEventListener("focusin", update);
      element.removeEventListener("focusout", update);
      viewport.removeEventListener("resize", update);
      viewport.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
      shellResize?.disconnect();
      element.style.removeProperty("--mobile-keyboard-inset");
    };
  }, [element]);

  // Query completion can make mobile WebKit reset its automatic viewport pan
  // without consistently emitting another VisualViewport event. Re-measure
  // after every bar render so a results update cannot strand the focused bar
  // below the keyboard.
  useLayoutEffect(() => updateRef.current());

  return setElement;
}
