import { describe, expect, it } from "vitest";
import {
  clampListScrollTop,
  getListDeletionScrollTop,
  getListPageChangeScrollTarget,
  shouldPreserveListScrollDuringRefill,
} from "./list-scroll-state";

describe("entity-list deletion scroll state", () => {
  it("preserves scroll while a non-final page waits for a replacement", () => {
    expect(shouldPreserveListScrollDuringRefill(1, 40, 99, 39)).toBe(true);
  });

  it("does not preserve beyond the real end of a shortened final page", () => {
    expect(shouldPreserveListScrollDuringRefill(3, 40, 85, 5)).toBe(false);
  });

  it("does not preserve an invalid page that must move to the new last page", () => {
    expect(shouldPreserveListScrollDuringRefill(3, 40, 80, 0)).toBe(false);
  });

  it("keeps a valid scroll position unchanged", () => {
    expect(clampListScrollTop(640, 1800, 700)).toBe(640);
  });

  it("clamps a removed tail position to the new bottom", () => {
    expect(clampListScrollTop(1200, 1350, 700)).toBe(650);
  });

  it("preserves an embedded detail-page position after deletion", () => {
    expect(getListDeletionScrollTop(12, 9, 12, 9, 640, 1800, 700)).toBe(640);
  });

  it("moves an embedded detail page only as far as its shortened bottom", () => {
    expect(getListDeletionScrollTop(12, 4, 12, 4, 1200, 1350, 700)).toBe(650);
  });

  it("does not restore embedded page scroll when the result did not shrink", () => {
    expect(getListDeletionScrollTop(12, 12, 12, 12, 640, 1800, 700)).toBeNull();
  });

  it("restores when cards disappear before Apollo updates the count", () => {
    expect(getListDeletionScrollTop(12, 12, 12, 9, 640, 1800, 700)).toBe(640);
  });

  it("starts normal pages at the top and removed pages at the end", () => {
    expect(getListPageChangeScrollTarget(1, 2, 3)).toBe("start");
    expect(getListPageChangeScrollTarget(3, 2, 2)).toBe("end");
    expect(getListPageChangeScrollTarget(2, 2, 2)).toBeNull();
  });
});
