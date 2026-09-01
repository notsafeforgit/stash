import type React from "react";
import { cn } from "src/lib/utils";
import { Tabs, TabsContent } from "src/components/ui/tabs";
import { DetailTabStrip } from "src/components/detail/detail-tab-strip";
import { useTabState } from "src/hooks/use-tab-state";
import { ListActivityContext } from "src/components/list/list-activity-context";

export interface DetailTabsTab {
  id: string;
  label: string;
  content: React.ReactNode;
}

export interface DetailTabsProps {
  tabs: DetailTabsTab[];
  activeTab: string;
  onTabChange: (id: string) => void;
}

/**
 * Tabs widget for collection-style detail pages (performer, studio, tag,
 * gallery, group). The page renders its own header (entity name, cover,
 * counts, etc.) above this; we only own the tab strip and panels.
 *
 * For leaf detail pages with a primary viewer (scene, image), use
 * `MediaDetailLayout` instead — it owns the entire split-pane shell.
 */
export function DetailTabs({ tabs, activeTab, onTabChange }: DetailTabsProps) {
  const {
    activeTab: resolvedActiveTab,
    selectTab,
    isMounted,
  } = useTabState({
    tabs,
    activeTab,
    onTabChange,
  });

  // Tab swap intentionally does NOT scroll the page on mobile — the
  // user's complaint was that any forced scroll moves the tab bar away
  // from where they were looking. Leaving the scroll alone:
  //   - When the user is scrolled deep enough that the sticky tab strip
  //     is pinned at the top of the scroll container, the panel-area
  //     `min-h-[calc(100dvh-2.5rem)]` below ensures the document is just
  //     tall enough that the browser's natural clamp lands at exactly
  //     `scroll = aside_height`. Sticky stays engaged, tabs stay at the
  //     top of the screen, the new panel begins from offset 0 right
  //     below the tabs.
  //   - When the user hasn't scrolled past the tab bar yet, no clamp
  //     happens and the tabs remain at their natural in-flow position;
  //     the new panel renders below them in the usual flow.
  // Either way the on-screen position of the tab bar is preserved.

  if (tabs.length === 0) return null;
  return (
    // `md:flex-1 md:min-h-0` makes Tabs fill the right column on the
    // split (sidebar+main) detail layout so each panel's EntityListPage
    // gets a bounded height for its internal scroll. Mobile / single-column
    // layout falls through to natural-height stacking.
    //
    // `max-md:min-h-[calc(100dvh-2.5rem)]` sizes the panel area to exactly
    // the scroll container's height on mobile (viewport minus the 40 px
    // DetailBackBar that sits above it; the global header is hidden on
    // mobile detail pages). Without this min-height, switching from a
    // tall Scenes list to a short Images list shrinks the document below
    // the user's scroll position, the browser clamps up, and the user
    // lands above the tabs with the details aside filling the screen.
    // Using the exact container height instead of `100dvh` avoids the
    // 40 px of empty scroll past the cards that the looser bound left
    // behind.
    <Tabs
      value={resolvedActiveTab}
      onValueChange={selectTab}
      className="md:flex-1 md:min-h-0 max-md:min-h-[calc(100dvh-2.5rem)]"
    >
      <DetailTabStrip tabs={tabs} sticky />
      {tabs.map((t) => (
        <TabsContent
          key={t.id}
          value={t.id}
          keepMounted={isMounted(t.id)}
          // Make the active panel a flex column on desktop so
          // EntityList's `flex flex-col flex-auto min-h-0` can size
          // against it and its inner scroll container picks up a
          // bounded height. Without this the list overflows the
          // outer `md:overflow-hidden` and gets clipped — no scroll.
          className={cn("md:flex md:flex-col md:min-h-0")}
        >
          {isMounted(t.id) ? (
            <ListActivityContext value={t.id === resolvedActiveTab}>
              {t.content}
            </ListActivityContext>
          ) : null}
        </TabsContent>
      ))}
    </Tabs>
  );
}
