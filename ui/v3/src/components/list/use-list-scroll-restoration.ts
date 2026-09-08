import { useLayoutEffect, useRef } from "react";
import {
  useElementScrollRestoration,
  useLocation,
} from "@tanstack/react-router";
import { getScrollRestorationKey } from "@/core/scroll-restoration";

/** TanStack owns the cache and navigation lifecycle. Lists may still be
 * showing skeletons when the router restores, so apply that same entry once
 * their data is ready. Never keep forcing an offset during normal scrolling. */
export function useListScrollRestoration(
  id: string,
  element: HTMLElement | null,
  ready: boolean,
  contentKey: string,
) {
  const href = useLocation({ select: getScrollRestorationKey });
  const entry = useElementScrollRestoration({
    id,
    getKey: getScrollRestorationKey,
  });
  // URL changes can precede the filter's effect-driven update on browser Back.
  // Keep the data identity too, so the final page receives its own restoration.
  const key = JSON.stringify([id, href, contentKey]);
  const pending = useRef({ key, entry });
  if (pending.current.key !== key) pending.current = { key, entry };

  useLayoutEffect(() => {
    if (!element || !ready || pending.current.key !== key) return;
    const saved = pending.current.entry;
    if (!saved) return;
    element.scrollLeft = saved.scrollX;
    element.scrollTop = saved.scrollY;
    pending.current.entry = undefined;
  }, [element, ready, key]);

  return { restorationKey: key, initialOffset: entry?.scrollY };
}
