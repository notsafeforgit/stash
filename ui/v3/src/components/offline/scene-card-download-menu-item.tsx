/**
 * Drop-in `<ContextMenuItem>` for the scene-card context menu — calls
 * the shared `useSceneDownloadAction` hook so the label / disabled
 * state / click handler stay in sync with the queue + IDB.
 *
 * Lives in `offline/` so the offline feature is self-contained;
 * `scene-card.tsx` imports it.
 */
import { ContextMenuItem } from "src/components/ui/context-menu";
import type { SceneCardScene } from "src/components/cards/scene-card";
import { useSceneDownloadAction } from "./download-action";

export function SceneCardDownloadMenuItem({
  scene,
}: {
  scene: SceneCardScene;
}) {
  const action = useSceneDownloadAction({ scene });
  return (
    <ContextMenuItem disabled={action.disabled} onClick={action.onSelect}>
      {action.label}
    </ContextMenuItem>
  );
}
