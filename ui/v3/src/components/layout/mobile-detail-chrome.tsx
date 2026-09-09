import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { useIntl } from "react-intl";
import { ChevronLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useVisualViewportBottomInset } from "@/hooks/use-visual-viewport-bottom-inset";
import { useMediaQuery } from "@/utils/screen";

type ChromeSlot = "list" | "tabs" | "actions";
type ChromeTargets = Record<ChromeSlot, HTMLDivElement | null>;

interface MobileDetailChromeContextValue {
  mobile: boolean;
  targets: ChromeTargets;
  setTarget: (slot: ChromeSlot, element: HTMLDivElement | null) => void;
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
  });
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
    () => ({ mobile, targets, setTarget }),
    [mobile, targets, setTarget],
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

function MobileDetailChromeSlot({
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
      className="relative shrink-0 border-t border-border bg-background pb-[env(safe-area-inset-bottom,0px)]"
      style={
        bottomInset > 0
          ? { transform: `translateY(-${bottomInset}px)` }
          : undefined
      }
    >
      <MobileDetailChromeSlot slot="list" />
      <MobileDetailChromeSlot slot="tabs" className="empty:hidden border-b" />
      <div className="flex min-h-11 items-center gap-2 px-3 py-1">
        <MobileDetailChromeSlot
          slot="actions"
          className="min-w-0 flex-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden touch-pan-x overscroll-x-contain"
        />
        {onBack && (
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
      </div>
    </div>
  );
}
