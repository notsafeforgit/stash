import type React from "react";
import { useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import { useNavigate, useRouter } from "@tanstack/react-router";
import * as GQL from "src/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "src/components/ui/context-menu";
import { DeleteDialog } from "src/components/detail/delete-dialog";
import { AutoTagDialog } from "src/components/detail/auto-tag-dialog";
import {
  StudioBulkEditSheet,
  type StudioBulkItem,
} from "src/components/detail/studio-bulk-edit-sheet";
import {
  useBulkCardActions,
  BulkContextMenuItems,
} from "./use-bulk-card-actions";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

export interface StudioContextMenuItem {
  id: string;
  name: string;
  favorite?: boolean;
}

interface UseStudioContextMenuProps {
  studio: StudioContextMenuItem;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export function useStudioContextMenu({
  studio,
  onSelectedChanged,
  onEdit,
}: UseStudioContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();

  const [updateStudio] = useMutation(GQL.StudioUpdateDocument);
  const [destroyStudio] = useEntityMutation(GQL.StudioDestroyDocument);
  const [destroyStudios] = useEntityMutation(GQL.StudiosDestroyDocument);

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
  } = useBulkCardActions<StudioBulkItem>(studio.id);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [autoTagOpen, setAutoTagOpen] = useState(false);
  const [bulkAutoTagOpen, setBulkAutoTagOpen] = useState(false);

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
  }

  async function handleConfirmedBulkDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyStudios({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Studio",
          listFieldName: "findStudios",
          itemsField: "studios",
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
          noun="studios"
          openInNewTabHref={`/studios/${studio.id}`}
          onEdit={() => setBulkEditOpen(true)}
          onAutoTag={() => setBulkAutoTagOpen(true)}
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
          <OpenInNewTabMenuItem href={`/studios/${studio.id}`} />
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={() =>
              updateStudio({
                variables: {
                  input: { id: studio.id, favorite: !studio.favorite },
                },
              })
            }
          >
            {studio.favorite
              ? intl.formatMessage({
                  id: "actions.unfavorite",
                  defaultMessage: "Remove from favorites",
                })
              : intl.formatMessage({
                  id: "actions.favorite",
                  defaultMessage: "Add to favorites",
                })}
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={
              onEdit ??
              (() =>
                navigate({
                  to: "/studios/$studioId",
                  params: { studioId: studio.id },
                  state: { returnTo: router.state.location.href },
                }))
            }
          >
            {intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}
            …
          </ContextMenuItem>
          <ContextMenuItem onClick={() => setAutoTagOpen(true)}>
            {intl.formatMessage({
              id: "actions.auto_tag",
              defaultMessage: "Auto tag",
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

  const dialogs = (
    <>
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={studio.name}
        onConfirm={handleConfirmedDelete}
      />
      <AutoTagDialog
        open={autoTagOpen}
        onOpenChange={setAutoTagOpen}
        entityType="studio"
        ids={[studio.id]}
        entityName={studio.name}
      />
      {showBulkActions && (
        <>
          <DeleteDialog
            open={bulkDeleteOpen}
            onOpenChange={setBulkDeleteOpen}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_studios_count",
                defaultMessage: "{count} studios",
              },
              { count: bulkCount },
            )}
            onConfirm={handleConfirmedBulkDelete}
          />
          <AutoTagDialog
            open={bulkAutoTagOpen}
            onOpenChange={setBulkAutoTagOpen}
            entityType="studio"
            ids={selectedItems.map((i) => i.id)}
          />
          <StudioBulkEditSheet
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

interface StudioRowContextMenuProps {
  studio: StudioContextMenuItem;
  children: React.ReactElement;
  onEdit?: () => void;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

export function StudioRowContextMenu({
  studio,
  children,
  onEdit,
  onSelectedChanged,
}: StudioRowContextMenuProps) {
  const { menuContent, dialogs, onContextMenuOpen } = useStudioContextMenu({
    studio,
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
