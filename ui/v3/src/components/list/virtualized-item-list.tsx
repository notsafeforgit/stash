import React, {
  useContext,
  useRef,
  useState,
  useLayoutEffect,
  useMemo,
  useCallback,
} from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { IHasID } from "@/utils/data";
import { cn } from "@/lib/utils";
import { DisplayMode } from "@/models/list-filter/types";
import { Skeleton } from "@/components/ui/skeleton";
import { ListScrollContext } from "./list-scroll-context";
import { shouldAdjustVirtualizedListScrollPosition } from "./list-scroll-state";

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
  preserveScrollDuringRefill: boolean;
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

export function VirtualizedItemList<TItem extends IHasID>({
  displayMode,
  mobileGridCols,
  zoomIndex,
  isMobile,
  cardIsPortrait,
  isLoading,
  itemsPerPage,
  preserveScrollDuringRefill,
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
  virtualizer.shouldAdjustScrollPositionOnItemSizeChange = (
    item,
    _delta,
    instance,
  ) =>
    shouldAdjustVirtualizedListScrollPosition(
      item.start,
      instance.scrollOffset ?? 0,
      instance.isScrolling,
      preserveScrollDuringRefill,
    );

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
