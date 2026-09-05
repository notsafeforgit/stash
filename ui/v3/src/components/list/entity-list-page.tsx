import { useState, useEffect, useRef, useMemo } from "react";
import type { OperationVariables } from "@apollo/client";
import { QueryError } from "@/components/query-error";
import type { IHasID } from "@/utils/data";
import type { ListFilterModel } from "@/models/list-filter/filter";
import { DisplayMode } from "@/models/list-filter/types";
import type { View } from "./views";
import { FilterBuilder } from "@/components/filters/filter-builder";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
  EmptyMedia,
} from "@/components/ui/empty";
import { useIntl } from "react-intl";
import { SearchXIcon } from "lucide-react";
import { EntityList } from "./entity-list";
import { useListSidebar } from "./use-list-sidebar";
import { useListSelect } from "./use-list-select";
import { useListKeyboardShortcuts } from "./use-list-keyboard-shortcuts";
import { MobileListBar } from "./mobile-list-bar";
import { MobileGridColsContext } from "./mobile-grid-context";
import { CardLayoutContext } from "./card-layout-context";
import { useListActivity } from "./list-activity-context";
import { CardAspectContext } from "./card-aspect-context";
import { ZoomIndexContext } from "./zoom-index-context";
import { EntityDataTable } from "./entity-data-table";
import {
  ListStateContext,
  type ListContextState,
  type BulkApplyTarget,
} from "./list-provider";
import type { EntityListPageConfig } from "./entity-list-types";
import { PhotoAlbumWall } from "./photo-album-wall";
import { VirtualizedItemList } from "./virtualized-item-list";
import { useListPageFilter } from "./use-list-page-filter";
import { useListData } from "./use-list-data";
import { useListPageRefill } from "./use-list-page-refill";

export type {
  EntityListPageConfig,
  LocalDataSource,
  PageNavHandle,
} from "./entity-list-types";

