import { describe, expect, it } from "vitest";
import {
  clampListScrollTop,
  getListPageChangeScrollTarget,
  shouldAdjustVirtualizedListScrollPosition,
  shouldPreserveListScrollDuringRefill,
} from "./list-scroll-state";

describe("entity-list scroll state", () => {
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

  it("starts normal pages at the top and removed pages at the end", () => {
    expect(getListPageChangeScrollTarget(1, 2, 3)).toBe("start");
    expect(getListPageChangeScrollTarget(3, 2, 2)).toBe("end");
    expect(getListPageChangeScrollTarget(2, 2, 2)).toBeNull();
  });

  it("does not interrupt active scrolling for virtual row measurements", () => {
    expect(
      shouldAdjustVirtualizedListScrollPosition(400, 800, true, false),
    ).toBe(false);
  });

  it("only corrects idle measurements above the viewport", () => {
    expect(
      shouldAdjustVirtualizedListScrollPosition(400, 800, false, false),
    ).toBe(true);
    expect(
      shouldAdjustVirtualizedListScrollPosition(900, 800, false, false),
    ).toBe(false);
    expect(
      shouldAdjustVirtualizedListScrollPosition(400, 800, false, true),
    ).toBe(false);
  });
});
