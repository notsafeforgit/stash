import { localNavigationHref } from "@/core/navigation";
import { useCallback, useEffect } from "react";
import {
  useNavigate,
  useRouter,
  useRouterState,
  type AnyRouter,
} from "@tanstack/react-router";
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

// Keep browsing origins within the router that visited them. Home is an entry
// point just like a list, including links on cards that omit returnTo state.
const lastBrowseHrefs = new WeakMap<AnyRouter, string>();

/**
 * Call once in the app shell (a component that's always mounted). Tracks the
 * current route and saves the href whenever the user visits Home or a list via
 * SPA navigation, including an initial visit to a filtered list. Detail links
 * without explicit returnTo state (such as table links) must retain that URL.
 */
export function useTrackBrowsePage() {
  const router = useRouter();
  const location = useRouterState({ select: (s) => s.location });
  useEffect(() => {
    const href = applicationPath(location.href);
    const pathname = href.split(/[?#]/, 1)[0] ?? "/";
    if (pathname === "/" || isListPathname(pathname)) {
      lastBrowseHrefs.set(router, href);
    }
  }, [location.href, router]);
}

/**
 * Returns a "go back" callback that navigates to the browsing origin.
 *
 * Priority:
 *  1. `returnTo` in router location state — set by list views when navigating
 *     to a detail page, and threaded through by queue navigation so that
 *     scene-to-scene movement never loses the original entry point.
 *  2. Last Home/list page visited by this router (useTrackBrowsePage).
 *  3. `defaultPath` — fallback for direct/external links.
 *
 * Usage in list views (when navigating to detail):
 *   navigate({ to: "/scenes/$sceneId", params: { sceneId }, state: { returnTo: router.state.location.href } })
 *
 * Usage in queue navigation (next/previous scene):
 *   const returnTo = router.state.location.state.returnTo;
 *   navigate({ to: "/scenes/$sceneId", params: { sceneId }, state: { returnTo } })
 */
export function useSmartBack(
  defaultPath: (typeof LIST_PATHNAMES)[number] | "/",
) {
  const navigate = useNavigate();
  const router = useRouter();

  return useCallback(() => {
    const lastBrowseHref = lastBrowseHrefs.get(router);
    const href =
      localNavigationHref(router.state.location.state.returnTo ?? "") ??
      (lastBrowseHref ? localNavigationHref(lastBrowseHref) : undefined);
    if (href) {
      void navigate({ href, state: { navigationDirection: "back" } });
      return;
    }

    // 3. Direct link / external referral
    navigate({ to: defaultPath, state: { navigationDirection: "back" } });
  }, [navigate, router, defaultPath]);
}
