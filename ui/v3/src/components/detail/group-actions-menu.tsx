import { useState } from "react";
import { useIntl } from "react-intl";
import { EllipsisVertical, Trash2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import { Button } from "src/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
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

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          render={<Button variant="outline" size="sm" />}
          aria-label={intl.formatMessage({
            id: "operations",
            defaultMessage: "Operations",
          })}
          title={intl.formatMessage({
            id: "operations",
            defaultMessage: "Operations",
          })}
        >
          <EllipsisVertical />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2 />
            {intl.formatMessage(
              {
                id: "actions.delete_entity",
                defaultMessage: "Delete {entityType}",
              },
              {
                entityType: intl
                  .formatMessage({ id: "group", defaultMessage: "group" })
                  .toLocaleLowerCase(),
              },
            )}
            …
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={group.name}
        onConfirm={handleConfirmedDelete}
      />
    </>
  );
}
