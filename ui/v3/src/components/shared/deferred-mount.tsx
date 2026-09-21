import {
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import { CardMediaContext } from "@/components/cards/card-media-context";

/** Mount nearby content once, retaining its state when it scrolls out of view. */
export function DeferredMount({
  children,
  fallback,
  eager = false,
  scrollRoot,
  rootMargin = "160px 0px",
  className,
  releaseDistantMedia = false,
}: {
  children: ReactNode;
  fallback: ReactNode;
  eager?: boolean;
  scrollRoot?: RefObject<Element | null>;
  rootMargin?: string;
  className?: string;
  /** Keep mounted state and geometry, but release distant card images/videos. */
  releaseDistantMedia?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [mounted, setMounted] = useState(eager);
  const [nearby, setNearby] = useState(eager);
  const parentMediaActive = useContext(CardMediaContext);

  useEffect(() => {
    if (mounted && !releaseDistantMedia) return;
    if (typeof IntersectionObserver === "undefined") {
      setMounted(true);
      setNearby(true);
      return;
    }
    const element = ref.current;
    if (!element) return;
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.some((entry) => entry.isIntersecting);
        setNearby(visible);
        if (visible) setMounted(true);
        if (visible && !releaseDistantMedia) observer.disconnect();
      },
      { root: scrollRoot?.current, rootMargin },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [mounted, scrollRoot, rootMargin, releaseDistantMedia]);

  return (
    <div ref={ref} className={className}>
      <CardMediaContext
        value={parentMediaActive && (!releaseDistantMedia || nearby)}
      >
        {mounted ? children : fallback}
      </CardMediaContext>
    </div>
  );
}
