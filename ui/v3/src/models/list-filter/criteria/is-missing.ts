import { CriterionModifier } from "src/core/generated-graphql";
import type { CriterionType } from "../types";
import {
  ModifierCriterionOption,
  StringCriterion,
  type Option,
} from "./criterion";

const CODE_OPTION = { value: "code", messageID: "scene_code" };
const COVER_OPTION = { value: "cover", messageID: "cover_image" };

export class IsMissingCriterion extends StringCriterion {
  public toCriterionInput(): string {
    return this.value;
  }
}

class IsMissingCriterionOption extends ModifierCriterionOption {
  constructor(messageID: string, type: CriterionType, options: Option[]) {
    super({
      messageID,
      type,
      options,
      modifierOptions: [],
      defaultModifier: CriterionModifier.Equals,
      sortOptions: true,
      makeCriterion: () => new IsMissingCriterion(this),
    });
  }
}

export const SceneIsMissingCriterionOption = new IsMissingCriterionOption(
  "isMissing",
  "is_missing",
  [
    "title",
    CODE_OPTION,
    "details",
    "director",
    "url",
    "date",
    "rating",
    COVER_OPTION,
    "galleries",
    "studio",
    "group",
    "performers",
    "tags",
    "stash_id",
  ],
);

export const ImageIsMissingCriterionOption = new IsMissingCriterionOption(
  "isMissing",
  "is_missing",
  [
    "title",
    "details",
    "photographer",
    "url",
    "date",
    CODE_OPTION,
    "rating",
    "galleries",
    "studio",
    "performers",
    "tags",
  ],
);

export const PerformerIsMissingCriterionOption = new IsMissingCriterionOption(
  "isMissing",
  "is_missing",
  [
    "url",
    "ethnicity",
    "country",
    "hair_color",
    "eye_color",
    "height",
    "weight",
    "measurements",
    "fake_tits",
    "penis_length",
    "circumcised",
    "career_start",
    "career_end",
    "tattoos",
    "piercings",
    "aliases",
    "gender",
    "birthdate",
    "death_date",
    "disambiguation",
    "tags",
    "image",
    "details",
    "rating",
    "stash_id",
  ],
);

export const GalleryIsMissingCriterionOption = new IsMissingCriterionOption(
  "isMissing",
  "is_missing",
  [
    "title",
    CODE_OPTION,
    "details",
    "photographer",
    "url",
    "date",
    "rating",
    COVER_OPTION,
    "studio",
    "performers",
    "tags",
    "scenes",
  ],
);

export const TagIsMissingCriterionOption = new IsMissingCriterionOption(
  "isMissing",
  "is_missing",
  ["image", "aliases", "description", "stash_id"],
);

export const StudioIsMissingCriterionOption = new IsMissingCriterionOption(
  "isMissing",
  "is_missing",
  ["image", "stash_id", "details", "url", "aliases", "tags", "rating"],
);

export const GroupIsMissingCriterionOption = new IsMissingCriterionOption(
  "isMissing",
  "is_missing",
  [
    "aliases",
    "description",
    "director",
    "date",
    "url",
    "rating",
    "studio",
    "performers",
    "tags",
    "front_image",
    "back_image",
    "scenes",
  ],
);
