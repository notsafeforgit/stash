// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createBufferedSeekPreview } from "./buffered-seek-preview";

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function mediaFixture(initiallyPaused = false) {
  const video = document.createElement("video");
  let paused = initiallyPaused;
  let time = 2;
  let seeking = false;
  let ranges = [
    [0, 10],
    [20, 30],
  ];
  const seeks: number[] = [];
  Object.defineProperties(video, {
    readyState: { get: () => 4 },
    paused: { get: () => paused },
    seeking: { get: () => seeking },
    currentTime: {
      get: () => time,
      set: (next: number) => {
        time = next;
        seeking = true;
        seeks.push(next);
      },
    },
    buffered: {
      get: (): TimeRanges => ({
        length: ranges.length,
        start: (i) => ranges[i]?.[0] ?? 0,
        end: (i) => ranges[i]?.[1] ?? 0,
      }),
    },
  });
  vi.spyOn(video, "pause").mockImplementation(() => {
    paused = true;
  });
  const resume = vi.fn();
  const suspend = vi.fn(() => resume);
  const controller = createBufferedSeekPreview();
  const preview = {
    ...controller,
    preview: (target: number) => controller.preview(video, target, suspend),
  };
  return {
    video,
    preview,
    seeks,
    suspend,
    resume,
    replaceRanges: (next: number[][]) => {
      ranges = next;
    },
    settle: () => {
      seeking = false;
      video.dispatchEvent(new Event("seeked"));
    },
    frame: () => vi.advanceTimersByTime(20),
  };
}

describe("buffered frame scrubbing", () => {
  it("pauses once and coalesces positions while a decoded seek is in flight", () => {
    const f = mediaFixture();
    f.preview.preview(3);
    f.preview.preview(4);
    f.frame();
    expect(f.seeks).toEqual([4]);
    f.preview.preview(6);
    f.frame();
    f.preview.preview(8);
    f.frame();
    expect(f.seeks).toEqual([4]);
    f.settle();
    f.frame();
    expect(f.seeks).toEqual([4, 8]);
    expect(f.video.pause).toHaveBeenCalledTimes(1);
    expect(f.suspend).toHaveBeenCalledTimes(1);
    expect(f.preview.take()).toMatchObject({ wasPaused: false, mediaTime: 2 });
  });

  it("does not seek into buffer holes or data evicted before the next frame", () => {
    const f = mediaFixture();
    for (const target of [-1, 10, 15, 30, 100]) {
      f.preview.preview(target);
      f.frame();
    }
    f.preview.preview(5);
    f.replaceRanges([[20, 30]]);
    f.frame();
    expect(f.seeks).toEqual([]);
    f.preview.preview(25);
    f.frame();
    expect(f.seeks).toEqual([25]);
    f.preview.dispose();
  });

  it.each([true, false])(
    "hands the original position and paused=%s intent to the commit owner",
    (paused) => {
      const f = mediaFixture(paused);
      f.preview.preview(5);
      f.frame();
      f.preview.preview(8);
      const state = f.preview.take();
      expect(state).toMatchObject({ wasPaused: paused, mediaTime: 2 });
      f.settle();
      f.frame();
      expect(f.seeks).toEqual([5]);
      expect(f.resume).not.toHaveBeenCalled();
      state?.resumeBuffering();
      expect(f.resume).toHaveBeenCalledTimes(1);
      expect(f.preview.take()).toBeNull();
    },
  );

  it("respects a new explicit pause instead of restoring old playback intent", () => {
    const f = mediaFixture();
    f.preview.preview(5);
    f.preview.setPaused(true);
    expect(f.preview.take()?.wasPaused).toBe(true);
    f.frame();
    expect(f.seeks).toEqual([]);
  });

  it("releases loading and queued seeks when the source is replaced", () => {
    const f = mediaFixture();
    f.preview.preview(5);
    f.frame();
    f.preview.preview(8);
    f.preview.dispose();
    f.settle();
    f.frame();
    f.preview.dispose();
    expect(f.seeks).toEqual([5]);
    expect(f.resume).toHaveBeenCalledTimes(1);
  });
});
