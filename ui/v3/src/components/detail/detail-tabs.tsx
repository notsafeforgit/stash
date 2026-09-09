import type React from "react";
import { useRef } from "react";
import { Tabs, TabsContent } from "src/components/ui/tabs";
import {
  DetailTabStrip,
  type DetailTabStripItem,
} from "src/components/detail/detail-tab-strip";
import { useTabState } from "src/hooks/use-tab-state";
import { ListActivityContext } from "src/components/list/list-activity-context";
import { useMobileDetailChrome } from "@/components/layout/mobile-detail-chrome";
import { MobileDetailSections } from "./mobile-detail-sections";

export interface DetailTabsTab extends DetailTabStripItem {
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
  const panelRef = useRef<HTMLDivElement>(null);
  const mobile = useMobileDetailChrome()?.mobile ?? false;
  const {
    activeTab: resolvedActiveTab,
    selectTab,
    isMounted,
  } = useTabState({
    tabs,
    activeTab,
    onTabChange,
  });

  function handleTabChange(id: string) {
    selectTab(id);
    if (mobile) panelRef.current?.scrollIntoView({ block: "start" });
  }

  if (tabs.length === 0) return null;
  return (
    // Match the collection scroller's height so switching to a short list
    // cannot clamp the page back into the entity information above it.
    <Tabs
      ref={panelRef}
      value={resolvedActiveTab}
      onValueChange={handleTabChange}
      orientation={mobile ? "vertical" : "horizontal"}
      className="flex-col md:flex-1 md:min-h-0 max-md:min-h-[100cqh]"
    >
      {mobile ? (
        <MobileDetailSections
          tabs={tabs}
          activeTab={resolvedActiveTab}
          onReselect={() =>
            panelRef.current?.scrollIntoView({ block: "start" })
          }
        />
      ) : (
        <DetailTabStrip tabs={tabs} />
      )}
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
          className="md:flex md:flex-col md:min-h-0"
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
