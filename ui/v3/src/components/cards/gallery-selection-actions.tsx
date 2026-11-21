import { useMemo, type PropsWithChildren } from "react";
import { GalleryListItems } from "@/components/list/entity-list-items";
import {
  BulkSelectionActionsProvider,
  useSelectionDialogs,
} from "@/components/list/selection-actions";
import {
  GalleryBulkEditSheet,
  GalleryDeleteDialog,
} from "@/components/detail/deferred-overlays";

export function GallerySelectionActions({ children }: PropsWithChildren) {
  const { selectedItems, open, dialogProps, totalCount, applyToAllTarget } =
    useSelectionDialogs(
      GalleryListItems.useItems(),
      GalleryListItems.useSelectionGetter(),
    );
  const actions = useMemo(
    () => ({
      noun: "galleries",
      onEdit: () => open("edit"),
      onDelete: () => open("delete"),
    }),
    [open],
  );
  return (
    <>
      <BulkSelectionActionsProvider value={actions}>
        {children}
      </BulkSelectionActionsProvider>
      {selectedItems.length > 0 && (
        <>
          <GalleryDeleteDialog
            {...dialogProps("delete")}
            ids={selectedItems.map((item) => item.id)}
          />
          <GalleryBulkEditSheet
            {...dialogProps("edit")}
            items={selectedItems}
            applyToAllTarget={applyToAllTarget}
            totalCount={totalCount}
          />
        </>
      )}
    </>
  );
}
