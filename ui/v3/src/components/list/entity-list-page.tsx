import React, {
  useContext,
  useState,
  useEffect,
  useLayoutEffect,
  useRef,
  useCallback,
  useMemo,
} from "react";
import { flushSync } from "react-dom";
import { RowsPhotoAlbum, type Photo } from "react-photo-album";
import "react-photo-album/rows.css";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useQuery } from "@apollo/client/react";
import { gql } from "graphql-tag";
import { useDebouncedValue } from "src/hooks/debounce";
import type { OperationVariables } from "@apollo/client";
import type { TypedDocumentNode } from "@graphql-typed-document-node/core";
import type { ColumnDef } from "@tanstack/react-table";
import type * as GQL from "src/core/generated-graphql";
import type { IHasID } from "src/utils/data";
import { cn } from "src/lib/utils";
import { ListFilterModel } from "src/models/list-filter/filter";
import { DisplayMode } from "src/models/list-filter/types";
import type { ISortByOption } from "src/models/list-filter/filter-options";
import type { View } from "src/components/list/views";
import { FilterBuilder } from "src/components/filters/filter-builder";
import { Skeleton } from "src/components/ui/skeleton";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
  EmptyMedia,
} from "src/components/ui/empty";
import { useIntl } from "react-intl";
import { SearchXIcon } from "lucide-react";
import { EntityList } from "./entity-list";
import { useFilterState } from "./use-filter-state";
import { useListSidebar } from "./use-list-sidebar";
import { useListSelect } from "./use-list-select";
import { useCachedQueryResult } from "./use-cached-query-result";
import { useListKeyboardShortcuts } from "./use-list-keyboard-shortcuts";
import { MobileListBar } from "./mobile-list-bar";
import { MobileGridColsContext } from "./mobile-grid-context";
import { CardLayoutContext } from "./card-layout-context";
import { ListScrollContext } from "./list-scroll-context";
import { CardAspectContext, type CardAspect } from "./card-aspect-context";
import { ZoomIndexContext } from "./zoom-index-context";
import { EntityDataTable } from "./entity-data-table";
import {
  ListStateContext,
  type ListContextState,
  type BulkApplyTarget,
} from "./list-provider";

// Sentinel query passed to `useQuery` when the page is sourcing data
// locally (`config.localSource`). The hook is always called — to keep
// hook order stable across modes — but skipped via `skip: true` so no
// network request fires.
const NOOP_QUERY = gql`
  query EntityListNoop {
    __typename
  }
`;

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

/**
 * Imperative handle for page navigation, populated by EntityListPage on a ref
 * provided through EntityListPageConfig.pageNavRef.
 */
export interface PageNavHandle {
  currentPage: number;
  totalPages: number;
  itemsPerPage: number;
  totalCount: number;
  nextPage: () => void;
  prevPage: () => void;
}

// Target row heights for wall mode by zoom index (0 = largest/tallest, 4 = smallest/shortest).
const WALL_TARGET_ROW_HEIGHTS = [500, 400, 300, 200, 150] as const;

// Varied aspect ratios cycled for skeleton wall photos so the layout looks natural.
const SKELETON_WALL_DIMS: [number, number][] = [
  [16, 9],
  [3, 4],
  [4, 3],
  [2, 3],
  [16, 10],
  [1, 1],
  [3, 5],
  [5, 3],
];

type WallPhoto = Photo & { id: string; itemIndex: number };

function SkeletonWallCard({
  width,
  height,
}: {
  width: number;
  height: number;
}) {
  return (
    <div
      className="relative overflow-hidden"
      style={{ width, height, touchAction: "pan-y" }}
    >
      <Skeleton className="absolute inset-0 rounded-none" />
      <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/60 via-black/30 to-transparent pt-6 pb-2 px-2 pointer-events-none flex flex-col gap-1">
        <Skeleton className="h-2.5 w-3/4 bg-white/25 rounded-sm" />
        <Skeleton className="h-2 w-1/2 bg-white/15 rounded-sm" />
      </div>
    </div>
  );
}

