import { describe, expect, it } from "vitest";
import { FilterMode, type SavedFilterDataFragment } from "./generated-graphql";
import {
  buildDefaultFilterFromSavedFilter,
  buildRenameSavedFilterInput,
} from "./saved-filters";

// Criteria the list model does not know about, including keys inside a
// condition value and on a group. A rename must send them back unchanged.
const storedCriteria = {
  root: {
    group: {
      operator: "AND",
      children: [
        {
          condition: {
            field: "rating",
            value: { value: [{ id: "3", label: "3" }], modifier: "INCLUDES" },
          },
        },
        {
          condition: {
            field: "future_criterion",
            value: {
              value: "keep",
              modifier: "EQUALS",
              extra: { nested: [1] },
            },
          },
        },
      ],
      future_group_key: true,
    },
  },
};

const stored = {
  __typename: "SavedFilter",
  id: "7",
  mode: FilterMode.Scenes,
  name: "Old name",
  find_filter: {
    __typename: "SavedFindFilterType",
    q: "beach",
    page: 2,
    per_page: 40,
    sort: "date",
    direction: "DESC",
  },
  filter_ast: storedCriteria,
  ui_options: {},
} as unknown as SavedFilterDataFragment;

describe("buildRenameSavedFilterInput", () => {
  it("changes only the name and keeps the stored identity and mode", () => {
    expect(buildRenameSavedFilterInput(stored, "New name")).toEqual({
      id: "7",
      mode: FilterMode.Scenes,
      name: "New name",
      find_filter: {
        q: "beach",
        page: 2,
        per_page: 40,
        sort: "date",
        direction: "DESC",
      },
      filter_ast: storedCriteria,
      ui_options: {},
    });
  });

  it("preserves criteria it does not understand, field for field", () => {
    const input = buildRenameSavedFilterInput(stored, "New name");
    expect(input.filter_ast).toEqual(storedCriteria);
    expect(input.filter_ast).toBe(stored.filter_ast);
  });

  it("drops only the Apollo typename from the find filter", () => {
    const input = buildRenameSavedFilterInput(stored, "New name");
    expect(input.find_filter).not.toHaveProperty("__typename");
  });

  it("keeps absent criteria null rather than inventing criteria", () => {
    const empty = {
      ...stored,
      find_filter: null,
      filter_ast: null,
    } as unknown as SavedFilterDataFragment;
    const input = buildRenameSavedFilterInput(empty, "Renamed");
    expect(input.find_filter).toBeNull();
    expect(input.filter_ast).toBeNull();
  });
});

describe("buildDefaultFilterFromSavedFilter", () => {
  it("copies the stored criteria without the Apollo typename", () => {
    expect(buildDefaultFilterFromSavedFilter(stored)).toEqual({
      mode: FilterMode.Scenes,
      find_filter: {
        q: "beach",
        page: 2,
        per_page: 40,
        sort: "date",
        direction: "DESC",
      },
      filter_ast: storedCriteria,
      ui_options: {},
    });
  });
});
