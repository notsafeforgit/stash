/** Keyframe times are rounded to milliseconds by the server. */
const SAME_FRAME_SECONDS = 0.002;
/** The server returns the greatest keyframe at or before `t + 0.010`. */
export const KEYFRAME_TOLERANCE_SECONDS = 0.01;

export interface ScrubStill {
  /** Scene time of the returned source keyframe. */
  time: number;
  /** Latest requested time known to resolve to this keyframe. */
  until: number;
  url: string;
  bytes: number;
  /** Background coverage, retained before foreground adjustments. */
  coarse: boolean;
}

/** Temporary compressed keyframe stills, not a collection of decoded GPU
 * textures. Only the currently displayed image is decoded. No OPFS/database
 * persistence; canonical still URLs can still be reused from the HTTP cache. */
export class ScrubFrameCache {
  private frames: ScrubStill[] = [];
  private bytes = 0;

  constructor(
    private readonly maxBytes = 4 * 1024 * 1024,
    private readonly maxFrames = 256,
  ) {}

  get size() {
    return this.frames.length;
  }

  get(time: number) {
    return this.frames.find(
      (frame) => Math.abs(frame.time - time) < SAME_FRAME_SECONDS,
    );
  }

  has(time: number) {
    return this.get(time) !== undefined;
  }

  /** Without a keyframe index, a request for `t` that returned keyframe `k`
   * establishes that every target in `[k, t]` resolves to the same still. */
  covering(time: number) {
    return this.frames.find(
      (frame) =>
        frame.time <= time + KEYFRAME_TOLERANCE_SECONDS &&
        time <= frame.until + SAME_FRAME_SECONDS,
    );
  }

  add(time: number, blob: Blob, coarse: boolean, until = time) {
    const existing = this.get(time);
    if (existing) {
      existing.until = Math.max(existing.until, until);
      existing.coarse ||= coarse;
      return existing;
    }
    if (blob.size > this.maxBytes) return undefined;
    while (
      this.frames.length &&
      (this.bytes + blob.size > this.maxBytes ||
        this.frames.length >= this.maxFrames)
    ) {
      // Retain broad coverage before recent fine adjustments.
      const fine = this.frames.findIndex((frame) => !frame.coarse);
      const [old] = this.frames.splice(fine < 0 ? 0 : fine, 1);
      this.bytes -= old!.bytes;
      URL.revokeObjectURL(old!.url);
    }
    const frame = {
      time,
      until: Math.max(time, until),
      url: URL.createObjectURL(blob),
      bytes: blob.size,
      coarse,
    };
    this.frames.push(frame);
    this.bytes += blob.size;
    return frame;
  }

  nearest(time: number, maxDistance: number) {
    let best: ScrubStill | undefined;
    for (const frame of this.frames)
      if (
        Math.abs(frame.time - time) <= maxDistance &&
        (!best || Math.abs(frame.time - time) < Math.abs(best.time - time))
      )
        best = frame;
    return best;
  }

  clear() {
    for (const frame of this.frames) URL.revokeObjectURL(frame.url);
    this.frames = [];
    this.bytes = 0;
  }
}

/** Breadth-first coverage: the middle, quarters, eighths, etc. Used as raw
 * still request times when the server cannot supply a keyframe index. */
export function scrubPreparationTimes(start: number, end: number): number[] {
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start)
    return [];
  const count = Math.min(63, Math.max(1, Math.ceil((end - start) / 4)));
  const result: number[] = [];
  for (let divisions = 2; result.length < count; divisions *= 2)
    for (
      let numerator = 1;
      numerator < divisions && result.length < count;
      numerator += 2
    )
      result.push(start + ((end - start) * numerator) / divisions);
  return result;
}
