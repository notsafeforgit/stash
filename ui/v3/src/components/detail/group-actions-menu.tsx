import { useState } from "react";
import { useIntl } from "react-intl";
import { Trash2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  EntityActionsMenu,
  type EntityActionItem,
} from "./entity-actions-menu";
import { DeleteDialog } from "src/components/detail/delete-dialog";

interface GroupActionsMenuProps {
  group: NonNullable<GQL.FindGroupQuery["findGroup"]>;
  /** Called once the group has been deleted so the page can navigate away. */
  onDeleted?: () => void;
}

export function GroupActionsMenu({ group, onDeleted }: GroupActionsMenuProps) {
  const intl = useIntl();
  const [deleteOpen, setDeleteOpen] = useState(false);

  const [destroyGroup] = useEntityMutation(GQL.GroupDestroyDocument);

  async function handleConfirmedDelete() {
    await destroyGroup({
      variables: { id: group.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Group",
          listFieldName: "findGroups",
          itemsField: "groups",
          ids: [group.id],
        });
      },
    });
    onDeleted?.();
  }

  const items: EntityActionItem[] = [
    {
      key: "delete",
      icon: Trash2,
      label:
        intl.formatMessage(
          {
            id: "actions.delete_entity",
            defaultMessage: "Delete {entityType}",
          },
          {
            entityType: intl
              .formatMessage({ id: "group", defaultMessage: "group" })
              .toLocaleLowerCase(),
          },
        ) + "…",
      onSelect: () => setDeleteOpen(true),
      destructive: true,
    },
  ];

  return (
    <>
      <EntityActionsMenu items={items} />

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={group.name}
        onConfirm={handleConfirmedDelete}
      />
    </>
  );
}
