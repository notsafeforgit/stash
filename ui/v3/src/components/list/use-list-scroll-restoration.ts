import { useLayoutEffect, useState } from "react";
import {
  trimPathRight,
  useElementScrollRestoration,
  useLocation,
  useMatch,
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
  const pathname = useMatch({
    strict: false,
    select: (match) => match.pathname,
  });
  const location = useLocation();
  const locationHref = getScrollRestorationKey(location);
  const [retainedHref, setRetainedHref] = useState(locationHref);
  // The destination URL arrives before the outgoing route unmounts. Keep
  // that list's offset and geometry on its own URL through the transition.
  // Read the rendered match so reused detail routes can change entity params.
  const href =
    trimPathRight(location.pathname) === trimPathRight(pathname)
      ? locationHref
      : retainedHref;
  if (retainedHref !== href) setRetainedHref(href);
  const entry = useElementScrollRestoration({
    id,
    getKey: () => href,
  });
  // URL changes can precede the filter's effect-driven update on browser Back.
  // Keep the data identity too, so the final page receives its own restoration.
  const key = JSON.stringify([id, href, contentKey]);
  // React owns these snapshots: an abandoned render must not re-arm a
  // restoration that the committed list has already consumed. Conditional
  // state adjustment rerenders this hook before its children commit.
  const [pending, setPending] = useState({ key, entry });
  if (pending.key !== key) setPending({ key, entry });

  useLayoutEffect(() => {
    if (!element || !ready || pending.key !== key) return;
    const saved = pending.entry;
    if (!saved) return;
    element.scrollLeft = saved.scrollX;
    element.scrollTop = saved.scrollY;
    setPending({ key, entry: undefined });
  }, [element, ready, key, pending]);

  return { restorationKey: key, initialOffset: entry?.scrollY };
}
