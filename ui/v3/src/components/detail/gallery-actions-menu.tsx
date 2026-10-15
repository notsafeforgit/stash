import { useState } from "react";
import { useIntl } from "react-intl";
import { Trash2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import {
  EntityActionsMenu,
  type EntityActionItem,
} from "./entity-actions-menu";
import { GalleryDeleteDialog } from "./deferred-overlays";
import { galleryLabel } from "src/lib/gallery-utils";
import { useShareAction } from "@/components/sharing/share-action";

interface GalleryActionsMenuProps {
  gallery: NonNullable<GQL.FindGalleryQuery["findGallery"]>;
  /** Called once the gallery has been deleted so the page can navigate away. */
  onDeleted?: () => void;
}

export function GalleryActionsMenu({
  gallery,
  onDeleted,
}: GalleryActionsMenuProps) {
  const intl = useIntl();
  const share = useShareAction([
    {
      kind: GQL.ShareEntityKind.Gallery,
      id: gallery.id,
      name: galleryLabel(gallery),
    },
  ]);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const items: EntityActionItem[] = [
    share.action,
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
              .formatMessage({ id: "gallery", defaultMessage: "gallery" })
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

      <GalleryDeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        ids={[gallery.id]}
        entityName={galleryLabel(gallery)}
        onDeleted={onDeleted}
      />
    </>
  );
}
