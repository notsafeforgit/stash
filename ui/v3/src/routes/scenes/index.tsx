import { useState, useMemo } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { EntityListPage } from "src/components/list";
import { useSceneListConfig } from "src/components/list/entity-list-configs";
import { View } from "src/components/list/views";
import { SceneEditSheet } from "src/components/detail/scene-edit-sheet";

function ScenesPage() {
  const [editingId, setEditingId] = useState<string | null>(null);
  const {
    config: sceneBase,
    lightboxElement,
    lightboxOpen,
  } = useSceneListConfig(setEditingId);

  const config = useMemo(
    () => ({
      ...sceneBase,
      view: View.Scenes,
      tableVisibilityKey: "scenes",
      // Recency-first for the global library view; embedded scenes tabs
      // (performer/tag/studio) keep their per-mode default ("date").
      defaultSort: "created_at",
    }),
    [sceneBase],
  );

  return (
    <>
      <EntityListPage
        config={config}
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <SceneEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

export const Route = createFileRoute("/scenes/")({
  component: ScenesPage,
});