interface PhotoAlbumWallProps<TItem extends IHasID> {
  items: TItem[];
  isLoading: boolean;
  zoomIndex: number;
  isMobile: boolean;
  selectedIds: Set<string>;
  onSelectChange: (id: string, selected: boolean, shiftKey: boolean) => void;
  onCardPreviewClick?: (item: TItem, allItems: TItem[], index: number) => void;
  renderCard: (
    item: TItem,
    isMobile: boolean,
    selected: boolean,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    onPreviewClick?: () => void,
  ) => React.ReactNode;
  getWallDimensions?: (item: TItem) => { width: number; height: number };
  itemsPerPage: number;
}

function PhotoAlbumWall<TItem extends IHasID>({
  items,
  isLoading,
  zoomIndex,
  isMobile,
  selectedIds,
  onSelectChange,
  onCardPreviewClick,
  renderCard,
  getWallDimensions,
  itemsPerPage,
}: PhotoAlbumWallProps<TItem>) {
  // Keep a ref that always reflects the current selectedIds so renderPhoto doesn't
  // need selectedIds as a dep — eliminates full-wall re-renders on each selection change.
  const selectedIdsRef = useRef(selectedIds);
  selectedIdsRef.current = selectedIds;

  const containerRef = useRef<HTMLDivElement>(null);

  // Imperatively sync data-selected attributes on article elements whenever
  // selectedIds changes. CSS rules ([data-selected="true"] ...) handle the visual state.
  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    container
      .querySelectorAll<HTMLElement>("article[data-id]")
      .forEach((article) => {
        const id = article.dataset.id;
        if (!id) return;
        if (selectedIds.has(id)) {
          article.dataset.selected = "true";
        } else {
          delete article.dataset.selected;
        }
      });
  }, [selectedIds]);

  const photos = useMemo<WallPhoto[]>(() => {
    if (isLoading) {
      return Array.from({ length: Math.min(itemsPerPage, 40) }, (_, i) => {
        const [w, h] = SKELETON_WALL_DIMS[i % SKELETON_WALL_DIMS.length];
        return {
          src: "",
          width: w,
          height: h,
          id: `skeleton-${i}`,
          itemIndex: i,
        };
      });
    }
    return items.map((item, i) => {
      const dims = getWallDimensions
        ? getWallDimensions(item)
        : { width: 16, height: 9 };
      const w = dims.width > 0 ? dims.width : 16;
      const h = dims.height > 0 ? dims.height : 9;
      return { src: "", width: w, height: h, id: item.id, itemIndex: i };
    });
  }, [isLoading, items, getWallDimensions, itemsPerPage]);

  const targetRowHeight =
    WALL_TARGET_ROW_HEIGHTS[Math.max(0, Math.min(4, zoomIndex))];

  const renderPhoto = useCallback(
    (
      _props: { onClick?: React.MouseEventHandler },
      context: { photo: WallPhoto; width: number; height: number },
    ) => {
      const { photo, width, height } = context;
      if (isLoading) {
        return (
          <SkeletonWallCard key={photo.id} width={width} height={height} />
        );
      }
      const item = items[photo.itemIndex];
      if (!item) return null;
      // Use ref so this callback doesn't need selectedIds as a dep.
      const isSelected = selectedIdsRef.current.has(item.id);
      const onSelectedChanged = (selected: boolean, shiftKey: boolean) =>
        onSelectChange(item.id, selected, shiftKey);
      const onPreviewClick = onCardPreviewClick
        ? () => onCardPreviewClick(item, items, photo.itemIndex)
        : undefined;
      return (
        <div key={photo.id} style={{ width, height, touchAction: "pan-y" }}>
          {renderCard(
            item,
            isMobile,
            isSelected,
            onSelectedChanged,
            onPreviewClick,
          )}
        </div>
      );
    },
    // selectedIds omitted intentionally — selectedIdsRef.current is always current,
    // and data-selected is synced imperatively via useLayoutEffect above.
    [
      isLoading,
      items,
      onSelectChange,
      onCardPreviewClick,
      isMobile,
      renderCard,
    ],
  );

  return (
    <div ref={containerRef}>
      <RowsPhotoAlbum
        photos={photos}
        spacing={2}
        padding={0}
        targetRowHeight={targetRowHeight}
        rowConstraints={{ singleRowMaxHeight: targetRowHeight }}
        render={{ photo: renderPhoto }}
      />
    </div>
  );
}

// ── Skeleton card ─────────────────────────────────────────────────────────────

