import { expect, it } from "vitest";
import { CriterionModifier, FilterMode } from "@/core/generated-graphql";
import { decodeURLJSON } from "@/utils/url-json";
import { ListFilterModel } from "./filter";

function titleFilter(mode: FilterMode, title: string) {
  const filter = new ListFilterModel(mode);
  filter.configureFromSavedFilter({
    filter_ast: {
      root: {
        condition: {
          field: "title",
          value: { modifier: CriterionModifier.Includes, value: title },
        },
      },
    },
  });
  return filter;
}

it.each([
  "ASCII",
  "café",
  "Ã©",
  "日本語 🎬",
])("round trips filter URLs and saved filters for %s", (title) => {
  const original = titleFilter(FilterMode.Scenes, title);
  expect(original.count()).toBe(1);
  const encoded = original.getEncodedParams().fa;
  expect(encoded).toMatch(/^u\./);
  const restored = new ListFilterModel(FilterMode.Scenes);
  restored.configureFromDecodedParams({ fa: encoded ?? undefined });
  expect(restored.makeFilterAST()).toEqual(original.makeFilterAST());
  const saved = new ListFilterModel(FilterMode.Scenes);
  saved.configureFromSavedFilter({ filter_ast: restored.makeFilterAst() });
  expect(saved.makeFilterAST()).toEqual(original.makeFilterAST());
  if (title !== "日本語 🎬" && encoded) {
    const legacy = btoa(JSON.stringify(decodeURLJSON(encoded)));
    restored.configureFromDecodedParams({ fa: legacy });
    expect(restored.makeFilterAST()).toEqual(original.makeFilterAST());
  }
});

it.each([
  "u.%%%",
  "u.e30",
  "not-base64",
])("fails closed for malformed filters: %s", (fa) => {
  const filter = new ListFilterModel(FilterMode.Scenes);
  expect(() => filter.configureFromDecodedParams({ fa })).toThrow();
});
