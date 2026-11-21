import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { useListContextOptional } from "src/components/list/list-provider";
import {
  type BulkSelectionActions,
  useBulkSelectionActions,
} from "src/components/list/selection-actions";
import {
  ContextMenuItem,
  ContextMenuSeparator,
} from "src/components/ui/context-menu";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

export function useBulkCardActions() {
  const actions = useBulkSelectionActions();
  const { getSelectedIds } = useListContextOptional();
  // Counted when the menu opens, so cards do not re-render as the
  // selection changes.
  const [bulkCount, setBulkCount] = useState(0);
  const onContextMenuOpen = useCallback(() => {
    // Any right-click while a multi-selection is active surfaces the bulk
    // menu, even if the right-clicked item isn't itself in the selection.
    setBulkCount(getSelectedIds().size);
  }, [getSelectedIds]);
  return {
    bulkActions: actions && bulkCount > 1 ? actions : undefined,
    bulkCount,
    onContextMenuOpen,
  };
}

interface BulkContextMenuItemsProps {
  count: number;
  actions: BulkSelectionActions;
  /** When provided, renders an "Open in new tab" entry that opens the
   *  right-clicked item (not the whole selection) in a new browser tab. */
  openInNewTabHref?: string;
}

export function BulkContextMenuItems({
  count,
  actions,
  openInNewTabHref,
}: BulkContextMenuItemsProps) {
  const intl = useIntl();
  const { noun, onEdit, onDelete, onGenerate, onMerge, onAutoTag, onDownload } =
    actions;
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
