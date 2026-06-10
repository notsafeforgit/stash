import { useState } from "react";
import { useIntl } from "react-intl";
import { EllipsisVertical, Trash2, Wand2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import { Button } from "src/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import { AutoTagDialog } from "src/components/detail/auto-tag-dialog";
import { DeleteDialog } from "src/components/detail/delete-dialog";

interface StudioActionsMenuProps {
  studio: NonNullable<GQL.FindStudioQuery["findStudio"]>;
  /** Called once the studio has been deleted so the page can navigate away. */
  onDeleted?: () => void;
}

export function StudioActionsMenu({
  studio,
  onDeleted,
}: StudioActionsMenuProps) {
  const intl = useIntl();
  const [autoTagOpen, setAutoTagOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const [destroyStudio] = useEntityMutation(GQL.StudioDestroyDocument);

  async function handleConfirmedDelete() {
    await destroyStudio({
      variables: { id: studio.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Studio",
          listFieldName: "findStudios",
          itemsField: "studios",
          ids: [studio.id],
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
          <DropdownMenuItem onClick={() => setAutoTagOpen(true)}>
            <Wand2 />
            {intl.formatMessage({
              id: "actions.auto_tag",
              defaultMessage: "Auto tag",
            })}
            …
          </DropdownMenuItem>
          <DropdownMenuSeparator />
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
                  .formatMessage({ id: "studio", defaultMessage: "studio" })
                  .toLocaleLowerCase(),
              },
            )}
            …
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <AutoTagDialog
        open={autoTagOpen}
        onOpenChange={setAutoTagOpen}
        entityType="studio"
        ids={[studio.id]}
        entityName={studio.name}
      />
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={studio.name}
        onConfirm={handleConfirmedDelete}
      />
    </>
  );
}
