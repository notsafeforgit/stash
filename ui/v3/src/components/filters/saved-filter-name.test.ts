import { describe, expect, it } from "vitest";
import {
  checkSavedFilterRename,
  findSavedFilterByName,
} from "./saved-filter-name";

const filters = [
  { id: "1", name: "Favourites" },
  { id: "2", name: "Unrated" },
];

describe("findSavedFilterByName", () => {
  it("matches names case-insensitively after trimming", () => {
    expect(findSavedFilterByName(filters, "  FAVOURITES ", "en-GB")?.id).toBe(
      "1",
    );
  });

  it("returns undefined when no name matches", () => {
    expect(findSavedFilterByName(filters, "Starred", "en-GB")).toBeUndefined();
  });

  it("ignores the excluded filter so a rename can change only its case", () => {
    expect(
      findSavedFilterByName(filters, "favourites", "en-GB", "1"),
    ).toBeUndefined();
  });
});

describe("checkSavedFilterRename", () => {
  const current = { id: "1", name: "Favourites" };

  it("rejects an empty or whitespace-only name", () => {
    expect(checkSavedFilterRename(filters, current, "   ", "en-GB")).toEqual({
      status: "empty",
    });
  });

  it("rejects a case-insensitive duplicate of another saved filter", () => {
    expect(
      checkSavedFilterRename(filters, current, "  unrated ", "en-GB"),
    ).toEqual({
      status: "duplicate",
      match: { id: "2", name: "Unrated" },
    });
  });

  it("accepts a case-only change to the same filter's own name", () => {
    expect(
      checkSavedFilterRename(filters, current, "FAVOURITES", "en-GB"),
    ).toEqual({ status: "valid", name: "FAVOURITES" });
  });

  it("treats the unchanged trimmed name as a no-op", () => {
    expect(
      checkSavedFilterRename(filters, current, " Favourites ", "en-GB"),
    ).toEqual({ status: "unchanged" });
  });

  it("returns the trimmed name for a new unique name", () => {
    expect(
      checkSavedFilterRename(filters, current, "  Top rated  ", "en-GB"),
    ).toEqual({ status: "valid", name: "Top rated" });
  });
});
