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

interface TagActionsMenuProps {
  tag: NonNullable<GQL.FindTagQuery["findTag"]>;
  /** Called once the tag has been deleted so the page can navigate away. */
  onDeleted?: () => void;
}

export function TagActionsMenu({ tag, onDeleted }: TagActionsMenuProps) {
  const intl = useIntl();
  const [autoTagOpen, setAutoTagOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const [destroyTag] = useEntityMutation(GQL.TagDestroyDocument);

  async function handleConfirmedDelete() {
    await destroyTag({
      variables: { id: tag.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Tag",
          listFieldName: "findTags",
          itemsField: "tags",
          ids: [tag.id],
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
                  .formatMessage({ id: "tag", defaultMessage: "tag" })
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
        entityType="tag"
        ids={[tag.id]}
        entityName={tag.name}
      />
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={tag.name}
        onConfirm={handleConfirmedDelete}
      />
    </>
  );
}
