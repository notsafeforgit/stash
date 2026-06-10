/**
 * FrontPageConfig — a Sheet panel for adding, removing, and reordering
 * FrontPage carousel rows.
 *
 * Uses @dnd-kit/sortable for drag-to-reorder.
 */

import type React from "react";
import { useEffect, useMemo, useState } from "react";
import { useIntl } from "react-intl";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Trash2, Plus, LayoutTemplateIcon } from "lucide-react";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import * as GQL from "src/core/generated-graphql";
import { useQuery } from "@apollo/client/react";
import {
  type FrontPageContent,
  type ICustomFilter,
  type ISavedFilterRow,
  generatePremadeFrontPageContent,
} from "src/core/config";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetFooter,
} from "src/components/ui/sheet";
import { Button } from "src/components/ui/button";

// ── SavedFilter row label (async name lookup) ──────────────────────────────────

interface SavedFilterRowItemProps {
  content: ISavedFilterRow;
  savedFilters: GQL.SavedFilterDataFragment[];
  onRemove: () => void;
  dragHandleProps: React.HTMLAttributes<HTMLButtonElement>;
}

function SavedFilterRowItem({
  content,
  savedFilters,
  onRemove,
  dragHandleProps,
}: SavedFilterRowItemProps) {
  const intl = useIntl();
  const found = savedFilters.find(
    (sf) => String(sf.id) === String(content.savedFilterId),
  );
  const label = found
    ? found.name
    : intl.formatMessage(
        { id: "saved_filter_row_label", defaultMessage: "Saved filter #{id}" },
        { id: content.savedFilterId },
      );

  return (
    <RowItemShell
      label={label}
      badge={intl.formatMessage({
        id: "saved_filter",
        defaultMessage: "Saved filter",
      })}
      onRemove={onRemove}
      dragHandleProps={dragHandleProps}
    />
  );
}

// ── Generic row item shell ─────────────────────────────────────────────────────

interface RowItemShellProps {
  label: string;
  badge?: string;
  onRemove: () => void;
  dragHandleProps: React.HTMLAttributes<HTMLButtonElement>;
}

function RowItemShell({
  label,
  badge,
  onRemove,
  dragHandleProps,
}: RowItemShellProps) {
  return (
    <div className="flex items-center gap-2 bg-card border border-border rounded-md px-2 py-2">
      <Button
        variant="ghost"
        size="icon-sm"
        className="cursor-grab active:cursor-grabbing shrink-0"
        {...dragHandleProps}
        aria-label="Drag to reorder"
      >
        <GripVertical size={16} />
      </Button>

      <div className="flex-1 min-w-0">
        <span className="text-sm truncate block">{label}</span>
        {badge && (
          <span className="text-xs text-muted-foreground">{badge}</span>
        )}
      </div>

      <Button
        variant="ghost"
        size="icon-sm"
        className="hover:text-destructive shrink-0"
        onClick={onRemove}
        aria-label="Remove row"
      >
        <Trash2 size={15} />
      </Button>
    </div>
  );
}

// ── Sortable row wrapper ───────────────────────────────────────────────────────

interface SortableRowProps {
  id: string;
  content: FrontPageContent;
  savedFilters: GQL.SavedFilterDataFragment[];
  onRemove: () => void;
}

function SortableRow({
  id,
  content,
  savedFilters,
  onRemove,
}: SortableRowProps) {
  const intl = useIntl();
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  };

  const dragHandleProps = {
    ...attributes,
    ...listeners,
  } as React.HTMLAttributes<HTMLButtonElement>;

  if (content.__typename === "SavedFilter") {
    return (
      <div ref={setNodeRef} style={style}>
        <SavedFilterRowItem
          content={content}
          savedFilters={savedFilters}
          onRemove={onRemove}
          dragHandleProps={dragHandleProps}
        />
      </div>
    );
  }

  const label =
    content.title ??
    (content.message
      ? intl.formatMessage(
          { id: content.message.id, defaultMessage: content.message.id },
          content.message.values,
        )
      : `${content.mode} – ${content.sortBy}`);

  return (
    <div ref={setNodeRef} style={style}>
      <RowItemShell
        label={label}
        badge={content.mode}
        onRemove={onRemove}
        dragHandleProps={dragHandleProps}
      />
    </div>
  );
}

// ── Add premade row picker ─────────────────────────────────────────────────────

interface AddRowMenuProps {
  premade: ICustomFilter[];
  savedFilters: GQL.SavedFilterDataFragment[];
  onAdd: (content: FrontPageContent) => void;
  onClose: () => void;
}

