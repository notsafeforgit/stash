import type * as GQL from "./generated-graphql";

export type SceneGenerationTarget =
  | { kind: "scenes"; ids: readonly string[] }
  | {
      kind: "matching";
      selection: GQL.GenerateSceneSelectionInput;
      count: number;
    }
  | { kind: "library" };

export function sceneGenerationScope(
  target: SceneGenerationTarget,
): Pick<GQL.GenerateMetadataInput, "sceneIDs" | "sceneSelection"> {
  switch (target.kind) {
    case "scenes":
      if (target.ids.length === 0) throw new Error("No scenes selected");
      return { sceneIDs: [...target.ids] };
    case "matching":
      return { sceneSelection: target.selection };
    case "library":
      return {};
  }
}
