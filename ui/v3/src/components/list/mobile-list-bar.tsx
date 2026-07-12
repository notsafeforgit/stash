import type React from "react";
import { startTransition, useCallback, useEffect, useState } from "react";
import { cn } from "src/lib/utils";
import {
  Funnel,
  Menu,
  Tags,
  Settings2,
  X,
  Check,
  LayoutGrid,
  Image,
  LayoutList,
  Table2,
  ChevronsLeft,
  ChevronsRight,
  Square,
  Columns2,
  RectangleVertical,
  RectangleHorizontal,
  Proportions,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { getSortDirectionIcon } from "./sort-icon";
import { FormattedMessage, useIntl } from "react-intl";
import type { ListFilterModel } from "src/models/list-filter/filter";
import type { ISortByOption } from "src/models/list-filter/filter-options";
import { DisplayMode } from "src/models/list-filter/types";
import { SortDirectionEnum } from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import {
  PinButton,
  PinnableComboBox,
} from "src/components/ui/pinnable-combo-box";
import { MobileNavSheet } from "src/components/layout/mobile-nav-sheet";
import {
  BottomSheet,
  BottomSheetHeader,
  BottomSheetTitle,
} from "src/components/ui/bottom-sheet";
import {
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "src/components/ui/pagination";
import type { View } from "src/components/list/views";
import type { CardAspect } from "src/components/list/card-aspect-context";
import { SearchInput } from "src/components/list/search-input";
import {
  TableToolbarSlot,
  useDeclareTableToolbarProvider,
} from "src/components/list/table-toolbar-slot";
import { useDefaultFilterActions } from "src/hooks/default-filter";
import { DefaultFilterConflict } from "src/components/filters/default-filter-conflict";
import { useVisualViewportBottomInset } from "src/hooks/use-visual-viewport-bottom-inset";

// ── Constants ─────────────────────────────────────────────────────────────────

const PAGE_SIZES = [20, 40, 60, 100];

// ── Pinned sort hook ──────────────────────────────────────────────────────────

function usePinnedSortOptions(mode: string) {
  const key = `list-pinned-sort:${mode}`;
  const [pinnedValues, setPinnedValues] = useState<string[]>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (!raw) return [];
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed)
        ? parsed.filter((v): v is string => typeof v === "string")
        : [];
    } catch {
      return [];
    }
  });

  useEffect(() => {
    try {
      window.localStorage.setItem(key, JSON.stringify(pinnedValues));
    } catch {
      // ignore
    }
  }, [key, pinnedValues]);

  const togglePinned = useCallback((value: string) => {
    setPinnedValues((curr) =>
      curr.includes(value) ? curr.filter((v) => v !== value) : [...curr, value],
    );
  }, []);

  return { pinnedValues, togglePinned };
}

// ── Display mode icon ─────────────────────────────────────────────────────────

function DisplayModeIcon({ mode }: { mode: DisplayMode }) {
  switch (mode) {
    case DisplayMode.Grid:
      return <LayoutGrid size={15} />;
    case DisplayMode.Wall:
      return <Image size={15} />;
    case DisplayMode.Details:
      return <LayoutList size={15} />;
    case DisplayMode.Table:
      return <Table2 size={15} />;
    default:
      return <LayoutGrid size={15} />;
  }
}

// ── Props ─────────────────────────────────────────────────────────────────────

export interface MobileListBarProps {
  filter: ListFilterModel;
  setFilter: (
    f: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel),
  ) => void;
  totalCount: number;
  activeFilterCount: number;
  hasSelection: boolean;
  selecting: boolean;
  selectedCount: number;
  onSelectAll: () => void;
  onSelectNone: () => void;
  onTaggerMode?: () => void;
  openFilterSidebar: () => void;
  mobileGridCols: 1 | 2;
  setMobileGridCols: (cols: 1 | 2) => void;
  /** When provided, shows a portrait/landscape/auto aspect-ratio toggle in Grid mode. */
  cardAspect?: CardAspect;
  setCardAspect?: (a: CardAspect) => void;
  /** When true, shows zoom controls in Grid and Wall modes. */
  zoomable?: boolean;
  /** When provided, shows Set/Clear default filter controls in the View options sheet. */
  view?: View;
  /** Override for the sort dropdown options — see `ListToolbar`. */
  sortOptions?: ISortByOption[];
}

