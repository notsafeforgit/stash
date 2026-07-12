import { Outlet, useRouterState } from "@tanstack/react-router";
import { Header } from "./header";
import { BottomTabBar } from "./bottom-tab-bar";
import { useNavHotkeys } from "src/hooks/use-nav-hotkeys";
import { useTrackListPage } from "src/hooks/use-smart-back";
import { DownloadProgressBar } from "src/components/offline/download-progress-bar";
import { DownloadNotifications } from "src/components/offline/download-notifications";

// Detail routes have their own bottom bar — hide the global nav bar on these.
// `/offline/{sceneId}` is the offline-player page; it's a "detail page" in the
// same sense (full-screen player, its own back affordance), so include it.
const DETAIL_ROUTE_RE =
  /^\/(scenes|performers|galleries|images|groups|studios|tags|offline)\/[^/]+$/;

export function AppShell() {
  useNavHotkeys();
  useTrackListPage();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const isDetailPage = DETAIL_ROUTE_RE.test(pathname);

  return (
    <div
      data-app-viewport
      className="flex h-dvh flex-col overflow-hidden bg-background text-foreground"
    >
      <Header />
      <DownloadProgressBar />
      <DownloadNotifications />
      <main
        className={`flex flex-col flex-1 min-h-0${isDetailPage ? " overflow-hidden" : " overflow-hidden pb-11 md:pb-0"}`}
      >
        <Outlet />
      </main>
      {!isDetailPage && <BottomTabBar />}
    </div>
  );
}
