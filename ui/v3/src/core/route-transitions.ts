import type { AnyRouter, RouterEvents } from "@tanstack/react-router";

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
  const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
  let animation: Animation | undefined;
  let surface: HTMLElement | undefined;
  let pending: RouteTransition | undefined;
  let frame: number | undefined;
  const holds = new Set<symbol>();
  const hide = () => {
    if (surface) {
      surface.hidden = true;
      surface.style.removeProperty("opacity");
    }
    surface = undefined;
  };
  const cancel = () => {
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
    pending = undefined;
    animation?.cancel();
    animation = undefined;
    hide();
  };
  const start = () => {
    frame = undefined;
    if (holds.size || !pending || !surface) return;
    if (preference.matches || document.hidden || !surface.isConnected) {
      cancel();
      return;
    }
    const running = surface.animate([{ opacity: 1 }, { opacity: 0 }], {
      id: pending,
      duration: 200,
      easing: "cubic-bezier(0.22, 1, 0.36, 1)",
    });
    pending = undefined;
    animation = running;
    const finish = () => {
      if (animation !== running) return;
      // Release the effect and paint surface when it finishes, including when
      // the user stays on this page for a long time after navigating.
      running.cancel();
      animation = undefined;
      hide();
    };
    void running.finished.then(finish, finish);
  };
  const schedule = () => {
    if (holds.size || !pending || frame !== undefined) return;
    // Give the new DOM one paint before starting the clock. Heavy first layout
    // must not consume most of the short animation before Safari shows it.
    frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(start);
    });
  };
  controllers.set(router, {
    hold: () => {
      const token = Symbol();
      holds.add(token);
      cancel();
      return () => {
        if (holds.delete(token)) schedule();
      };
    },
  });
  const before = router.subscribe("onBeforeLoad", cancel);
  const resolved = router.subscribe("onResolved", (event) => {
    const direction = transitionForNavigation(event);
    // Router state updates can resolve the same location again immediately
    // after a commit. They must not cancel that commit's pending reveal.
    if (!direction) return;
    cancel();
    if (preference.matches || event.toLocation.state.routeMotion === false)
      return;
    surface =
      document.querySelector<HTMLElement>(
        "[data-route-viewport] > [data-route-transition]",
      ) ?? undefined;
    if (!surface?.animate || document.hidden) return;
    surface.hidden = false;
    surface.style.opacity = "1";
    pending = direction;
    schedule();
  });
  preference.addEventListener("change", cancel);
  document.addEventListener("visibilitychange", cancel);
  return () => {
    before();
    resolved();
    preference.removeEventListener("change", cancel);
    document.removeEventListener("visibilitychange", cancel);
    controllers.delete(router);
    cancel();
  };
}
