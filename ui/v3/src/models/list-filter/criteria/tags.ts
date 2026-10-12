import { CriterionModifier } from "src/core/generated-graphql";
import {
  ModifierCriterionOption,
  IHierarchicalLabeledIdCriterion,
  type SavedCriterion,
} from "./criterion";
import type { CriterionType } from "../types";

const modifierOptions = [
  CriterionModifier.IncludesAll,
  CriterionModifier.Includes,
  CriterionModifier.Excludes,
  CriterionModifier.Equals,
  CriterionModifier.IsNull,
  CriterionModifier.NotNull,
];

const defaultModifier = CriterionModifier.IncludesAll;
const inputType = "tags";
const defaultStudioTagHierarchyMode = "exact" as const;

class BaseTagsCriterionOption extends ModifierCriterionOption {
  constructor(messageID: string, type: CriterionType) {
    super({
      messageID,
      type,
      modifierOptions,
      defaultModifier,
      inputType,
      makeCriterion: () => new TagsCriterion(this),
    });
  }
}

export const TagsCriterionOption = new BaseTagsCriterionOption("tags", "tags");
export const SceneTagsCriterionOption = new BaseTagsCriterionOption(
  "scene_tags",
  "scene_tags",
);
export const PerformerTagsCriterionOption = new BaseTagsCriterionOption(
  "performer_tags",
  "performer_tags",
);
export const ParentTagsCriterionOption = new BaseTagsCriterionOption(
  "parent_tags",
  "parents",
);
export const ChildTagsCriterionOption = new BaseTagsCriterionOption(
  "sub_tags",
  "children",
);

export class TagsCriterion extends IHierarchicalLabeledIdCriterion {}

export class StudioTagsCriterion extends TagsCriterion {
  constructor(type: ModifierCriterionOption) {
    super(type, {
      items: [],
      excluded: [],
      depth: 0,
      hierarchyMode: defaultStudioTagHierarchyMode,
    });
  }

  public override toCriterionInputValue() {
    return {
      ...this.toCriterionInput(),
      hierarchy_mode: this.value.hierarchyMode ?? defaultStudioTagHierarchyMode,
    };
  }

  public override cloneValues() {
    super.cloneValues();
    this.value.hierarchyMode ??= defaultStudioTagHierarchyMode;
  }

  public override fromDecodedParams(i: unknown): void {
    super.fromDecodedParams(i);
    this.value.hierarchyMode ??= defaultStudioTagHierarchyMode;
  }

  public override setFromSavedCriterion(
    criterion: SavedCriterion<{
      items: { id: string; label: string }[];
      excluded: { id: string; label: string }[];
      depth: number;
      hierarchyMode?:
        | "exact"
        | "ancestors"
        | "descendants"
        | "ancestors_descendants";
    }>,
  ) {
    super.setFromSavedCriterion(criterion);
    this.value.hierarchyMode =
      criterion.value?.hierarchyMode ?? defaultStudioTagHierarchyMode;
  }
}

export const StudioTagsCriterionOption = new ModifierCriterionOption({
  messageID: "studio_tags",
  type: "studio_tags",
  modifierOptions,
  defaultModifier,
  inputType,
  makeCriterion: (o) => new StudioTagsCriterion(o as ModifierCriterionOption),
});
