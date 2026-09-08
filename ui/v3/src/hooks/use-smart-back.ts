import { localNavigationHref } from "@/core/navigation";
import { useCallback, useEffect } from "react";
import { useNavigate, useRouter, useRouterState } from "@tanstack/react-router";
import { applicationPath } from "@/core/platform-url";

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
  "/offline",
] as const;

function isListPathname(pathname: string): boolean {
  return LIST_PATHNAMES.some((p) => pathname === p || pathname === p + "/");
}

// Module-level: the last list-page href visited in this SPA session.
// Cleared on page reload (module re-evaluates). Updated by useTrackListPage.
let _lastListHref: string | null = null;

/**
 * Call once in the app shell (a component that's always mounted). Tracks the
 * current route and saves the href whenever the user visits a list page via
 * SPA navigation, including an initial visit to a filtered list. Detail links
 * without explicit returnTo state (such as table links) must retain that URL.
 */
export function useTrackListPage() {
  const location = useRouterState({ select: (s) => s.location });
  useEffect(() => {
    const href = applicationPath(location.href);
    if (isListPathname(href.split(/[?#]/, 1)[0] ?? "/")) {
      _lastListHref = href;
    }
  }, [location.href]);
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
 *   const returnTo = router.state.location.state.returnTo;
 *   navigate({ to: "/scenes/$sceneId", params: { sceneId }, state: { returnTo } })
 */
export function useSmartBack(defaultPath: (typeof LIST_PATHNAMES)[number]) {
  const navigate = useNavigate();
  const router = useRouter();

  return useCallback(() => {
    const href =
      localNavigationHref(router.state.location.state.returnTo ?? "") ??
      (_lastListHref ? localNavigationHref(_lastListHref) : undefined);
    if (href) {
      void navigate({ href, viewTransition: true });
      return;
    }

    // 3. Direct link / external referral
    navigate({ to: defaultPath, viewTransition: true });
  }, [navigate, router, defaultPath]);
}
