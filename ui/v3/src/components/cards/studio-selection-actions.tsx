import { useMemo, type PropsWithChildren } from "react";
import { useIntl } from "react-intl";
import * as GQL from "@/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "@/core/client";
import { StudioListItems } from "@/components/list/entity-list-items";
import {
  BulkSelectionActionsProvider,
  useSelectionDialogs,
} from "@/components/list/selection-actions";
import { DeleteDialog } from "@/components/detail/delete-dialog";
import { AutoTagDialog } from "@/components/detail/auto-tag-dialog";
import { StudioBulkEditSheet } from "@/components/detail/deferred-overlays";

export function StudioSelectionActions({ children }: PropsWithChildren) {
  const intl = useIntl();
  const {
    selectedItems,
    open,
    dialogProps,
    totalCount,
    applyToAllTarget,
    onRemoveSelection,
  } = useSelectionDialogs(
    StudioListItems.useItems(),
    StudioListItems.useSelectionGetter(),
  );
  const [destroyStudios] = useEntityMutation(GQL.StudiosDestroyDocument);
  const actions = useMemo(
    () => ({
      noun: "studios",
      onEdit: () => open("edit"),
      onAutoTag: () => open("auto-tag"),
      onDelete: () => open("delete"),
    }),
    [open],
  );

  async function handleConfirmedDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyStudios({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Studio",
          listFieldName: "findStudios",
          itemsField: "studios",
          ids,
        });
        onRemoveSelection?.(ids);
      },
    });
  }

  return (
    <>
      <BulkSelectionActionsProvider value={actions}>
        {children}
      </BulkSelectionActionsProvider>
      {selectedItems.length > 0 && (
        <>
          <DeleteDialog
            {...dialogProps("delete")}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_studios_count",
                defaultMessage: "{count} studios",
              },
              { count: selectedItems.length },
            )}
            onConfirm={handleConfirmedDelete}
          />
          <AutoTagDialog
            {...dialogProps("auto-tag")}
            entityType="studio"
            ids={selectedItems.map((i) => i.id)}
          />
          <StudioBulkEditSheet
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
