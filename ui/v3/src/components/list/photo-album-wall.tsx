import type React from "react";
import { useRef, useLayoutEffect, useMemo, useCallback } from "react";
import { RowsPhotoAlbum, type Photo } from "react-photo-album";
import "react-photo-album/rows.css";
import type { IHasID } from "@/utils/data";
import { Skeleton } from "@/components/ui/skeleton";

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

export function PhotoAlbumWall<TItem extends IHasID>({
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
        article
          .querySelector("[data-card-select]")
          ?.setAttribute("aria-pressed", String(selectedIds.has(id)));
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
