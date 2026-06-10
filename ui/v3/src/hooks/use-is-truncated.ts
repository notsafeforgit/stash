import { useEffect, useLayoutEffect, useRef, useState } from "react";

/**
 * Tracks whether the element overflows its container horizontally (i.e. is
 * being visually clipped by `truncate` / `max-w-*` / etc.). Re-measures after
 * every render (content changes always re-render) and via ResizeObserver for
 * container-only size changes. Returns `[ref, truncated]`.
 */
export function useIsTruncated<T extends HTMLElement = HTMLElement>() {
  const ref = useRef<T>(null);
  const [truncated, setTruncated] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setTruncated(el.scrollWidth > el.clientWidth + 0.5);
  });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      setTruncated(el.scrollWidth > el.clientWidth + 0.5);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, truncated] as const;
}
