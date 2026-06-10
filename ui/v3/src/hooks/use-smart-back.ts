import { useCallback, useEffect, useRef } from "react";
import { useNavigate, useRouter, useRouterState } from "@tanstack/react-router";

// Extend TanStack Router's history state to allow returnTo passthrough.
declare module "@tanstack/react-router" {
  interface HistoryState {
    returnTo?: string;
  }
}

// Bare list pathnames — no trailing slash, no ID segment
const LIST_PATHNAMES = [
  "/scenes",
  "/performers",
  "/galleries",
  "/groups",
  "/images",
  "/studios",
  "/tags",
];

function isListPathname(pathname: string): boolean {
  return LIST_PATHNAMES.some((p) => pathname === p || pathname === p + "/");
}

// Module-level: the last list-page href visited in this SPA session.
// Cleared on page reload (module re-evaluates). Updated by useTrackListPage.
let _lastListHref: string | null = null;

/**
 * Call once in the app shell (a component that's always mounted). Tracks the
 * current route and saves the href whenever the user visits a list page via
 * SPA navigation. Skips the initial mount so that a hard-refresh on a
 * filtered list URL does NOT pre-populate _lastListHref — only navigations
 * that happen within the current SPA session are recorded.
 */
export function useTrackListPage() {
  const location = useRouterState({ select: (s) => s.location });
  const isMountRef = useRef(true);
  useEffect(() => {
    if (isMountRef.current) {
      isMountRef.current = false;
      return;
    }
    if (isListPathname(location.pathname)) {
      _lastListHref = location.href;
    }
  }, [location.href, location.pathname]);
}

/**
 * Returns a "go back" callback that navigates to the correct list view.
 *
 * Priority:
 *  1. `returnTo` in router location state — set by list views when navigating
 *     to a detail page, and threaded through by queue navigation so that
 *     scene-to-scene movement never loses the original entry point.
 *  2. Last list page visited in this SPA session (tracked by useTrackListPage).
 *  3. `defaultPath` — fallback for direct/external links.
 *
 * Usage in list views (when navigating to detail):
 *   navigate({ to: "/scenes/$sceneId", params: { sceneId }, state: { returnTo: router.state.location.href } })
 *
 * Usage in queue navigation (next/previous scene):
 *   const returnTo = (router.state.location.state as { returnTo?: string } | null)?.returnTo;
 *   navigate({ to: "/scenes/$sceneId", params: { sceneId }, state: { returnTo } })
 */
export function useSmartBack(defaultPath: string) {
  const navigate = useNavigate();
  const router = useRouter();

  return useCallback(() => {
    // 1. Prefer explicit returnTo threaded through router state
    const state = router.state.location.state;
    if (state?.returnTo) {
      // Cast — returnTo is a runtime URL string, not a registered route path.
      navigate({ to: state.returnTo as never, viewTransition: true });
      return;
    }

    // 2. Last list page visited in this SPA session
    if (_lastListHref) {
      navigate({ to: _lastListHref as never, viewTransition: true });
      return;
    }

    // 3. Direct link / external referral
    navigate({ to: defaultPath, viewTransition: true });
  }, [navigate, router, defaultPath]);
}
