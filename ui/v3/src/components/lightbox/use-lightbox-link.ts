import { useRouterState } from "@tanstack/react-router";
import { applicationPath } from "@/core/platform-url";
import { useMediaQuery } from "@/utils/screen";

/** Keep installed-app navigation in the same router/cache and retain the
 * browsing origin for the detail page's Back and deletion actions. */
export function useLightboxLink() {
  const standalone = useMediaQuery("(display-mode: standalone)");
  const href = useRouterState({ select: (state) => state.location.href });
  return {
    target: standalone ? undefined : "_blank",
    rel: "noreferrer",
    // The lightbox already added a temporary same-URL history entry.
    replace: standalone,
    state: { returnTo: applicationPath(href) },
  } as const;
}
