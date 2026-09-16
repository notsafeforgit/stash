// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { createPausedFrame } from "./paused-frame";

afterEach(() => vi.restoreAllMocks());

function fixture() {
  const video = document.createElement("video");
  const state = {
    paused: false,
    seeking: false,
    ended: false,
    readyState: 4,
    currentTime: 12.5,
    currentSrc: "blob:current-source",
    remote: { state: "disconnected" },
  };
  const seeks: number[] = [];
  let callback: VideoFrameRequestCallback | undefined;
  let counter = 0;
  const request = vi.fn((next: VideoFrameRequestCallback) => {
    callback = next;
    return ++counter;
  });
  const cancel = vi.fn(() => {
    callback = undefined;
  });
  for (const key of Object.keys(state) as (keyof typeof state)[]) {
    Object.defineProperty(video, key, {
      get: () => state[key],
      ...(key === "currentTime" && {
        set: (time: number) => {
          state.currentTime = time;
          seeks.push(time);
        },
      }),
    });
  }
  Object.defineProperties(video, {
    buffered: {
      value: {
        length: 2,
        start: (index: number) => (index === 0 ? 0 : 10),
        end: (index: number) => (index === 0 ? 5 : 20),
      } satisfies TimeRanges,
    },
    requestVideoFrameCallback: { value: request, configurable: true },
    cancelVideoFrameCallback: { value: cancel },
  });
  const controller = createPausedFrame();
  controller.attach(video);
  const pause = vi.fn(() => {
    state.paused = true;
  });
  const play = vi.fn(() => {
    state.paused = false;
  });
  const frame = (time: number, displayAt = performance.now()) => {
    const pending = callback;
    callback = undefined;
    pending?.(performance.now(), {
      mediaTime: time,
      expectedDisplayTime: displayAt,
      presentationTime: displayAt,
      presentedFrames: counter,
      width: 1920,
      height: 1080,
    });
  };
  return {
    video,
    state,
    seeks,
    controller,
    frame,
    pause,
    play,
    cancel,
    request,
  };
}

describe("explicit pause frame preservation", () => {
  it("repositions a clock ahead of the visible frame before resuming", () => {
    const f = fixture();
    f.frame(12);
    f.controller.preserveOnPause(f.pause);
    expect(f.pause).toHaveBeenCalledOnce();
    expect(f.seeks).toEqual([12.000001]);
    expect(f.state.paused).toBe(true);
    f.controller.preserveOnPause(f.play);
    expect(f.play).toHaveBeenCalledOnce();
    expect(f.state.paused).toBe(false);
    expect(f.seeks).toHaveLength(1);
  });

  it("uses the displayed frame instead of a frame queued for the next vsync", () => {
    const f = fixture();
    f.frame(12, performance.now() - 20);
    f.frame(12 + 1 / 30, performance.now() + 30);
    f.controller.preserveOnPause(f.pause);
    expect(f.seeks).toEqual([12.000001]);
  });

  it("does not rewind past a marker's fractional start", () => {
    const f = fixture();
    f.frame(12);
    f.controller.preserveOnPause(f.pause, 12.015);
    expect(f.seeks).toEqual([12.015]);
  });

  it.each([
    "seeking",
    "emptied",
    "loadstart",
  ])("forgets frames on %s and resumes observing the new presentation", (event) => {
    const f = fixture();
    f.frame(12);
    f.video.dispatchEvent(new Event(event));
    f.controller.preserveOnPause(f.pause);
    expect(f.seeks).toEqual([]);
    f.play();
    f.frame(15);
    f.controller.preserveOnPause(f.pause);
    expect(f.seeks).toEqual([15.000001]);
  });

  it.each([
    "buffer hole",
    "stale",
    "future",
    "old source",
    "seeking",
    "ended",
    "no data",
    "remote",
    "hidden",
  ])("preserves the pause command without seeking for %s", (condition) => {
    const f = fixture();
    f.frame(
      condition === "buffer hole" ? 8 : 12,
      performance.now() +
        (condition === "stale" ? -300 : condition === "future" ? 100 : 0),
    );
    if (condition === "old source") f.state.currentSrc = "blob:new-source";
    if (condition === "seeking") f.state.seeking = true;
    if (condition === "ended") f.state.ended = true;
    if (condition === "no data") f.state.readyState = 0;
    if (condition === "remote") f.state.remote.state = "connected";
    if (condition === "hidden")
      vi.spyOn(document, "hidden", "get").mockReturnValue(true);
    f.controller.preserveOnPause(f.pause);
    expect(f.pause).toHaveBeenCalledOnce();
    expect(f.state.paused).toBe(true);
    expect(f.seeks).toEqual([]);
  });

  it("leaves native playback intact when frame callbacks are unsupported", () => {
    const f = fixture();
    f.controller.attach(null);
    Object.defineProperty(f.video, "requestVideoFrameCallback", {
      value: undefined,
    });
    f.controller.attach(f.video);
    f.controller.preserveOnPause(f.pause);
    expect(f.pause).toHaveBeenCalledOnce();
    expect(f.seeks).toEqual([]);
  });

  it("does not overwrite a seek performed by the command", () => {
    const f = fixture();
    f.frame(12);
    f.controller.preserveOnPause(() => {
      f.pause();
      f.state.seeking = true;
      f.state.currentTime = 18;
    });
    expect(f.state.currentTime).toBe(18);
    expect(f.seeks).toEqual([]);
  });

  it("cancels observation and discards frames when the video is detached", () => {
    const f = fixture();
    f.frame(12);
    const requested = f.request.mock.calls.length;
    f.controller.attach(null);
    f.frame(15);
    expect(f.cancel).toHaveBeenCalledOnce();
    expect(f.request).toHaveBeenCalledTimes(requested);
    f.controller.attach(f.video);
    f.controller.preserveOnPause(f.pause);
    expect(f.seeks).toEqual([]);
  });

  it("ignores a queued callback from a superseded presentation", () => {
    const f = fixture();
    const old = f.request.mock.calls.at(-1)?.[0];
    f.controller.reset();
    old?.(performance.now(), {
      mediaTime: 12,
      expectedDisplayTime: performance.now(),
      presentationTime: performance.now(),
      presentedFrames: 1,
      width: 1920,
      height: 1080,
    });
    f.controller.preserveOnPause(f.pause);
    expect(f.seeks).toEqual([]);
    f.play();
    f.frame(15);
    f.controller.preserveOnPause(f.pause);
    expect(f.seeks).toEqual([15.000001]);
  });
});
