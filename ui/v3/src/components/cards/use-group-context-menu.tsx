import type React from "react";
import { useState } from "react";
import { useIntl } from "react-intl";
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
import {
  GroupBulkEditSheet,
  type GroupBulkItem,
} from "src/components/detail/group-bulk-edit-sheet";
import {
  useBulkCardActions,
  BulkContextMenuItems,
} from "./use-bulk-card-actions";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

export interface GroupContextMenuItem {
  id: string;
  name: string;
}

interface UseGroupContextMenuProps {
  group: GroupContextMenuItem;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export function useGroupContextMenu({
  group,
  onSelectedChanged,
  onEdit,
}: UseGroupContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();

  const [destroyGroup] = useEntityMutation(GQL.GroupDestroyDocument);
  const [destroyGroups] = useEntityMutation(GQL.GroupsDestroyDocument);

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
  } = useBulkCardActions<GroupBulkItem>(group.id);

  const [deleteOpen, setDeleteOpen] = useState(false);

  async function handleConfirmedDelete() {
    await destroyGroup({
      variables: { id: group.id },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Group",
          listFieldName: "findGroups",
          itemsField: "groups",
          ids: [group.id],
        });
      },
    });
  }

  async function handleConfirmedBulkDelete() {
    const ids = selectedItems.map((i) => i.id);
    await destroyGroups({
      variables: { ids },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Group",
          listFieldName: "findGroups",
          itemsField: "groups",
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
          noun="groups"
          openInNewTabHref={`/groups/${group.id}`}
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
          <OpenInNewTabMenuItem href={`/groups/${group.id}`} />
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={
              onEdit ??
              (() =>
                navigate({
                  to: "/groups/$groupId",
                  params: { groupId: group.id },
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
        entityName={group.name}
        onConfirm={handleConfirmedDelete}
      />
      {showBulkActions && (
        <>
          <DeleteDialog
            open={bulkDeleteOpen}
            onOpenChange={setBulkDeleteOpen}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_groups_count",
                defaultMessage: "{count} groups",
              },
              { count: bulkCount },
            )}
            onConfirm={handleConfirmedBulkDelete}
          />
          <GroupBulkEditSheet
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

interface GroupRowContextMenuProps {
  group: GroupContextMenuItem;
  children: React.ReactElement;
  onEdit?: () => void;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

export function GroupRowContextMenu({
  group,
  children,
  onEdit,
  onSelectedChanged,
}: GroupRowContextMenuProps) {
  const { menuContent, dialogs, onContextMenuOpen } = useGroupContextMenu({
    group,
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
