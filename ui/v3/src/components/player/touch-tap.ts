export const TOUCH_TAP_MAX_DURATION_MS = 250;
export const TOUCH_TAP_MOVE_TOLERANCE_PX = 10;
export const TOUCH_HOLD_MOVE_TOLERANCE_PX = 16;

interface TouchPosition {
  x: number;
  y: number;
}

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

/**
 * Movement may cancel a long-press only before its action activates. Once the
 * temporary speed boost has claimed the single-finger gesture, it remains
 * active until release; multi-touch still cancels immediately.
 */
export function shouldCancelTouchHoldOnMove(
  start: TouchPosition,
  current: TouchPosition | null,
  touchCount: number,
  active: boolean,
): boolean {
  if (touchCount !== 1 || !current) return true;
  if (active) return false;
  return (
    Math.hypot(current.x - start.x, current.y - start.y) >
    TOUCH_HOLD_MOVE_TOLERANCE_PX
  );
}