// Default empty-state for the list page — shown when the data source
// returns zero items and isn't loading. Pages can override via
// `EntityListPageConfig.emptyState`.
function DefaultListEmptyState({ active }: { active: boolean }) {
  const intl = useIntl();
  return (
    <Empty className="w-auto border border-dashed border-border rounded-lg my-6 mx-3">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchXIcon />
        </EmptyMedia>
        <EmptyTitle>
          {intl.formatMessage(
            active
              ? {
                  id: "list.empty.no_results_title",
                  defaultMessage: "No results found",
                }
              : {
                  id: "list.empty.no_items_title",
                  defaultMessage: "Nothing here yet",
                },
          )}
        </EmptyTitle>
        <EmptyDescription>
          {intl.formatMessage(
            active
              ? {
                  id: "list.empty.no_results_description",
                  defaultMessage:
                    "Try clearing or adjusting filters to see more.",
                }
              : {
                  id: "list.empty.no_items_description",
                  defaultMessage: "There are no items to display.",
                },
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

interface EntityListPageProps<
  TData,
  TItem extends IHasID,
  TVariables extends OperationVariables,
> {
  config: EntityListPageConfig<TData, TItem, TVariables>;
  /** Whether to sync filter state to the URL (default: true). Pass false for embedded panels. */
  useURL?: boolean;
  /** Pre-populated filter used as the starting state (e.g. with a locked criterion). */
  defaultFilter?: ListFilterModel;
  /** Pin the mobile chrome bar to the bottom of the viewport instead of in-flow (for embedded use). */
  mobileChromeFixed?: boolean;
  /** When true, list keyboard shortcuts (arrow-key pagination, etc.) are suppressed. */
  keyboardShortcutsDisabled?: boolean;
  /**
   * View identifier for this list. Overrides config.view when provided.
   * Required for embedded tabs to enable per-context default filter persistence.
   */
  view?: View;
}

export function EntityListPage<
  TData,
  TItem extends IHasID,
  TVariables extends OperationVariables,
>({
  config,
  useURL,
  defaultFilter,
  mobileChromeFixed,
  keyboardShortcutsDisabled,
  view: viewProp,
}: EntityListPageProps<TData, TItem, TVariables>) {
  const isActive = useListActivity();
  const {
    filterMode,
    view: configView,
    defaultSort,
    source,
    sidebarContent: sidebarContentOverride,
    sortOptions: sortOptionsOverride,
    emptyState: emptyStateOverride,
    renderCard,
    onCardPreviewClick,
    zoomable,
    cardIsPortrait,
    supportsCardAspect: supportsCardAspectProp,
    tableColumns,
    tableVisibilityKey,
    renderTableRow,
    pageNavRef,
    onItemsChanged,
    renderTagger,
    getWallDimensions,
    pageActions,
  } = config;

  // viewProp (from the component prop) takes precedence over config.view.
  const view = viewProp ?? configView;

  const supportsCardAspect = supportsCardAspectProp ?? true;

  const {
    filter,
    setFilter,
    mobileGridCols,
    setMobileGridCols,
    cardAspect,
    setCardAspect,
  } = useListPageFilter({
    filterMode,
    view,
    defaultSort,
    useURL,
    isActive,
    defaultFilter,
  });
  const [taggerActive, setTaggerActive] = useState(false);
  const sidebarState = useListSidebar(view);
  const [currentSavedFilterName, setCurrentSavedFilterName] = useState<
    string | undefined
  >();
  const { isMobileSidebar, openFilterSidebar } = sidebarState;

  const { count, items, loading, error, hasData, refetch, refreshing } =
    useListData(source, filter);
  let isLoading = loading;

  // First-paint split: even on a warm Apollo cache, mounting 40+ cards is
  // expensive enough that View Transitions on back-nav from a detail page
  // wait visibly for the reconcile to land. Force the first commit to render
  // skeletons (cheap), then flip on the next animation frame so real cards
  // mount in the second commit. The cross-fade then lands on skeletons
  // immediately and cards stream in ~16ms later.
  const [firstPaintReady, setFirstPaintReady] = useState(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => setFirstPaintReady(true));
    return () => cancelAnimationFrame(id);
  }, []);
  if (!firstPaintReady) isLoading = true;

  const listSelect = useListSelect(items);

  const preserveScrollDuringRefill = useListPageRefill({
    remote: source.kind === "graphql",
    filter,
    setFilter,
    count,
    items,
    isLoading,
    error,
    refetch,
  });

  const applyToAllTarget = useMemo<BulkApplyTarget>(
    () => ({
      findFilter: { ...filter.makeFindFilter(), page: undefined, per_page: -1 },
      filterAST: filter.makeFilterAST(),
    }),
    [filter],
  );

  // Stable context: only changes when query results or filter change, never on selection.
  // Cards read selection imperatively via getSelectedIds/getSelectedItems when needed
  // (e.g. on context-menu open) rather than subscribing to reactive values.
  const listStateContextValue = useMemo<ListContextState<TItem>>(
    () => ({
      selectable: true,
      items,
      totalCount: count,
      applyToAllTarget,
      getSelectedIds: listSelect.getSelectedIds,
      getSelectedItems: listSelect.getSelectedItems,
      onSelectAll: listSelect.onSelectAll,
      onSelectNone: listSelect.onSelectNone,
    }),
    [
      items,
      count,
      applyToAllTarget,
      listSelect.getSelectedIds,
      listSelect.getSelectedItems,
      listSelect.onSelectAll,
      listSelect.onSelectNone,
    ],
  );

  // FilterBuilder expects the root to be a group node; top-level filter ASTs
  // are always groups in practice (the URL decoder wraps bare conditions in a group).
  const filterAst =
    filter.filterAst?.kind === "group" ? filter.filterAst : undefined;
  const activeFilterCount = filter.count();

  const totalPages = Math.ceil(count / filter.itemsPerPage);

  // Populate the pageNavRef with the current navigation state so consumers
  // can drive page changes imperatively (e.g. from inside a lightbox).
  useEffect(() => {
    if (!pageNavRef) return;
    pageNavRef.current = {
      currentPage: filter.currentPage,
      totalPages,
      itemsPerPage: filter.itemsPerPage,
      totalCount: count,
      nextPage: () => {
        if (filter.currentPage < totalPages)
          setFilter(filter.changePage(filter.currentPage + 1));
      },
      prevPage: () => {
        if (filter.currentPage > 1)
          setFilter(filter.changePage(filter.currentPage - 1));
      },
    };
  });

  // Notify the consumer whenever a new batch of card items arrives
  // (skips the initial load so it only fires on subsequent changes).
  // Trips on the page-slice items array so it works for both data
  // sources — for GraphQL this is `extractResult(result.data).items`,
  // for local source it's the slice the local filter returned.
  const isInitialDataRef = useRef(true);
  // Refs so the effect doesn't need to list onItemsChanged as a dep —
  // the callback should always use the latest version without causing re-fires.
  const onItemsChangedRef = useRef(onItemsChanged);
  onItemsChangedRef.current = onItemsChanged;
  useEffect(() => {
    if (isLoading) return;
    if (isInitialDataRef.current) {
      isInitialDataRef.current = false;
      return;
    }
    if (onItemsChangedRef.current) {
      onItemsChangedRef.current(items);
    }
  }, [items, isLoading]);

  useListKeyboardShortcuts({
    currentPage: filter.currentPage,
    pages: totalPages,
    onChangePage: (page) => setFilter(filter.changePage(page)),
    showEditFilter: openFilterSidebar,
    onSelectAll: listSelect.onSelectAll,
    onSelectNone: listSelect.onSelectNone,
    onInvertSelection: listSelect.onInvertSelection,
    selectModeActive: listSelect.selecting,
    disabled: keyboardShortcutsDisabled || !isActive,
  });

  const sidebarContent = sidebarContentOverride ?? (
    <FilterBuilder
      mode={filterMode}
      view={view}
      filter={filter}
      setFilter={setFilter}
      root={filterAst}
      onChange={(node) => {
        const next = filter.clone();
        next.filterAst = node;
        setFilter(next);
      }}
      isOpen={sidebarState.showSidebar}
      currentSavedFilterName={currentSavedFilterName}
      onCurrentSavedFilterChange={(next) =>
        setCurrentSavedFilterName(next?.name)
      }
      lockedRoot={filter.lockedFilterAst}
    />
  );

  const mobileChrome = (
    <MobileListBar
      filter={filter}
      setFilter={setFilter}
      totalCount={count}
      activeFilterCount={activeFilterCount}
      hasSelection={listSelect.hasSelection}
      selecting={listSelect.selecting}
      selectedCount={listSelect.selectedItems.length}
      onSelectAll={listSelect.onSelectAll}
      onSelectNone={listSelect.onSelectNone}
      onTaggerMode={renderTagger ? () => setTaggerActive((v) => !v) : undefined}
      openFilterSidebar={openFilterSidebar}
      mobileGridCols={mobileGridCols}
      setMobileGridCols={setMobileGridCols}
      cardAspect={supportsCardAspect ? cardAspect : undefined}
      setCardAspect={supportsCardAspect ? setCardAspect : undefined}
      zoomable={zoomable}
      view={view}
      sortOptions={sortOptionsOverride}
    />
  );

  return (
    <EntityList
      sidebarState={sidebarState}
      filter={filter}
      setFilter={setFilter}
      listSelect={listSelect}
      activeFilterCount={activeFilterCount}
      view={view}
      totalCount={count}
      preserveScrollDuringRefill={preserveScrollDuringRefill}
      sidebarContent={sidebarContent}
      currentSavedFilterName={currentSavedFilterName}
      mobileChrome={mobileChrome}
      mobileChromeFixed={mobileChromeFixed}
      zoomable={zoomable}
      cardAspect={supportsCardAspect ? cardAspect : undefined}
      setCardAspect={supportsCardAspect ? setCardAspect : undefined}
      sortOptions={sortOptionsOverride}
      pageActions={pageActions}
    >
      {error && (
        <QueryError
          error={error}
          retry={refetch}
          retrying={refreshing}
          stale={hasData}
        />
      )}
      {error && !hasData ? null : taggerActive && renderTagger ? (
        renderTagger(items)
      ) : !isLoading && items.length === 0 ? (
        // The "active" branch fires whenever the user has narrowed
        // the result set — search term, filter conditions, or (for
        // the local source) any filter that survived through into
        // `localSource.filter`. We can't introspect localSource's
        // internal extra-state, so we approximate by checking
        // searchTerm + activeFilterCount; pages that need a
        // smarter signal can supply their own emptyState.
        (emptyStateOverride ?? (
          <DefaultListEmptyState
            active={
              activeFilterCount > 0 || filter.searchTerm.trim().length > 0
            }
          />
        ))
      ) : filter.displayMode === DisplayMode.Table && tableColumns ? (
        // Table rows pull from the same list context the cards do — so
        // SelectAllMenuItem, BulkContextMenuItems, and useBulkCardActions
        // all see the same selectable/selectedItems/onSelectAll handles.
        <ListStateContext.Provider value={listStateContextValue}>
          <EntityDataTable
            items={items}
            columns={tableColumns}
            filter={filter}
            setFilter={setFilter}
            listSelect={listSelect}
            visibilityKey={tableVisibilityKey ?? String(filterMode)}
            isPending={isLoading}
            totalCount={count}
            preserveScrollDuringRefill={preserveScrollDuringRefill}
            renderRow={renderTableRow}
          />
        </ListStateContext.Provider>
      ) : (
        <ListStateContext.Provider value={listStateContextValue}>
          <CardLayoutContext.Provider
            value={
              filter.displayMode === DisplayMode.Details
                ? "details"
                : filter.displayMode === DisplayMode.Wall
                  ? "wall"
                  : "grid"
            }
          >
            <CardAspectContext.Provider value={cardAspect}>
              <ZoomIndexContext.Provider value={filter.zoomIndex}>
                <MobileGridColsContext.Provider value={mobileGridCols}>
                  {/* data-selecting drives checkbox/overlay visibility via CSS — no card re-renders needed */}
                  <div data-selecting={listSelect.selecting || undefined}>
                    {filter.displayMode === DisplayMode.Wall ? (
                      <PhotoAlbumWall<TItem>
                        items={items}
                        isLoading={isLoading}
                        zoomIndex={filter.zoomIndex}
                        isMobile={isMobileSidebar}
                        selectedIds={listSelect.selectedIds}
                        onSelectChange={listSelect.onSelectChange}
                        onCardPreviewClick={onCardPreviewClick}
                        renderCard={renderCard}
                        getWallDimensions={getWallDimensions}
                        itemsPerPage={filter.itemsPerPage}
                      />
                    ) : (
                      // Row-virtualized: only viewport-visible rows mount their
                      // cards, so 40-item pages mount ~6–10 cards instead of
                      // all 40. Skeletons are rendered through the same path
                      // when the list is loading.
                      <VirtualizedItemList<TItem>
                        displayMode={filter.displayMode}
                        mobileGridCols={mobileGridCols}
                        zoomIndex={filter.zoomIndex}
                        isMobile={isMobileSidebar}
                        cardIsPortrait={cardIsPortrait}
                        isLoading={isLoading}
                        itemsPerPage={filter.itemsPerPage}
                        preserveScrollDuringRefill={preserveScrollDuringRefill}
                        items={items}
                        selectedIds={listSelect.selectedIds}
                        onSelectChange={listSelect.onSelectChange}
                        onCardPreviewClick={onCardPreviewClick}
                        renderCard={renderCard}
                      />
                    )}
                  </div>
                </MobileGridColsContext.Provider>
              </ZoomIndexContext.Provider>
            </CardAspectContext.Provider>
          </CardLayoutContext.Provider>
        </ListStateContext.Provider>
      )}
    </EntityList>
  );
}