function SkeletonCard({
  isPortrait,
  isDetails,
}: {
  isPortrait?: boolean;
  isDetails?: boolean;
}) {
  return (
    <div
      className={cn(
        "entity-card relative overflow-hidden rounded-xl bg-card ring-1 ring-foreground/10 flex",
        isDetails ? "flex-row" : "flex-col",
      )}
    >
      <Skeleton
        className={cn(
          "shrink-0 rounded-none",
          isDetails
            ? "w-32 self-stretch"
            : cn("w-full", isPortrait ? "aspect-[2/3]" : "aspect-video"),
        )}
      />
      <div className="entity-card-body flex flex-col gap-2 px-3 py-2.5 flex-1">
        <Skeleton className="h-3.5 w-3/4" />
        <Skeleton className="h-3 w-1/2" />
      </div>
    </div>
  );
}

function useCardAspectPref(
  filterMode: string,
): [CardAspect, (a: CardAspect) => void] {
  const key = `list-card-aspect:${filterMode}`;
  const [aspect, setAspect] = useState<CardAspect>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw === "portrait" || raw === "landscape" || raw === "auto")
        return raw;
    } catch {
      // ignore
    }
    return "auto";
  });

  useEffect(() => {
    try {
      window.localStorage.setItem(key, aspect);
    } catch {
      // ignore
    }
  }, [key, aspect]);

  return [aspect, setAspect];
}

function useMobileGridColumns(
  filterMode: string,
): [1 | 2, (cols: 1 | 2) => void] {
  const key = `list-mobile-grid-cols:${filterMode}`;
  const [cols, setCols] = useState<1 | 2>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      return raw === "1" ? 1 : 2;
    } catch {
      return 2;
    }
  });

  useEffect(() => {
    try {
      window.localStorage.setItem(key, String(cols));
    } catch {
      // ignore
    }
  }, [key, cols]);

  return [cols, setCols];
}

// Per-view localStorage so each context (root scenes, performer
// scenes, tag scenes, …) tracks its own preference. The legacy
// per-filterMode keys are intentionally not migrated — at worst the
// user re-picks once.
function useZoomPref(scope: string): [number, (z: number) => void] {
  const key = `list-zoom:${scope}`;
  const [zoom, setZoomState] = useState<number>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw !== null) {
        const parsed = parseInt(raw, 10);
        if (!Number.isNaN(parsed)) return Math.max(0, Math.min(4, parsed));
      }
    } catch {
      // ignore
    }
    return 1;
  });

  const setZoom = useCallback(
    (z: number) => {
      setZoomState(z);
      try {
        window.localStorage.setItem(key, String(z));
      } catch {
        // ignore
      }
    },
    [key],
  );

  return [zoom, setZoom];
}

// Per-view localStorage — see `useZoomPref` above.
function useDisplayModePref(
  scope: string,
  options: DisplayMode[],
): [DisplayMode, (m: DisplayMode) => void] {
  const key = `list-display-mode:${scope}`;
  const [mode, setModeState] = useState<DisplayMode>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw !== null) {
        const parsed = parseInt(raw, 10) as DisplayMode;
        if (options.includes(parsed)) return parsed;
      }
    } catch {
      // ignore
    }
    return options[0];
  });

  const setMode = useCallback(
    (m: DisplayMode) => {
      setModeState(m);
      try {
        window.localStorage.setItem(key, String(m));
      } catch {
        // ignore
      }
    },
    [key],
  );

  return [mode, setMode];
}

interface MemoCardProps<TItem extends IHasID> {
  item: TItem;
  isMobile: boolean;
  isSelected: boolean;
  onSelectChange: (id: string, selected: boolean, shiftKey: boolean) => void;
  onCardPreviewClick?: (item: TItem, allItems: TItem[], index: number) => void;
  allItems: TItem[];
  index: number;
  renderCard: (
    item: TItem,
    isMobile: boolean,
    selected: boolean,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    onPreviewClick?: () => void,
  ) => React.ReactNode;
}

