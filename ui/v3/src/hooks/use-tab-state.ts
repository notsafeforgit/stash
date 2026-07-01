import { useCallback, useEffect, useMemo, useState } from "react";

export interface UseTabStateTab {
  id: string;
  /** Single-character keyboard shortcut. Optional. */
  shortcut?: string;
}

export interface UseTabStateOptions<T extends UseTabStateTab> {
  tabs: T[];
  /** Controlled active tab id. If omitted, the hook owns the state. */
  activeTab?: string;
  /** Notified on tab change (both controlled and uncontrolled). */
  onTabChange?: (id: string) => void;
  /**
   * When true, global keydown handlers fire the tab whose `shortcut`
   * matches the pressed key (ignored while an input/textarea is focused).
   * Defaults to false.
   */
  enableShortcuts?: boolean;
}

export interface UseTabStateResult {
  activeTab: string;
  selectTab: (id: string) => void;
  /** True iff this tab has ever been activated. Use to gate panel mount
   *  for lazy-keepMounted semantics (avoids firing N parallel queries
   *  on initial page load). */
  isMounted: (id: string) => boolean;
}

/**
 * Shared tab state for the detail-page layouts (MediaDetailLayout and
 * DetailTabs). Owns:
 *   - Controlled/uncontrolled active tab id.
 *   - A lazy-mount set: each tab mounts the first time it's activated
 *     and stays mounted thereafter, so the panel's internal state
 *     (filter, sort, page) is preserved across tab switches.
 *   - Optional global keyboard shortcuts (per-tab `shortcut` field).
 */
export function useTabState<T extends UseTabStateTab>({
  tabs,
  activeTab: controlledTab,
  onTabChange,
  enableShortcuts = false,
}: UseTabStateOptions<T>): UseTabStateResult {
  const [internalTab, setInternalTab] = useState(tabs[0]?.id ?? "");
  const tabIds = useMemo(() => new Set(tabs.map((tab) => tab.id)), [tabs]);
  const defaultTab = tabs[0]?.id ?? "";
  const requestedTab = controlledTab ?? internalTab;
  const activeTab =
    requestedTab && requestedTab !== "default" && tabIds.has(requestedTab)
      ? requestedTab
      : defaultTab;

  const selectTab = useCallback(
    (id: string) => {
      if (!tabIds.has(id)) return;
      setInternalTab(id);
      onTabChange?.(id);
    },
    [onTabChange, tabIds],
  );

  useEffect(() => {
    if (controlledTab === undefined || controlledTab === activeTab) return;
    if (!activeTab) return;
    onTabChange?.(activeTab);
  }, [activeTab, controlledTab, onTabChange]);

  // Lazy keepMounted: once a tab is activated it stays mounted. Initialised
  // to include the starting tab so its panel renders on first paint.
  const [mounted, setMounted] = useState<Set<string>>(() =>
    activeTab ? new Set([activeTab]) : new Set(),
  );
  useEffect(() => {
    if (!activeTab || mounted.has(activeTab)) return;
    setMounted((prev) => {
      const next = new Set(prev);
      next.add(activeTab);
      return next;
    });
  }, [activeTab, mounted]);

  useEffect(() => {
    if (!enableShortcuts) return;
    function handleKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null;
      if (
        !target ||
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      ) {
        return;
      }
      for (const tab of tabs) {
        if (tab.shortcut && e.key === tab.shortcut) {
          selectTab(tab.id);
          return;
        }
      }
    }
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, [enableShortcuts, tabs, selectTab]);

  const isMounted = useCallback((id: string) => mounted.has(id), [mounted]);

  return { activeTab, selectTab, isMounted };
}
