import { describe, expect, it } from "vitest";
import { displayedQueryData } from "./use-cached-query-result";

describe("list query recovery", () => {
  const cached = { key: "page=1", data: { count: 1, items: ["scene"] } };
  it("keeps usable content after a failed refresh", () => {
    expect(
      displayedQueryData(
        { loading: false, error: new Error("offline") },
        cached.key,
        cached,
      ),
    ).toBe(cached.data);
  });
  it("never presents another page as a response to a failed query", () => {
    expect(
      displayedQueryData(
        { loading: false, error: new Error("offline") },
        "page=2",
        cached,
      ),
    ).toBeUndefined();
  });
  it("keeps counts during paging and replaces them with a successful empty result", () => {
    expect(displayedQueryData({ loading: true }, "page=2", cached)).toBe(
      cached.data,
    );
    const empty = { count: 0, items: [] };
    expect(
      displayedQueryData({ loading: false, data: empty }, "page=2", cached),
    ).toBe(empty);
  });
});
