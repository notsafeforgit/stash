import { useState, useMemo } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { EntityListPage } from "src/components/list";
import { useImageListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import { ImageEditSheet } from "src/components/detail/image-edit-sheet";

function ImagesPage() {
  const [editingId, setEditingId] = useState<string | null>(null);
  const {
    config: imageBase,
    lightboxElement,
    lightboxOpen,
  } = useImageListConfig(setEditingId);

  const config = useMemo(
    () => ({
      ...imageBase,
      view: View.Images,
      tableVisibilityKey: "images",
      // Override the per-mode default ("path") for the global library view —
      // recency makes more sense when browsing the whole library, while the
      // path default still applies to embedded performer/tag/studio image
      // tabs where filesystem order is the natural browse mode.
      defaultSort: "created_at",
    }),
    [imageBase],
  );

  return (
    <>
      <EntityListPage
        config={config}
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <ImageEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

export const Route = createFileRoute("/images/")({
  component: ImagesPage,
});
