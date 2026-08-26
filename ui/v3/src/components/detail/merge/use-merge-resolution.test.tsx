import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { defineMergeField } from "./merge-types";
import { useMergeResolution } from "./use-merge-resolution";

interface Entity {
  value: string;
}

interface Update {
  value?: string;
}

const fields = [
  defineMergeField<Entity, Update, string>({
    key: "value",
    labelId: "value",
    defaultLabel: "Value",
    read: (entity) => entity.value,
    isEmpty: (value) => value === "",
    isEqual: (a, b) => a === b,
    preview: (value) => value,
    toUpdate: (input, value) => {
      input.value = value;
    },
  }),
];

function projectDefaultChoice(projectKeepValues: boolean) {
  const projected: Update = {};

  function Harness() {
    const { applyResolutions } = useMergeResolution({
      fields,
      destination: { value: "destination" },
      sources: [
        {
          id: "1",
          entity: { value: "source" },
          label: "Source",
        },
      ],
      projectKeepValues,
    });
    applyResolutions(projected, {});
    return null;
  }

  renderToStaticMarkup(<Harness />);
  return projected;
}

describe("useMergeResolution", () => {
  it("projects a default keep choice for safe merge APIs", () => {
    expect(projectDefaultChoice(true)).toEqual({ value: "destination" });
  });

  it("continues omitting keep choices for legacy merge callers", () => {
    expect(projectDefaultChoice(false)).toEqual({});
  });
});
