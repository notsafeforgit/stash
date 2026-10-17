/** Temporary compressed snapshots, not a collection of decoded GPU textures.
 * Only the currently displayed image is decoded. No OPFS/database persistence. */
export class ScrubFrameCache {
  private frames: {
    time: number;
    url: string;
    bytes: number;
    coarse: boolean;
  }[] = [];
  private bytes = 0;

  constructor(
    private readonly maxBytes = 4 * 1024 * 1024,
    private readonly maxFrames = 96,
  ) {}

  get size() {
    return this.frames.length;
  }

  has(time: number) {
    return this.frames.some((frame) => Math.abs(frame.time - time) < 0.05);
  }

  add(time: number, blob: Blob, coarse: boolean) {
    if (blob.size > this.maxBytes || this.has(time)) return;
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
    this.frames.push({
      time,
      url: URL.createObjectURL(blob),
      bytes: blob.size,
      coarse,
    });
    this.bytes += blob.size;
  }

  nearest(time: number, maxDistance: number) {
    let best: (typeof this.frames)[number] | undefined;
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

/** Breadth-first coverage: the middle, quarters, eighths, etc. Fine sampling
 * follows the active gesture through the full-frame-rate scrub stream. */
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
