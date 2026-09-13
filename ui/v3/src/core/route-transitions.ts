import type { AnyRouter, RouterEvents } from "@tanstack/react-router";

type RouteTransition = "route-forward" | "route-back" | "route-replace";

declare module "@tanstack/react-router" {
  interface HistoryState {
    /** Semantic direction for actions such as Smart Back, which push a URL. */
    navigationDirection?: "forward" | "back";
  }
}

function transitionForNavigation({
  fromLocation,
  toLocation,
}: RouterEvents["onBeforeLoad"]): RouteTransition | undefined {
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

/** Install once per router. Links, imperative navigation, and browser history
 * share this policy; an exceptional navigation can opt out with
 * `viewTransition: false`. The browser owns snapshots, so route components,
 * players, and forms keep their normal React lifecycle. */
export function installRouteTransitions(
  router: Pick<AnyRouter, "subscribe" | "update">,
) {
  return router.subscribe("onBeforeLoad", (event) => {
    const transition = transitionForNavigation(event);
    const enabled =
      transition &&
      typeof window !== "undefined" &&
      !window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    // Gate before the native call: TanStack doesn't evaluate `types` callbacks
    // in browsers that support View Transitions but not transition types.
    // Those browsers still get a content crossfade, with the same opt-outs.
    router.update({
      defaultViewTransition: enabled ? { types: ["route", transition] } : false,
    });
  });
}
