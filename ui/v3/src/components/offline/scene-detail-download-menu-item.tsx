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
  const action = useSceneDownloadAction({ scene });
  return (
    <DropdownMenuItem disabled={action.disabled} onClick={action.onSelect}>
      <Download />
      {action.label}
    </DropdownMenuItem>
  );
}
