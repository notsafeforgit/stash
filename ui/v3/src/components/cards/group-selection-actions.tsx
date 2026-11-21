import { useMemo, type PropsWithChildren } from "react";
import { useIntl } from "react-intl";
import * as GQL from "@/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "@/core/client";
import { GroupListItems } from "@/components/list/entity-list-items";
import {
  BulkSelectionActionsProvider,
  useSelectionDialogs,
} from "@/components/list/selection-actions";
import { DeleteDialog } from "@/components/detail/delete-dialog";
import { GroupBulkEditSheet } from "@/components/detail/deferred-overlays";

export function GroupSelectionActions({ children }: PropsWithChildren) {
  const intl = useIntl();
  const {
    selectedItems,
    open,
    dialogProps,
    totalCount,
    applyToAllTarget,
    onRemoveSelection,
  } = useSelectionDialogs(
    GroupListItems.useItems(),
    GroupListItems.useSelectionGetter(),
  );
  const [destroyGroups] = useEntityMutation(GQL.GroupsDestroyDocument);
  const actions = useMemo(
    () => ({
      noun: "groups",
      onEdit: () => open("edit"),
      onDelete: () => open("delete"),
    }),
    [open],
  );

  async function handleConfirmedDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyGroups({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Group",
          listFieldName: "findGroups",
          itemsField: "groups",
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
                id: "dialogs.delete_groups_count",
                defaultMessage: "{count} groups",
              },
              { count: selectedItems.length },
            )}
            onConfirm={handleConfirmedDelete}
          />
          <GroupBulkEditSheet
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