function AddRowMenu({
  premade,
  savedFilters,
  onAdd,
  onClose,
}: AddRowMenuProps) {
  const intl = useIntl();

  return (
    <div className="flex flex-col gap-1">
      <p className="text-xs font-semibold text-muted-foreground uppercase px-1 pb-1">
        {intl.formatMessage({ id: "premade", defaultMessage: "Premade" })}
      </p>
      {premade.map((item, i) => {
        const label =
          item.title ??
          (item.message
            ? intl.formatMessage(
                { id: item.message.id, defaultMessage: item.message.id },
                item.message.values,
              )
            : `${item.mode} – ${item.sortBy}`);
        return (
          <Button
            key={i}
            variant="ghost"
            className="justify-start h-auto px-2 py-1.5 font-normal text-sm"
            onClick={() => {
              onAdd(item);
              onClose();
            }}
          >
            {label}
          </Button>
        );
      })}

      {savedFilters.length > 0 && (
        <>
          <p className="text-xs font-semibold text-muted-foreground uppercase px-1 pt-2 pb-1">
            {intl.formatMessage({
              id: "saved_filters",
              defaultMessage: "Saved filters",
            })}
          </p>
          {savedFilters.map((sf) => (
            <Button
              key={sf.id}
              variant="ghost"
              className="justify-start h-auto px-2 py-1.5 font-normal text-sm"
              onClick={() => {
                onAdd({
                  __typename: "SavedFilter",
                  savedFilterId: Number(sf.id),
                });
                onClose();
              }}
            >
              {sf.name}
              <span className="text-muted-foreground text-xs ml-2">
                {sf.mode}
              </span>
            </Button>
          ))}
        </>
      )}
    </div>
  );
}

// ── FrontPageConfig Sheet ──────────────────────────────────────────────────────

interface FrontPageConfigProps {
  open: boolean;
  rows: FrontPageContent[];
  onClose: () => void;
  onSave: (rows: FrontPageContent[]) => void;
}

export function FrontPageConfig({
  open,
  rows: initialRows,
  onClose,
  onSave,
}: FrontPageConfigProps) {
  const intl = useIntl();
  const [rows, setRows] = useState<FrontPageContent[]>(initialRows);
  const [showAddMenu, setShowAddMenu] = useState(false);

  // Sync when parent rows change (e.g. after save)
  useEffect(() => {
    setRows(initialRows);
  }, [initialRows]);

  const { data: savedFiltersData } = useQuery<
    GQL.FindSavedFiltersQuery,
    GQL.FindSavedFiltersQueryVariables
  >(GQL.FindSavedFiltersDocument, { variables: { mode: undefined } });
  const savedFilters = savedFiltersData?.findSavedFilters ?? [];

  const premade = useMemo(() => generatePremadeFrontPageContent(intl), [intl]);

  const sensors = useSensors(useSensor(PointerSensor));

  // Stable IDs for dnd-kit: use index-based keys since content isn't guaranteed unique
  const ids = useMemo(() => rows.map((_, i) => String(i)), [rows]);

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = ids.indexOf(String(active.id));
    const newIndex = ids.indexOf(String(over.id));
    if (oldIndex === -1 || newIndex === -1) return;

    setRows((prev) => arrayMove(prev, oldIndex, newIndex));
  }

  function removeRow(index: number) {
    setRows((prev) => prev.filter((_, i) => i !== index));
  }

  function addRow(content: FrontPageContent) {
    setRows((prev) => [...prev, content]);
  }

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-full sm:max-w-md flex flex-col gap-0 p-0"
      >
        <SheetHeader className="px-4 pt-4 pb-3 border-b border-border">
          <SheetTitle>
            {intl.formatMessage({
              id: "frontpage_configure",
              defaultMessage: "Customise homepage",
            })}
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-4 py-3 flex flex-col gap-2">
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
          >
            <SortableContext items={ids} strategy={verticalListSortingStrategy}>
              {rows.map((content, i) => (
                <SortableRow
                  key={ids[i]}
                  id={ids[i]}
                  content={content}
                  savedFilters={savedFilters}
                  onRemove={() => removeRow(i)}
                />
              ))}
            </SortableContext>
          </DndContext>

          {rows.length === 0 && (
            <Empty className="border border-dashed border-border rounded-lg my-4">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <LayoutTemplateIcon />
                </EmptyMedia>
                <EmptyTitle>
                  {intl.formatMessage({
                    id: "frontpage_no_rows",
                    defaultMessage: "No rows. Add one below.",
                  })}
                </EmptyTitle>
              </EmptyHeader>
            </Empty>
          )}

          {showAddMenu ? (
            <div className="border border-border rounded-md p-3 mt-2">
              <AddRowMenu
                premade={premade}
                savedFilters={savedFilters}
                onAdd={addRow}
                onClose={() => setShowAddMenu(false)}
              />
            </div>
          ) : (
            <Button
              variant="outline"
              className="border-dashed text-muted-foreground hover:text-foreground w-full mt-2"
              onClick={() => setShowAddMenu(true)}
            >
              <Plus size={15} />
              {intl.formatMessage({ id: "add_row", defaultMessage: "Add row" })}
            </Button>
          )}
        </div>

        <SheetFooter className="px-4 pb-4 pt-3 border-t border-border flex flex-row gap-2">
          <Button variant="outline" className="flex-1" onClick={onClose}>
            {intl.formatMessage({ id: "cancel", defaultMessage: "Cancel" })}
          </Button>
          <Button className="flex-1" onClick={() => onSave(rows)}>
            {intl.formatMessage({ id: "save", defaultMessage: "Save" })}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
