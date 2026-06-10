import { useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import { Cog, EllipsisVertical, RefreshCcw, Trash2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import {
  DeleteDialog,
  DeleteFilesList,
} from "src/components/detail/delete-dialog";
import { ImageGenerateDialog } from "src/components/detail/image-generate-dialog";
import { useToast } from "src/hooks/toast";
import { imagePath, imageTitle } from "src/core/files";

export interface ImageActionsMenuProps {
  image: NonNullable<GQL.FindImageQuery["findImage"]>;
  onDeleted?: () => void;
}

export function ImageActionsMenu({ image, onDeleted }: ImageActionsMenuProps) {
  const intl = useIntl();
  const toast = useToast();

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [generateOpen, setGenerateOpen] = useState(false);

  const [scan] = useMutation(GQL.MetadataScanDocument);
  const [destroyImage] = useEntityMutation(GQL.ImageDestroyDocument);

  const filePath = image.visual_files.length > 0 ? imagePath(image) : null;

  async function handleRescan() {
    if (!filePath) return;
    try {
      await scan({ variables: { input: { paths: [filePath] } } });
      toast.success(
        intl.formatMessage(
          {
            id: "toast.rescanning_entity",
            defaultMessage: "Rescanning {count} {singularEntity}",
          },
          {
            count: 1,
            singularEntity: intl
              .formatMessage({ id: "image", defaultMessage: "image" })
              .toLocaleLowerCase(),
          },
        ),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  async function handleDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    const imageId = image.id;
    await destroyImage({
      variables: {
        id: imageId,
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Image",
          listFieldName: "findImages",
          itemsField: "images",
          ids: [imageId],
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
          {filePath && (
            <DropdownMenuItem onClick={handleRescan}>
              <RefreshCcw />
              {intl.formatMessage({
                id: "actions.rescan",
                defaultMessage: "Rescan",
              })}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onClick={() => setGenerateOpen(true)}>
            <Cog />
            {intl.formatMessage({
              id: "actions.generate",
              defaultMessage: "Generate",
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
                  .formatMessage({ id: "image", defaultMessage: "image" })
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
        entityName={imageTitle(image) || undefined}
        showFileOptions
        details={
          image.visual_files.length > 0 ? (
            <DeleteFilesList paths={image.visual_files.map((f) => f.path)} />
          ) : undefined
        }
        detailsLabel={intl.formatMessage(
          {
            id: "dialogs.delete_show_files_count",
            defaultMessage:
              "Show {count, plural, one {# file} other {# files}}",
          },
          { count: image.visual_files.length },
        )}
        onConfirm={handleDelete}
      />
      <ImageGenerateDialog
        open={generateOpen}
        onOpenChange={setGenerateOpen}
        imageIds={[image.id]}
      />
    </>
  );
}
