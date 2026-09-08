import { describe, expect, it } from "vitest";
import {
  bulkCustomFieldsInput,
  bulkCustomFieldsSchema,
  type BulkCustomFieldChange,
} from "./bulk-custom-fields";

const summary = {
  count: 2,
  shared_names: ["score", "old", "blank", "keep"],
  partial_names: ["private"],
};
const schema = bulkCustomFieldsSchema(summary);

describe("bulk custom-field updates", () => {
  it("keeps existing values, clears values separately from removing fields, and adds new fields", () => {
    expect(bulkCustomFieldsInput(undefined)).toBeUndefined();
    expect(bulkCustomFieldsInput({ shared: [], added: [] })).toBeUndefined();
    expect(
      bulkCustomFieldsInput({
        shared: [{ name: "keep", action: "keep", value: "ignored" }],
        added: [],
      }),
    ).toBeUndefined();
    expect(
      bulkCustomFieldsInput({
        shared: [
          { name: "keep", action: "keep", value: "ignored" },
          { name: "score", action: "set", value: "12.5" },
          { name: "blank", action: "clear", value: "ignored" },
          { name: "old", action: "remove", value: "ignored" },
        ],
        added: [{ name: "new", value: "hello" }],
      }),
    ).toEqual({
      partial: { score: 12.5, blank: "", new: "hello" },
      remove: ["old"],
    });
  });

  it("preserves literal field names, blank values, and non-decimal text", () => {
    const input = bulkCustomFieldsInput({
      shared: [],
      added: [
        { name: "__proto__", value: "literal" },
        { name: "blank", value: "" },
        { name: "code", value: "0012" },
        { name: "date", value: "2026-09-08" },
        { name: "negative", value: "-2" },
        { name: "overflow", value: "9".repeat(400) },
      ],
    });
    expect(JSON.stringify(input)).toBe(
      `{"partial":{"__proto__":"literal","blank":"","code":"0012","date":"2026-09-08","negative":-2,"overflow":"${"9".repeat(400)}"},"remove":[]}`,
    );
  });

  it.each([
    "",
    " ",
    " leading",
    "trailing ",
    "x".repeat(65),
    "é".repeat(33),
  ])("rejects invalid new field names: %j", (name) => {
    expect(
      schema.safeParse({ shared: [], added: [{ name, value: "x" }] }).success,
    ).toBe(false);
  });

  it("rejects duplicate actions and set/remove conflicts on the same name", () => {
    for (const action of ["set", "clear", "remove"] as const) {
      const shared: BulkCustomFieldChange[] = [
        { name: "score", action: "set", value: "1" },
        { name: "score", action, value: "2" },
      ];
      expect(
        schema
          .safeParse({ shared, added: [] })
          .error?.issues.map((issue) => issue.path),
      ).toEqual([
        ["shared", 0, "name"],
        ["shared", 1, "name"],
      ]);
    }
    expect(
      schema.safeParse({
        shared: [],
        added: [
          { name: "new", value: "1" },
          { name: "new", value: "2" },
        ],
      }).success,
    ).toBe(false);
  });

  it("allows changes only to shared fields and additions only to unused names", () => {
    for (const action of ["set", "clear", "remove"] as const) {
      expect(
        schema.safeParse({
          shared: [{ name: "private", action, value: "" }],
          added: [],
        }).success,
      ).toBe(false);
      expect(
        schema.safeParse({
          shared: [{ name: "missing", action, value: "" }],
          added: [],
        }).success,
      ).toBe(false);
      expect(
        schema.safeParse({
          shared: [{ name: "score", action, value: "" }],
          added: [],
        }).success,
      ).toBe(true);
    }
    for (const name of ["score", "private"]) {
      expect(
        schema.safeParse({ shared: [], added: [{ name, value: "" }] }).success,
      ).toBe(false);
    }
  });

  it("requires a loaded nonempty target for changes but allows unrelated edits", () => {
    for (const unavailable of [undefined, { ...summary, count: 0 }]) {
      const check = bulkCustomFieldsSchema(unavailable);
      expect(check.safeParse(undefined).success).toBe(true);
      expect(
        check.safeParse({ shared: [], added: [{ name: "new", value: "" }] })
          .success,
      ).toBe(false);
    }
  });

  it("matches the server's UTF-8 length limit without trimming or folding names", () => {
    expect(
      schema.safeParse({
        shared: [],
        added: [
          { name: "é".repeat(32), value: "" },
          { name: "Score", value: "1" },
          { name: "__proto__", value: "literal" },
        ],
      }).success,
    ).toBe(true);
  });
});
