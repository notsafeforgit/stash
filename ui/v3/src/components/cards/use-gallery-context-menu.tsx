import type React from "react";
import { useState } from "react";
import { useIntl } from "react-intl";
import { useNavigate, useRouter } from "@tanstack/react-router";
import * as GQL from "src/core/generated-graphql";
import { galleryLabel } from "src/lib/gallery-utils";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "src/components/ui/context-menu";
import { DeleteDialog } from "src/components/detail/delete-dialog";
import {
  GalleryBulkEditSheet,
  type GalleryBulkItem,
} from "src/components/detail/gallery-bulk-edit-sheet";
import {
  useBulkCardActions,
  BulkContextMenuItems,
} from "./use-bulk-card-actions";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

// Structural shape needed by the context menu — the caller passes the wider
// gallery item; we read only id/title/files/folder for label + delete confirmation.
export interface GalleryContextMenuItem {
  id: string;
  title?: string | null;
  files?: Array<{ path: string }>;
  folder?: { path: string; basename?: string | null } | null;
}

interface UseGalleryContextMenuProps {
  gallery: GalleryContextMenuItem;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export function useGalleryContextMenu({
  gallery,
  onSelectedChanged,
  onEdit,
}: UseGalleryContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();

  const [destroyGallery] = useEntityMutation(GQL.GalleryDestroyDocument);

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
  } = useBulkCardActions<GalleryBulkItem>(gallery.id);

  const [deleteOpen, setDeleteOpen] = useState(false);

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
  }

  async function handleConfirmedBulkDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    const ids = selectedItems.map((i) => i.id);
    await destroyGallery({
      variables: {
        ids,
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Gallery",
          listFieldName: "findGalleries",
          itemsField: "galleries",
          ids,
        });
      },
    });
  }

  const menuContent = (
    <ContextMenuContent>
      {showBulkActions ? (
        <BulkContextMenuItems
          count={bulkCount}
          noun="galleries"
          openInNewTabHref={`/galleries/${gallery.id}`}
          onEdit={() => setBulkEditOpen(true)}
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
          <OpenInNewTabMenuItem href={`/galleries/${gallery.id}`} />
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={
              onEdit ??
              (() =>
                navigate({
                  to: "/galleries/$galleryId",
                  params: { galleryId: gallery.id },
                  state: { returnTo: router.state.location.href },
                }))
            }
          >
            {intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}
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

  const dialogs = (
    <>
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={galleryLabel(gallery)}
        showFileOptions
        deleteFileLabel={intl.formatMessage({
          id: "dialogs.delete_gallery_files",
          defaultMessage: "Delete gallery files",
        })}
        onConfirm={handleConfirmedDelete}
      />
      {showBulkActions && (
        <>
          <DeleteDialog
            open={bulkDeleteOpen}
            onOpenChange={setBulkDeleteOpen}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_galleries_count",
                defaultMessage: "{count} galleries",
              },
              { count: bulkCount },
            )}
            showFileOptions
            deleteFileLabel={intl.formatMessage({
              id: "dialogs.delete_gallery_files",
              defaultMessage: "Delete gallery files",
            })}
            onConfirm={handleConfirmedBulkDelete}
          />
          <GalleryBulkEditSheet
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

interface GalleryRowContextMenuProps {
  gallery: GalleryContextMenuItem;
  children: React.ReactElement;
  onEdit?: () => void;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

export function GalleryRowContextMenu({
  gallery,
  children,
  onEdit,
  onSelectedChanged,
}: GalleryRowContextMenuProps) {
  const { menuContent, dialogs, onContextMenuOpen } = useGalleryContextMenu({
    gallery,
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
