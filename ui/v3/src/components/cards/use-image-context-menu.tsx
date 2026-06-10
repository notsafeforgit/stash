import type React from "react";
import { useState } from "react";
import { useIntl } from "react-intl";
import { useNavigate, useRouter } from "@tanstack/react-router";
import * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "src/components/ui/context-menu";
import {
  DeleteDialog,
  DeleteFilesList,
} from "src/components/detail/delete-dialog";
import { ImageGenerateDialog } from "src/components/detail/image-generate-dialog";
import {
  ImageBulkEditSheet,
  type ImageBulkItem,
} from "src/components/detail/image-bulk-edit-sheet";
import {
  useBulkCardActions,
  BulkContextMenuItems,
} from "./use-bulk-card-actions";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

// Local re-declaration of the image shape SceneCard uses. We can't import
// `ImageCardImage` from image-card.tsx without creating a circular runtime
// dependency, but the type is structural — only `id` and `visual_files`
// are read here. The wider shape (paths/studio/etc.) is supplied by the
// caller via the generic.
export interface ImageContextMenuItem {
  id: string;
  visual_files: Array<{ path: string }>;
}

interface UseImageContextMenuProps {
  image: ImageContextMenuItem;
  /** Forwarded to the "Select" menu item; absent for callers without selection. */
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  /** Edit handler. When absent, falls back to navigating to detail. */
  onEdit?: () => void;
  /** Optional cover-setter actions; surfaced only when supplied. */
  onSetGalleryCover?: () => void;
  onSetPerformerImage?: () => void;
  onSetStudioImage?: () => void;
  onSetTagImage?: () => void;
}

/**
 * Owns the default image context-menu wiring shared by ImageCard and the
 * images table row: dialog state, destroy mutations, bulk-action snapshot,
 * and the menu/dialog JSX. Callers stitch the returned pieces into their
 * trigger element.
 */
