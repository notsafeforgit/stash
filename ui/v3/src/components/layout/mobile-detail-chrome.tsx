import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useLayoutEffect,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { useIntl } from "react-intl";
import { ChevronLeft, Ellipsis, Menu } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from "@/components/ui/drawer";
import { BottomSheetCloseFooter } from "@/components/ui/bottom-sheet";
import { MobileNavSheet } from "@/components/layout/mobile-nav-sheet";
import { MobileToolbarRow } from "./mobile-toolbar";
import { useVisualViewportBottomInset } from "@/hooks/use-visual-viewport-bottom-inset";
import { useMediaQuery } from "@/utils/screen";

type ChromeSlot = "list" | "tabs" | "actions" | "list-actions" | "pagination";
type ChromeTargets = Record<ChromeSlot, HTMLDivElement | null>;
type ChromePanel = "sections" | "actions" | "navigation" | null;
type ChromeInteraction = "search" | "selection" | null;

interface MobileDetailChromeContextValue {
  mobile: boolean;
  targets: ChromeTargets;
  setTarget: (slot: ChromeSlot, element: HTMLDivElement | null) => void;
  panel: ChromePanel;
  setPanel: (panel: ChromePanel) => void;
  interaction: ChromeInteraction;
  setInteraction: (interaction: ChromeInteraction) => void;
}

const MobileDetailChromeContext =
  createContext<MobileDetailChromeContextValue | null>(null);

export function MobileDetailChromeProvider({
  children,
  breakpoint = "md",
}: {
  children: ReactNode;
  breakpoint?: "md" | "lg";
}) {
  const mobile = useMediaQuery(
    breakpoint === "md" ? "(max-width: 767px)" : "(max-width: 1023px)",
  );
  const [targets, setTargets] = useState<ChromeTargets>({
    list: null,
    tabs: null,
    actions: null,
    "list-actions": null,
    pagination: null,
  });
  const [panel, setPanel] = useState<ChromePanel>(null);
  const [interaction, setInteraction] = useState<ChromeInteraction>(null);
  const setTarget = useCallback(
    (slot: ChromeSlot, element: HTMLDivElement | null) => {
      setTargets((previous) =>
        previous[slot] === element
          ? previous
          : { ...previous, [slot]: element },
      );
    },
    [],
  );
  const value = useMemo(
    () => ({
      mobile,
      targets,
      setTarget,
      panel,
      setPanel,
      interaction,
      setInteraction,
    }),
    [mobile, targets, setTarget, panel, interaction],
  );
  return (
    <MobileDetailChromeContext value={value}>
      {children}
    </MobileDetailChromeContext>
  );
}

export function useMobileDetailChrome() {
  return useContext(MobileDetailChromeContext);
}

/** Only the active embedded list owns the replacement search/selection row. */
export function useMobileDetailInteraction(interaction: ChromeInteraction) {
  const chrome = useMobileDetailChrome();
  const mobile = chrome?.mobile;
  const setInteraction = chrome?.setInteraction;
  useLayoutEffect(() => {
    if (!mobile || !setInteraction) return;
    setInteraction(interaction);
    return () => setInteraction(null);
  }, [mobile, setInteraction, interaction]);
}

/** Portals retain the form, list and tab contexts of their original owner. */
export function MobileDetailChromePortal({
  slot,
  children,
}: {
  slot: ChromeSlot;
  children: ReactNode;
}) {
  const chrome = useMobileDetailChrome();
  if (!chrome?.mobile) return children;
  const target = chrome.targets[slot];
  return target ? createPortal(children, target) : null;
}

export function MobileDetailChromeSlot({
  slot,
  className,
}: {
  slot: ChromeSlot;
  className?: string;
}) {
  const setTarget = useMobileDetailChrome()?.setTarget;
  const ref = useCallback(
    (element: HTMLDivElement | null) => setTarget?.(slot, element),
    [setTarget, slot],
  );
  return <div ref={ref} className={className} data-detail-chrome-slot={slot} />;
}

/** Render beside the page scroller: flex layout reserves the exact height. */
export function MobileDetailFooter({ onBack }: { onBack?: () => void }) {
  const chrome = useMobileDetailChrome();
  const intl = useIntl();
  const { ref, bottomInset } = useVisualViewportBottomInset<HTMLDivElement>();
  if (!chrome?.mobile) return null;

  return (
    <div
      ref={ref}
      data-mobile-detail-footer
      className="@container relative shrink-0 border-t border-border bg-background pb-[env(safe-area-inset-bottom,0px)]"
      style={
        bottomInset > 0
          ? { transform: `translateY(-${bottomInset}px)` }
          : undefined
      }
    >
      <MobileNavSheet
        open={chrome.panel === "navigation"}
        onOpenChange={(open) => chrome.setPanel(open ? "navigation" : null)}
      />
      <MobileToolbarRow>
        <Button
          hidden={chrome.interaction !== null}
          variant="ghost"
          size="icon-lg"
          className="size-11 shrink-0"
          onClick={() => chrome.setPanel("navigation")}
          aria-label={intl.formatMessage({
            id: "navigation",
            defaultMessage: "Navigation",
          })}
        >
          <Menu />
        </Button>
        <div
          hidden={chrome.interaction !== null}
          className="min-w-11 flex-1 overflow-hidden"
        >
          <MobileDetailChromeSlot slot="tabs" />
        </div>
        <MobileDetailChromeSlot
          slot="list"
          className={chrome.interaction ? "min-w-0 flex-1" : "shrink-0"}
        />
        <Drawer
          open={chrome.panel === "actions"}
          onOpenChange={(open) => chrome.setPanel(open ? "actions" : null)}
        >
          <DrawerTrigger
            hidden={chrome.interaction !== null}
            render={
              <Button
                variant="ghost"
                size="icon-lg"
                className="size-11 shrink-0"
                aria-label={intl.formatMessage({
                  id: "actions.entity_actions",
                  defaultMessage: "Entity actions",
                })}
              />
            }
          >
            <Ellipsis />
          </DrawerTrigger>
          <DrawerContent keepMounted>
            <DrawerHeader className="shrink-0 py-2">
              <DrawerTitle>
                {intl.formatMessage({
                  id: "actions.entity_actions",
                  defaultMessage: "Entity actions",
                })}
              </DrawerTitle>
            </DrawerHeader>
            <div className="min-h-0 overflow-y-auto overscroll-contain px-3 pb-2">
              <MobileDetailChromeSlot
                slot="actions"
                className="empty:hidden [&_button]:min-h-11 [&_button]:min-w-11"
              />
              <MobileDetailChromeSlot
                slot="list-actions"
                className="empty:hidden"
              />
            </div>
            <BottomSheetCloseFooter />
          </DrawerContent>
        </Drawer>
        {onBack && !chrome.interaction && (
          <Button
            variant="ghost"
            size="icon-lg"
            className="size-11 shrink-0"
            onClick={onBack}
            aria-label={intl.formatMessage({ id: "actions.back" })}
          >
            <ChevronLeft />
          </Button>
        )}
      </MobileToolbarRow>
    </div>
  );
}
