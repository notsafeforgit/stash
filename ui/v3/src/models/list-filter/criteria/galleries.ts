import {
  CriterionModifier,
  type MultiCriterionInput,
} from "src/core/generated-graphql";
import {
  ModifierCriterionOption,
  IHierarchicalLabeledIdCriterion,
} from "./criterion";

const inputType = "galleries";

const modifierOptions = [
  CriterionModifier.IncludesAll,
  CriterionModifier.Includes,
  CriterionModifier.Excludes,
  CriterionModifier.Equals,
  CriterionModifier.IsNull,
  CriterionModifier.NotNull,
];

const defaultModifier = CriterionModifier.IncludesAll;

export const GalleriesCriterionOption: ModifierCriterionOption =
  new ModifierCriterionOption({
    messageID: "galleries",
    type: "galleries",
    modifierOptions,
    defaultModifier,
    inputType,
    makeCriterion: () => new GalleriesCriterion(GalleriesCriterionOption),
  });

export class GalleriesCriterion extends IHierarchicalLabeledIdCriterion {
  public override toCriterionInput(): MultiCriterionInput {
    // The shared selector stores included/excluded IDs in the same shape as
    // hierarchical selectors. Gallery membership itself has no hierarchy.
    const { value, excludes, modifier } = super.toCriterionInput();
    return { value, excludes, modifier };
  }
}
