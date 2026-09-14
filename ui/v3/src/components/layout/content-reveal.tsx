import { useLayoutEffect, useRef, type ComponentProps } from "react";
import {
  createContentReveal,
  type ContentRevealKind,
} from "@/core/content-reveal";
import { motion } from "@/core/motion";
import { cn } from "@/lib/utils";

/** Put beside the changed content in a positioned viewport, outside its scroller.
 * It never snapshots, transforms, or remounts the content beneath it. */
export function ContentReveal({
  className,
  tone = "page",
  ...props
}: Omit<ComponentProps<"div">, "children" | "hidden" | "aria-hidden"> & {
  tone?: "page" | "media";
}) {
  return (
    <div
      {...props}
      data-content-reveal
      aria-hidden="true"
      hidden
      className={cn(
        "pointer-events-none absolute inset-0 z-50 max-h-dvh opacity-0",
        tone === "media" ? "bg-black" : "bg-background",
        className,
      )}
    />
  );
}

/** Only explicit view identity changes reveal; mounting and background refreshes
 * stay still. The caller keeps its panels, focus and scroll state mounted. */
export function useContentReveal(
  value: string | number,
  kind: Extract<ContentRevealKind, "list-view" | "detail-tab" | "focused-view">,
  active = true,
) {
  const surface = useRef<HTMLDivElement>(null);
  const previous = useRef(value);
  const controller = useRef<ReturnType<typeof createContentReveal> | null>(
    null,
  );
  useLayoutEffect(() => {
    return () => {
      controller.current?.dispose();
      controller.current = null;
    };
  }, []);
  useLayoutEffect(() => {
    const changed = previous.current !== value;
    previous.current = value;
    if (!active) controller.current?.cancel();
    else if (changed && surface.current?.animate) {
      controller.current ??= createContentReveal();
      controller.current.play(surface.current, kind, motion.duration.content);
    }
  }, [value, kind, active]);
  return surface;
}