// ── MobileListBar ─────────────────────────────────────────────────────────────

export const MobileListBar: React.FC<MobileListBarProps> = ({
  filter,
  setFilter,
  totalCount,
  activeFilterCount,
  hasSelection,
  selecting,
  selectedCount,
  onSelectAll,
  onSelectNone,
  onTaggerMode,
  openFilterSidebar,
  mobileGridCols,
  setMobileGridCols,
  cardAspect,
  setCardAspect,
  zoomable,
  view,
  sortOptions: sortOptionsOverride,
}) => {
  const intl = useIntl();
  const [navOpen, setNavOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const defaultFilter = useDefaultFilterActions(view, filter);
  const { bottomInset, ref: barRef } =
    useVisualViewportBottomInset<HTMLDivElement>();

  const onSearch = useCallback(
    (value: string) => {
      // See entity-list.tsx for the rationale — non-urgent transition
      // so heavy list-card re-renders don't block typing.
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

  // Settings helpers
  const { displayModeOptions } = filter.options;
  const sortByOptions = sortOptionsOverride ?? filter.options.sortByOptions;

  // Reserve the column-manager slot on behalf of the view-options drawer.
  // The drawer's body only mounts when the sheet is open, but the slot's
  // owner (this bar) is always rendered — so EntityDataTable can know to
  // skip its inline fallback even while the drawer is closed.
  useDeclareTableToolbarProvider(filter.displayMode === DisplayMode.Table);
  const { pinnedValues: pinnedSortValues, togglePinned: toggleSortPin } =
    usePinnedSortOptions(filter.mode);

  const sortOptions = sortByOptions.map((opt) => ({
    value: opt.value,
    label: intl.formatMessage({ id: opt.messageID, defaultMessage: opt.value }),
  }));
  const currentSortLabel =
    sortOptions.find((o) => o.value === (filter.sortBy ?? ""))?.label ?? "";
  const totalPages = Math.ceil(totalCount / filter.itemsPerPage);
  const canPrev = filter.currentPage > 1;
  const canNext = filter.currentPage < totalPages;
  const pageStart = (filter.currentPage - 1) * filter.itemsPerPage + 1;
  const pageEnd = Math.min(
    filter.currentPage * filter.itemsPerPage,
    totalCount,
  );

  function setSortBy(value: string) {
    setFilter(filter.setSortBy(value || undefined));
  }
  function toggleSortDirection() {
    setFilter(filter.toggleSortDirection());
  }
  function setPageSize(size: number) {
    setFilter(filter.setPageSize(size));
  }
  function setDisplayMode(mode: DisplayMode) {
    setFilter(filter.setDisplayMode(mode));
  }

  return (
    <>
      {/* Global nav sheet */}
      <MobileNavSheet open={navOpen} onOpenChange={setNavOpen} />

      {/* View-options sheet (sort / display mode / page size / pagination) */}
      <BottomSheet open={settingsOpen} onOpenChange={setSettingsOpen}>
        <BottomSheetHeader className="border-b border-border shrink-0 py-3! px-4!">
          <BottomSheetTitle>
            {intl.formatMessage({
              id: "view_options",
              defaultMessage: "View options",
            })}
          </BottomSheetTitle>
        </BottomSheetHeader>

        <div className="p-4 flex flex-col gap-5">
          {/* Sort by — hidden in Table mode */}
          {sortByOptions.length > 0 &&
            filter.displayMode !== DisplayMode.Table && (
              <div className="flex items-center gap-3">
                <span className="text-sm text-muted-foreground shrink-0 w-20">
                  {intl.formatMessage({
                    id: "search_filter.sort_by",
                    defaultMessage: "Sort by",
                  })}
                </span>
                <PinnableComboBox
                  triggerClassName="flex-1"
                  side="top"
                  currentLabel={currentSortLabel || "…"}
                  options={sortOptions}
                  selectedValue={filter.sortBy ?? ""}
                  onSelect={setSortBy}
                  pinnedValues={pinnedSortValues}
                  renderItemAddon={(value, isPinned) => (
                    <PinButton
                      pinned={isPinned}
                      onToggle={() => toggleSortPin(value)}
                    />
                  )}
                />
                {(() => {
                  const SortDirIcon = getSortDirectionIcon(
                    filter.sortBy,
                    filter.sortDirection,
                  );
                  return (
                    <Button
                      variant={
                        filter.sortDirection === SortDirectionEnum.Desc
                          ? "secondary"
                          : "ghost"
                      }
                      size="icon-sm"
                      onClick={toggleSortDirection}
                      title={
                        filter.sortDirection === SortDirectionEnum.Asc
                          ? "Ascending"
                          : "Descending"
                      }
                    >
                      <SortDirIcon size={14} />
                    </Button>
                  );
                })()}
              </div>
            )}

          {/* Display mode */}
          {displayModeOptions.length > 1 && (
            <div className="flex items-center gap-3">
              <span className="text-sm text-muted-foreground shrink-0 w-20">
                {intl.formatMessage({
                  id: "view",
                  defaultMessage: "View",
                })}
              </span>
              <div className="flex gap-1.5">
                {displayModeOptions.map((mode) => (
                  <Button
                    key={mode}
                    variant={
                      filter.displayMode === mode ? "secondary" : "ghost"
                    }
                    size="icon-sm"
                    onClick={() => setDisplayMode(mode)}
                  >
                    <DisplayModeIcon mode={mode} />
                  </Button>
                ))}
              </div>
            </div>
          )}

          {/* Columns (Table mode only) — EntityDataTable portals its
              column-manager Sheet trigger (which carries its own "Columns"
              label) into this slot, no surrounding label needed. */}
          {filter.displayMode === DisplayMode.Table && (
            <TableToolbarSlot className="flex" />
          )}

          {/* Grid columns (Grid mode only) */}
          {filter.displayMode === DisplayMode.Grid && (
            <div className="flex items-center gap-3">
              <span className="text-sm text-muted-foreground shrink-0 w-20">
                {intl.formatMessage({
                  id: "columns",
                  defaultMessage: "Columns",
                })}
              </span>
              <div className="flex gap-1.5">
                <Button
                  variant={mobileGridCols === 1 ? "secondary" : "ghost"}
                  size="icon-sm"
                  onClick={() => setMobileGridCols(1)}
                  title="1 column"
                >
                  <Square size={15} />
                </Button>
                <Button
                  variant={mobileGridCols === 2 ? "secondary" : "ghost"}
                  size="icon-sm"
                  onClick={() => setMobileGridCols(2)}
                  title="2 columns"
                >
                  <Columns2 size={15} />
                </Button>
              </div>
            </div>
          )}

          {/* Zoom (Grid + Wall mode, when zoomable) */}
          {zoomable && filter.displayMode === DisplayMode.Wall && (
            <div className="flex items-center gap-3">
              <span className="text-sm text-muted-foreground shrink-0 w-20">
                {intl.formatMessage({ id: "zoom", defaultMessage: "Zoom" })}
              </span>
              <div className="flex gap-1.5 items-center">
                {/* zoomIndex 0 = fewest columns (largest cards = most zoomed in) */}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={filter.zoomIndex >= 4}
                  onClick={() =>
                    setFilter((prev) =>
                      prev.setZoom(Math.min(4, prev.zoomIndex + 1)),
                    )
                  }
                  title={intl.formatMessage({
                    id: "zoom_out",
                    defaultMessage: "Zoom out",
                  })}
                >
                  <ZoomOut size={15} />
                </Button>
                <div className="flex items-center gap-[3px] px-0.5" aria-hidden>
                  {[0, 1, 2, 3, 4].map((i) => (
                    // See list-toolbar.tsx: fixed-size dots + scale
                    // transform avoids layout jiggle during rapid
                    // zoom changes.
                    <span
                      key={i}
                      className={cn(
                        "w-2 h-2 rounded-full transition-[transform,background-color] duration-150",
                        i === 4 - filter.zoomIndex
                          ? "bg-foreground"
                          : "bg-muted-foreground/40 scale-75",
                      )}
                    />
                  ))}
                </div>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={filter.zoomIndex <= 0}
                  onClick={() =>
                    setFilter((prev) =>
                      prev.setZoom(Math.max(0, prev.zoomIndex - 1)),
                    )
                  }
                  title={intl.formatMessage({
                    id: "zoom_in",
                    defaultMessage: "Zoom in",
                  })}
                >
                  <ZoomIn size={15} />
                </Button>
              </div>
            </div>
          )}

          {/* Aspect ratio (Grid mode + supported entities only) */}
          {filter.displayMode === DisplayMode.Grid &&
            cardAspect !== undefined &&
            setCardAspect && (
              <div className="flex items-center gap-3">
                <span className="text-sm text-muted-foreground shrink-0 w-20">
                  {intl.formatMessage({
                    id: "card_aspect",
                    defaultMessage: "Aspect",
                  })}
                </span>
                <div className="flex gap-1.5">
                  <Button
                    variant={cardAspect === "portrait" ? "secondary" : "ghost"}
                    size="icon-sm"
                    onClick={() => setCardAspect("portrait")}
                    title={intl.formatMessage({
                      id: "card_aspect_portrait",
                      defaultMessage: "Portrait",
                    })}
                  >
                    <RectangleVertical size={15} />
                  </Button>
                  <Button
                    variant={cardAspect === "landscape" ? "secondary" : "ghost"}
                    size="icon-sm"
                    onClick={() => setCardAspect("landscape")}
                    title={intl.formatMessage({
                      id: "card_aspect_landscape",
                      defaultMessage: "Landscape",
                    })}
                  >
                    <RectangleHorizontal size={15} />
                  </Button>
                  <Button
                    variant={cardAspect === "auto" ? "secondary" : "ghost"}
                    size="icon-sm"
                    onClick={() => setCardAspect("auto")}
                    title={intl.formatMessage({
                      id: "card_aspect_auto",
                      defaultMessage: "Auto",
                    })}
                  >
                    <Proportions size={15} />
                  </Button>
                </div>
              </div>
            )}

          {/* Page size */}
          <div className="flex items-center gap-3">
            <span className="text-sm text-muted-foreground shrink-0 w-20">
              {intl.formatMessage({
                id: "search_filter.page_size",
                defaultMessage: "Per page",
              })}
            </span>
            <div className="flex gap-1.5 flex-wrap">
              {PAGE_SIZES.map((size) => (
                <Button
                  key={size}
                  variant={filter.itemsPerPage === size ? "secondary" : "ghost"}
                  size="sm"
                  onClick={() => setPageSize(size)}
                >
                  {size}
                </Button>
              ))}
            </div>
          </div>

          {/* Default filter — only shown when a view is provided */}
          {view && (
            <div className="flex items-center gap-2 flex-wrap border-t border-border pt-4">
              <Button
                variant="outline"
                size="sm"
                disabled={defaultFilter.saving}
                onClick={defaultFilter.setCurrent}
              >
                <FormattedMessage id="actions.set_current_sort_as_default" />
              </Button>
              {defaultFilter.hasDefault && (
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={defaultFilter.saving}
                  onClick={defaultFilter.clear}
                >
                  <X size={14} />
                  <FormattedMessage
                    id="actions.clear_default_filter"
                    defaultMessage="Clear default"
                  />
                </Button>
              )}
              {defaultFilter.hasConflict && (
                <DefaultFilterConflict
                  disabled={defaultFilter.saving}
                  onUseLegacy={defaultFilter.useLegacy}
                  onKeepV3={defaultFilter.keepV3}
                />
              )}
            </div>
          )}
        </div>
      </BottomSheet>

      {/* ── Bottom bar ────────────────────────────────────────────────────────── */}
      <div
        ref={barRef}
        className="relative z-50 flex flex-col bg-background border-t border-border shrink-0 pb-[env(safe-area-inset-bottom,0px)]"
        style={
          bottomInset > 0
            ? { transform: `translateY(-${bottomInset}px)` }
            : undefined
        }
      >
        {/* Pagination strip */}
        <div className="grid grid-cols-[1fr_auto_1fr] items-center border-b border-border/50 px-1 py-0.5">
          <div className="flex items-center gap-0.5">
            <PaginationLink
              size="icon-sm"
              disabled={!canPrev}
              onClick={() => setFilter(filter.changePage(1))}
              aria-label={intl.formatMessage({ id: "pagination.first" })}
            >
              <ChevronsLeft size={14} />
            </PaginationLink>
            <PaginationPrevious
              disabled={!canPrev}
              onClick={() =>
                setFilter(filter.changePage(filter.currentPage - 1))
              }
              className="[&_span]:block"
              text={intl.formatMessage({
                id: "pagination.previous_short",
                defaultMessage: "Prev",
              })}
            />
          </div>
          <span className="text-center text-xs text-muted-foreground whitespace-nowrap px-2">
            {totalCount > 0 ? `${pageStart}–${pageEnd} / ${totalCount}` : "0"}
          </span>
          <div className="flex items-center gap-0.5 justify-end">
            <PaginationNext
              disabled={!canNext}
              onClick={() =>
                setFilter(filter.changePage(filter.currentPage + 1))
              }
              className="[&_span]:block"
            />
            <PaginationLink
              size="icon-sm"
              disabled={!canNext}
              onClick={() => setFilter(filter.changePage(totalPages))}
              aria-label={intl.formatMessage({ id: "pagination.last" })}
            >
              <ChevronsRight size={14} />
            </PaginationLink>
          </div>
        </div>

        {/* Icon row */}
        <div className="flex items-center gap-2 px-3 min-h-11">
          {selecting || hasSelection ? (
            // Selection mode chrome
            <div className="flex flex-auto items-center gap-1">
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={onSelectNone}
                title={intl.formatMessage({ id: "actions.select_none" })}
              >
                <X size={16} />
              </Button>
              <span className="text-sm min-w-6 text-center">
                {selectedCount}
              </span>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={onSelectAll}
                title={intl.formatMessage({ id: "actions.select_all" })}
              >
                <Check size={16} />
              </Button>
            </div>
          ) : (
            <>
              {/* Left icons */}
              <div className="flex shrink-0 items-center gap-1">
                {/* Nav / entity swap */}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setNavOpen(true)}
                  aria-label={intl.formatMessage({
                    id: "navigation",
                    defaultMessage: "Navigation",
                  })}
                >
                  <Menu size={18} />
                </Button>

                {/* Filter toggle */}
                <Button
                  variant="ghost"
                  size="icon"
                  className={cn(
                    "relative hover:bg-transparent",
                    activeFilterCount > 0
                      ? "text-foreground"
                      : "text-muted-foreground",
                  )}
                  onClick={openFilterSidebar}
                  aria-label={intl.formatMessage({
                    id: "search_filter.edit_filter",
                    defaultMessage: "Filters",
                  })}
                >
                  <Funnel size={18} />
                  {activeFilterCount > 0 && (
                    <span className="absolute right-0 top-0 bg-primary text-primary-foreground rounded-full text-[0.625rem] font-semibold leading-none min-w-4 px-1 py-0.5 text-center">
                      {activeFilterCount}
                    </span>
                  )}
                </Button>
              </div>

              {/* Search input */}
              <SearchInput
                value={filter.searchTerm}
                onChange={onSearch}
                className="flex-1 min-w-0"
                inputClassName="w-full bg-transparent border-0 border-b border-border/60 rounded-none text-sm text-foreground placeholder:text-muted-foreground px-1 py-1 outline-none focus-visible:ring-0 focus:border-primary transition-colors h-auto"
              />

              {/* Right icons */}
              <div className="flex shrink-0 items-center gap-1">
                {/* View / sort / settings */}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setSettingsOpen(true)}
                  aria-label={intl.formatMessage({
                    id: "view_options",
                    defaultMessage: "View options",
                  })}
                >
                  <Settings2 size={18} />
                </Button>

                {/* Tagger mode */}
                {onTaggerMode && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={onTaggerMode}
                    aria-label={intl.formatMessage({
                      id: "actions.tagger",
                      defaultMessage: "Tagger",
                    })}
                  >
                    <Tags size={18} />
                  </Button>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </>
  );
};
