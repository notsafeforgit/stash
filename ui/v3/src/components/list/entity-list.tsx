import type React from "react";
import {
  startTransition,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { cn } from "src/lib/utils";
import { Bookmark, Funnel, SlidersHorizontal } from "lucide-react";
import { useIntl } from "react-intl";
import type { IListSelect } from "./use-list-select";
import type { ListFilterModel } from "src/models/list-filter/filter";
import type { CardAspect } from "./card-aspect-context";
import { SidebarStateContext } from "./use-list-sidebar";
import type { IListSidebarState } from "./use-list-sidebar";
import { ListPagination, PaginationMeta } from "./list-pagination";
import { ListToolbar, type ListToolbarProps } from "./list-toolbar";
import { SearchInput } from "./search-input";
import { TableToolbarSlotProvider } from "./table-toolbar-slot";
import {
  BottomSheet,
  BottomSheetHeader,
  BottomSheetTitle,
} from "src/components/ui/bottom-sheet";
import { Button } from "src/components/ui/button";
import { ListScrollContext } from "./list-scroll-context";
import type { View } from "./views";
import { PluginFilterExtras } from "src/plugins/filter-extras";

// ── EntityList ────────────────────────────────────────────────────────────────

export interface EntityListProps {
  /** Sidebar state produced by useListSidebar */
  sidebarState: IListSidebarState;
  /** Current filter model */
  filter: ListFilterModel;
  /** Set filter callback (mirrors IFilterStateHook) */
  setFilter: (
    f: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel),
  ) => void;
  /** List selection state from useListSelect */
  listSelect: IListSelect;
  /** Number of active filter conditions (for the badge on the filter toggle) */
  activeFilterCount: number;
  /** Persisted list view name exposed to plugin filter extensions. */
  view?: View;
  /** Total number of matching items (for pagination) */
  totalCount: number;

  // ── Slots ──────────────────────────────────────────────────────────────────

  /** Content rendered inside the sidebar panel (FilterBuilder + header) */
  sidebarContent: React.ReactNode;
  /** Main content area (SceneCardGrid, table, etc.) */
  children: React.ReactNode;
  /** Operations component passed through to ListToolbar */
  operationComponent?: React.ReactNode;
  /** Page-level action(s) rendered at the right end of the desktop chrome bar
   *  (e.g. a "+ New" button). Hidden in selection mode. */
  pageActions?: React.ReactNode;
  /** Mobile chrome component rendered below the content area */
  mobileChrome?: React.ReactNode;
  /**
   * When true, the mobile chrome bar is rendered fixed to the bottom of the
   * viewport (for embedded list contexts where the page scrolls naturally).
   * Bottom padding is added to the scroll area to keep content clear of it.
   */
  mobileChromeFixed?: boolean;
  /** Modal rendered outside the shell */
  modal?: React.ReactNode;

  // ── Toolbar props (forwarded to ListToolbar) ──────────────────────────────
  onEdit?: ListToolbarProps["onEdit"];
  onDelete?: ListToolbarProps["onDelete"];
  zoomable?: ListToolbarProps["zoomable"];
  /** When provided, shows an aspect-ratio toggle in the desktop toolbar (Grid mode). */
  cardAspect?: CardAspect;
  setCardAspect?: (a: CardAspect) => void;
  /** Forwarded to ListToolbar — overrides the sort dropdown options. */
  sortOptions?: ListToolbarProps["sortOptions"];

  // ── Meta line ──────────────────────────────────────────────────────────────
  currentSavedFilterName?: string;
  metadataByline?: React.ReactNode;

  // ── Outer container ────────────────────────────────────────────────────────
  /** Extra class names for the root container div */
  className?: string;
}

