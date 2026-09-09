import { useState } from "react";
import { useIntl } from "react-intl";
import { Trash2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  EntityActionsMenu,
  type EntityActionItem,
} from "./entity-actions-menu";
import {
  DeleteDialog,
  DeleteFilesList,
} from "src/components/detail/delete-dialog";
import { galleryLabel } from "src/lib/gallery-utils";

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
  const [deleteOpen, setDeleteOpen] = useState(false);

  const [destroyGallery] = useEntityMutation(GQL.GalleryDestroyDocument);

  async function handleConfirmedDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    await destroyGallery({
      variables: {
        ids: [gallery.id],
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Gallery",
          listFieldName: "findGalleries",
          itemsField: "galleries",
          ids: [gallery.id],
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

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={galleryLabel(gallery)}
        showFileOptions
        deleteFileLabel={intl.formatMessage({
          id: "dialogs.delete_gallery_files",
          defaultMessage: "Delete gallery files",
        })}
        details={
          gallery.files.length > 0 ? (
            <DeleteFilesList paths={gallery.files.map((f) => f.path)} />
          ) : undefined
        }
        detailsLabel={intl.formatMessage(
          {
            id: "dialogs.delete_show_files_count",
            defaultMessage:
              "Show {count, plural, one {# file} other {# files}}",
          },
          { count: gallery.files.length },
        )}
        onConfirm={handleConfirmedDelete}
      />
    </>
  );
}
