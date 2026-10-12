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

it.each(["ASCII", "café", "Ã©", "日本語 🎬"])(
  "round trips filter URLs and saved filters for %s",
  (title) => {
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
  },
);

it.each(["u.%%%", "u.e30", "not-base64"])(
  "fails closed for malformed filters: %s",
  (fa) => {
    const filter = new ListFilterModel(FilterMode.Scenes);
    expect(() => filter.configureFromDecodedParams({ fa })).toThrow();
  },
);

it("round trips production dates through saved filters, URLs and query criteria", () => {
  const original = new ListFilterModel(FilterMode.Scenes);
  original.configureFromSavedFilter({
    filter_ast: {
      root: {
        condition: {
          field: "production_date",
          value: {
            modifier: CriterionModifier.Equals,
            value: { value: "2010-06-15" },
          },
        },
      },
    },
  });
  expect(original.count()).toBe(1);
  expect(JSON.stringify(original.makeFilterAST())).toContain(
    '"production_date"',
  );
  const restored = new ListFilterModel(FilterMode.Scenes);
  restored.configureFromDecodedParams({
    fa: original.getEncodedParams().fa ?? undefined,
  });
  expect(restored.makeFilterAST()).toEqual(original.makeFilterAST());
  const saved = new ListFilterModel(FilterMode.Scenes);
  saved.configureFromSavedFilter({ filter_ast: restored.makeFilterAst() });
  expect(saved.makeFilterAST()).toEqual(original.makeFilterAST());
});

it("preserves nested alternatives and repeated fields in native saved filters", () => {
  const title = (value: string) => ({
    condition: {
      field: "title",
      value: { modifier: CriterionModifier.Includes, value },
    },
  });
  const ast = {
    root: {
      group: {
        operator: "OR",
        children: [
          title("first"),
          {
            group: {
              operator: "AND",
              children: [title("second"), title("third")],
            },
          },
        ],
      },
    },
  };
  const original = new ListFilterModel(FilterMode.Scenes);
  original.configureFromSavedFilter({ filter_ast: ast });
  expect(original.makeFilterAst()).toEqual(ast);
  const restored = new ListFilterModel(FilterMode.Scenes);
  restored.configureFromSavedFilter({ filter_ast: original.makeFilterAst() });
  expect(restored.makeFilterAst()).toEqual(ast);
  expect(restored.makeFilterAST()).toEqual(original.makeFilterAST());

  restored.configureFromSavedFilter({ filter_ast: null });
  expect(restored.makeFilterAst()).toBeUndefined();
  expect(restored.count()).toBe(0);
});

it("rejects retired flat URL and saved-filter formats", () => {
  const filter = new ListFilterModel(FilterMode.Scenes);
  expect(() =>
    filter.configureFromQueryString("?c=%7B%22type%22%3A%22title%22%7D"),
  ).toThrow(/Flat filter URLs/);
  const legacy = { find_filter: null, object_filter: { title: "old" } };
  expect(() => filter.configureFromSavedFilter(legacy)).toThrow(
    /object_filter/,
  );
});

it("serializes custom-field conditions in the AST URL without a flat side channel", () => {
  const filter = new ListFilterModel(FilterMode.Scenes);
  filter.configureFromSavedFilter({
    filter_ast: {
      root: {
        condition: {
          field: "custom_fields",
          value: {
            value: [{ field: "note", modifier: "EQUALS", value: "café" }],
          },
        },
      },
    },
  });
  const params = new URLSearchParams(filter.makeQueryParameters());
  expect(params.has("c")).toBe(false);
  expect(params.has("fa")).toBe(true);
  const restored = titleFilter(
    FilterMode.Scenes,
    "replace this previous filter",
  );
  restored.configureFromQueryString(params.toString());
  expect(restored.makeFilterAST()).toEqual(filter.makeFilterAST());
  expect(restored.count()).toBe(1);
});

it("preserves studio-tag hierarchy and labelled UUID selections in ASTs", () => {
  const filter = new ListFilterModel(FilterMode.Scenes);
  const id = "00a0cf9d-4659-4e18-891e-2fc13ee26af0";
  filter.configureFromSavedFilter({
    filter_ast: {
      root: {
        condition: {
          field: "studio_tags",
          value: {
            modifier: "INCLUDES",
            value: {
              items: [{ id, label: "Studio tag" }],
              excluded: [],
              depth: 2,
              hierarchyMode: "ancestors",
            },
          },
        },
      },
    },
  });
  expect(filter.makeFilterAST()).toEqual({
    root: {
      condition: {
        field: "studio_tags",
        value: {
          value: [id],
          excludes: [],
          modifier: "INCLUDES",
          depth: 2,
          hierarchy_mode: "ancestors",
        },
      },
    },
  });
  const restored = new ListFilterModel(FilterMode.Scenes);
  restored.configureFromQueryString(filter.makeQueryParameters());
  expect(restored.makeFilterAST()).toEqual(filter.makeFilterAST());
});
