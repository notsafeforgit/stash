import type React from "react";
import { useState } from "react";
import { useIntl } from "react-intl";
import { useNavigate, useRouter } from "@tanstack/react-router";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
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
import { SceneGenerateDialog } from "src/components/detail/scene-generate-dialog";
import { SceneMergeDialog } from "src/components/detail/scene-merge-dialog";
import {
  SceneBulkEditSheet,
  type SceneBulkItem,
} from "src/components/detail/scene-bulk-edit-sheet";
import { SceneCardDownloadMenuItem } from "src/components/offline/scene-card-download-menu-item";
import { useBulkSceneDownload } from "src/components/offline/download-action";
import {
  useBulkCardActions,
  BulkContextMenuItems,
} from "./use-bulk-card-actions";
import { OpenInNewTabMenuItem } from "./open-in-new-tab-menu-item";
import { SelectAllMenuItem } from "./select-all-menu-item";
import type { SceneCardScene } from "./scene-card";

interface UseSceneContextMenuProps {
  scene: SceneCardScene;
  /** Forwarded to the "Select" menu item; absent for callers without selection (e.g. embedded panels). */
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  /** Edit handler. When absent, falls back to navigating to the detail page. */
  onEdit?: () => void;
}

/**
 * Owns the default scene context-menu wiring shared by SceneCard and the
 * scenes table row: dialog state, destroy mutations, bulk-action snapshot,
 * and the menu/dialog JSX. Callers stitch the returned pieces into their
 * trigger element.
 */
