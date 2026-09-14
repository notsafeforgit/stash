import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { ContentReveal } from "./content-reveal";

/** A bounded paint surface fades over the new view. Animating this empty layer
 * avoids promoting every image, scroller and video under the page to a moving
 * compositor subtree, which is particularly expensive on mobile WebKit. */
export function RouteViewport({
  children,
  className,
  ...props
}: ComponentProps<"main">) {
  return (
    <main
      {...props}
      data-route-viewport
      className={cn(
        "relative isolate flex min-h-0 flex-1 flex-col overflow-hidden",
        className,
      )}
    >
      {children}
      <ContentReveal data-route-transition />
    </main>
  );
}