function MemoCardInner<TItem extends IHasID>({
  item,
  isMobile,
  isSelected,
  onSelectChange,
  onCardPreviewClick,
  allItems,
  index,
  renderCard,
}: MemoCardProps<TItem>) {
  const onSelectedChanged = useCallback(
    (selected: boolean, shiftKey: boolean) =>
      onSelectChange(item.id, selected, shiftKey),
    [item.id, onSelectChange],
  );
  const onPreviewClick = useMemo(
    () =>
      onCardPreviewClick
        ? () => onCardPreviewClick(item, allItems, index)
        : undefined,
    [onCardPreviewClick, item, index, allItems],
  );
  return (
    <>
      {renderCard(
        item,
        isMobile,
        isSelected,
        onSelectedChanged,
        onPreviewClick,
      )}
    </>
  );
}
// React.memo erases the generic; reassert it via a casted function signature.
const MemoCard = React.memo(MemoCardInner) as <TItem extends IHasID>(
  props: MemoCardProps<TItem>,
) => React.ReactElement;

// ── VirtualizedItemList ───────────────────────────────────────────────────────
// Row-level virtualizer for grid + details modes. Only the rows whose Y range
// overlaps the scroll viewport (plus a small overscan) mount their cards, so
// a 40-item page mounts ~6–10 cards instead of all 40. This both speeds up
// initial reconcile (back-nav from detail pages no longer waits for 40 cards
// to mount) and keeps interactions smooth as the user scrolls.
//
// Wall mode is excluded — `PhotoAlbumWall` already does its own justified-row
// virtualization via `react-photo-album`.

// Min card width for grid mode (auto-fill semantics) per zoom index.
const GRID_MIN_PX = [420, 320, 240, 180, 140] as const;

interface VirtualizedItemListProps<TItem extends IHasID> {
  displayMode: DisplayMode;
  mobileGridCols: 1 | 2;
  zoomIndex: number;
  isMobile: boolean;
  cardIsPortrait?: boolean;
  isLoading: boolean;
  itemsPerPage: number;
  items: TItem[];
  selectedIds: Set<string>;
  onSelectChange: (id: string, selected: boolean, shiftKey: boolean) => void;
  onCardPreviewClick?: (item: TItem, allItems: TItem[], index: number) => void;
  renderCard: (
    item: TItem,
    isMobile: boolean,
    selected: boolean,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    onPreviewClick?: () => void,
  ) => React.ReactNode;
}

