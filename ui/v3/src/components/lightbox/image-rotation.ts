import { ImageRotateDirection } from "src/core/generated-graphql";

export function inverseImageRotationDirection(
  direction: ImageRotateDirection,
): ImageRotateDirection {
  switch (direction) {
    case ImageRotateDirection.Cw:
      return ImageRotateDirection.Ccw;
    case ImageRotateDirection.Ccw:
      return ImageRotateDirection.Cw;
    case ImageRotateDirection.Flip:
      return ImageRotateDirection.Flip;
  }
}
