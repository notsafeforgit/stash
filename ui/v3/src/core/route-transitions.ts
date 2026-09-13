import type { AnyRouter, RouterEvents } from "@tanstack/react-router";

type RouteTransition = "route-forward" | "route-back" | "route-replace";

declare module "@tanstack/react-router" {
  interface HistoryState {
    /** Semantic direction for actions such as Smart Back, which push a URL. */
    navigationDirection?: "forward" | "back";
    /** Opt out of route motion for an exceptional navigation. */
    routeMotion?: false;
  }
}

function transitionForNavigation({
  fromLocation,
  toLocation,
}: Pick<RouterEvents["onResolved"], "fromLocation" | "toLocation">):
  | RouteTransition
  | undefined {
  const fromPath = fromLocation?.pathname.replace(/\/+$/, "");
  const toPath = toLocation.pathname.replace(/\/+$/, "");
  // Initial visits, filters, tabs, hashes, and revalidation stay still.
  if (!fromLocation || fromPath === toPath) return;

  const fromIndex = fromLocation.state.__TSR_index;
  const toIndex = toLocation.state.__TSR_index;
  if (toIndex < fromIndex) return "route-back";
  if (toIndex === fromIndex) return "route-replace";
  const direction = toLocation.state.navigationDirection;
  // An explicit hint lets Smart Back return to a sibling entity, too.
  if (toIndex > fromIndex && direction) {
    return direction === "back" ? "route-back" : "route-forward";
  }
  const returnsToParent = fromPath?.startsWith(`${toPath}/`);
  if (returnsToParent) return "route-back";
  return "route-forward";
}

/** Animate committed content without taking snapshots or delaying the router.
 * Native view-transition capture can stall WebKit on image-heavy lists. The
 * Web Animations API keeps this small effect interruptible, preserves React
 * state, and leaves shell controls and portaled dialogs outside the animation. */
export function installRouteTransitions(
  router: Pick<AnyRouter, "subscribe" | "update">,
) {
  router.update({ defaultViewTransition: false });
  if (typeof window === "undefined") return () => {};
  const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
  let animation: Animation | undefined;
  const cancel = () => {
    animation?.cancel();
    animation = undefined;
  };
  const before = router.subscribe("onBeforeLoad", cancel);
  const resolved = router.subscribe("onResolved", (event) => {
    cancel();
    const direction = transitionForNavigation(event);
    if (
      !direction ||
      preference.matches ||
      event.toLocation.state.routeMotion === false
    )
      return;
    const viewport = document.querySelector<HTMLElement>(
      "[data-route-viewport]",
    );
    if (!viewport?.animate) return;
    const x =
      direction === "route-back" ? -8 : direction === "route-forward" ? 8 : 0;
    animation = viewport.animate(
      [
        { opacity: 0, transform: `translateX(${x}px)` },
        { opacity: 1, transform: "translateX(0)" },
      ],
      {
        id: direction,
        duration: 180,
        easing: "cubic-bezier(0.22, 1, 0.36, 1)",
      },
    );
  });
  preference.addEventListener("change", cancel);
  return () => {
    before();
    resolved();
    preference.removeEventListener("change", cancel);
    cancel();
  };
}