function VirtualizedItemList<TItem extends IHasID>({
  displayMode,
  mobileGridCols,
  zoomIndex,
  isMobile,
  cardIsPortrait,
  isLoading,
  itemsPerPage,
  items,
  selectedIds,
  onSelectChange,
  onCardPreviewClick,
  renderCard,
}: VirtualizedItemListProps<TItem>) {
  // EntityList provides the scroll element directly (callback-ref + state).
  // Null on the first commit, populated on the second; the virtualizer
  // computes 0 rows on the first commit and the actual rows on the second.
  const scrollEl = useContext(ListScrollContext);
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(0);

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const sync = () => {
      const w = el.getBoundingClientRect().width;
      if (w > 0) setContainerWidth(w);
    };
    sync();
    const ro = new ResizeObserver(sync);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const isDetails = displayMode === DisplayMode.Details;
  // Tailwind `gap-2 p-2 md:gap-4 md:p-4`. `md` breakpoint is 768px; we proxy
  // via `isMobile` (the sidebar's narrow-screen flag) since `md` ≈ "not narrow".
  const gap = isMobile ? 8 : 16;
  const pad = isMobile ? 8 : 16;

  const lanes = useMemo(() => {
    if (isDetails) return 1;
    if (isMobile) return mobileGridCols;
    if (containerWidth === 0) return 1;
    const inner = Math.max(0, containerWidth - pad * 2);
    const minPx = GRID_MIN_PX[Math.max(0, Math.min(4, zoomIndex))];
    // CSS grid auto-fill formula: floor((inner + gap) / (minPx + gap))
    return Math.max(1, Math.floor((inner + gap) / (minPx + gap)));
  }, [
    isDetails,
    isMobile,
    mobileGridCols,
    containerWidth,
    zoomIndex,
    gap,
    pad,
  ]);

  const total = isLoading ? Math.min(itemsPerPage, 40) : items.length;
  const rowCount = lanes > 0 ? Math.ceil(total / lanes) : 0;

  // Row-height estimate: card image aspect × column width + ~80px body.
  // Details cards are a fixed-height flex-row layout.
  const estimateSize = useCallback(() => {
    if (isDetails) return 96;
    if (containerWidth === 0 || lanes === 0) return 280;
    const inner = Math.max(0, containerWidth - pad * 2);
    const colWidth = (inner - gap * (lanes - 1)) / lanes;
    const aspectH = cardIsPortrait ? colWidth * 1.5 : colWidth * 0.5625;
    return Math.round(aspectH + 80) + gap;
  }, [isDetails, containerWidth, lanes, gap, pad, cardIsPortrait]);

  const virtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => scrollEl,
    estimateSize,
    overscan: 2,
    measureElement:
      typeof ResizeObserver !== "undefined"
        ? (el) => el.getBoundingClientRect().height
        : undefined,
  });

  const totalSize = virtualizer.getTotalSize();
  const virtualRows = virtualizer.getVirtualItems();

  return (
    <div
      ref={containerRef}
      style={{ paddingLeft: pad, paddingRight: pad, paddingTop: pad }}
    >
      {/* Spacer establishes the scrollable height; rows are absolute-positioned within. */}
      <div
        style={{
          position: "relative",
          height: totalSize + pad,
          width: "100%",
        }}
      >
        {virtualRows.map((vRow) => {
          const rowStart = vRow.index * lanes;
          const rowEnd = Math.min(rowStart + lanes, total);
          return (
            <div
              key={vRow.key}
              data-index={vRow.index}
              ref={virtualizer.measureElement}
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                transform: `translateY(${vRow.start}px)`,
                paddingBottom: gap,
                display: isDetails ? "flex" : "grid",
                flexDirection: isDetails ? "column" : undefined,
                gridTemplateColumns: !isDetails
                  ? `repeat(${lanes}, minmax(0, 1fr))`
                  : undefined,
                columnGap: !isDetails ? gap : undefined,
              }}
            >
              {Array.from({ length: rowEnd - rowStart }).map((_, i) => {
                const idx = rowStart + i;
                if (isLoading) {
                  return (
                    <SkeletonCard
                      key={idx}
                      isPortrait={cardIsPortrait}
                      isDetails={isDetails}
                    />
                  );
                }
                const item = items[idx];
                if (!item) return null;
                return (
                  <MemoCard<TItem>
                    key={item.id}
                    item={item}
                    isMobile={isMobile}
                    isSelected={selectedIds.has(item.id)}
                    onSelectChange={onSelectChange}
                    onCardPreviewClick={onCardPreviewClick}
                    allItems={items}
                    index={idx}
                    renderCard={renderCard}
                  />
                );
              })}
            </div>
          );
        })}
      </div>
    </div>
  );
}

export interface LocalDataSource<TItem extends IHasID> {
  /** Raw items prior to filter / sort / pagination. */
  items: TItem[];
  /**
   * Project the raw items down to the current page using the live
   * `ListFilterModel`. Implementations apply search, sort, and
   * pagination locally — the chrome calls this once per debounced
   * filter change.
   */
  filter: (
    items: TItem[],
    filter: ListFilterModel,
  ) => { count: number; items: TItem[] };
  /** Optional loading flag (e.g. while the initial IDB scan settles). */
  loading?: boolean;
}

