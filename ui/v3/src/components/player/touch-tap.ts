export const TOUCH_TAP_MAX_DURATION_MS = 250;
export const TOUCH_TAP_MOVE_TOLERANCE_PX = 10;

export interface TouchTapCandidate {
  startedAt: number;
  startX: number;
  startY: number;
  moved: boolean;
}

export function exceedsTouchTapMovement(
  candidate: Pick<TouchTapCandidate, "startX" | "startY">,
  clientX: number,
  clientY: number,
): boolean {
  return (
    Math.hypot(clientX - candidate.startX, clientY - candidate.startY) >
    TOUCH_TAP_MOVE_TOLERANCE_PX
  );
}

export function isCompletedTouchTap(
  candidate: TouchTapCandidate,
  endedAt: number,
  clientX: number,
  clientY: number,
): boolean {
  const duration = endedAt - candidate.startedAt;
  return (
    !candidate.moved &&
    duration >= 0 &&
    duration <= TOUCH_TAP_MAX_DURATION_MS &&
    !exceedsTouchTapMovement(candidate, clientX, clientY)
  );
}
