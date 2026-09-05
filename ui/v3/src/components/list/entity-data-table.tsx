import React, { useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { useIntl } from "react-intl";

import {
  type Column,
  type ColumnDef,
  type ColumnFiltersState,
  type ColumnOrderState,
  type OnChangeFn,
  type RowData,
  type RowSelectionState,
  type SortingState,
  type VisibilityState,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";

// Extend TanStack Table's column meta type so `meta.label` is typed.
// TypeScript's declaration-merging rule requires the augmented interface
// to restate the original type parameters verbatim (TData, TValue).
// `_typeAnchor` is a never-set phantom field whose only purpose is to
// reference those parameters so ESLint's no-unused-vars stays quiet.
declare module "@tanstack/react-table" {
  interface ColumnMeta<TData extends RowData, TValue> {
    label?: string;
    readonly _typeAnchor?: readonly [TData, TValue];
  }
}
import { GripVertical, Settings2 } from "lucide-react";
import { SearchXIcon } from "lucide-react";
import {
  DndContext,
  type DragEndEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { IHasID } from "src/utils/data";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { SortDirectionEnum } from "src/core/generated-graphql";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "src/components/ui/table";
import { Button } from "src/components/ui/button";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import { Checkbox } from "src/components/ui/checkbox";
import { Skeleton } from "src/components/ui/skeleton";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "src/components/ui/sheet";
import type { IListSelect } from "./use-list-select";
import {
  useTableToolbarHasProvider,
  useTableToolbarSlotEl,
} from "./table-toolbar-slot";
import {
  useListPageChangeScrollPosition,
  usePreservedListScrollPosition,
} from "./list-scroll-state";

// ── Column visibility persistence ─────────────────────────────────────────────

function loadColumnVisibility(key: string): VisibilityState {
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return {};
    return JSON.parse(raw) as VisibilityState;
  } catch {
    return {};
  }
}

function saveColumnVisibility(key: string, state: VisibilityState) {
  try {
    window.localStorage.setItem(key, JSON.stringify(state));
  } catch {
    // ignore
  }
}

// ── Column order persistence ───────────────────────────────────────────────────

function loadColumnOrder(key: string): ColumnOrderState {
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return [];
    return JSON.parse(raw) as ColumnOrderState;
  } catch {
    return [];
  }
}

function saveColumnOrder(key: string, order: ColumnOrderState) {
  try {
    window.localStorage.setItem(key, JSON.stringify(order));
  } catch {
    // ignore
  }
}

// ── Selection checkbox column ─────────────────────────────────────────────────

export function selectionColumn<T extends IHasID>(): ColumnDef<T> {
  return {
    id: "select",
    header: ({ table }) => (
      <Checkbox
        checked={table.getIsAllPageRowsSelected() || undefined}
        indeterminate={table.getIsSomePageRowsSelected()}
        onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
        aria-label="Select all"
      />
    ),
    cell: ({ row }) => (
      <Checkbox
        checked={row.getIsSelected()}
        onCheckedChange={(value) => row.toggleSelected(!!value)}
        onClick={(e) => e.stopPropagation()}
        aria-label="Select row"
      />
    ),
    enableSorting: false,
    enableHiding: false,
    size: 40,
    minSize: 40,
    maxSize: 40,
  };
}

// ── Sheet-based column manager (visibility + drag-to-reorder) ─────────────────

function SortableColumnRow<TData>({
  column,
}: {
  column: Column<TData, unknown>;
}) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: column.id });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  };

  const label =
    column.columnDef.meta?.label ??
    (typeof column.columnDef.header === "string"
      ? column.columnDef.header
      : column.id);

  return (
    <div
      ref={setNodeRef}
      style={style}
      className="flex items-center gap-2 bg-card border border-border rounded-md px-2 py-2"
    >
      <Button
        variant="ghost"
        size="icon-sm"
        className="text-muted-foreground hover:text-foreground cursor-grab active:cursor-grabbing shrink-0 hover:bg-transparent"
        {...attributes}
        {...listeners}
        aria-label="Drag to reorder"
      >
        <GripVertical size={16} />
      </Button>
      <span className="flex-1 text-sm truncate">{label}</span>
      <Checkbox
        checked={column.getIsVisible()}
        onCheckedChange={(v) => column.toggleVisibility(!!v)}
      />
    </div>
  );
}

