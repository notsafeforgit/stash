import { Outlet, useRouterState } from "@tanstack/react-router";
import { Header } from "./header";
import { BottomTabBar } from "./bottom-tab-bar";
import { MobileNavigationProvider } from "./mobile-navigation";
import { RouteViewport } from "./route-viewport";
import { useNavHotkeys } from "src/hooks/use-nav-hotkeys";
import { useTrackBrowsePage } from "src/hooks/use-smart-back";
import { DownloadProgressBar } from "src/components/offline/download-progress-bar";
import { DownloadNotifications } from "src/components/offline/download-notifications";

// Detail routes have their own bottom bar. Settings also owns its navigation.
// `/offline/{sceneId}` is the offline-player page; it's a "detail page" in the
// same sense (full-screen player, its own back affordance), so include it.
const DETAIL_ROUTE_RE =
  /^\/(scenes|performers|galleries|images|groups|studios|tags|offline)\/[^/]+$/;

export function AppShell() {
  useNavHotkeys();
  useTrackBrowsePage();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const isDetailPage = DETAIL_ROUTE_RE.test(pathname);
  const ownsMobileNavigation =
    isDetailPage ||
    pathname === "/settings" ||
    pathname.startsWith("/settings/");

  return (
    <MobileNavigationProvider>
      <div
        data-app-viewport
        className="flex h-dvh flex-col overflow-hidden bg-background text-foreground"
      >
        <Header />
        <DownloadProgressBar />
        <DownloadNotifications />
        <RouteViewport>
          <Outlet />
        </RouteViewport>
        {!ownsMobileNavigation && <BottomTabBar />}
      </div>
    </MobileNavigationProvider>
  );
}
