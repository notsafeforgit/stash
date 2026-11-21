import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import { useIntl } from "react-intl";
import {
  Cog,
  Download,
  Ellipsis,
  GitMerge,
  Pencil,
  Share2,
  Trash2,
  Wand2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from "@/components/ui/drawer";
import {
  EntityActionsList,
  EntityActionsMenu,
  type EntityActionItem,
} from "@/components/detail/entity-actions-menu";
import { ShareSelectionButton } from "@/components/sharing/share-action";
import { useListContextOptional } from "./list-provider";

/** Opens an entity's bulk dialogs for the list's current selection. */
export interface BulkSelectionActions {
  /** English plural noun for the card menu's count labels, e.g. "scenes". */
  noun: string;
  onEdit: () => void;
  onDelete: () => void;
  onGenerate?: () => void;
  onMerge?: () => void;
  onAutoTag?: () => void;
  onDownload?: () => void;
}

const BulkSelectionActionsContext = createContext<BulkSelectionActions | null>(
  null,
);

export const BulkSelectionActionsProvider =
  BulkSelectionActionsContext.Provider;

export function useBulkSelectionActions() {
  return useContext(BulkSelectionActionsContext);
}

/**
 * The list's selection, captured when a menu or action opens so it stays
 * fixed while the selection changes or empties behind it.
 */
export function useSelectionSnapshot<TItem extends { id: string }>(
  items: readonly TItem[],
  getSelectedItems?: () => readonly TItem[],
) {
  const { getSelectedIds } = useListContextOptional();
  const [selection, setSelection] = useState<readonly TItem[]>([]);
  const selectedItems = useMemo(() => {
    const byId = new Map(items.map((item) => [item.id, item]));
    return selection.map((item) => byId.get(item.id) ?? item);
  }, [items, selection]);
  const capture = useCallback(() => {
    const ids = getSelectedIds();
    const next =
      getSelectedItems?.() ?? items.filter((item) => ids.has(item.id));
    setSelection(next);
    return next;
  }, [getSelectedIds, getSelectedItems, items]);
  return { selectedItems, capture };
}

/** A selection snapshot plus which of an entity's bulk dialogs is open. */
export function useSelectionDialogs<TItem extends { id: string }>(
  items: readonly TItem[],
  getSelectedItems?: () => readonly TItem[],
) {
  const { totalCount, applyToAllTarget, onRemoveSelection } =
    useListContextOptional();
  const { selectedItems, capture } = useSelectionSnapshot(
    items,
    getSelectedItems,
  );
  const [dialog, setDialog] = useState<string | null>(null);
  const open = useCallback(
    (next: string) => {
      capture();
      setDialog(next);
    },
    [capture],
  );
  const dialogProps = (name: string) => ({
    open: dialog === name,
    onOpenChange: (value: boolean) =>
      setDialog((current) =>
        value ? name : current === name ? null : current,
      ),
  });
  return {
    selectedItems,
    capture,
    open,
    dialogProps,
    totalCount,
    applyToAllTarget,
    onRemoveSelection,
  };
}

/**
 * Selection actions that do not need a selected item on screen: a sheet on
 * phones and a menu in the desktop toolbar.
 */
export function ListSelectionActions({
  count,
  mobile,
  onShare,
  onDelete,
}: {
  count: number;
  mobile: boolean;
  onShare?: () => void;
  /** Deletion for lists without bulk dialogs of their own. */
  onDelete?: () => void;
}) {
  const intl = useIntl();
  const bulk = useBulkSelectionActions();
  const [open, setOpen] = useState(false);
  const shareLabel = intl.formatMessage({
    id: "sharing.share",
    defaultMessage: "Share…",
  });

  const items: EntityActionItem[] = [];
  if (onShare && mobile)
    items.push({
      key: "share",
      icon: Share2,
      label: shareLabel,
      onSelect: onShare,
    });
  if (bulk) {
    items.push({
      key: "edit",
      icon: Pencil,
      label: `${intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}…`,
      onSelect: bulk.onEdit,
    });
    if (bulk.onGenerate)
      items.push({
        key: "generate",
        icon: Cog,
        label: `${intl.formatMessage({ id: "actions.generate", defaultMessage: "Generate" })}…`,
        onSelect: bulk.onGenerate,
      });
    if (bulk.onAutoTag)
      items.push({
        key: "auto-tag",
        icon: Wand2,
        label: `${intl.formatMessage({ id: "actions.auto_tag", defaultMessage: "Auto tag" })}…`,
        onSelect: bulk.onAutoTag,
      });
    if (bulk.onMerge)
      items.push({
        key: "merge",
        icon: GitMerge,
        label: intl.formatMessage({
          id: "actions.merge_into",
          defaultMessage: "Merge into…",
        }),
        onSelect: bulk.onMerge,
      });
    if (bulk.onDownload)
      items.push({
        key: "download",
        icon: Download,
        label: intl.formatMessage({
          id: "actions.download",
          defaultMessage: "Download",
        }),
        onSelect: bulk.onDownload,
      });
  }
  const remove = bulk?.onDelete ?? onDelete;
  if (remove) {
    if (items.length > 0)
      items.push({ key: "delete-separator", separator: true });
    items.push({
      key: "delete",
      icon: Trash2,
      label: `${intl.formatMessage({ id: "actions.delete", defaultMessage: "Delete" })}…`,
      destructive: true,
      onSelect: remove,
    });
  }

  const share = onShare ? (
    <ShareSelectionButton compact={mobile} onShare={onShare} />
  ) : null;
  const label = intl.formatMessage({
    id: "actions.selection_actions",
    defaultMessage: "Selection actions",
  });
  const heading = intl.formatMessage(
    { id: "list.selected_count", defaultMessage: "{count} selected" },
    { count },
  );

  if (!mobile)
    return (
      <>
        {share}
        {items.length > 0 && (
          <EntityActionsMenu items={items} label={label} heading={heading} />
        )}
      </>
    );
  // On a phone the bar has room for one button, so sharing joins the sheet.
  if (items.length === 0 || (items.length === 1 && onShare)) return share;
  return (
    <Drawer open={open} onOpenChange={setOpen}>
      <DrawerTrigger
        render={
          <Button
            variant="ghost"
            size="icon-lg"
            className="size-11 shrink-0"
            aria-label={label}
          />
        }
      >
        <Ellipsis />
      </DrawerTrigger>
      <DrawerContent>
        <DrawerHeader className="shrink-0 py-2">
          <DrawerTitle>{heading}</DrawerTitle>
        </DrawerHeader>
        <div className="min-h-0 overflow-y-auto overscroll-contain px-3 pb-2">
          <EntityActionsList items={items} onAction={() => setOpen(false)} />
        </div>
      </DrawerContent>
    </Drawer>
  );
}
