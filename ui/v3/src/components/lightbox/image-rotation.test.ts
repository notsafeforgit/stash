import { describe, expect, it } from "vitest";
import { ImageRotateDirection } from "src/core/generated-graphql";
import { inverseImageRotationDirection } from "./image-rotation";

describe("inverseImageRotationDirection", () => {
  it.each([
    [ImageRotateDirection.Cw, ImageRotateDirection.Ccw],
    [ImageRotateDirection.Ccw, ImageRotateDirection.Cw],
    [ImageRotateDirection.Flip, ImageRotateDirection.Flip],
  ])("maps %s to %s", (direction, inverse) => {
    expect(inverseImageRotationDirection(direction)).toBe(inverse);
  });
});
