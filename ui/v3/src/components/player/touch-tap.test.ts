import { describe, expect, it } from "vitest";
import {
  exceedsTouchTapMovement,
  isCompletedTouchTap,
  shouldCancelTouchHoldOnMove,
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

describe("scene-player touch holds", () => {
  const start = { x: 20, y: 30 };

  it("tolerates small movement while the hold is pending", () => {
    expect(shouldCancelTouchHoldOnMove(start, { x: 30, y: 40 }, 1, false)).toBe(
      false,
    );
  });

  it("cancels an intentional drag before the hold activates", () => {
    expect(shouldCancelTouchHoldOnMove(start, { x: 40, y: 30 }, 1, false)).toBe(
      true,
    );
  });

  it("keeps an active speed hold claimed despite finger movement", () => {
    expect(shouldCancelTouchHoldOnMove(start, { x: 80, y: 80 }, 1, true)).toBe(
      false,
    );
  });

  it("cancels an active hold when another finger joins", () => {
    expect(shouldCancelTouchHoldOnMove(start, null, 2, true)).toBe(true);
  });
});
