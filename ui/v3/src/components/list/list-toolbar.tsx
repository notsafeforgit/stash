import type React from "react";
import { useCallback, useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { cn } from "src/lib/utils";
import {
  LayoutGrid,
  Image,
  LayoutList,
  Table2,
  X,
  Check,
  RectangleVertical,
  RectangleHorizontal,
  Proportions,
} from "lucide-react";
import { getSortDirectionIcon } from "./sort-icon";
import { Button } from "src/components/ui/button";
import {
  PinButton,
  PinnableComboBox,
} from "src/components/ui/pinnable-combo-box";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { DisplayMode } from "src/models/list-filter/types";
import type { ISortByOption } from "src/models/list-filter/filter-options";
import { SortDirectionEnum } from "src/core/generated-graphql";
import type { IListSelect } from "./use-list-select";
import type { CardAspect } from "./card-aspect-context";
import {
  TableToolbarSlot,
  useDeclareTableToolbarProvider,
} from "./table-toolbar-slot";

// ── Page sizes ────────────────────────────────────────────────────────────────

const PAGE_SIZES = [20, 40, 60, 100, 250, 500];

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

// ── View mode icons ───────────────────────────────────────────────────────────

const DisplayModeIcon: React.FC<{ mode: DisplayMode }> = ({ mode }) => {
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
};

function displayModeLabel(mode: DisplayMode): string {
  switch (mode) {
    case DisplayMode.Grid:
      return "Grid";
    case DisplayMode.Wall:
      return "Wall";
    case DisplayMode.Tagger:
      return "Tagger";
    case DisplayMode.Details:
      return "Details";
    case DisplayMode.Table:
      return "Table";
    default:
      return "";
  }
}

// ── Selection section ─────────────────────────────────────────────────────────

const SelectionSection: React.FC<{
  selectedCount: number;
  onSelectAll: () => void;
  onSelectNone: () => void;
}> = ({ selectedCount, onSelectAll, onSelectNone }) => {
  const intl = useIntl();

  return (
    <div className="flex items-center gap-1">
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onSelectNone}
        title={intl.formatMessage({ id: "actions.select_none" })}
      >
        <X size={14} />
      </Button>
      <span className="text-sm text-muted-foreground tabular-nums">
        {selectedCount}
      </span>
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onSelectAll}
        title={intl.formatMessage({ id: "actions.select_all" })}
      >
        <Check size={14} />
      </Button>
    </div>
  );
};

// ── ListToolbar ───────────────────────────────────────────────────────────────

export interface ListToolbarProps {
  filter: ListFilterModel;
  setFilter: (
    f: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel),
  ) => void;
  listSelect: IListSelect;
  onEdit?: () => void;
  onDelete?: () => void;
  /** Rendered in the operations slot (replaces default edit/delete buttons). */
  operationComponent?: React.ReactNode;
  /** Show zoom +/- buttons when in Grid mode. */
  zoomable?: boolean;
  /** When provided, shows a portrait/landscape/auto aspect-ratio toggle in Grid mode. */
  cardAspect?: CardAspect;
  setCardAspect?: (a: CardAspect) => void;
  /**
   * Override for the sort dropdown. When provided, the toolbar shows
   * these options instead of `filter.options.sortByOptions`. Used by
   * the offline list to expose only the sort keys it can actually
   * compute locally.
   */
  sortOptions?: ISortByOption[];
}

