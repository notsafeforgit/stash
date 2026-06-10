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
import { PerformerMergeDialog } from "src/components/detail/performer-merge-dialog";
import {
  PerformerBulkEditSheet,
  type PerformerBulkItem,
} from "src/components/detail/performer-bulk-edit-sheet";
import {
  useBulkCardActions,
  BulkContextMenuItems,
} from "./use-bulk-card-actions";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

export interface PerformerContextMenuItem {
  id: string;
  name: string;
  disambiguation?: string | null;
  image_path?: string | null;
  birthdate?: string | null;
  death_date?: string | null;
  favorite?: boolean;
}

interface UsePerformerContextMenuProps {
  performer: PerformerContextMenuItem;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export function usePerformerContextMenu({
  performer,
  onSelectedChanged,
  onEdit,
}: UsePerformerContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();

  const [updatePerformer] = useMutation(GQL.PerformerUpdateDocument);
  const [destroyPerformer] = useEntityMutation(GQL.PerformerDestroyDocument);
  const [destroyPerformers] = useEntityMutation(GQL.PerformersDestroyDocument);

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
  } = useBulkCardActions<PerformerBulkItem>(performer.id);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [autoTagOpen, setAutoTagOpen] = useState(false);
  const [bulkAutoTagOpen, setBulkAutoTagOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [bulkMergeOpen, setBulkMergeOpen] = useState(false);

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
  }

  async function handleConfirmedBulkDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyPerformers({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Performer",
          listFieldName: "findPerformers",
          itemsField: "performers",
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
          noun="performers"
          openInNewTabHref={`/performers/${performer.id}`}
          onEdit={() => setBulkEditOpen(true)}
          onAutoTag={() => setBulkAutoTagOpen(true)}
          onMerge={() => setBulkMergeOpen(true)}
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
          <OpenInNewTabMenuItem href={`/performers/${performer.id}`} />
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={() =>
              updatePerformer({
                variables: {
                  input: { id: performer.id, favorite: !performer.favorite },
                },
              })
            }
          >
            {performer.favorite
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
                  to: "/performers/$performerId",
                  params: { performerId: performer.id },
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
          <ContextMenuItem onClick={() => setMergeOpen(true)}>
            {intl.formatMessage({
              id: "actions.merge_into",
              defaultMessage: "Merge into…",
            })}
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
        entityName={displayName}
        onConfirm={handleConfirmedDelete}
      />
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
      {showBulkActions && (
        <>
          <DeleteDialog
            open={bulkDeleteOpen}
            onOpenChange={setBulkDeleteOpen}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_performers_count",
                defaultMessage: "{count} performers",
              },
              { count: bulkCount },
            )}
            onConfirm={handleConfirmedBulkDelete}
          />
          <AutoTagDialog
            open={bulkAutoTagOpen}
            onOpenChange={setBulkAutoTagOpen}
            entityType="performer"
            ids={selectedItems.map((i) => i.id)}
          />
          <PerformerMergeDialog
            open={bulkMergeOpen}
            onOpenChange={setBulkMergeOpen}
            sources={selectedItems.map((i) => ({
              id: i.id,
              name: i.name,
              disambiguation: i.disambiguation,
              image_path: i.image_path,
              birthdate: i.birthdate,
              death_date: i.death_date,
            }))}
          />
          <PerformerBulkEditSheet
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

interface PerformerRowContextMenuProps {
  performer: PerformerContextMenuItem;
  children: React.ReactElement;
  onEdit?: () => void;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

export function PerformerRowContextMenu({
  performer,
  children,
  onEdit,
  onSelectedChanged,
}: PerformerRowContextMenuProps) {
  const { menuContent, dialogs, onContextMenuOpen } = usePerformerContextMenu({
    performer,
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
