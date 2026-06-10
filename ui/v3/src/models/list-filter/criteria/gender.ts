import {
  CriterionModifier,
  type GenderCriterionInput,
  type GenderEnum,
} from "src/core/generated-graphql";
import { genderStrings, stringToGender } from "src/utils/gender";
import {
  ModifierCriterionOption,
  type SavedCriterion,
  StringCriterion,
} from "./criterion";

export const GenderCriterionOption = new ModifierCriterionOption({
  messageID: "gender",
  type: "gender",
  options: genderStrings,
  modifierOptions: [
    CriterionModifier.Includes,
    CriterionModifier.Excludes,
    CriterionModifier.IsNull,
    CriterionModifier.NotNull,
  ],
  defaultModifier: CriterionModifier.Includes,
  makeCriterion: () => new GenderCriterion(),
});

export class GenderCriterion extends StringCriterion {
  constructor(value: string = "") {
    super(GenderCriterionOption);
    this.value = value;
  }

  public toCriterionInput(): GenderCriterionInput {
    const value = [stringToGender(this.value)].filter((v) => v) as GenderEnum[];

    return {
      value_list: value,
      modifier: this.modifier,
    };
  }

  public fromDecodedParams(i: unknown): void {
    const criterion = i as { value?: unknown };
    if (Array.isArray(criterion.value)) {
      this.value =
        typeof criterion.value[0] === "string" ? criterion.value[0] : "";
      return;
    }

    super.fromDecodedParams(i);
  }

  public setFromSavedCriterion(criterion: SavedCriterion<string[] | string>) {
    // backwards compatibility - if the value is a string, convert it to an array
    if (typeof criterion.value === "string") {
      this.value = criterion.value;
      this.modifier = criterion.modifier;
      return;
    }

    if (Array.isArray(criterion.value)) {
      this.value =
        typeof criterion.value[0] === "string" ? criterion.value[0] : "";
      this.modifier = criterion.modifier;
      return;
    }

    super.setFromSavedCriterion(criterion);
  }
}