export interface EntityListPageConfig<TData, TItem extends IHasID> {
  filterMode: GQL.FilterMode;
  view?: View;
  defaultSort?: string;
  /**
   * GraphQL data source. Required unless `localSource` is provided.
   * The list page calls `useQuery(query, makeVariables(filter))`,
   * caches the result via `useCachedQueryResult`, and pulls the page
   * slice out of it via `extractResult`.
   */
  query?: TypedDocumentNode<TData, OperationVariables>;
  makeVariables?: (filter: ListFilterModel) => OperationVariables;
  extractResult?: (data: TData | undefined) => {
    count: number;
    items: TItem[];
  };
  /**
   * Local in-memory data source. When provided, supersedes the
   * GraphQL query path: the page applies filter / sort / pagination
   * by calling `localSource.filter(...)` against the supplied items.
   * Used by the offline view to render an entity-list UI over IDB-
   * snapshotted scenes.
   */
  localSource?: LocalDataSource<TItem>;
  /**
   * Sidebar override. When set, replaces the default GraphQL
   * `FilterBuilder`. The offline view passes a custom sidebar with
   * the small subset of filters that apply to local data (status,
   * studio, performers, tags) instead of the full criterion set.
   */
  sidebarContent?: React.ReactNode;
  /**
   * Sort dropdown override. When set, the toolbar shows these sort
   * options instead of `ListFilterModel.options.sortByOptions`.
   * Used by the offline list to expose only the sort keys it can
   * actually compute locally (downloaded_at, title, date, duration,
   * filesize, resolution).
   */
  sortOptions?: ISortByOption[];
  /**
   * Override the empty-state UI shown when the data source returns
   * zero items. Defaults to a generic "No results" `<Empty>` block.
   * Pages with a meaningfully different empty case (e.g. offline:
   * "no scenes downloaded yet") supply their own.
   */
  emptyState?: React.ReactNode;
  renderCard: (
    item: TItem,
    isMobile: boolean,
    selected: boolean,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
    onPreviewClick?: () => void,
  ) => React.ReactNode;
  /** Called when the preview area of a card is clicked/tapped. Receives the
   *  clicked item, all currently-displayed items, and the item's index. */
  onCardPreviewClick?: (item: TItem, allItems: TItem[], index: number) => void;
  zoomable?: boolean;
  /** When true, skeleton cards use portrait (2:3) aspect ratio instead of landscape (16:9). */
  cardIsPortrait?: boolean;
  /** Column definitions for the Table display mode. When provided, Table mode is available. */
  tableColumns?: ColumnDef<TItem>[];
  /** localStorage key for persisting column visibility (e.g. "scenes"). Defaults to filterMode. */
  tableVisibilityKey?: string;
  /**
   * Optional per-row wrapper for Table display mode. Receives the item, the
   * default `<TableRow>` JSX, and a per-row select callback (so the wrapped
   * context menu's "Select" item can put the user into select mode without
   * the table having to render the checkbox column up-front). Returns a
   * wrapped node — typically a `ContextMenu` matching the card view.
   */
  renderTableRow?: (
    item: TItem,
    defaultRow: React.ReactElement,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
  ) => React.ReactNode;
  /**
   * Show a portrait/landscape/auto aspect-ratio toggle in grid mode.
   * Defaults to `true`. Set to `false` to suppress the toggle for a specific
   * entity list.
   */
  supportsCardAspect?: boolean;
  /**
   * When provided, a tagger mode button appears in the mobile bar and tagger
   * panel is shown when active. Receives the current page's items.
   */
  renderTagger?: (items: TItem[]) => React.ReactNode;
  /**
   * Optional ref that EntityListPage will populate with page-navigation helpers.
   * Use this to advance pages imperatively (e.g. from a lightbox).
   */
  pageNavRef?: { current: PageNavHandle | null };
  /**
   * Called whenever a new batch of card items arrives from the server
   * (fires on every data change except the initial load). Use this together
   * with pageNavRef to update a lightbox's slides after a page advance.
   */
  onItemsChanged?: (items: TItem[]) => void;
  /**
   * Returns the natural pixel dimensions of an item for wall layout.
   * Used by react-photo-album to compute justified row heights.
   * When omitted, wall mode falls back to 16:9.
   */
  getWallDimensions?: (item: TItem) => { width: number; height: number };
  /**
   * Page-level action(s) rendered at the right end of the desktop chrome
   * bar (e.g. a "+ New" button). Hidden in selection mode.
   */
  pageActions?: React.ReactNode;
}