export const EntityList: React.FC<EntityListProps> = ({
  sidebarState,
  filter,
  setFilter,
  listSelect,
  activeFilterCount,
  view,
  totalCount,
  sidebarContent,
  children,
  operationComponent,
  pageActions,
  mobileChrome,
  mobileChromeFixed,
  modal,
  onEdit,
  onDelete,
  zoomable,
  cardAspect,
  setCardAspect,
  sortOptions,
  currentSavedFilterName,
  metadataByline,
  className,
}) => {
  const intl = useIntl();
  const {
    showSidebar,
    sectionOpen,
    setSectionOpen,
    isMobileSidebar,
    closeFilterSidebar,
    openFilterSidebar,
  } = sidebarState;

  const onSearch = useCallback(
    (value: string) => {
      // Mark the search-driven filter update as a non-urgent React 18
      // transition. The list-card render that follows can be heavy
      // enough to drop typing frames; flagging it as a transition lets
      // React yield between commits so the input event loop stays
      // responsive (loading state still shows immediately via
      // `isPending` upstream). All search interactions go through this
      // callback, so this catches the SearchInput debounce + clear
      // path.
      startTransition(() => {
        setFilter((prev) => {
          const next = prev.clone();
          next.searchTerm = value;
          next.currentPage = 1;
          return next;
        });
      });
    },
    [setFilter],
  );

  // Add a body class so the global CSS can hide the BottomTabBar (which
  // MobileListBar replaces) and remove any tab-bar bottom padding.
  useEffect(() => {
    if (!isMobileSidebar) return;
    document.body.classList.add("mobile-list-view");
    return () => document.body.classList.remove("mobile-list-view");
  }, [isMobileSidebar]);

  // Track the scroll container as state (not a ref) so descendants can react
  // to it via `ListScrollContext` — the virtualizer needs the element to be
  // available in render so `getScrollElement()` returns non-null on the second
  // commit. `useState`'s setter doubles as a callback ref: React calls it with
  // the DOM element on attach and `null` on detach.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  // Reset scroll position when the page changes.
  const lastScrolledPageRef = useRef(filter.currentPage);
  useEffect(() => {
    if (lastScrolledPageRef.current === filter.currentPage) return;
    lastScrolledPageRef.current = filter.currentPage;
    scrollEl?.scrollTo({ top: 0 });
  });

  return (
    <TableToolbarSlotProvider>
      <div className={cn("flex flex-col flex-auto min-h-0", className)}>
        {modal}

        {/* ── Mobile filter Sheet ── */}
        {isMobileSidebar && (
          <BottomSheet
            open={showSidebar}
            onOpenChange={(open) =>
              open ? openFilterSidebar() : closeFilterSidebar()
            }
            className="h-[88svh] max-h-[88svh]"
            blocksListShortcuts={false}
          >
            <BottomSheetHeader className="border-b border-border shrink-0 py-3! px-4!">
              <BottomSheetTitle className="flex items-center text-base gap-2">
                <Funnel className="size-4" />
                {intl.formatMessage({
                  id: "search_filter.edit_filter",
                  defaultMessage: "Filters",
                })}
                {activeFilterCount > 0 && (
                  <span className="bg-primary text-primary-foreground rounded-full text-[0.7rem] font-semibold leading-none px-[0.4rem] py-[0.15rem]">
                    {activeFilterCount}
                  </span>
                )}
              </BottomSheetTitle>
            </BottomSheetHeader>
            <div className="flex-1 overflow-y-auto">{sidebarContent}</div>
          </BottomSheet>
        )}

        <SidebarStateContext.Provider value={{ sectionOpen, setSectionOpen }}>
          <div className="flex flex-auto min-h-0">
            {/* ── Desktop filter sidebar ── */}
            {!isMobileSidebar && (
              <aside
                className={cn(
                  "filter-sidebar-panel shrink-0 border-r border-border flex flex-col h-full overflow-y-auto overflow-x-hidden transition-[width] duration-200 ease-in-out",
                  showSidebar ? "w-[320px]" : "w-0",
                )}
              >
                <div className="w-[320px]">{sidebarContent}</div>
              </aside>
            )}

            {/* ── Main content column ── */}
            <div className="flex flex-col flex-1 min-h-0 min-w-0">
              {/* Desktop chrome bar — sticky to the top of the scroll region.
                Architecturally the bar already sits outside the scroll
                container, but `sticky top-0` is defensive in case any parent
                ever scrolls instead of the inner area. */}
              {!isMobileSidebar && (
                <div className="sticky top-0 z-10 border-b border-border bg-background/90 backdrop-blur px-3 py-1.5 shrink-0">
                  <div className="flex items-center gap-1.5 flex-wrap">
                    {/* Filter toggle button */}
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={intl.formatMessage({
                        id: "search_filter.edit_filter",
                        defaultMessage: "Filters",
                      })}
                      aria-pressed={showSidebar}
                      className="relative text-muted-foreground hover:bg-secondary hover:text-foreground aria-pressed:text-foreground"
                      onClick={
                        showSidebar ? closeFilterSidebar : openFilterSidebar
                      }
                    >
                      <SlidersHorizontal size={15} />
                      {activeFilterCount > 0 && (
                        <span className="absolute -right-1 -top-1 bg-primary text-primary-foreground rounded-full text-[0.625rem] font-semibold leading-none min-w-4 px-1 py-0.5 text-center">
                          {activeFilterCount}
                        </span>
                      )}
                    </Button>

                    <SearchInput
                      value={filter.searchTerm}
                      onChange={onSearch}
                      className="w-44"
                    />

                    {currentSavedFilterName && (
                      <span
                        className="hidden xl:inline-flex items-center gap-1 text-xs text-muted-foreground bg-muted/60 rounded-md px-2 py-0.5 max-w-[14rem] truncate"
                        title={`Using saved filter: ${currentSavedFilterName}`}
                      >
                        <Bookmark size={11} className="shrink-0" />
                        <span className="truncate">
                          {currentSavedFilterName}
                        </span>
                      </span>
                    )}

                    <ListToolbar
                      filter={filter}
                      setFilter={setFilter}
                      listSelect={listSelect}
                      onEdit={onEdit}
                      onDelete={onDelete}
                      operationComponent={operationComponent}
                      zoomable={zoomable}
                      cardAspect={cardAspect}
                      setCardAspect={setCardAspect}
                      sortOptions={sortOptions}
                    />

                    <div className="ml-auto flex items-center gap-2">
                      <span className="hidden md:inline-flex text-xs text-muted-foreground tabular-nums">
                        <PaginationMeta
                          currentPage={filter.currentPage}
                          itemsPerPage={filter.itemsPerPage}
                          totalItems={totalCount}
                          metadataByline={metadataByline}
                        />
                      </span>

                      <ListPagination
                        currentPage={filter.currentPage}
                        itemsPerPage={filter.itemsPerPage}
                        totalItems={totalCount}
                        onChangePage={(page) =>
                          setFilter(filter.changePage(page))
                        }
                      />

                      {pageActions && !listSelect.hasSelection && (
                        <div className="flex items-center gap-1">
                          {pageActions}
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              )}

              <PluginFilterExtras filter={filter} view={view} />

              {/* Scrollable content area — bounded so the scrollbar stops here.
                We deliberately don't render a spinner overlay during loading;
                the children render skeleton cards in-place instead, which
                stays consistent across the grid / details / wall modes. */}
              <div className="relative flex-1 min-h-0">
                <div
                  ref={setScrollEl}
                  className={cn(
                    "relative h-full overflow-y-auto overflow-x-hidden",
                    mobileChromeFixed &&
                      isMobileSidebar &&
                      "pb-[calc(5rem+env(safe-area-inset-bottom,0px))]",
                  )}
                >
                  <ListScrollContext.Provider value={scrollEl}>
                    {children}
                  </ListScrollContext.Provider>
                </div>
              </div>

              {/* Mobile chrome — fixed to viewport bottom (embedded) or in-flow (standalone) */}
              {isMobileSidebar &&
                mobileChrome &&
                (mobileChromeFixed ? (
                  <div className="fixed bottom-0 left-0 right-0 z-50">
                    {mobileChrome}
                  </div>
                ) : (
                  mobileChrome
                ))}
            </div>
          </div>
        </SidebarStateContext.Provider>
      </div>
    </TableToolbarSlotProvider>
  );
};
