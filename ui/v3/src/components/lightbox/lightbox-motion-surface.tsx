import { useLayoutEffect, useRef } from "react";

/** Mounts with YARL's measured controls, before their first paint. */
export function LightboxMotionSurface({
  onReady,
}: {
  onReady: (element: HTMLElement) => () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (ref.current) return onReady(ref.current);
  }, [onReady]);
  return (
    <div
      ref={ref}
      aria-hidden="true"
      data-lightbox-reveal=""
      className="pointer-events-none absolute inset-0 z-100 bg-black opacity-0"
    />
  );
}
