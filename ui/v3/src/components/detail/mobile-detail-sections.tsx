import { useEffect, useRef } from "react";
import { useIntl } from "react-intl";
import { ChevronUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  MobileDetailChromePortal,
  MobileDetailChromeSlot,
  useMobileDetailChrome,
} from "@/components/layout/mobile-detail-chrome";
import type { DetailTabStripItem } from "./detail-tab-strip";

/** The original Tabs context owns selection and panels, including inside the popover. */
export function MobileDetailSections({
  tabs,
  activeTab,
  onReselect,
}: {
  tabs: readonly DetailTabStripItem[];
  activeTab: string;
  onReselect: () => void;
}) {
  const intl = useIntl();
  const chrome = useMobileDetailChrome();
  const setPanel = chrome?.setPanel;
  const previousTab = useRef(activeTab);
  useEffect(() => {
    if (previousTab.current === activeTab) return;
    previousTab.current = activeTab;
    setPanel?.(null);
  }, [activeTab, setPanel]);
  if (!chrome) return null;
  const label = intl.formatMessage({
    id: "accessibility.detail_sections",
    defaultMessage: "Detail sections",
  });

  return (
    <MobileDetailChromePortal slot="tabs">
      <Popover
        open={chrome.panel === "sections"}
        onOpenChange={(open) => chrome.setPanel(open ? "sections" : null)}
      >
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              className="h-11 w-full min-w-0 justify-between px-2"
              aria-label={label}
            />
          }
        >
          <span className="truncate">
            {tabs.find((tab) => tab.id === activeTab)?.label}
          </span>
          <ChevronUp data-icon="inline-end" />
        </PopoverTrigger>
        <PopoverContent
          side="top"
          align="start"
          keepMounted
          className="w-80 max-w-[calc(100vw-1.5rem)] max-h-[70svh] overflow-y-auto"
        >
          <PopoverTitle className="sr-only">{label}</PopoverTitle>
          <TabsList
            aria-label={label}
            activateOnFocus={false}
            className="h-auto w-full flex-col items-stretch gap-1 p-0"
          >
            {tabs.map((tab) => (
              <TabsTrigger
                key={tab.id}
                value={tab.id}
                className="min-h-11 flex-none justify-start px-3"
                onClick={() => {
                  if (tab.id === activeTab) onReselect();
                  chrome.setPanel(null);
                }}
              >
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
          <MobileDetailChromeSlot slot="pagination" className="empty:hidden" />
        </PopoverContent>
      </Popover>
    </MobileDetailChromePortal>
  );
}
