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
  TagBulkEditSheet,
  type TagBulkItem,
} from "src/components/detail/tag-bulk-edit-sheet";
import {
  useBulkCardActions,
  BulkContextMenuItems,
} from "./use-bulk-card-actions";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

export interface TagContextMenuItem {
  id: string;
  name: string;
  favorite?: boolean;
}

interface UseTagContextMenuProps {
  tag: TagContextMenuItem;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export function useTagContextMenu({
  tag,
  onSelectedChanged,
  onEdit,
}: UseTagContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();

  const [updateTag] = useMutation(GQL.TagUpdateDocument);
  const [destroyTag] = useEntityMutation(GQL.TagDestroyDocument);
  const [destroyTags] = useEntityMutation(GQL.TagsDestroyDocument);

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
  } = useBulkCardActions<TagBulkItem>(tag.id);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [autoTagOpen, setAutoTagOpen] = useState(false);
  const [bulkAutoTagOpen, setBulkAutoTagOpen] = useState(false);

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
  }

  async function handleConfirmedBulkDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyTags({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Tag",
          listFieldName: "findTags",
          itemsField: "tags",
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
          noun="tags"
          openInNewTabHref={`/tags/${tag.id}`}
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
          <OpenInNewTabMenuItem href={`/tags/${tag.id}`} />
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={() =>
              updateTag({
                variables: { input: { id: tag.id, favorite: !tag.favorite } },
              })
            }
          >
            {tag.favorite
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
                  to: "/tags/$tagId",
                  params: { tagId: tag.id },
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
        entityName={tag.name}
        onConfirm={handleConfirmedDelete}
      />
      <AutoTagDialog
        open={autoTagOpen}
        onOpenChange={setAutoTagOpen}
        entityType="tag"
        ids={[tag.id]}
        entityName={tag.name}
      />
      {showBulkActions && (
        <>
          <DeleteDialog
            open={bulkDeleteOpen}
            onOpenChange={setBulkDeleteOpen}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_tags_count",
                defaultMessage: "{count} tags",
              },
              { count: bulkCount },
            )}
            onConfirm={handleConfirmedBulkDelete}
          />
          <AutoTagDialog
            open={bulkAutoTagOpen}
            onOpenChange={setBulkAutoTagOpen}
            entityType="tag"
            ids={selectedItems.map((i) => i.id)}
          />
          <TagBulkEditSheet
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

interface TagRowContextMenuProps {
  tag: TagContextMenuItem;
  children: React.ReactElement;
  onEdit?: () => void;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

export function TagRowContextMenu({
  tag,
  children,
  onEdit,
  onSelectedChanged,
}: TagRowContextMenuProps) {
  const { menuContent, dialogs, onContextMenuOpen } = useTagContextMenu({
    tag,
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
