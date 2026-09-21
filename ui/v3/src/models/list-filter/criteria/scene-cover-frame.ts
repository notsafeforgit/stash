import type { IntlShape } from "react-intl";
import { z } from "zod";
import {
  CriterionModifier,
  SceneCoverFrame,
  type SceneCoverFrameCriterionInput,
} from "@/core/generated-graphql";
import { ModifierCriterionOption, StringCriterion } from "./criterion";

const messageIDs = {
  [SceneCoverFrame.Default]: "scene_cover.frame_default",
  [SceneCoverFrame.Specific]: "scene_cover.frame_specific",
  [SceneCoverFrame.Unknown]: "scene_cover.frame_unknown",
} satisfies Record<SceneCoverFrame, string>;

const inputSchema = z.object({
  value: z.enum(SceneCoverFrame),
  modifier: z.enum([CriterionModifier.Equals, CriterionModifier.NotEquals]),
});

export const SceneCoverFrameCriterionOption = new ModifierCriterionOption({
  messageID: "scene_cover.frame_filter",
  type: "cover_frame",
  options: Object.values(SceneCoverFrame).map((value) => ({
    value,
    messageID: messageIDs[value],
  })),
  sortOptions: false,
  modifierOptions: [CriterionModifier.Equals, CriterionModifier.NotEquals],
  makeCriterion: () => new SceneCoverFrameCriterion(),
});

export class SceneCoverFrameCriterion extends StringCriterion {
  constructor() {
    super(SceneCoverFrameCriterionOption);
    this.value = SceneCoverFrame.Specific;
  }

  public override isValid(): boolean {
    return inputSchema.safeParse(this).success;
  }

  protected override getLabelValue(intl: IntlShape): string {
    const { value } = this.toCriterionInput();
    return intl.formatMessage({ id: messageIDs[value] });
  }

  public override toCriterionInput(): SceneCoverFrameCriterionInput {
    return inputSchema.parse({ value: this.value, modifier: this.modifier });
  }

  public override fromDecodedParams(input: unknown): void {
    this.setFromSavedCriterion(input);
  }

  public override setFromSavedCriterion(input: unknown): void {
    const parsed = inputSchema.parse(input);
    this.value = parsed.value;
    this.modifier = parsed.modifier;
  }
}
