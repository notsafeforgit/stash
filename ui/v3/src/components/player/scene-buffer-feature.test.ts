// @vitest-environment jsdom
import { HTMLVideoAdapter } from "@videojs/media/dom";
import type { MediaBufferState, TimeRangeLike } from "@videojs/media";
import { afterEach, expect, it, vi } from "vitest";
import { sceneBufferFeature } from "./scene-buffer-feature";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

it("tracks silent buffer growth and eviction without publishing unchanged snapshots or polling hidden/detached players", () => {
  vi.useFakeTimers();
  const element = document.createElement("video");
  const media = new HTMLVideoAdapter();
  const lifetime = new AbortController();
  let ranges: [number, number][] = [];
  let readyState = 0;
  let hidden = false;
  const buffered = vi.spyOn(element, "buffered", "get").mockImplementation(
    (): TimeRangeLike => ({
      length: ranges.length,
      start(index) {
        const range = ranges[index];
        if (!range) throw new Error("Invalid range");
        return range[0];
      },
      end(index) {
        const range = ranges[index];
        if (!range) throw new Error("Invalid range");
        return range[1];
      },
    }),
  );
  vi.spyOn(element, "readyState", "get").mockImplementation(() => readyState);
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
  let state: MediaBufferState = { buffered: [], seekable: [] };
  const publish = vi.fn((patch: Partial<MediaBufferState>) => {
    state = { ...state, ...patch };
  });
  media.attach(element);
  try {
    sceneBufferFeature.attach?.({
      target: { media, container: document.createElement("div") },
      signal: lifetime.signal,
      get: () => state,
      set: publish,
      store: { state: {}, subscribe: () => () => {} },
      reportError: (error) => {
        throw error;
      },
    });
    buffered.mockClear();
    vi.advanceTimersByTime(1000);
    expect(buffered).not.toHaveBeenCalled();

    readyState = 1;
    element.dispatchEvent(new Event("loadedmetadata"));
    ranges = [[0, 2]];
    vi.advanceTimersByTime(250);
    expect(state.buffered).toEqual([[0, 2]]);
    const initial = state.buffered;
    vi.advanceTimersByTime(1000);
    expect(publish).toHaveBeenCalledTimes(1);
    expect(state.buffered).toBe(initial);

    ranges = [
      [0, 8],
      [10, 12],
    ];
    vi.advanceTimersByTime(250);
    expect(state.buffered).toEqual(ranges);
    expect(initial).toEqual([[0, 2]]);
    ranges = [[6, 8]];
    vi.advanceTimersByTime(250);
    expect(state.buffered).toEqual([[6, 8]]);

    hidden = true;
    document.dispatchEvent(new Event("visibilitychange"));
    buffered.mockClear();
    ranges = [];
    vi.advanceTimersByTime(1000);
    expect(buffered).not.toHaveBeenCalled();
    hidden = false;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(state.buffered).toEqual([]);

    readyState = 0;
    element.dispatchEvent(new Event("emptied"));
    buffered.mockClear();
    vi.advanceTimersByTime(1000);
    expect(buffered).not.toHaveBeenCalled();
    readyState = 1;
    element.dispatchEvent(new Event("loadedmetadata"));
    lifetime.abort();
    buffered.mockClear();
    vi.advanceTimersByTime(1000);
    element.dispatchEvent(new Event("progress"));
    expect(buffered).not.toHaveBeenCalled();
  } finally {
    lifetime.abort();
    media.destroy();
  }
});