export function useSceneContextMenu({
  scene,
  onSelectedChanged,
  onEdit,
}: UseSceneContextMenuProps) {
  const intl = useIntl();
  const navigate = useNavigate();
  const router = useRouter();

  const [destroyScene] = useEntityMutation(GQL.SceneDestroyDocument);
  const [destroyScenes] = useEntityMutation(GQL.ScenesDestroyDocument);

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
  } = useBulkCardActions<SceneBulkItem>(scene.id);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [generateOpen, setGenerateOpen] = useState(false);
  const [bulkGenerateOpen, setBulkGenerateOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [bulkMergeOpen, setBulkMergeOpen] = useState(false);

  const bulkDownload = useBulkSceneDownload();

  async function handleConfirmedDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    const sceneId = scene.id;
    await destroyScene({
      variables: {
        id: sceneId,
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Scene",
          listFieldName: "findScenes",
          itemsField: "scenes",
          ids: [sceneId],
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
    await destroyScenes({
      variables: {
        ids,
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Scene",
          listFieldName: "findScenes",
          itemsField: "scenes",
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
          noun="scenes"
          openInNewTabHref={`/scenes/${scene.id}`}
          onEdit={() => setBulkEditOpen(true)}
          onGenerate={() => setBulkGenerateOpen(true)}
          onMerge={() => setBulkMergeOpen(true)}
          onDownload={() =>
            // selectedItems is typed as SceneBulkItem[] for the bulk-edit
            // sheet's needs, but the runtime objects come from the list
            // provider whose TItem is SceneCardScene (the card's actual
            // item type). Cast back to the wider shape so the download
            // path can read codecs / dimensions.
            void bulkDownload(
              selectedItems as unknown as readonly SceneCardScene[],
            )
          }
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
          <OpenInNewTabMenuItem href={`/scenes/${scene.id}`} />
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={
              onEdit ??
              (() =>
                navigate({
                  to: "/scenes/$sceneId",
                  params: { sceneId: scene.id },
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
          <ContextMenuItem onClick={() => setMergeOpen(true)}>
            {intl.formatMessage({
              id: "actions.merge_into",
              defaultMessage: "Merge into…",
            })}
          </ContextMenuItem>
          <ContextMenuSeparator />
          <SceneCardDownloadMenuItem scene={scene} />
          <ContextMenuSeparator />
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
        entityName={objectTitle(scene) || undefined}
        showFileOptions
        deleteFileLabel={intl.formatMessage({
          id: "dialogs.delete_file_and_funscript",
          defaultMessage: "Delete file and funscript",
        })}
        details={
          scene.files.length > 0 ? (
            <DeleteFilesList paths={scene.files.map((f) => f.path)} />
          ) : undefined
        }
        detailsLabel={intl.formatMessage(
          {
            id: "dialogs.delete_show_files_count",
            defaultMessage:
              "Show {count, plural, one {# file} other {# files}}",
          },
          { count: scene.files.length },
        )}
        onConfirm={handleConfirmedDelete}
      />
      <SceneGenerateDialog
        open={generateOpen}
        onOpenChange={setGenerateOpen}
        sceneIds={[scene.id]}
        hasMarkers={(scene.scene_markers?.length ?? 0) > 0}
        hasInteractive={scene.interactive ?? false}
      />
      <SceneMergeDialog
        open={mergeOpen}
        onOpenChange={setMergeOpen}
        sources={[
          {
            id: scene.id,
            title: objectTitle(scene),
            date: scene.date ?? null,
            paths: { screenshot: scene.paths.screenshot ?? null },
            studio: scene.studio ? { name: scene.studio.name } : null,
            performers: scene.performers?.map((p) => ({ name: p.name })) ?? [],
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
                id: "dialogs.delete_scenes_count",
                defaultMessage: "{count} scenes",
              },
              { count: bulkCount },
            )}
            showFileOptions
            deleteFileLabel={intl.formatMessage({
              id: "dialogs.delete_file_and_funscript",
              defaultMessage: "Delete file and funscript",
            })}
            details={(() => {
              const items =
                selectedItems as unknown as readonly SceneCardScene[];
              const paths = items.flatMap(
                (s) => s.files?.map((f) => f.path) ?? [],
              );
              return paths.length > 0 ? (
                <DeleteFilesList paths={paths} />
              ) : undefined;
            })()}
            detailsLabel={intl.formatMessage(
              {
                id: "dialogs.delete_show_files_count",
                defaultMessage:
                  "Show {count, plural, one {# file} other {# files}}",
              },
              {
                count: (
                  selectedItems as unknown as readonly SceneCardScene[]
                ).reduce((n, s) => n + (s.files?.length ?? 0), 0),
              },
            )}
            onConfirm={handleConfirmedBulkDelete}
          />
          <SceneGenerateDialog
            open={bulkGenerateOpen}
            onOpenChange={setBulkGenerateOpen}
            sceneIds={selectedItems.map((i) => i.id)}
          />
          <SceneMergeDialog
            open={bulkMergeOpen}
            onOpenChange={setBulkMergeOpen}
            sources={selectedItems.map((i) => ({
              id: i.id,
              title: i.title,
              date: i.date ?? null,
              paths: { screenshot: i.paths?.screenshot ?? null },
              studio: i.studio ? { name: i.studio.name } : null,
              performers: i.performers?.map((p) => ({ name: p.name })) ?? [],
            }))}
          />
          <SceneBulkEditSheet
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

interface SceneRowContextMenuProps {
  scene: SceneCardScene;
  /** The trigger element — typically a `<TableRow>`. */
  children: React.ReactElement;
  onEdit?: () => void;
  /**
   * Called when the user picks "Select" from the menu. Wires the row into
   * the same selection flow the card view uses, so picking it puts the user
   * into select mode (and the checkbox column appears).
   */
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
}

/**
 * Wraps a scene-table row's JSX with the same context menu the card view
 * uses. The trigger element (`children`) becomes the right-click target via
 * Base UI's `render` prop, so it stays a `<tr>` in the DOM (no wrapping
 * `<div>` that would break table semantics).
 */
export function SceneRowContextMenu({
  scene,
  children,
  onEdit,
  onSelectedChanged,
}: SceneRowContextMenuProps) {
  const { menuContent, dialogs, onContextMenuOpen } = useSceneContextMenu({
    scene,
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
