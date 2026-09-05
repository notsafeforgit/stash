import type React from "react";
import { useId, useLayoutEffect, useRef, useState } from "react";
import { cn } from "src/lib/utils";
import { ArrowLeft, PanelLeftClose, PanelLeftOpen, XIcon } from "lucide-react";
import { useIntl } from "react-intl";
import { Tabs, TabsContent } from "src/components/ui/tabs";
import { Button } from "src/components/ui/button";
import { DetailTabStrip } from "src/components/detail/detail-tab-strip";
import { useTabState } from "src/hooks/use-tab-state";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface DetailTab {
  id: string;
  label: string;
  /** Keyboard shortcut key (single character) */
  shortcut?: string;
  content: React.ReactNode;
}

export interface MediaDetailLayoutProps {
  /** Content rendered in the primary slot (video player, image viewer, etc.) */
  primaryContent: React.ReactNode;
  /** Title shown above the primary content */
  title?: string;
  /** Tabs for the info pane */
  tabs: DetailTab[];
  /** Active tab controlled externally */
  activeTab?: string;
  onTabChange?: (tabId: string) => void;
  /** Extra content below the primary content (e.g. entity toolbar / metadata summary) */
  headerContent?: React.ReactNode;
  /** Called when the back button is pressed */
  onBack?: () => void;
  /** Extra class names on the root element */
  className?: string;
  /**
   * When true, on mobile the player and tab content share a single
   * scroll container — the player renders at its natural height and the
   * user scrolls past it to reach the tab content. The bottom bar (back
   * + tab triggers) stays pinned. Use on leaf detail pages (scenes,
   * images) where the player/image is the focus and there's no benefit
   * to keeping it pinned at the top. Desktop layout is unchanged.
   */
  mobilePageScroll?: boolean;
  /**
   * Promotes the existing primary viewer to a fixed, viewport-sized surface
   * without moving or remounting it. The layout's own scroll position is
   * preserved while focused and restored on exit.
   */
  primaryFocusMode?: boolean;
  /** Closes `primaryFocusMode`; rendered as an always-visible control. */
  onClosePrimaryFocus?: () => void;
  /** Preferred focus-restoration target after leaving focus mode. */
  primaryFocusReturnRef?: React.RefObject<HTMLElement | null>;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Split-pane detail shell for leaf pages with a primary viewer (video
 * player, image). Desktop is a 320px sidebar + 1fr primary column;
 * mobile is a vertical stack with a fixed bottom tab bar.
 *
 * For collection-style detail pages (performer, studio, tag, gallery,
 * group) — just header + tabs, no primary viewer — use `DetailTabs`
 * from `./detail-tabs.tsx`.
 */
export const MediaDetailLayout: React.FC<MediaDetailLayoutProps> = ({
  primaryContent,
  title,
  tabs,
  activeTab: controlledTab,
  onTabChange,
  headerContent,
  onBack,
  className,
  mobilePageScroll = false,
  primaryFocusMode = false,
  onClosePrimaryFocus,
  primaryFocusReturnRef,
}) => {
  const intl = useIntl();
  const panelId = useId();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const rootRef = useRef<HTMLDivElement>(null);
  const primaryFocusRef = useRef<HTMLDivElement>(null);
  const closeFocusButtonRef = useRef<HTMLButtonElement>(null);
  const savedScrollTopRef = useRef(0);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const wasPrimaryFocusedRef = useRef(false);

  const { activeTab, selectTab, isMounted } = useTabState({
    tabs,
    activeTab: controlledTab,
    onTabChange,
    enableShortcuts: !primaryFocusMode,
  });

  useLayoutEffect(() => {
    const root = rootRef.current;
    const wasFocused = wasPrimaryFocusedRef.current;

    if (primaryFocusMode && !wasFocused) {
      savedScrollTopRef.current = root?.scrollTop ?? 0;
      previousFocusRef.current =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      closeFocusButtonRef.current?.focus({ preventScroll: true });
    } else if (!primaryFocusMode && wasFocused) {
      if (root) root.scrollTop = savedScrollTopRef.current;
      const returnFocus =
        primaryFocusReturnRef?.current ?? previousFocusRef.current;
      returnFocus?.focus({ preventScroll: true });
      // Focusing the original trigger should not alter the restored scroller,
      // but re-apply it for browsers that ignore `preventScroll`.
      if (root) root.scrollTop = savedScrollTopRef.current;
      previousFocusRef.current = null;
    }

    wasPrimaryFocusedRef.current = primaryFocusMode;
  }, [primaryFocusMode, primaryFocusReturnRef]);

  function handlePrimaryFocusKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (!primaryFocusMode || e.key !== "Tab") return;
    const container = primaryFocusRef.current;
    if (!container) return;
    const focusable = Array.from(
      container.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((element) => element.getClientRects().length > 0);
    if (focusable.length === 0) return;

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }

  return (
    <Tabs
      ref={rootRef}
      value={activeTab}
      onValueChange={selectTab}
      className={cn(
        "flex flex-col flex-1 min-h-0",
        // Mobile scroll ownership: in `mobilePageScroll` mode the outer
        // container scrolls (player + tab content together); otherwise
        // the inner sidebar owns the scroll. Desktop always clips here
        // so the sidebar's own scroll area can take over.
        primaryFocusMode
          ? "overflow-hidden overscroll-none"
          : mobilePageScroll
            ? "overflow-y-auto overscroll-contain lg:overflow-hidden"
            : "overflow-hidden",
        "lg:grid lg:grid-rows-[1fr] lg:gap-0",
        // 320px matches the `lg:w-80` sidebar used by the entity
        // detail pages (performer / studio / tag / group) so the
        // detail sidebar reads at one consistent width across every
        // entity type.
        sidebarOpen ? "lg:grid-cols-[320px_1fr]" : "lg:grid-cols-[1fr]",
        className,
      )}
    >
      {/* ── Left sidebar: content column ────────────────────────────────────────
          Mobile (default)        : order-2 (below player), fills remaining
                                    height, sidebar owns the scroll
          Mobile (page-scroll)    : order-2, content-sized — outer container
                                    owns the scroll
          Desktop                 : order-1 (left), fixed width — hidden when
                                    collapsed                                  */}
      <div
        inert={primaryFocusMode ? true : undefined}
        aria-hidden={primaryFocusMode || undefined}
        className={cn(
          "order-2 lg:order-1 flex flex-col lg:border-r lg:border-border lg:flex-1 lg:min-h-0 lg:overflow-hidden",
          // Default mobile: fills remaining flex space and owns its own
          // scroll. Page-scroll: content-sized, but `shrink-0` prevents
          // the parent flex column from compressing tab content into the
          // available space — without it, short tabs (Details / File
          // info) get squished to fit and overflow never triggers, so
          // the outer scroll has nothing to scroll.
          mobilePageScroll ? "shrink-0" : "flex-1 min-h-0",
          // On desktop, collapse hides the sidebar entirely
          sidebarOpen ? "" : "lg:hidden",
        )}
      >
        {/* Desktop-only header: back + title + collapse button */}
        <div className="hidden lg:flex items-center gap-2 px-3 py-2.5 border-b border-border shrink-0">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={onBack}
            className="text-muted-foreground hover:text-foreground"
            aria-label={intl.formatMessage({ id: "actions.back" })}
          >
            <ArrowLeft />
          </Button>

          {title && (
            <h1 className="flex-1 min-w-0 text-base font-semibold leading-[1.4] truncate">
              {title.replace(/_/g, "_\u200B")}
            </h1>
          )}

          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => setSidebarOpen(false)}
            className="text-muted-foreground hover:text-foreground"
            aria-label="Collapse panel"
          >
            <PanelLeftClose />
          </Button>
        </div>

        {/* headerContent (toolbar / metadata summary) */}
        {headerContent && (
          <div className="px-3 pt-2 pb-0 shrink-0">{headerContent}</div>
        )}

        {/* Mobile-only title */}
        {title && (
          <h1 className="lg:hidden text-lg font-semibold leading-[1.4] mt-2 mx-3 mb-0 break-words shrink-0">
            {title.replace(/_/g, "_\u200B")}
          </h1>
        )}

        {/* Desktop tab list — hidden on mobile (mobile uses the fixed
            bottom toolbar below). `DetailTabStrip` owns the
            horizontal-scroll behaviour so 5+ labels in the 320px
            sidebar don't get clipped at the column edge. */}
        <DetailTabStrip tabs={tabs} className="hidden lg:flex" />

        {/* Shared tab panels — desktop always scrolls here. Mobile only
            scrolls here in default mode; in page-scroll mode the outer
            container owns the scroll and this is just a content block. */}
        <div
          className={cn(
            "lg:flex-1 lg:min-h-0 lg:overflow-y-auto overscroll-contain overflow-x-hidden pb-12 lg:pb-0",
            mobilePageScroll ? "" : "flex-1 min-h-0 overflow-y-auto",
          )}
        >
          {tabs.map((tab) => (
            <TabsContent
              key={tab.id}
              value={tab.id}
              id={`${panelId}-${tab.id}`}
              keepMounted={isMounted(tab.id)}
              className="p-3"
            >
              {isMounted(tab.id) ? tab.content : null}
            </TabsContent>
          ))}

          {/* iOS home indicator clearance — mobile only */}
          <div
            className="lg:hidden"
            style={{ height: "env(safe-area-inset-bottom, 0px)" }}
          />
        </div>
      </div>

      {/* ── Right / top: player column ──────────────────────────────────────────
          Mobile  : order-1 (above content), natural fluid height
          Desktop : order-2 (right), flex-1, full height, player centred       */}
      {/* biome-ignore lint/a11y/noStaticElementInteractions: Focus mode traps keyboard focus on this existing container to preserve the live player. */}
      {/* biome-ignore lint/a11y/useAriaPropsSupportedByRole: The dynamic role is dialog exactly when aria-modal is set. */}
      <div
        ref={primaryFocusRef}
        onKeyDown={handlePrimaryFocusKeyDown}
        role={primaryFocusMode ? "dialog" : undefined}
        aria-modal={primaryFocusMode ? true : undefined}
        aria-label={
          primaryFocusMode
            ? intl.formatMessage({
                id: "scene_viewer",
                defaultMessage: "Scene viewer",
              })
            : undefined
        }
        className={cn(
          "order-1 lg:order-2 bg-black min-w-0 min-h-0 lg:flex lg:flex-col lg:overflow-hidden",
          // Default mobile clips so the player column doesn't push past
          // the sidebar's flex boundary. Page-scroll mode wants the
          // player at its natural height inside the outer scroll —
          // `shrink-0` is the same fix as on the sidebar above: keep
          // the flex column from compressing the player to make
          // everything fit within the parent height.
          primaryFocusMode
            ? "fixed inset-0 z-[9999] h-[100dvh] w-screen flex flex-col overflow-hidden overscroll-none touch-none pb-[env(safe-area-inset-bottom,0px)]"
            : mobilePageScroll
              ? "relative shrink-0"
              : "relative overflow-hidden",
        )}
      >
        {primaryFocusMode && onClosePrimaryFocus && (
          <Button
            ref={closeFocusButtonRef}
            type="button"
            variant="ghost"
            onClick={onClosePrimaryFocus}
            data-player-hotkeys-disabled=""
            className="absolute right-3 z-50 h-11 rounded-full border border-white/15 bg-black/60 px-3 text-white shadow-lg backdrop-blur-sm hover:bg-black/75 hover:text-white"
            style={{ top: "max(0.75rem, env(safe-area-inset-top, 0px))" }}
            aria-label={intl.formatMessage({
              id: "actions.close_scene_viewer",
              defaultMessage: "Close scene viewer",
            })}
          >
            <XIcon />
            {intl.formatMessage({
              id: "actions.close_scene_viewer",
              defaultMessage: "Close scene viewer",
            })}
          </Button>
        )}

        {/* Expand button — desktop only, shown when sidebar is collapsed.
            Floats over the player column so re-expanding the sidebar
            doesn't shift the primary content down. */}
        {!sidebarOpen && !primaryFocusMode && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => setSidebarOpen(true)}
            className="hidden lg:flex absolute top-2 left-2 z-10 text-white/70 bg-black/40 hover:bg-black/60 hover:text-white"
            aria-label="Expand panel"
          >
            <PanelLeftOpen />
          </Button>
        )}

        {/* Player — fills remaining space; video maintains aspect ratio via fill mode */}
        <div
          className={cn(
            "lg:flex-1 lg:min-h-0 min-w-0",
            primaryFocusMode && "flex-1 min-h-0",
          )}
        >
          {primaryContent}
        </div>
      </div>

      {/* ── Mobile fixed bottom bar — hidden on desktop ─────────────────────── */}
      {!primaryFocusMode && (
        <div
          className="lg:hidden fixed bottom-0 inset-x-0 z-50 flex flex-col border-t border-border bg-background/95 backdrop-blur-sm"
          role="toolbar"
        >
          <div className="flex items-stretch h-12">
            {/* Back button */}
            <Button
              variant="ghost"
              className="h-full rounded-none border-0 border-r border-border px-3 text-muted-foreground hover:text-foreground hover:bg-transparent"
              onClick={onBack}
              aria-label={intl.formatMessage({ id: "actions.back" })}
            >
              <ArrowLeft size={18} />
            </Button>

            {/* Tab buttons */}
            <nav
              className="flex-1 flex overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
              aria-label={intl.formatMessage({
                id: "accessibility.detail_sections",
                defaultMessage: "Detail sections",
              })}
            >
              {tabs.map((tab) => (
                <Button
                  key={tab.id}
                  variant="ghost"
                  aria-pressed={tab.id === activeTab}
                  aria-controls={`${panelId}-${tab.id}`}
                  className={cn(
                    "flex-1 h-full rounded-none px-2 min-w-[3.5rem] text-sm whitespace-nowrap font-normal border-t-2 border-t-transparent text-muted-foreground hover:text-foreground hover:bg-transparent",
                    tab.id === activeTab &&
                      "border-t-primary text-foreground font-medium",
                  )}
                  onClick={() => selectTab(tab.id)}
                  title={
                    tab.shortcut ? `${tab.label} (${tab.shortcut})` : tab.label
                  }
                >
                  {tab.label}
                </Button>
              ))}
            </nav>
          </div>

          {/* iOS home indicator clearance */}
          <div style={{ height: "env(safe-area-inset-bottom, 0px)" }} />
        </div>
      )}
    </Tabs>
  );
};
