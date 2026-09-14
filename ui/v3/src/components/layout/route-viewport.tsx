import { type ComponentProps, useLayoutEffect, useRef } from "react";
import { useRouter, useRouterState } from "@tanstack/react-router";
import { commitRouteMotion } from "@/core/route-transitions";
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
  const router = useRouter();
  const surface = useRef<HTMLDivElement>(null);
  const pathname = useRouterState({
    select: (state) => state.matches.at(-1)?.pathname,
  });
  useLayoutEffect(() => {
    commitRouteMotion(router, pathname, surface.current);
  }, [router, pathname]);

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
      <ContentReveal ref={surface} data-route-transition />
    </main>
  );
}
