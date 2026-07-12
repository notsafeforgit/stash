import { describe, expect, it } from "vitest";
import { calculateVisualViewportBottomInset } from "./use-visual-viewport-bottom-inset";

describe("calculateVisualViewportBottomInset", () => {
  it("returns the keyboard-sized gap below a shrunken visual viewport", () => {
    expect(
      calculateVisualViewportBottomInset(800, {
        height: 470,
        offsetTop: 0,
      }),
    ).toBe(330);
  });

  it("accounts for a visual viewport that has been panned", () => {
    expect(
      calculateVisualViewportBottomInset(800, {
        height: 470,
        offsetTop: 60,
      }),
    ).toBe(270);
  });

  it("does not return a negative inset when content resizing is native", () => {
    expect(
      calculateVisualViewportBottomInset(470, {
        height: 470,
        offsetTop: 0,
      }),
    ).toBe(0);
  });
});
