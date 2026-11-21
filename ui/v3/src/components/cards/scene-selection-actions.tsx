import { useMemo, type PropsWithChildren } from "react";
import { useIntl } from "react-intl";
import { SceneListItems } from "@/components/list/entity-list-items";
import {
  BulkSelectionActionsProvider,
  useSelectionDialogs,
} from "@/components/list/selection-actions";
import { useBulkMediaDelete } from "@/components/detail/use-bulk-media-delete";
import {
  DeleteDialog,
  DeleteFilesList,
} from "@/components/detail/delete-dialog";
import {
  SceneBulkEditSheet,
  SceneGenerateDialog,
  SceneMergeDialog,
} from "@/components/detail/deferred-overlays";
import { useBulkSceneDownload } from "@/components/offline/download-action";

export function SceneSelectionActions({ children }: PropsWithChildren) {
  const intl = useIntl();
  const {
    selectedItems,
    capture,
    open,
    dialogProps,
    totalCount,
    applyToAllTarget,
  } = useSelectionDialogs(
    SceneListItems.useItems(),
    SceneListItems.useSelectionGetter(),
  );
  const deleteScenes = useBulkMediaDelete("Scene");
  const download = useBulkSceneDownload();

  const actions = useMemo(
    () => ({
      noun: "scenes",
      onEdit: () => open("edit"),
      onGenerate: () => open("generate"),
      onMerge: () => open("merge"),
      onDownload: () => void download(capture()),
      onDelete: () => open("delete"),
    }),
    [open, capture, download],
  );

  const paths = selectedItems.flatMap((s) => s.files?.map((f) => f.path) ?? []);

  return (
    <>
      <BulkSelectionActionsProvider value={actions}>
        {children}
      </BulkSelectionActionsProvider>
      {selectedItems.length > 0 && (
        <>
          <DeleteDialog
            {...dialogProps("delete")}
            entityCountLabel={intl.formatMessage(
              {
                id: "dialogs.delete_scenes_count",
                defaultMessage: "{count} scenes",
              },
              { count: selectedItems.length },
            )}
            showFileOptions
            deleteFileLabel={intl.formatMessage({
              id: "dialogs.delete_file_and_funscript",
              defaultMessage: "Delete file and funscript",
            })}
            details={
              paths.length > 0 ? <DeleteFilesList paths={paths} /> : undefined
            }
            detailsLabel={intl.formatMessage(
              {
                id: "dialogs.delete_show_files_count",
                defaultMessage:
                  "Show {count, plural, one {# file} other {# files}}",
              },
              { count: paths.length },
            )}
            onConfirm={(options, onProgress) =>
              deleteScenes(
                selectedItems.map((i) => i.id),
                options,
                onProgress,
              )
            }
          />
          <SceneGenerateDialog
            {...dialogProps("generate")}
            sceneIds={selectedItems.map((i) => i.id)}
            applyToAllTarget={applyToAllTarget}
            totalCount={totalCount}
          />
          <SceneMergeDialog
            {...dialogProps("merge")}
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
            {...dialogProps("edit")}
            items={selectedItems}
            applyToAllTarget={applyToAllTarget}
            totalCount={totalCount}
          />
        </>
      )}
    </>
  );
}