export const ListToolbar: React.FC<ListToolbarProps> = ({
  filter,
  setFilter,
  listSelect,
  operationComponent,
  zoomable = false,
  cardAspect,
  setCardAspect,
  sortOptions: sortOptionsOverride,
}) => {
  const intl = useIntl();
  const { selectedIds, onSelectAll, onSelectNone } = listSelect;
  const hasSelection = selectedIds.size > 0;
  const { displayModeOptions } = filter.options;
  const sortByOptions = sortOptionsOverride ?? filter.options.sortByOptions;
  const { pinnedValues: pinnedSortValues, togglePinned: toggleSortPin } =
    usePinnedSortOptions(filter.mode);

  const sortOptions = sortByOptions.map((opt) => ({
    value: opt.value,
    label: intl.formatMessage({ id: opt.messageID, defaultMessage: opt.value }),
  }));
  const currentSortLabel =
    sortOptions.find((o) => o.value === (filter.sortBy ?? ""))?.label ?? "";

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

  // zoomIndex 0 = fewest columns (largest cards = most zoomed in)
  // zoomIndex 4 = most columns (smallest cards = most zoomed out)
  // So "zoom in" (+) means decrease the index; "zoom out" (-) means increase it.
  //
  // Functional updaters are required for rapid clicks: View Transitions
  // defers its callback by ~1 frame, so the React-rendered `filter` may
  // not have caught up by the time the next click fires. The functional
  // form lets `setFilter` substitute the latest intended zoom (tracked
  // in a ref) before computing the next value.
  function zoomIn() {
    setFilter((prev) => prev.setZoom(Math.max(0, prev.zoomIndex - 1)));
  }

  function zoomOut() {
    setFilter((prev) => prev.setZoom(Math.min(4, prev.zoomIndex + 1)));
  }

  const showZoom =
    zoomable &&
    (filter.displayMode === DisplayMode.Grid ||
      filter.displayMode === DisplayMode.Wall);

  // Tell EntityDataTable that this toolbar will host the column-manager slot
  // whenever the user is in Table mode — suppresses the inline fallback even
  // before the slot div has its DOM ref.
  useDeclareTableToolbarProvider(filter.displayMode === DisplayMode.Table);

  return (
    <div className="flex items-center gap-1.5 flex-wrap">
      {hasSelection ? (
        <SelectionSection
          selectedCount={selectedIds.size}
          onSelectAll={onSelectAll}
          onSelectNone={onSelectNone}
        />
      ) : (
        <>
          {/* Sort selector — hidden in Table mode (column headers handle sort) */}
          {sortByOptions.length > 0 &&
            filter.displayMode !== DisplayMode.Table && (
              <div className="flex items-center gap-1">
                <PinnableComboBox
                  currentLabel={currentSortLabel}
                  options={sortOptions}
                  selectedValue={filter.sortBy ?? ""}
                  triggerClassName="h-7 text-xs"
                  pinnedValues={pinnedSortValues}
                  pinnedSectionLabel="Pinned"
                  onSelect={setSortBy}
                  renderItemAddon={(value, isPinned) => (
                    <PinButton
                      pinned={isPinned}
                      pinnedTitle="Unpin sort"
                      unpinnedTitle="Pin sort"
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
                      variant="ghost"
                      size="icon-sm"
                      onClick={toggleSortDirection}
                      title={
                        filter.sortDirection === SortDirectionEnum.Asc
                          ? "Ascending"
                          : "Descending"
                      }
                    >
                      <SortDirIcon size={13} />
                    </Button>
                  );
                })()}
              </div>
            )}

          {/* Page size selector — fixed width so the trigger only ever needs
              to fit the widest option ("100"). */}
          <Select
            value={String(filter.itemsPerPage)}
            onValueChange={(v) => setPageSize(Number(v))}
          >
            <SelectTrigger
              size="sm"
              className="w-[4.25rem] px-2 text-xs tabular-nums"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {PAGE_SIZES.map((n) => (
                <SelectItem key={n} value={String(n)}>
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* View mode toggle */}
          {displayModeOptions.length > 1 && (
            <div className="flex items-center gap-0.5">
              {displayModeOptions.map((mode) => (
                <Button
                  key={mode}
                  variant={filter.displayMode === mode ? "secondary" : "ghost"}
                  size="icon-sm"
                  onClick={() => setDisplayMode(mode)}
                  title={displayModeLabel(mode)}
                >
                  <DisplayModeIcon mode={mode} />
                </Button>
              ))}
            </div>
          )}

          {/* Table-mode column-manager — EntityDataTable portals its
              "Columns" Sheet trigger into this slot so the button sits
              inline with the chrome instead of consuming a row above the
              table. */}
          {filter.displayMode === DisplayMode.Table && (
            <TableToolbarSlot className="flex items-center" />
          )}

          {/* Zoom controls — + zooms in (fewer larger cards), − zooms out (more smaller cards) */}
          {showZoom && (
            <div className="flex items-center gap-0.5">
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={zoomOut}
                disabled={filter.zoomIndex >= 4}
                title={intl.formatMessage({
                  id: "zoom_out",
                  defaultMessage: "Zoom out",
                })}
              >
                −
              </Button>
              {/* 5 dots: leftmost = most zoomed in (index 0), rightmost = most zoomed out (index 4) */}
              <div
                className="flex items-center gap-[3px] px-1 select-none"
                aria-hidden
              >
                {[0, 1, 2, 3, 4].map((i) => (
                  // Fixed w-2/h-2 keeps every dot occupying the same
                  // box; we differentiate active vs inactive via
                  // `scale` (a transform — doesn't affect layout) and
                  // background colour. Without this, animating `width`
                  // during rapid zoom clicks shifts all the neighbours
                  // mid-transition and the +/− buttons jiggle.
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
                onClick={zoomIn}
                disabled={filter.zoomIndex <= 0}
                title={intl.formatMessage({
                  id: "zoom_in",
                  defaultMessage: "Zoom in",
                })}
              >
                +
              </Button>
            </div>
          )}

          {/* Aspect ratio toggle (Grid mode + supported entities only) */}
          {filter.displayMode === DisplayMode.Grid &&
            cardAspect !== undefined &&
            setCardAspect && (
              <div className="flex items-center gap-0.5">
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
            )}
        </>
      )}

      {/* Operations slot — shown in both selection and normal modes */}
      {operationComponent && (
        <div className="flex items-center gap-1">{operationComponent}</div>
      )}
    </div>
  );
};
