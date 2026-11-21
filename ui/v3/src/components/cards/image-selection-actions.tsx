import { useMemo, type PropsWithChildren } from "react";
import { useIntl } from "react-intl";
import { ImageListItems } from "@/components/list/entity-list-items";
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
  ImageBulkEditSheet,
  ImageGenerateDialog,
} from "@/components/detail/deferred-overlays";

export function ImageSelectionActions({ children }: PropsWithChildren) {
  const intl = useIntl();
  const { selectedItems, open, dialogProps, totalCount, applyToAllTarget } =
    useSelectionDialogs(
      ImageListItems.useItems(),
      ImageListItems.useSelectionGetter(),
    );
  const deleteImages = useBulkMediaDelete("Image");
  const actions = useMemo(
    () => ({
      noun: "images",
      onEdit: () => open("edit"),
      onGenerate: () => open("generate"),
      onDelete: () => open("delete"),
    }),
    [open],
  );
  const paths = selectedItems.flatMap(
    (i) => i.visual_files?.map((f) => f.path) ?? [],
  );
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
                id: "dialogs.delete_images_count",
                defaultMessage: "{count} images",
              },
              { count: selectedItems.length },
            )}
            showFileOptions
            deleteFileLabel={intl.formatMessage({
              id: "dialogs.delete_file",
              defaultMessage: "Delete file",
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
              deleteImages(
                selectedItems.map((i) => i.id),
                options,
                onProgress,
              )
            }
          />
          <ImageGenerateDialog
            {...dialogProps("generate")}
            imageIds={selectedItems.map((i) => i.id)}
          />
          <ImageBulkEditSheet
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
