import { useMemo, type PropsWithChildren } from "react";
import { useIntl } from "react-intl";
import * as GQL from "@/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "@/core/client";
import { TagListItems } from "@/components/list/entity-list-items";
import {
  BulkSelectionActionsProvider,
  useSelectionDialogs,
} from "@/components/list/selection-actions";
import { DeleteDialog } from "@/components/detail/delete-dialog";
import { AutoTagDialog } from "@/components/detail/auto-tag-dialog";
import { TagBulkEditSheet } from "@/components/detail/deferred-overlays";

export function TagSelectionActions({ children }: PropsWithChildren) {
  const intl = useIntl();
  const {
    selectedItems,
    open,
    dialogProps,
    totalCount,
    applyToAllTarget,
    onRemoveSelection,
  } = useSelectionDialogs(
    TagListItems.useItems(),
    TagListItems.useSelectionGetter(),
  );
  const [destroyTags] = useEntityMutation(GQL.TagsDestroyDocument);
  const actions = useMemo(
    () => ({
      noun: "tags",
      onEdit: () => open("edit"),
      onAutoTag: () => open("auto-tag"),
      onDelete: () => open("delete"),
    }),
    [open],
  );

  async function handleConfirmedDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyTags({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Tag",
          listFieldName: "findTags",
          itemsField: "tags",
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
                id: "dialogs.delete_tags_count",
                defaultMessage: "{count} tags",
              },
              { count: selectedItems.length },
            )}
            onConfirm={handleConfirmedDelete}
          />
          <AutoTagDialog
            {...dialogProps("auto-tag")}
            entityType="tag"
            ids={selectedItems.map((i) => i.id)}
          />
          <TagBulkEditSheet
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
