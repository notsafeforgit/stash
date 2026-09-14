import type { AnyRouter, RouterEvents } from "@tanstack/react-router";
import { createContentReveal } from "./content-reveal";
import { motion } from "./motion";

type RouteTransition = "route-forward" | "route-back" | "route-replace";

const controllers = new WeakMap<object, { hold: () => () => void }>();

/** Keep route motion behind a navigation overlay until its exit has finished.
 * Routing and data loading continue immediately. Each hold releases once. */
export function holdRouteMotion(router: object): () => void {
  return controllers.get(router)?.hold() ?? (() => {});
}

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

/** Reveal committed content through a small, empty paint layer. The page itself
 * remains untransformed and fully opaque; no native snapshots are captured. */
export function installRouteTransitions(
  router: Pick<AnyRouter, "subscribe" | "update">,
) {
  router.update({ defaultViewTransition: false });
  if (typeof window === "undefined") return () => {};
  const reveal = createContentReveal();
  controllers.set(router, reveal);
  const before = router.subscribe("onBeforeLoad", reveal.cancel);
  const resolved = router.subscribe("onResolved", (event) => {
    const direction = transitionForNavigation(event);
    // Router state updates can resolve the same location again immediately
    // after a commit. They must not cancel that commit's pending reveal.
    if (!direction) return;
    if (event.toLocation.state.routeMotion === false) {
      reveal.cancel();
      return;
    }
    reveal.play(
      document.querySelector<HTMLElement>(
        "[data-route-viewport] > [data-route-transition]",
      ),
      direction,
      motion.duration.page,
    );
  });
  return () => {
    before();
    resolved();
    controllers.delete(router);
    reveal.dispose();
  };
}
