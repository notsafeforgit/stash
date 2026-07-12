import { useEffect, useLayoutEffect, useRef, useState } from "react";

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
 * Tracks the part of the layout viewport hidden below the visual viewport
 * while a text field inside the returned element is focused.
 */
export function useVisualViewportBottomInset<T extends HTMLElement>() {
  const [element, setElement] = useState<T | null>(null);
  const [bottomInset, setBottomInset] = useState(0);
  const updateRef = useRef<() => void>(() => {});

  useEffect(() => {
    if (!element) return;

    const viewport = window.visualViewport;
    if (!viewport) return;

    let frame: number | null = null;

    const update = () => {
      frame = null;
      const activeElement = document.activeElement;
      const hasFocusedInput =
        element.contains(activeElement) && acceptsTextInput(activeElement);

      setBottomInset(
        hasFocusedInput
          ? calculateVisualViewportBottomInset(
              element
                .closest<HTMLElement>("[data-app-viewport]")
                ?.getBoundingClientRect().height ??
                document.documentElement.clientHeight,
              viewport,
            )
          : 0,
      );
    };

    const scheduleUpdate = () => {
      if (frame !== null) cancelAnimationFrame(frame);
      frame = requestAnimationFrame(update);
    };

    updateRef.current = update;

    element.addEventListener("focusin", scheduleUpdate);
    element.addEventListener("focusout", scheduleUpdate);
    viewport.addEventListener("resize", scheduleUpdate);
    viewport.addEventListener("scroll", scheduleUpdate);
    window.addEventListener("resize", scheduleUpdate);
    scheduleUpdate();

    return () => {
      updateRef.current = () => {};
      if (frame !== null) cancelAnimationFrame(frame);
      element.removeEventListener("focusin", scheduleUpdate);
      element.removeEventListener("focusout", scheduleUpdate);
      viewport.removeEventListener("resize", scheduleUpdate);
      viewport.removeEventListener("scroll", scheduleUpdate);
      window.removeEventListener("resize", scheduleUpdate);
    };
  }, [element]);

  // Query completion can make mobile WebKit reset its automatic viewport pan
  // without consistently emitting another VisualViewport event. Re-measure
  // after every bar render so a results update cannot strand the focused bar
  // below the keyboard.
  useLayoutEffect(() => updateRef.current());

  return { bottomInset, ref: setElement };
}
