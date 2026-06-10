import { orientationStrings, stringToOrientation } from "src/utils/orientation";
import type { CriterionType } from "../types";
import { ModifierCriterionOption, StringCriterion } from "./criterion";
import type {
  CriterionModifier,
  OrientationCriterionInput,
  OrientationEnum,
} from "src/core/generated-graphql";

export class OrientationCriterion extends StringCriterion {
  public toCriterionInput(): OrientationCriterionInput {
    return {
      value: [stringToOrientation(this.value)].filter(
        (v) => v,
      ) as OrientationEnum[],
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

  public setFromSavedCriterion(criterion: unknown) {
    const savedCriterion = criterion as { value?: unknown; modifier?: unknown };
    if (Array.isArray(savedCriterion.value)) {
      this.value =
        typeof savedCriterion.value[0] === "string"
          ? savedCriterion.value[0]
          : "";
      if (savedCriterion.modifier) {
        this.modifier = savedCriterion.modifier as CriterionModifier;
      }
      return;
    }

    super.setFromSavedCriterion(criterion);
  }
}

class BaseOrientationCriterionOption extends ModifierCriterionOption {
  constructor(value: CriterionType) {
    super({
      messageID: value,
      type: value,
      options: orientationStrings,
      makeCriterion: () => new OrientationCriterion(this),
    });
  }
}

export const OrientationCriterionOption = new BaseOrientationCriterionOption(
  "orientation",
);
