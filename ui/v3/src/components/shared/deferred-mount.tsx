import {
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";

/** Mount nearby content once, retaining its state when it scrolls out of view. */
export function DeferredMount({
  children,
  fallback,
  eager = false,
  scrollRoot,
}: {
  children: ReactNode;
  fallback: ReactNode;
  eager?: boolean;
  scrollRoot?: RefObject<Element | null>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [mounted, setMounted] = useState(eager);

  useEffect(() => {
    if (mounted) return;
    if (typeof IntersectionObserver === "undefined") {
      setMounted(true);
      return;
    }
    const element = ref.current;
    if (!element) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return;
        setMounted(true);
        observer.disconnect();
      },
      { root: scrollRoot?.current, rootMargin: "160px 0px" },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [mounted, scrollRoot]);

  return <div ref={ref}>{mounted ? children : fallback}</div>;
}
