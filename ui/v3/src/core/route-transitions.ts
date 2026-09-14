import type {
  AnyRouter,
  RouterEvents,
  RouterHistory,
} from "@tanstack/react-router";
import { createContentReveal } from "./content-reveal";
import { motion } from "./motion";

type RouteTransition = "route-forward" | "route-back" | "route-replace";

const controllers = new WeakMap<
  object,
  {
    hold: () => () => void;
    commit: (pathname: string | undefined, surface: HTMLElement | null) => void;
  }
>();

/** Called by the viewport's layout effect, before the destination can paint. */
export function commitRouteMotion(
  router: object,
  pathname: string | undefined,
  surface: HTMLElement | null,
) {
  controllers.get(router)?.commit(pathname, surface);
}

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
  router: Pick<AnyRouter, "subscribe" | "update"> & { history: RouterHistory },
) {
  router.update({ defaultViewTransition: false });
  if (typeof window === "undefined") return () => {};
  const reveal = createContentReveal();
  let pending: { pathname: string; direction: RouteTransition } | undefined;
  let traversingHistory = false;
  // Safari restores its own swipe snapshot for browser Back/Forward. Adding
  // another entrance effect after that snapshot produces a visible flash.
  const history = router.history.subscribe(({ action }) => {
    traversingHistory = action.type !== "PUSH" && action.type !== "REPLACE";
  });
  controllers.set(router, {
    hold: reveal.hold,
    commit(pathname, surface) {
      if (!pending || pending.pathname !== pathname) return;
      const { direction } = pending;
      pending = undefined;
      reveal.play(surface, direction, motion.duration.page);
    },
  });
  const before = router.subscribe("onBeforeLoad", (event) => {
    reveal.cancel();
    pending = undefined;
    const direction = transitionForNavigation(event);
    if (
      direction &&
      !traversingHistory &&
      event.toLocation.state.routeMotion !== false
    ) {
      pending = { pathname: event.toLocation.pathname, direction };
    }
  });
  return () => {
    before();
    history();
    controllers.delete(router);
    reveal.dispose();
  };
}
