const SOURCE_TRACKING_PATHS = new Set([
  "/source-posts",
  "/account-review",
  "/collections",
  "/review-queue",
  "/archive-activity",
  "/import-history",
  "/saved-actions",
]);

export function isSourceTrackingPath(pathname: string) {
  return SOURCE_TRACKING_PATHS.has(pathname.replace(/\/$/, ""));
}

const SOURCE_TRACKING_TABS = new Set([
  "source-review",
  "source-accounts",
  "source-albums",
]);

export function sourceTrackingTabs<T extends { id: string }>(
  tabs: T[],
  enabled: boolean,
) {
  return enabled
    ? tabs
    : tabs.filter((tab) => !SOURCE_TRACKING_TABS.has(tab.id));
}