interface EntityListPageProps<TData, TItem extends IHasID> {
  config: EntityListPageConfig<TData, TItem>;
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

export function EntityListPage<TData, TItem extends IHasID>({
  config,
  useURL,
  defaultFilter,
  mobileChromeFixed,
  keyboardShortcutsDisabled,
  view: viewProp,
}: EntityListPageProps<TData, TItem>) {
  const {
    filterMode,
    view: configView,
    defaultSort,
    query,
    makeVariables,
    extractResult,
    localSource,
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

  // Display mode + zoom are UI preferences — not part of the URL or
  // filter predicate. Scoped per `view` (root-scenes vs
  // performer-scenes vs tag-scenes …) so each context can keep its own
  // layout. Falls back to `filterMode` when no view is supplied.
  const prefScope = view ?? filterMode;
  const emptyFilterForOptions = new ListFilterModel(filterMode);
  const [displayModePref, setDisplayModePref] = useDisplayModePref(
    prefScope,
    emptyFilterForOptions.options.displayModeOptions,
  );

  const [zoomPref, setZoomPref] = useZoomPref(prefScope);
  // `zoomPref` is React state, so it lags behind by a render. Rapid
  // clicks fire before the previous render has committed, so each
  // handler reads the same (stale) state and computes the same target.
  // `intendedZoomRef` mirrors the latest *intended* zoom, updated
  // synchronously inside `setFilter`, so back-to-back clicks compound
  // correctly. The View Transition path also defers state updates by
  // a frame, which makes this even more important there.
  const intendedZoomRef = useRef(zoomPref);
  useEffect(() => {
    intendedZoomRef.current = zoomPref;
  }, [zoomPref]);

  const { filter: rawFilter, setFilter: rawSetFilter } = useFilterState({
    filterMode,
    view,
    defaultSort,
    useURL,
    defaultFilter,
    defaultDisplayMode: displayModePref,
  });

  // Apply display mode and zoom prefs into the live filter without going through URL.
  // Must be memoized — creating a new object every render would make useDebouncedValue
  // never settle, keeping isLoading permanently true.
  const filter = useMemo(() => {
    let f =
      rawFilter.displayMode === displayModePref
        ? rawFilter
        : rawFilter.setDisplayMode(displayModePref);
    if (f.zoomIndex !== zoomPref) {
      f = f.setZoom(zoomPref);
    }
    return f;
  }, [rawFilter, displayModePref, zoomPref]);

  // Wrap setFilter to intercept display mode / zoom changes and persist them.
  // Use `filter` (the memoized value with prefs applied) as the base for
  // functional updaters — callers always have `filter` in scope, not rawFilter.
  // Only call rawSetFilter when URL-relevant parameters actually change; zoom
  // and displayMode are UI-only prefs that don't belong in the URL, and routing
  // through rawSetFilter for those would trigger router.history.replace
  // unnecessarily, adding latency to instant UI actions like zoom.
  //
  // Zoom changes go through `document.startViewTransition` on supporting
  // browsers — without naming any elements, so the browser snapshots the
  // page before and after as two whole-page bitmaps and crossfades between
  // them on the GPU. That's a single composited animation regardless of
  // how many cards are on screen, so it stays smooth even on dense grids.
  // The trade-off: cards don't visibly morph from old position to new —
  // they just dissolve through. `flushSync` ensures the new lane count is
  // committed inside the transition callback so the "after" snapshot is
  // taken with the new layout.
  const setFilter = useCallback(
    (f: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel)) => {
      // For functional updaters: substitute the latest intended zoom into
      // `prev` so callers like the toolbar's zoomIn/zoomOut compute their
      // target from the most recent click rather than the stale render.
      const baseFilter =
        filter.zoomIndex !== intendedZoomRef.current
          ? filter.setZoom(intendedZoomRef.current)
          : filter;
      const next = typeof f === "function" ? f(baseFilter) : f;
      if (next.displayMode !== displayModePref) {
        setDisplayModePref(next.displayMode);
      }
      const zoomChanged = next.zoomIndex !== intendedZoomRef.current;
      const urlDiffers =
        next.makeQueryParameters() !== rawFilter.makeQueryParameters();
      const applyNonZoom = () => {
        if (urlDiffers) rawSetFilter(next);
      };

      if (zoomChanged) {
        // Update intended zoom synchronously so rapid follow-up clicks
        // see the new target before React commits.
        intendedZoomRef.current = next.zoomIndex;
        // Skip View Transitions on mobile: the settings drawer's
        // swipe-to-dismiss tracking races with the VT snapshot and
        // rapid taps inside the drawer end up closing it. Snap zoom
        // changes there instead — the screen is small enough that
        // the crossfade adds little visible value.
        const isMobile =
          typeof window !== "undefined" &&
          window.matchMedia("(max-width: 767px)").matches;
        const canVT =
          typeof document !== "undefined" &&
          typeof document.startViewTransition === "function" &&
          !isMobile;
        if (canVT) {
          // Each rapid click starts its own VT. `startViewTransition`
          // skips any in-flight transition and starts fresh with the
          // current state as "old"; per spec the skipped transition's
          // update callback still runs, so every intermediate
          // `setZoomPref` commits in order. Visually the user sees a
          // single crossfade from the initial state to the final
          // zoom level — the intermediate stops are skipped over,
          // which matches what they're asking for when mashing the
          // button.
          document.startViewTransition(() => {
            flushSync(() => {
              setZoomPref(next.zoomIndex);
              applyNonZoom();
            });
          });
          return;
        }
        setZoomPref(next.zoomIndex);
      }
      applyNonZoom();
    },
    [
      rawFilter,
      filter,
      rawSetFilter,
      displayModePref,
      setDisplayModePref,
      setZoomPref,
    ],
  );

  const [mobileGridCols, setMobileGridCols] = useMobileGridColumns(filterMode);
  const [cardAspect, setCardAspect] = useCardAspectPref(filterMode);
  const [taggerActive, setTaggerActive] = useState(false);
  const sidebarState = useListSidebar(view);
  const [currentSavedFilterName, setCurrentSavedFilterName] = useState<
    string | undefined
  >();
  const { isMobileSidebar, openFilterSidebar } = sidebarState;

  // Debounce query variables so rapid page/sort changes don't each fire a
  // separate request. The spinner appears immediately (via isPending) while
  // the debounce is settling.
  const debouncedFilter = useDebouncedValue(filter, 150);

  // GraphQL path. When `localSource` is in play we still call useQuery
  // (with `skip: true` and a noop document) to keep the hook order
  // stable across modes — the result is ignored.
  const rawResult = useQuery(query ?? NOOP_QUERY, {
    variables:
      query && makeVariables ? makeVariables(debouncedFilter) : undefined,
    skip: !query,
  });
  const result = useCachedQueryResult(debouncedFilter, rawResult);

  // True whenever the *query* variables (or local-filter inputs) will
  // change vs the debounced state. Zoom and displayMode are UI-only —
  // they don't affect makeVariables() output, so we compare serialized
  // variables rather than filter object identity to avoid a spurious
  // loading flash when those UI-only prefs change.
  const queryVarsChanged = useMemo(() => {
    if (localSource) {
      // Local source: the only inputs that drive a re-page are the
      // serialised query parameters (search / sort / page / perPage).
      // Filter object identity changes on every render via the prefs
      // wrapper, but the serialised params don't, so this stays stable.
      return (
        filter.makeQueryParameters() !== debouncedFilter.makeQueryParameters()
      );
    }
    if (!makeVariables) return false;
    return (
      JSON.stringify(makeVariables(filter)) !==
      JSON.stringify(makeVariables(debouncedFilter))
    );
  }, [localSource, makeVariables, filter, debouncedFilter]);

  // Items used for card rendering, table rendering, and selection.
  let count: number;
  let items: TItem[];
  let isLoading: boolean;
  if (localSource) {
    const sliced = localSource.filter(localSource.items, debouncedFilter);
    count = sliced.count;
    items = sliced.items;
    isLoading = !!localSource.loading || queryVarsChanged;
  } else {
    const extracted = extractResult
      ? extractResult(result.data as TData | undefined)
      : { count: 0, items: [] as TItem[] };
    count = extracted.count;
    items = extracted.items;
    isLoading = queryVarsChanged || result.isPending || result.loading;
  }

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

  // After a delete (or any cache modification that drops items from the
  // current page), Apollo's cache update leaves the page short — items
  // were spliced out without re-querying, so a page that used to hold
  // perPage items now holds fewer. Detect that gap by comparing the
  // page's actual item count to what it should hold given the current
  // total, and refetch to pull replacements up from the next page.
  // Also handle the case where deletes pushed the user past the last
  // page (e.g. cleared an entire trailing page) by snapping back to the
  // new last page. Skipped for the local-source path since IDB-backed
  // lists slice their items locally and don't have a "page beyond what
  // we've fetched" notion.
  const { refetch } = rawResult;
  useEffect(() => {
    if (isLoading || localSource || !query) return;

    const totalPages = Math.max(1, Math.ceil(count / filter.itemsPerPage));

    if (filter.currentPage > totalPages) {
      setFilter((f) => f.changePage(totalPages));
      return;
    }

    const isLastPage = filter.currentPage === totalPages;
    const expected = isLastPage
      ? Math.max(0, count - (filter.currentPage - 1) * filter.itemsPerPage)
      : filter.itemsPerPage;

    if (items.length < expected) {
      void refetch();
    }
  }, [
    items.length,
    count,
    filter,
    isLoading,
    localSource,
    query,
    refetch,
    setFilter,
  ]);

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
    disabled: keyboardShortcutsDisabled,
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
      {taggerActive && renderTagger ? (
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
