import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { useListContextOptional } from "src/components/list/list-provider";
import {
  ContextMenuItem,
  ContextMenuSeparator,
} from "src/components/ui/context-menu";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

export function useBulkCardActions<TItem extends { id: string }>(
  _itemId: string,
) {
  const {
    getSelectedItems,
    totalCount,
    applyToAllTarget,
    onSelectAll,
    onSelectNone,
  } = useListContextOptional<TItem>();
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [bulkEditOpen, setBulkEditOpen] = useState(false);
  // Computed lazily when the context menu opens — avoids subscribing to
  // selectedIds/selectedItems and causing re-renders on every selection change.
  const [showBulkActions, setShowBulkActions] = useState(false);
  const [selectedItems, setSelectedItems] = useState<TItem[]>([]);

  const onContextMenuOpen = useCallback(() => {
    // Any right-click while a multi-selection is active surfaces the bulk
    // menu — even if the right-clicked item isn't itself in the selection.
    // Without this, a selection that's scrolled off-screen has no way to
    // be acted on without scrolling back to a selected item.
    const items = getSelectedItems() as TItem[];
    setShowBulkActions(items.length > 1);
    setSelectedItems(items);
  }, [getSelectedItems]);

  return {
    selectedItems,
    totalCount,
    applyToAllTarget,
    bulkCount: selectedItems.length,
    showBulkActions,
    bulkDeleteOpen,
    setBulkDeleteOpen,
    bulkEditOpen,
    setBulkEditOpen,
    onContextMenuOpen,
    /** Selects every item on the current page. Routed through the
     *  list provider so the same action drives the toolbar's select-
     *  all button and any context-menu entry that wants to expose it. */
    onSelectAll,
    /** Clears all selections — useful for menu items like "Deselect all". */
    onSelectNone,
  };
}

interface BulkContextMenuItemsProps {
  count: number;
  /** English plural noun, e.g. "scenes", "tags". Used as {noun} in the locale string. */
  noun: string;
  onEdit: () => void;
  onDelete: () => void;
  /** When provided, renders an "Open in new tab" entry that opens the
   *  right-clicked item (not the whole selection) in a new browser tab. */
  openInNewTabHref?: string;
  /** When provided, renders a "Generate {count} {noun}" entry above Delete. */
  onGenerate?: () => void;
  /** When provided, renders a "Merge {count} {noun} into…" entry above Delete. */
  onMerge?: () => void;
  /** When provided, renders an "Auto tag {count} {noun}" entry above Delete. */
  onAutoTag?: () => void;
  /** When provided, renders a "Download {count} {noun}" entry. Used by
   *  scene cards to bulk-enqueue selections into the offline download
   *  queue. */
  onDownload?: () => void;
}

export function BulkContextMenuItems({
  count,
  noun,
  onEdit,
  onDelete,
  openInNewTabHref,
  onGenerate,
  onMerge,
  onAutoTag,
  onDownload,
}: BulkContextMenuItemsProps) {
  const intl = useIntl();
  // Lead with Select all so the user can extend an in-progress selection
  // without leaving the menu; everything below operates on the current
  // selection only.
  return (
    <>
      <SelectAllMenuItem />
      {openInNewTabHref && <OpenInNewTabMenuItem href={openInNewTabHref} />}
      <ContextMenuSeparator />
      <ContextMenuItem onClick={onEdit}>
        {intl.formatMessage(
          { id: "actions.edit_count", defaultMessage: "Edit {count} {noun}" },
          { count, noun },
        )}
        …
      </ContextMenuItem>
      {onGenerate && (
        <ContextMenuItem onClick={onGenerate}>
          {intl.formatMessage(
            {
              id: "actions.generate_count",
              defaultMessage: "Generate {count} {noun}",
            },
            { count, noun },
          )}
          …
        </ContextMenuItem>
      )}
      {onMerge && (
        <ContextMenuItem onClick={onMerge}>
          {intl.formatMessage(
            {
              id: "actions.merge_count",
              defaultMessage: "Merge {count} {noun} into…",
            },
            { count, noun },
          )}
        </ContextMenuItem>
      )}
      {onAutoTag && (
        <ContextMenuItem onClick={onAutoTag}>
          {intl.formatMessage(
            {
              id: "actions.auto_tag_count",
              defaultMessage: "Auto tag {count} {noun}",
            },
            { count, noun },
          )}
          …
        </ContextMenuItem>
      )}
      {onDownload && (
        <ContextMenuItem onClick={onDownload}>
          {intl.formatMessage(
            {
              id: "actions.download_count",
              defaultMessage: "Download {count} {noun}",
            },
            { count, noun },
          )}
        </ContextMenuItem>
      )}
      <ContextMenuItem variant="destructive" onClick={onDelete}>
        {intl.formatMessage(
          {
            id: "actions.delete_count",
            defaultMessage: "Delete {count} {noun}",
          },
          { count, noun },
        )}
        …
      </ContextMenuItem>
    </>
  );
}
