/**
 * Drop-in `<DropdownMenuItem>` for the scene detail actions menu.
 * Equivalent to `<SceneCardDownloadMenuItem>` but for the
 * dropdown-menu primitive instead of context-menu.
 */
import { Download } from "lucide-react";
import { DropdownMenuItem } from "src/components/ui/dropdown-menu";
import type * as GQL from "src/core/generated-graphql";
import { useSceneDownloadAction } from "./download-action";

export function SceneDetailDownloadMenuItem({
  scene,
}: {
  scene: NonNullable<GQL.FindSceneQuery["findScene"]>;
}) {
  // The hook accepts a structurally-narrower scene shape than the
  // FindScene query result; cast through `unknown` to satisfy the
  // `Pick<SceneCardScene, …>` projection without forcing a deep
  // mapping (every field the hook reads exists on FindScene's scene
  // with a compatible type).
  const action = useSceneDownloadAction({
    scene: scene as unknown as Parameters<
      typeof useSceneDownloadAction
    >[0]["scene"],
  });
  return (
    <DropdownMenuItem disabled={action.disabled} onClick={action.onSelect}>
      <Download />
      {action.label}
    </DropdownMenuItem>
  );
}
