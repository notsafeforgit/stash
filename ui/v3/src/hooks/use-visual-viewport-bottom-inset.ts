import { useEffect, useState } from "react";

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
          ? calculateVisualViewportBottomInset(window.innerHeight, viewport)
          : 0,
      );
    };

    const scheduleUpdate = () => {
      if (frame !== null) cancelAnimationFrame(frame);
      frame = requestAnimationFrame(update);
    };

    element.addEventListener("focusin", scheduleUpdate);
    element.addEventListener("focusout", scheduleUpdate);
    viewport.addEventListener("resize", scheduleUpdate);
    viewport.addEventListener("scroll", scheduleUpdate);
    window.addEventListener("resize", scheduleUpdate);
    scheduleUpdate();

    return () => {
      if (frame !== null) cancelAnimationFrame(frame);
      element.removeEventListener("focusin", scheduleUpdate);
      element.removeEventListener("focusout", scheduleUpdate);
      viewport.removeEventListener("resize", scheduleUpdate);
      viewport.removeEventListener("scroll", scheduleUpdate);
      window.removeEventListener("resize", scheduleUpdate);
    };
  }, [element]);

  return { bottomInset, ref: setElement };
}
