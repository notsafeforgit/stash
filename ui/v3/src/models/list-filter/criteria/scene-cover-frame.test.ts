import { createIntl } from "react-intl";
import { expect, it } from "vitest";
import {
  CriterionModifier,
  FilterMode,
  SceneCoverFrame,
} from "@/core/generated-graphql";
import enGB from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import { ListFilterModel } from "../filter";
import { SceneCoverFrameCriterion } from "./scene-cover-frame";

it.each(Object.values(SceneCoverFrame))(
  "round trips %s through saved filters and URLs with localized labels",
  (value) => {
    const original = new ListFilterModel(FilterMode.Scenes);
    original.configureFromSavedFilter({
      filter_ast: {
        root: {
          condition: {
            field: "cover_frame",
            value: { value, modifier: CriterionModifier.Equals },
          },
        },
      },
    });
    expect(original.count()).toBe(1);
    expect(JSON.stringify(original.makeFilterAST())).toContain(
      JSON.stringify({
        field: "cover_frame",
        value: { value, modifier: CriterionModifier.Equals },
      }),
    );
    const restored = new ListFilterModel(FilterMode.Scenes);
    restored.configureFromDecodedParams({
      fa: original.getEncodedParams().fa ?? undefined,
    });
    expect(restored.makeFilterAST()).toEqual(original.makeFilterAST());
    const saved = new ListFilterModel(FilterMode.Scenes);
    saved.configureFromSavedFilter({ filter_ast: restored.makeFilterAst() });
    expect(saved.makeFilterAST()).toEqual(original.makeFilterAST());
    const criterion = new SceneCoverFrameCriterion();
    criterion.setFromSavedCriterion({
      value,
      modifier: CriterionModifier.NotEquals,
    });
    const label = criterion.getLabel(
      createIntl({ locale: "en-GB", messages: flattenMessages(enGB) }),
    );
    expect(label).toContain("Cover frame");
    expect(label).not.toContain(value);
    expect(criterion.toCriterionInput()).toEqual({
      value,
      modifier: CriterionModifier.NotEquals,
    });
  },
);

it.each([
  null,
  {},
  { value: "unrecognized", modifier: CriterionModifier.Equals },
  { value: SceneCoverFrame.Default, modifier: CriterionModifier.Includes },
])(
  "rejects malformed cover criteria without dropping the filter: %s",
  (value) => {
    const criterion = new SceneCoverFrameCriterion();
    expect(() => criterion.fromDecodedParams(value)).toThrow();
    expect(() => criterion.setFromSavedCriterion(value)).toThrow();
  },
);