function SheetColumnManager<TData>({
  table,
}: {
  table: ReturnType<typeof useReactTable<TData>>;
}) {
  const intl = useIntl();
  const sensors = useSensors(useSensor(PointerSensor));

  const hideable = table.getAllColumns().filter((col) => col.getCanHide());
  if (hideable.length === 0) return null;

  const savedOrder = table.getState().columnOrder;
  const orderedHideable: Column<TData, unknown>[] =
    savedOrder.length > 0
      ? (savedOrder
          .map((id) => hideable.find((c) => c.id === id))
          .filter(Boolean) as Column<TData, unknown>[])
      : hideable;

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const ids = orderedHideable.map((c) => c.id);
    const oldIndex = ids.indexOf(String(active.id));
    const newIndex = ids.indexOf(String(over.id));
    const newHideableOrder = arrayMove(ids, oldIndex, newIndex);
    const nonHideableIds = table
      .getAllColumns()
      .filter((c) => !c.getCanHide())
      .map((c) => c.id);
    table.setColumnOrder([...nonHideableIds, ...newHideableOrder]);
  }

  return (
    <Sheet>
      <SheetTrigger
        render={
          <Button variant="outline" size="sm" className="gap-1.5 text-xs" />
        }
      >
        <Settings2 size={14} />
        {intl.formatMessage({ id: "columns" })}
      </SheetTrigger>
      <SheetContent
        side="right"
        className="flex flex-col gap-0 p-0 w-72 sm:max-w-72"
      >
        <SheetHeader className="p-4 pb-2">
          <SheetTitle>
            {intl.formatMessage({ id: "columns_toggle_label" })}
          </SheetTitle>
        </SheetHeader>
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={orderedHideable.map((c) => c.id)}
            strategy={verticalListSortingStrategy}
          >
            <div className="flex flex-col gap-2 overflow-y-auto flex-1 px-4 pb-4 pt-2">
              {orderedHideable.map((col) => (
                <SortableColumnRow<TData> key={col.id} column={col} />
              ))}
            </div>
          </SortableContext>
        </DndContext>
      </SheetContent>
    </Sheet>
  );
}

// ── EntityDataTable ───────────────────────────────────────────────────────────

export interface EntityDataTableProps<TItem extends IHasID> {
  items: TItem[];
  columns: ColumnDef<TItem>[];
  filter: ListFilterModel;
  setFilter: (
    f: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel),
  ) => void;
  listSelect: IListSelect<TItem>;
  /** localStorage key prefix for persisting column visibility, e.g. "table-cols:scenes" */
  visibilityKey?: string;
  /** When true the query is in-flight — suppress the "no results" state */
  isPending?: boolean;
  totalCount: number;
  preserveScrollDuringRefill: boolean;
  /**
   * Optional per-row wrapper. Receives the item, the default `<TableRow>`
   * JSX, and a per-row select callback (so the wrapped context menu's
   * "Select" item can put the user into select mode without the table
   * having to render the checkbox column up-front). Returns a wrapped node.
   */
  renderRow?: (
    item: TItem,
    defaultRow: React.ReactElement,
    onSelectedChanged: (selected: boolean, shiftKey: boolean) => void,
  ) => React.ReactNode;
}

