/** Convert viewport deltas into a surface rotated by a quarter turn. */
export function presentationCoordinates(
  x: number,
  y: number,
  rotation: 0 | 90 | -90,
) {
  return rotation === 90
    ? { x: y, y: -x }
    : rotation === -90
      ? { x: -y, y: x }
      : { x, y };
}
