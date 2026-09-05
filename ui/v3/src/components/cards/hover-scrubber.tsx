import type React from "react";
import { useRef, useCallback } from "react";
import { cn } from "src/lib/utils";

interface HoverScrubberProps {
  count: number;
  onIndex: (index: number) => void;
  className?: string;
  children?: React.ReactNode;
}

/**
 * Maps pointer X position over a container to a discrete index in [0, count).
 * Invokes onIndex on each mousemove. Renders a thin vertical indicator line
 * that tracks the cursor. Desktop-only — only relevant under `@media (hover: hover)`.
 */
export const HoverScrubber: React.FC<HoverScrubberProps> = ({
  count,
  onIndex,
  className,
  children,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const indicatorRef = useRef<HTMLDivElement>(null);

  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (!containerRef.current || count <= 0) return;
      const { left, width } = containerRef.current.getBoundingClientRect();
      const ratio = Math.max(0, Math.min(1, (e.clientX - left) / width));
      const index = Math.min(count - 1, Math.floor(ratio * count));

      if (indicatorRef.current) {
        indicatorRef.current.style.left = `${ratio * 100}%`;
      }

      onIndex(index);
    },
    [count, onIndex],
  );

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: This optional pointer preview has no action; the card link and preview button remain keyboard accessible.
    <div
      ref={containerRef}
      className={cn("relative", className)}
      onMouseMove={handleMouseMove}
    >
      {children}
      {/* Thin vertical scrub-position indicator */}
      <div
        ref={indicatorRef}
        className="pointer-events-none absolute inset-y-0 w-px -translate-x-1/2 bg-white/70 shadow"
        style={{ left: "0%" }}
      />
    </div>
  );
};