export function EntityDataTable<TItem extends IHasID>({
  items,
  columns,
  filter,
  setFilter,
  listSelect,
  visibilityKey,
  isPending,
  totalCount,
  preserveScrollDuringRefill,
  renderRow,
}: EntityDataTableProps<TItem>) {
  const intl = useIntl();

  // ── Sorting: sync with filter ───────────────────────────────────────────────
  const sorting: SortingState = useMemo(() => {
    if (!filter.sortBy) return [];
    return [
      {
        id: filter.sortBy,
        desc: filter.sortDirection === SortDirectionEnum.Desc,
      },
    ];
  }, [filter.sortBy, filter.sortDirection]);

  const onSortingChange: OnChangeFn<SortingState> = (updater) => {
    const next = typeof updater === "function" ? updater(sorting) : updater;
    if (next.length === 0) {
      // cleared — keep current sort but toggle direction
      setFilter(filter.toggleSortDirection());
    } else {
      const { id, desc } = next[0];
      const sameField = id === filter.sortBy;
      if (sameField) {
        setFilter(filter.toggleSortDirection());
      } else {
        const updated = filter.setSortBy(id);
        if (desc !== (updated.sortDirection === SortDirectionEnum.Desc)) {
          setFilter(updated.toggleSortDirection());
        } else {
          setFilter(updated);
        }
      }
    }
  };

  // ── Row selection: sync with listSelect ────────────────────────────────────
  // getRowId uses row.id (string), so RowSelectionState keys are entity IDs.
  const rowSelection: RowSelectionState = useMemo(() => {
    const sel: RowSelectionState = {};
    listSelect.selectedIds.forEach((id) => {
      sel[id] = true;
    });
    return sel;
  }, [listSelect.selectedIds]);

  const onRowSelectionChange: OnChangeFn<RowSelectionState> = (updater) => {
    const next =
      typeof updater === "function" ? updater(rowSelection) : updater;

    const nextIds = new Set(
      Object.entries(next)
        .filter(([, v]) => v)
        .map(([k]) => k),
    );
    const prevIds = new Set(
      Object.entries(rowSelection)
        .filter(([, v]) => v)
        .map(([k]) => k),
    );

    nextIds.forEach((id) => {
      if (!prevIds.has(id)) listSelect.onSelectChange(id, true, false);
    });
    prevIds.forEach((id) => {
      if (!nextIds.has(id)) listSelect.onSelectChange(id, false, false);
    });
  };

  // ── Column visibility persistence ──────────────────────────────────────────
  const storageKey = visibilityKey ? `table-cols:${visibilityKey}` : undefined;
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(
    () => (storageKey ? loadColumnVisibility(storageKey) : {}),
  );

  const onColumnVisibilityChange: OnChangeFn<VisibilityState> = (updater) => {
    setColumnVisibility((prev) => {
      const next = typeof updater === "function" ? updater(prev) : updater;
      if (storageKey) saveColumnVisibility(storageKey, next);
      return next;
    });
  };

  // The "select" column rides alongside the persisted visibility but its
  // shown/hidden state is driven by listSelect.selecting (true once the user
  // enters select mode or has anything selected). Forced on top of the
  // persisted map so users can't permanently hide/show it via the column
  // manager — the manager still excludes it because enableHiding: false.
  const effectiveColumnVisibility: VisibilityState = useMemo(
    () => ({ ...columnVisibility, select: listSelect.selecting }),
    [columnVisibility, listSelect.selecting],
  );

  // ── Column order persistence ────────────────────────────────────────────────
  const orderKey = visibilityKey
    ? `table-col-order:${visibilityKey}`
    : undefined;

  // Non-hideable columns (select) are always pinned at the front and excluded
  // from localStorage — only the hideable portion is saved/restored.
  const nonHideableIds = useMemo(
    () =>
      columns
        .filter((c) => c.enableHiding === false)
        .map((c) => c.id as string),
    [columns],
  );

  const defaultHideableOrder = useMemo(
    () =>
      columns
        .filter((c) => c.enableHiding !== false)
        .map((c) => c.id as string),
    [columns],
  );

  const [columnOrder, setColumnOrderState] = useState<ColumnOrderState>(() => {
    const savedHideable = orderKey ? loadColumnOrder(orderKey) : [];
    const hideable =
      savedHideable.length > 0 ? savedHideable : defaultHideableOrder;
    return [...nonHideableIds, ...hideable];
  });

  const onColumnOrderChange: OnChangeFn<ColumnOrderState> = (updater) => {
    setColumnOrderState((prev) => {
      const next = typeof updater === "function" ? updater(prev) : updater;
      if (orderKey) {
        // Only persist the hideable portion
        saveColumnOrder(
          orderKey,
          next.filter((id) => !nonHideableIds.includes(id)),
        );
      }
      return next;
    });
  };

  // ── Column filters state (unused but required by type) ─────────────────────
  const [columnFilters] = useState<ColumnFiltersState>([]);

  // ── Table instance ─────────────────────────────────────────────────────────
  const table = useReactTable<TItem>({
    data: items,
    columns,
    state: {
      sorting,
      rowSelection,
      columnVisibility: effectiveColumnVisibility,
      columnFilters,
      columnOrder,
    },
    manualSorting: true,
    enableMultiSort: false,
    onSortingChange,
    onRowSelectionChange,
    onColumnVisibilityChange,
    onColumnOrderChange,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => row.id,
  });

  const isEmpty = !isPending && table.getRowModel().rows.length === 0;
  const isSkeletonState = isPending && table.getRowModel().rows.length === 0;

  // Varying skeleton widths for a more natural look
  const SKELETON_WIDTHS = ["w-3/4", "w-1/2", "w-2/3", "w-5/6", "w-1/3"];

  // The column-manager Sheet portals into the toolbar/drawer slot when one
  // is mounted. While a slot owner is registered (e.g. mobile drawer with
  // its body unmounted because it's closed) we suppress the button entirely
  // — the user opens the drawer to access it. Only when no provider exists
  // (e.g. an embedded list without the EntityList chrome) do we fall back
  // to the original inline-above-the-table render.
  const toolbarSlotEl = useTableToolbarSlotEl();
  const toolbarHasProvider = useTableToolbarHasProvider();
  const columnManager = <SheetColumnManager table={table} />;

  // The table owns a nested scroll container so its sticky header works.
  // Apply the same deletion-refill preservation and removed-page clamping as
  // the card layouts use on EntityList's outer scroll container.
  const [tableContainerEl, setTableContainerEl] =
    useState<HTMLDivElement | null>(null);
  usePreservedListScrollPosition(tableContainerEl, preserveScrollDuringRefill);
  useListPageChangeScrollPosition(
    tableContainerEl,
    filter.currentPage,
    filter.itemsPerPage,
    totalCount,
  );

  return (
    // h-full so the Table container (below) becomes the scroll context — a
    // sticky `<th>` only sticks relative to its closest scrolling
    // ancestor, so we want that to be the table container itself rather than
    // the page-level scroll area.
    <div className="flex h-full flex-col gap-2 p-2">
      {toolbarSlotEl ? (
        createPortal(columnManager, toolbarSlotEl)
      ) : toolbarHasProvider ? null : (
        <div className="flex justify-end">{columnManager}</div>
      )}

      {isEmpty ? (
        <Empty className="border border-dashed border-border rounded-lg">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>
              {intl.formatMessage({
                id: "filter_no_results",
                defaultMessage: "No results found",
              })}
            </EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table
          containerRef={setTableContainerEl}
          containerClassName="min-h-0 flex-1 overflow-auto"
        >
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  // sticky lives on each `<th>` (not the `<thead>`) so it
                  // works under `border-collapse: collapse` — sticky on
                  // `<thead>` is broken in WebKit/iOS Safari with the
                  // default border-collapse.
                  <TableHead
                    key={header.id}
                    className="sticky top-0 z-10 bg-background"
                    style={
                      header.column.getSize() !== 150
                        ? { width: header.column.getSize() }
                        : undefined
                    }
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {isSkeletonState
              ? Array.from({ length: 10 }).map((_, rowIdx) => (
                  <TableRow key={rowIdx}>
                    {table.getVisibleFlatColumns().map((col, colIdx) => (
                      <TableCell key={col.id}>
                        {col.id === "select" ? (
                          <Skeleton className="size-4 rounded-sm" />
                        ) : col.id === "thumbnail" ? (
                          <Skeleton className="h-10 w-10 rounded" />
                        ) : (
                          <Skeleton
                            className={`h-3 ${SKELETON_WIDTHS[(rowIdx + colIdx) % SKELETON_WIDTHS.length]}`}
                          />
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              : table.getRowModel().rows.map((row) => {
                  const isSelected = row.getIsSelected();
                  // In select mode, clicks on blank row chrome toggle that
                  // row (matches the card view's tap-to-select). Skip the
                  // handler for controls and copyable values so navigation,
                  // menus, and selecting a file path work independently.
                  const onRowClick = listSelect.selecting
                    ? (e: React.MouseEvent<HTMLTableRowElement>) => {
                        const target = e.target as HTMLElement;
                        if (
                          target.closest(
                            'a, button, input, label, [role="button"], [role="menuitem"], [data-selectable-text], code, pre',
                          )
                        ) {
                          return;
                        }
                        listSelect.onSelectChange(
                          row.original.id,
                          !isSelected,
                          e.shiftKey,
                        );
                      }
                    : undefined;
                  const defaultRow = (
                    <TableRow
                      key={row.id}
                      data-state={isSelected ? "selected" : undefined}
                      className={
                        listSelect.selecting ? "cursor-pointer" : undefined
                      }
                      onClick={onRowClick}
                    >
                      {row.getVisibleCells().map((cell) => (
                        <TableCell key={cell.id}>
                          {flexRender(
                            cell.column.columnDef.cell,
                            cell.getContext(),
                          )}
                        </TableCell>
                      ))}
                    </TableRow>
                  );
                  const onRowSelectedChanged = (
                    selected: boolean,
                    shiftKey: boolean,
                  ) =>
                    listSelect.onSelectChange(
                      row.original.id,
                      selected,
                      shiftKey,
                    );
                  return (
                    <React.Fragment key={row.id}>
                      {renderRow
                        ? renderRow(
                            row.original,
                            defaultRow,
                            onRowSelectedChanged,
                          )
                        : defaultRow}
                    </React.Fragment>
                  );
                })}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
