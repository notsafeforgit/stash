import { describe, expect, it } from "vitest";
import {
  exceedsTouchTapMovement,
  isCompletedTouchTap,
  type TouchTapCandidate,
} from "./touch-tap";

const candidate = (
  overrides: Partial<TouchTapCandidate> = {},
): TouchTapCandidate => ({
  startedAt: 100,
  startX: 20,
  startY: 30,
  moved: false,
  ...overrides,
});

describe("scene-player touch taps", () => {
  it("accepts a quick release within the movement tolerance", () => {
    expect(isCompletedTouchTap(candidate(), 250, 25, 35)).toBe(true);
  });

  it("rejects a hold even when the finger stays still", () => {
    expect(isCompletedTouchTap(candidate(), 351, 20, 30)).toBe(false);
  });

  it("rejects a drag that returns to its starting point", () => {
    expect(isCompletedTouchTap(candidate({ moved: true }), 200, 20, 30)).toBe(
      false,
    );
  });

  it("detects movement outside the tap tolerance", () => {
    expect(exceedsTouchTapMovement(candidate(), 31, 30)).toBe(true);
    expect(exceedsTouchTapMovement(candidate(), 30, 30)).toBe(false);
  });
});
