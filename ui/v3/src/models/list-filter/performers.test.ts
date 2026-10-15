import { createIntl } from "react-intl";
import { describe, expect, it } from "vitest";
import { CriterionModifier, FilterMode } from "@/core/generated-graphql";
import enGB from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import { ListFilterModel } from "./filter";
import {
  astCriterionOptions,
  createASTCondition,
  filterASTConditionLabel,
  filterASTNodeToGraphQL,
  flattenFilterASTConditions,
} from "./filter-ast";
import { StringCriterion } from "./criteria/criterion";

const intl = createIntl({ locale: "en-GB", messages: flattenMessages(enGB) });

describe("performer name filters", () => {
  it("offers Names and Canonical name with their existing matching scopes", () => {
    const options = astCriterionOptions(FilterMode.Performers);
    expect(options.some((option) => option.type === "aliases")).toBe(false);
    for (const [field, label] of [
      ["names", "Names"],
      ["name", "Canonical name"],
    ] as const) {
      const condition = createASTCondition(FilterMode.Performers, field);
      const criterion = condition.criterion as StringCriterion;
      criterion.value = "River";
      expect(filterASTConditionLabel(condition, intl, false)).toContain(label);
      expect(filterASTNodeToGraphQL(condition)).toEqual({
        condition: {
          field,
          value: {
            modifier: CriterionModifier.Equals,
            value: "River",
            case_sensitive: false,
          },
        },
      });
    }
  });

  it.each([
    CriterionModifier.Includes,
    CriterionModifier.Excludes,
    CriterionModifier.Equals,
    CriterionModifier.NotEquals,
    CriterionModifier.MatchesRegex,
    CriterionModifier.NotMatchesRegex,
  ])("preserves saved noncanonical-name searches using %s", (modifier) => {
    const ast = {
      root: {
        condition: {
          field: "aliases",
          value: { modifier, value: "River", case_sensitive: true },
        },
      },
    };
    const original = new ListFilterModel(FilterMode.Performers);
    original.configureFromSavedFilter({ filter_ast: ast });
    const restored = new ListFilterModel(FilterMode.Performers);
    restored.configureFromDecodedParams({
      fa: original.getEncodedParams().fa ?? undefined,
    });
    const saved = new ListFilterModel(FilterMode.Performers);
    saved.configureFromSavedFilter({ filter_ast: restored.makeFilterAst() });
    for (const filter of [original, restored, saved]) {
      expect(filter.makeFilterAST()).toEqual(ast);
      const [condition] = flattenFilterASTConditions(filter.filterAst);
      expect(condition).toBeDefined();
      expect(filterASTConditionLabel(condition!, intl, false)).toContain(
        "Other names",
      );
    }
  });

  it("labels missing other names without changing the saved field or studio/tag aliases", () => {
    for (const mode of [
      FilterMode.Performers,
      FilterMode.Studios,
      FilterMode.Tags,
    ]) {
      const condition = createASTCondition(mode, "is_missing");
      (condition.criterion as StringCriterion).value = "aliases";
      expect(filterASTConditionLabel(condition, intl, false)).toContain(
        mode === FilterMode.Performers ? "Other names" : "Aliases",
      );
      expect(filterASTNodeToGraphQL(condition)).toEqual({
        condition: { field: "is_missing", value: "aliases" },
      });
    }
  });
});
