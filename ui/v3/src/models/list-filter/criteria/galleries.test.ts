import { expect, it } from "vitest";
import {
  CriterionModifier,
  FilterGroupOperator,
  FilterMode,
} from "@/core/generated-graphql";
import { ListFilterModel } from "../filter";
import { createASTConditionFromCriterion, createASTGroup } from "../filter-ast";
import { GalleriesCriterion, GalleriesCriterionOption } from "./galleries";

it.each([FilterMode.Images, FilterMode.Scenes])(
  "sends flat gallery membership for %s, including saved and linked filters",
  (mode) => {
    const gallery = "7fcfe6aa-cff3-4c57-b5ad-10c164391d2f";
    const excluded = "400bf9fe-1b8c-4435-9359-450d31ec0781";
    const criterion = new GalleriesCriterion(GalleriesCriterionOption);
    criterion.value = {
      items: [{ id: gallery, label: "Included gallery" }],
      excluded: [{ id: excluded, label: "Excluded gallery" }],
      depth: 0,
    };
    const filter = new ListFilterModel(mode);
    filter.filterAst = createASTConditionFromCriterion(mode, criterion);
    const expected = {
      root: {
        condition: {
          field: "galleries",
          value: {
            value: [gallery],
            excludes: [excluded],
            modifier: CriterionModifier.IncludesAll,
          },
        },
      },
    };
    expect(filter.makeFilterAST()).toEqual(expected);
    const saved = new ListFilterModel(mode);
    saved.configureFromSavedFilter({ filter_ast: filter.makeFilterAst() });
    expect(saved.makeFilterAST()).toEqual(expected);
    const linked = new ListFilterModel(mode);
    linked.configureFromDecodedParams({
      fa: filter.getEncodedParams().fa ?? undefined,
    });
    expect(linked.makeFilterAST()).toEqual(expected);
    // Embedded gallery lists use locked conditions rather than editable ones.
    linked.filterAst = undefined;
    linked.lockedFilterAst = createASTGroup(mode, FilterGroupOperator.And, [
      createASTConditionFromCriterion(mode, criterion),
    ]);
    expect(linked.makeFilterAST()).toEqual({
      root: { group: { operator: "AND", children: [expected.root] } },
    });
  },
);

it("keeps null gallery filters and clears excluded memberships when the modifier changes", () => {
  const criterion = new GalleriesCriterion(GalleriesCriterionOption);
  criterion.value.excluded = [{ id: "excluded", label: "Excluded" }];
  criterion.modifier = CriterionModifier.IsNull;
  expect(criterion.isValid()).toBe(true);
  expect(criterion.toCriterionInput()).toEqual({
    value: [],
    excludes: [],
    modifier: CriterionModifier.IsNull,
  });
});
