import {
  type CircumcisionCriterionInput,
  type CircumcisedEnum,
  CriterionModifier,
} from "src/core/generated-graphql";
import { circumcisedStrings, stringToCircumcised } from "src/utils/circumcised";
import {
  type SavedCriterion,
  ModifierCriterionOption,
  StringCriterion,
} from "./criterion";

export const CircumcisedCriterionOption = new ModifierCriterionOption({
  messageID: "circumcised",
  type: "circumcised",
  modifierOptions: [
    CriterionModifier.Includes,
    CriterionModifier.Excludes,
    CriterionModifier.IsNull,
    CriterionModifier.NotNull,
  ],
  defaultModifier: CriterionModifier.Includes,
  options: circumcisedStrings,
  makeCriterion: () => new CircumcisedCriterion(),
});

export class CircumcisedCriterion extends StringCriterion {
  constructor(value: string = "") {
    super(CircumcisedCriterionOption);
    this.value = value;
  }

  public toCriterionInput(): CircumcisionCriterionInput {
    const value = [stringToCircumcised(this.value)].filter(
      (v) => v,
    ) as CircumcisedEnum[];

    return {
      value,
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
