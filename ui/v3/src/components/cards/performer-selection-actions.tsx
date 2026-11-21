import { useMemo, type PropsWithChildren } from "react";
import { useIntl } from "react-intl";
import * as GQL from "@/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "@/core/client";
import { PerformerListItems } from "@/components/list/entity-list-items";
import {
  BulkSelectionActionsProvider,
  useSelectionDialogs,
} from "@/components/list/selection-actions";
import { DeleteDialog } from "@/components/detail/delete-dialog";
import { AutoTagDialog } from "@/components/detail/auto-tag-dialog";
import {
  PerformerBulkEditSheet,
  PerformerMergeDialog,
} from "@/components/detail/deferred-overlays";

export function PerformerSelectionActions({ children }: PropsWithChildren) {
  const intl = useIntl();
  const {
    selectedItems,
    open,
    dialogProps,
    totalCount,
    applyToAllTarget,
    onRemoveSelection,
  } = useSelectionDialogs(
    PerformerListItems.useItems(),
    PerformerListItems.useSelectionGetter(),
  );
  const [destroyPerformers] = useEntityMutation(GQL.PerformersDestroyDocument);
  const actions = useMemo(
    () => ({
      noun: "performers",
      onEdit: () => open("edit"),
      onAutoTag: () => open("auto-tag"),
      onMerge: () => open("merge"),
      onDelete: () => open("delete"),
    }),
    [open],
  );

  async function handleConfirmedDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyPerformers({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Performer",
          listFieldName: "findPerformers",
          itemsField: "performers",
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
                id: "dialogs.delete_performers_count",
                defaultMessage: "{count} performers",
              },
              { count: selectedItems.length },
            )}
            onConfirm={handleConfirmedDelete}
          />
          <AutoTagDialog
            {...dialogProps("auto-tag")}
            entityType="performer"
            ids={selectedItems.map((i) => i.id)}
          />
          <PerformerMergeDialog
            {...dialogProps("merge")}
            sources={selectedItems.map((i) => ({
              id: i.id,
              name: i.name,
              disambiguation: i.disambiguation,
              image_path: i.image_path,
              birthdate: i.birthdate,
              death_date: i.death_date,
            }))}
          />
          <PerformerBulkEditSheet
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
