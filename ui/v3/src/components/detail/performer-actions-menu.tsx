import { useState } from "react";
import { useIntl } from "react-intl";
import { EllipsisVertical, GitMerge, Trash2, Wand2 } from "lucide-react";
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
import { PerformerMergeDialog } from "src/components/detail/performer-merge-dialog";

interface PerformerActionsMenuProps {
  performer: NonNullable<GQL.FindPerformerQuery["findPerformer"]>;
  /** Called once the performer has been deleted so the page can navigate away. */
  onDeleted?: () => void;
}

export function PerformerActionsMenu({
  performer,
  onDeleted,
}: PerformerActionsMenuProps) {
  const intl = useIntl();
  const [autoTagOpen, setAutoTagOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const [destroyPerformer] = useEntityMutation(GQL.PerformerDestroyDocument);

  const displayName = performer.disambiguation
    ? `${performer.name} (${performer.disambiguation})`
    : performer.name;

  async function handleConfirmedDelete() {
    await destroyPerformer({
      variables: { id: performer.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Performer",
          listFieldName: "findPerformers",
          itemsField: "performers",
          ids: [performer.id],
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
          <DropdownMenuItem onClick={() => setMergeOpen(true)}>
            <GitMerge />
            {intl.formatMessage({
              id: "actions.merge_into",
              defaultMessage: "Merge into…",
            })}
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
                  .formatMessage({
                    id: "performer",
                    defaultMessage: "performer",
                  })
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
        entityType="performer"
        ids={[performer.id]}
        entityName={displayName}
      />
      <PerformerMergeDialog
        open={mergeOpen}
        onOpenChange={setMergeOpen}
        sources={[
          {
            id: performer.id,
            name: performer.name,
            disambiguation: performer.disambiguation,
            image_path: performer.image_path,
            birthdate: performer.birthdate,
            death_date: performer.death_date,
          },
        ]}
      />
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={displayName}
        onConfirm={handleConfirmedDelete}
      />
    </>
  );
}
