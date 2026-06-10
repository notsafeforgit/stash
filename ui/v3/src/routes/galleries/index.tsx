import { useState, useMemo } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { EntityListPage } from "src/components/list";
import { useGalleryListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import { useGalleryTableColumns } from "./-table-columns";
import { GalleryEditSheet } from "src/components/detail/gallery-edit-sheet";

function GalleriesPage() {
  const tableColumns = useGalleryTableColumns();
  const [editingId, setEditingId] = useState<string | null>(null);
  const galleryBase = useGalleryListConfig(setEditingId);

  const config = useMemo(
    () => ({
      ...galleryBase,
      view: View.Galleries,
      tableColumns,
      tableVisibilityKey: "galleries",
      // Recency-first for the global library view; embedded gallery tabs
      // keep their per-mode default.
      defaultSort: "created_at",
    }),
    [galleryBase, tableColumns],
  );

  return (
    <>
      <EntityListPage config={config} />
      <GalleryEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}

export const Route = createFileRoute("/galleries/")({
  component: GalleriesPage,
});
