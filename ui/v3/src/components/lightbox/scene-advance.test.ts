import { describe, expect, it } from "vitest";
import { followingSlide } from "./scene-advance";

const slides = ["a", "b", "c"];

describe("followingSlide", () => {
  it("returns the next slide inside a carousel", () => {
    expect(followingSlide(slides, "a", false)).toBe("b");
  });

  it("wraps a looping carousel from its last slide", () => {
    expect(followingSlide(slides, "c", false)).toBe("a");
  });

  it("stops at the end of a finite carousel", () => {
    expect(followingSlide(slides, "c", true)).toBeUndefined();
  });

  it("never advances a lone slide onto itself", () => {
    expect(followingSlide(["only"], "only", false)).toBeUndefined();
  });

  it("returns nothing for a slide that is not in the list", () => {
    expect(followingSlide(slides, "missing", false)).toBeUndefined();
  });
});
