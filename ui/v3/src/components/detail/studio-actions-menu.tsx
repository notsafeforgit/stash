import { useState } from "react";
import { useIntl } from "react-intl";
import { Trash2, Wand2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  EntityActionsMenu,
  type EntityActionItem,
} from "./entity-actions-menu";
import { AutoTagDialog } from "src/components/detail/auto-tag-dialog";
import { DeleteDialog } from "src/components/detail/delete-dialog";
import { useShareAction } from "@/components/sharing/share-action";

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
  const share = useShareAction([], undefined, {
    kind: GQL.ShareLibraryKind.Studio,
    id: studio.id,
    name: studio.name,
  });
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

  const items: EntityActionItem[] = [
    share.action,
    {
      key: "auto-tag",
      icon: Wand2,
      label:
        intl.formatMessage({
          id: "actions.auto_tag",
          defaultMessage: "Auto tag",
        }) + "…",
      onSelect: () => setAutoTagOpen(true),
    },
    { key: "delete-separator", separator: true },
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
              .formatMessage({ id: "studio", defaultMessage: "studio" })
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
      {share.dialog}

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
