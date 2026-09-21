import {
  deferredDialog,
  deferredEditSheet,
} from "@/components/shared/deferred-overlay";

export const SceneEditSheet = deferredEditSheet(async () => ({
  default: (await import("./scene-edit-sheet")).SceneEditSheet,
}));
export const SceneBulkEditSheet = deferredDialog(async () => ({
  default: (await import("./scene-bulk-edit-sheet")).SceneBulkEditSheet,
}));
export const ImageEditSheet = deferredEditSheet(async () => ({
  default: (await import("./image-edit-sheet")).ImageEditSheet,
}));
export const ImageBulkEditSheet = deferredDialog(async () => ({
  default: (await import("./image-bulk-edit-sheet")).ImageBulkEditSheet,
}));
export const GalleryEditSheet = deferredEditSheet(async () => ({
  default: (await import("./gallery-edit-sheet")).GalleryEditSheet,
}));
export const GalleryBulkEditSheet = deferredDialog(async () => ({
  default: (await import("./gallery-bulk-edit-sheet")).GalleryBulkEditSheet,
}));
export const GroupEditSheet = deferredEditSheet(async () => ({
  default: (await import("./group-edit-sheet")).GroupEditSheet,
}));
export const GroupBulkEditSheet = deferredDialog(async () => ({
  default: (await import("./group-bulk-edit-sheet")).GroupBulkEditSheet,
}));
export const PerformerEditSheet = deferredEditSheet(async () => ({
  default: (await import("./performer-edit-sheet")).PerformerEditSheet,
}));
export const PerformerBulkEditSheet = deferredDialog(async () => ({
  default: (await import("./performer-bulk-edit-sheet")).PerformerBulkEditSheet,
}));
export const StudioEditSheet = deferredEditSheet(async () => ({
  default: (await import("./studio-edit-sheet")).StudioEditSheet,
}));
export const StudioBulkEditSheet = deferredDialog(async () => ({
  default: (await import("./studio-bulk-edit-sheet")).StudioBulkEditSheet,
}));
export const TagEditSheet = deferredEditSheet(async () => ({
  default: (await import("./tag-edit-sheet")).TagEditSheet,
}));
export const TagBulkEditSheet = deferredDialog(async () => ({
  default: (await import("./tag-bulk-edit-sheet")).TagBulkEditSheet,
}));
export const SceneGenerateDialog = deferredDialog(async () => ({
  default: (await import("./scene-generate-dialog")).SceneGenerateDialog,
}));
export const ImageGenerateDialog = deferredDialog(async () => ({
  default: (await import("./image-generate-dialog")).ImageGenerateDialog,
}));
export const SceneMergeDialog = deferredDialog(async () => ({
  default: (await import("./scene-merge-dialog")).SceneMergeDialog,
}));
export const PerformerMergeDialog = deferredDialog(async () => ({
  default: (await import("./performer-merge-dialog")).PerformerMergeDialog,
}));
export const SceneCreateSheet = deferredDialog(async () => ({
  default: (await import("./scene-create-sheet")).SceneCreateSheet,
}));
export const PerformerCreateSheet = deferredDialog(async () => ({
  default: (await import("./performer-create-sheet")).PerformerCreateSheet,
}));
export const StudioCreateSheet = deferredDialog(async () => ({
  default: (await import("./studio-create-sheet")).StudioCreateSheet,
}));
export const TagCreateSheet = deferredDialog(async () => ({
  default: (await import("./tag-create-sheet")).TagCreateSheet,
}));
export const GroupCreateSheet = deferredDialog(async () => ({
  default: (await import("./group-create-sheet")).GroupCreateSheet,
}));
