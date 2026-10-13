import type React from "react";
import { useState } from "react";
import { useIntl } from "react-intl";
import { useNavigate, useRouter } from "@tanstack/react-router";
import * as GQL from "@/core/generated-graphql";
import { removeEntitiesFromCache, useEntityMutation } from "@/core/client";
import { entityDestination } from "@/core/navigation";
import { DeleteDialog } from "@/components/detail/delete-dialog";
import {
  ContextMenu,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { EntityContextMenuContent } from "./entity-context-menu-content";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";

interface MarkerContextMenuProps {
  marker: Pick<GQL.SceneMarkerDataFragment, "id" | "seconds" | "scene">;
  title: string;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

export function useMarkerContextMenu({
  marker,
  title,
  onSelectedChanged,
}: MarkerContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [destroyMarker] = useEntityMutation(GQL.SceneMarkerDestroyDocument);
  const destination = entityDestination.scene(marker.scene.id, {
    tab: "markers",
    t: marker.seconds,
  });

  async function confirmDelete() {
    const result = await destroyMarker({
      variables: { id: marker.id },
      update(cache, { data }) {
        if (!data?.sceneMarkerDestroy) return;
        removeEntitiesFromCache({
          cache,
          typename: "SceneMarker",
          listFieldName: "findSceneMarkers",
          itemsField: "scene_markers",
          ids: [marker.id],
        });
      },
    });
    if (!result.data?.sceneMarkerDestroy)
      throw new Error(
        intl.formatMessage({ id: "dialogs.delete_marker_failed" }),
      );
  }

  return {
    menuContent: (
      <EntityContextMenuContent title={title}>
        {onSelectedChanged && (
          <ContextMenuItem onClick={() => onSelectedChanged(true, false)}>
            {intl.formatMessage({
              id: "actions.select",
              defaultMessage: "Select",
            })}
          </ContextMenuItem>
        )}
        <SelectAllMenuItem />
        <OpenInNewTabMenuItem href={router.buildLocation(destination).href} />
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() =>
            navigate({
              ...destination,
              state: { returnTo: router.state.location.href },
            })
          }
        >
          {intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}…
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
      </EntityContextMenuContent>
    ),
    dialogs: deleteOpen ? (
      <DeleteDialog
        open
        onOpenChange={setDeleteOpen}
        entityName={title}
        onConfirm={confirmDelete}
      />
    ) : null,
  };
}

export function MarkerRowContextMenu({
  children,
  ...props
}: MarkerContextMenuProps & { children: React.ReactElement }) {
  const { menuContent, dialogs } = useMarkerContextMenu(props);
  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger render={children} />
        {menuContent}
      </ContextMenu>
      {dialogs}
    </>
  );
}