export function useImageContextMenu({
  image,
  onSelectedChanged,
  onEdit,
  onSetGalleryCover,
  onSetPerformerImage,
  onSetStudioImage,
  onSetTagImage,
}: UseImageContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();

  const [destroyImage] = useEntityMutation(GQL.ImageDestroyDocument, {
    update(cache) {
      removeEntitiesFromCache({
        cache,
        typename: "Image",
        listFieldName: "findImages",
        itemsField: "images",
        ids: [image.id],
      });
    },
  });
  const [destroyImages] = useEntityMutation(GQL.ImagesDestroyDocument);

  const {
    selectedItems,
    totalCount,
    applyToAllTarget,
    bulkCount,
    showBulkActions,
    bulkDeleteOpen,
    setBulkDeleteOpen,
    bulkEditOpen,
    setBulkEditOpen,
    onContextMenuOpen,
  } = useBulkCardActions<ImageBulkItem>(image.id);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [generateOpen, setGenerateOpen] = useState(false);
  const [bulkGenerateOpen, setBulkGenerateOpen] = useState(false);

  async function handleConfirmedDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    await destroyImage({
      variables: {
        id: image.id,
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
    });
  }

  async function handleConfirmedBulkDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    const ids = selectedItems.map((i) => i.id);
    await destroyImages({
      variables: {
        ids,
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Image",
          listFieldName: "findImages",
          itemsField: "images",
          ids,
        });
      },
    });
  }

  const hasCoverSetters =
    onSetGalleryCover ||
    onSetPerformerImage ||
    onSetStudioImage ||
    onSetTagImage;

  const menuContent = (
    <ContextMenuContent>
      {showBulkActions ? (
        <BulkContextMenuItems
          count={bulkCount}
          noun="images"
          openInNewTabHref={`/images/${image.id}`}
          onEdit={() => setBulkEditOpen(true)}
          onGenerate={() => setBulkGenerateOpen(true)}
          onDelete={() => setBulkDeleteOpen(true)}
        />
      ) : (
        <>
          {onSelectedChanged && (
            <ContextMenuItem onClick={() => onSelectedChanged(true, false)}>
              {intl.formatMessage({
                id: "actions.select",
                defaultMessage: "Select",
              })}
            </ContextMenuItem>
          )}
          <SelectAllMenuItem />
          <OpenInNewTabMenuItem href={`/images/${image.id}`} />
          <ContextMenuSeparator />
          {onSetGalleryCover && (
            <ContextMenuItem onClick={onSetGalleryCover}>
              {intl.formatMessage({
                id: "actions.set_as_gallery_cover",
                defaultMessage: "Set as gallery cover",
              })}
            </ContextMenuItem>
          )}
          {onSetPerformerImage && (
            <ContextMenuItem onClick={onSetPerformerImage}>
              {intl.formatMessage({
                id: "actions.set_as_performer_image",
                defaultMessage: "Set as performer image",
              })}
            </ContextMenuItem>
          )}
          {onSetStudioImage && (
            <ContextMenuItem onClick={onSetStudioImage}>
              {intl.formatMessage({
                id: "actions.set_as_studio_image",
                defaultMessage: "Set as studio image",
              })}
            </ContextMenuItem>
          )}
          {onSetTagImage && (
            <ContextMenuItem onClick={onSetTagImage}>
              {intl.formatMessage({
                id: "actions.set_as_tag_image",
                defaultMessage: "Set as tag image",
              })}
            </ContextMenuItem>
          )}
          {hasCoverSetters && <ContextMenuSeparator />}
          <ContextMenuItem
            onClick={
              onEdit ??
              (() =>
                navigate({
                  to: "/images/$imageId",
                  params: { imageId: image.id },
                  state: { returnTo: router.state.location.href },
                }))
            }
          >
            {intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}
            …
          </ContextMenuItem>
          <ContextMenuItem onClick={() => setGenerateOpen(true)}>
            {intl.formatMessage({
              id: "actions.generate",
              defaultMessage: "Generate",
            })}
            …
          </ContextMenuItem>
          <ContextMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
          >
            {intl.formatMessage({
              id: "actions.delete",
              defaultMessage: "Delete",
            })}
            …
          </ContextMenuItem>
        </>
      )}
    </ContextMenuContent>
  );

  // Cast helper: selectedItems carries visual_files at runtime via the
  // list-page slim fragment, even though ImageBulkItem doesn't model it.
  const bulkVisualFilePaths = (
    selectedItems as unknown as readonly ImageContextMenuItem[]
  ).flatMap((i) => i.visual_files?.map((f) => f.path) ?? []);

  const dialogs = (
    <>
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={imageTitle(image as Parameters<typeof imageTitle>[0])}
        showFileOptions
        deleteFileLabel={intl.formatMessage({
          id: "dialogs.delete_file",
          defaultMessage: "Delete file",
        })}
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
        onConfirm={handleConfirmedDelete}
      />
      <ImageGenerateDialog
        open={generateOpen}
        onOpenChange={setGenerateOpen}
        imageIds={[image.id]}
      />
      {showBulkActions && (
        <>
          <DeleteDialog
            open={bulkDeleteOpen}
            onOpenChange={setBulkDeleteOpen}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_images_count",
                defaultMessage: "{count} images",
              },
              { count: bulkCount },
            )}
            showFileOptions
            deleteFileLabel={intl.formatMessage({
              id: "dialogs.delete_file",
              defaultMessage: "Delete file",
            })}
            details={
              bulkVisualFilePaths.length > 0 ? (
                <DeleteFilesList paths={bulkVisualFilePaths} />
              ) : undefined
            }
            detailsLabel={intl.formatMessage(
              {
                id: "dialogs.delete_show_files_count",
                defaultMessage:
                  "Show {count, plural, one {# file} other {# files}}",
              },
              { count: bulkVisualFilePaths.length },
            )}
            onConfirm={handleConfirmedBulkDelete}
          />
          <ImageGenerateDialog
            open={bulkGenerateOpen}
            onOpenChange={setBulkGenerateOpen}
            imageIds={selectedItems.map((i) => i.id)}
          />
          <ImageBulkEditSheet
            open={bulkEditOpen}
            onOpenChange={setBulkEditOpen}
            items={selectedItems}
            applyToAllTarget={applyToAllTarget}
            totalCount={totalCount}
          />
        </>
      )}
    </>
  );

  return { menuContent, dialogs, onContextMenuOpen };
}

// ── Row wrapper ───────────────────────────────────────────────────────────────

interface ImageRowContextMenuProps {
  image: ImageContextMenuItem;
  children: React.ReactElement;
  onEdit?: () => void;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

/**
 * Wraps an image-table row's JSX with the same context menu the card view
 * uses. Uses Base UI's `render` prop so the trigger stays a `<tr>` (no
 * wrapping `<div>` that would break table semantics).
 */
export function ImageRowContextMenu({
  image,
  children,
  onEdit,
  onSelectedChanged,
}: ImageRowContextMenuProps) {
  const { menuContent, dialogs, onContextMenuOpen } = useImageContextMenu({
    image,
    onEdit,
    onSelectedChanged,
  });
  return (
    <>
      <ContextMenu
        onOpenChange={(open) => {
          if (open) onContextMenuOpen();
        }}
      >
        <ContextMenuTrigger render={children} />
        {menuContent}
      </ContextMenu>
      {dialogs}
    </>
  );
}
