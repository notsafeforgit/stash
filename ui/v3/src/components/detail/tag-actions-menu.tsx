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

  const items: EntityActionItem[] = [
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
              .formatMessage({ id: "tag", defaultMessage: "tag" })
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
