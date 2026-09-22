// @vitest-environment jsdom
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { usePlayerRecovery } from "./use-player-recovery";

let container: HTMLDivElement;
let root: Root;
let previewing: boolean;
const seek = vi.fn();
const reload = vi.fn();

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  previewing = false;
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function render() {
  const rootRef = createRef<HTMLDivElement>();
  function Fixture() {
    usePlayerRecovery({
      finalSrc: "https://example.test/scene/1/stream.master.m3u8",
      rootRef,
      reloading: false,
      offsetStart: 4,
      isSeekPreviewActive: () => previewing,
      handleSeek: seek,
      forceRemountAt: reload,
    });
    return (
      <div ref={rootRef}>
        <video>
          <track kind="captions" />
        </video>
      </div>
    );
  }
  await act(async () => root.render(<Fixture />));
  const video = container.querySelector("video");
  if (!video) throw new Error("Missing video");
  Object.defineProperties(video, {
    currentTime: { value: 10, configurable: true },
    buffered: {
      value: { length: 1, start: () => 20, end: () => 30 },
    },
  });
  return video;
}

it("recovers an ordinary native seek outside the buffer after scrubbing stops", async () => {
  const video = await render();
  video.dispatchEvent(new Event("seeking"));
  vi.advanceTimersByTime(249);
  expect(seek).not.toHaveBeenCalled();
  vi.advanceTimersByTime(1);
  expect(seek).toHaveBeenCalledExactlyOnceWith(14);
});

it.each(["before the native seek", "before recovery runs"])(
  "leaves the held preview in control when it starts %s",
  async (when) => {
    const video = await render();
    previewing = when === "before the native seek";
    video.dispatchEvent(new Event("seeking"));
    vi.advanceTimersByTime(100);
    previewing = true;
    vi.advanceTimersByTime(1000);
    expect(seek).not.toHaveBeenCalled();
    expect(reload).not.toHaveBeenCalled();
    previewing = false;
    vi.advanceTimersByTime(1000);
    expect(seek).not.toHaveBeenCalled();
    // A later native-fullscreen seek still gets normal recovery.
    video.dispatchEvent(new Event("seeking"));
    vi.advanceTimersByTime(250);
    expect(seek).toHaveBeenCalledExactlyOnceWith(14);
  },
);

async function watchdogFixture(frameCallbacks = true) {
  const rootRef = createRef<HTMLDivElement>();
  const state = {
    time: 10,
    paused: false,
    total: 30,
    dropped: 0,
    hidden: false,
    offscreen: false,
  };
  const callbacks = new Map<number, VideoFrameRequestCallback>();
  let callbackId = 0;
  const requestFrame = vi.fn((callback: VideoFrameRequestCallback) => {
    callbacks.set(++callbackId, callback);
    return callbackId;
  });
  const cancelFrame = vi.fn((id: number) => callbacks.delete(id));
  vi.spyOn(document, "hidden", "get").mockImplementation(() => state.hidden);

  function Fixture({ source }: { source: string }) {
    usePlayerRecovery({
      finalSrc: source,
      rootRef,
      reloading: false,
      offsetStart: 4,
      isSeekPreviewActive: () => previewing,
      handleSeek: seek,
      forceRemountAt: reload,
    });
    return (
      <div ref={rootRef}>
        <video
          ref={(video) => {
            if (!video) return;
            Object.defineProperties(video, {
              currentTime: { configurable: true, get: () => state.time },
              paused: { configurable: true, get: () => state.paused },
              videoWidth: { configurable: true, value: 320 },
              getBoundingClientRect: {
                configurable: true,
                value: () =>
                  new DOMRect(0, state.offscreen ? 2000 : 0, 320, 180),
              },
              getVideoPlaybackQuality: {
                configurable: true,
                value: () => ({
                  totalVideoFrames: state.total,
                  droppedVideoFrames: state.dropped,
                }),
              },
              requestVideoFrameCallback: {
                configurable: true,
                value: frameCallbacks ? requestFrame : undefined,
              },
              cancelVideoFrameCallback: {
                configurable: true,
                value: frameCallbacks ? cancelFrame : undefined,
              },
            });
          }}
        >
          <track kind="captions" />
        </video>
      </div>
    );
  }
  const source = "https://example.test/scene/1/stream.master.m3u8";
  await act(async () => root.render(<Fixture source={source} />));
  const video = container.querySelector("video");
  if (!video) throw new Error("Missing video");
  video.dispatchEvent(new Event("playing"));
  return {
    state,
    video,
    callbacks,
    cancelFrame,
    async replaceSource() {
      await act(async () => root.render(<Fixture source={`${source}?_r=1`} />));
      video.dispatchEvent(new Event("loadstart"));
      video.dispatchEvent(new Event("playing"));
    },
    advance(
      milliseconds: number,
      {
        clock = true,
        picture = true,
        dropped = false,
        decoded = picture,
        sameFrame = false,
      }: {
        clock?: boolean;
        picture?: boolean;
        dropped?: boolean;
        decoded?: boolean;
        sameFrame?: boolean;
      } = {},
    ) {
      for (let elapsed = 0; elapsed < milliseconds; elapsed += 250) {
        if (clock) {
          state.time += 0.25;
          video.dispatchEvent(new Event("timeupdate"));
        }
        if (decoded || dropped) state.total += 8;
        if (dropped) state.dropped += 8;
        if (picture) {
          const pending = [...callbacks.values()];
          callbacks.clear();
          for (const callback of pending)
            callback(performance.now(), {
              mediaTime: sameFrame ? 10 : state.time,
            } as VideoFrameCallbackMetadata);
        }
        vi.advanceTimersByTime(250);
      }
    },
  };
}

it.each([true, false])(
  "recovers frozen video while the audio clock continues (frame callbacks=%s)",
  async (frameCallbacks) => {
    const f = await watchdogFixture(frameCallbacks);
    f.advance(3000);
    f.advance(5000, { picture: false });
    expect(reload).toHaveBeenCalledOnce();
    expect(reload.mock.calls[0]?.[0]).toBeGreaterThanOrEqual(20);
    expect(reload.mock.calls[0]?.[1]).toBe(true);
  },
);

it("does not count dropped frames as picture progress", async () => {
  const f = await watchdogFixture(false);
  f.advance(5000, { picture: false, dropped: true });
  expect(reload).toHaveBeenCalledOnce();
});

it("uses presented frames even when the decoder counter keeps increasing", async () => {
  const f = await watchdogFixture();
  f.advance(5000, { picture: false, decoded: true });
  expect(reload).toHaveBeenCalledOnce();
});

it("leaves normal frame presentation and buffered preview seeks alone", async () => {
  const f = await watchdogFixture();
  for (const time of [3, 3.1, 3.15, 7, 7.01, 4]) {
    f.state.time = time;
    f.video.dispatchEvent(new Event("seeking"));
    f.advance(1000);
  }
  f.advance(12000);
  expect(reload).not.toHaveBeenCalled();
});

it.each(["paused", "previewing", "hidden"])(
  "does not recover during %s and gives resumed playback a fresh grace period",
  async (reason) => {
    const f = await watchdogFixture();
    if (reason === "paused") f.state.paused = true;
    if (reason === "previewing") previewing = true;
    if (reason === "hidden") f.state.hidden = true;
    f.advance(8000, { picture: false });
    expect(reload).not.toHaveBeenCalled();
    f.state.paused = false;
    previewing = false;
    f.state.hidden = false;
    if (reason === "paused") f.video.dispatchEvent(new Event("play"));
    if (reason === "hidden")
      document.dispatchEvent(new Event("visibilitychange"));
    f.advance(3000, { picture: false });
    expect(reload).not.toHaveBeenCalled();
    f.advance(2000, { picture: false });
    expect(reload).toHaveBeenCalledOnce();
  },
);

it("waits for playback to start after loading a new source", async () => {
  const f = await watchdogFixture();
  f.video.dispatchEvent(new Event("loadstart"));
  f.advance(8000, { picture: false, clock: false });
  expect(reload).not.toHaveBeenCalled();
  f.video.dispatchEvent(new Event("playing"));
  f.advance(5000, { picture: false, clock: false });
  expect(reload).toHaveBeenCalledOnce();
});

it("retains the recovery cooldown when the source URL changes", async () => {
  const f = await watchdogFixture();
  f.advance(4000, { picture: false, clock: false });
  expect(reload).toHaveBeenCalledOnce();
  expect(reload.mock.calls[0]?.[1]).toBe(false);
  await f.replaceSource();
  f.advance(14000, { picture: false, clock: false });
  expect(reload).toHaveBeenCalledOnce();
  f.advance(1000, { picture: false, clock: false });
  expect(reload).toHaveBeenCalledTimes(2);
});

it("cancels its pending frame callback when the source is replaced", async () => {
  const f = await watchdogFixture();
  expect(f.callbacks.size).toBe(1);
  await f.replaceSource();
  expect(f.cancelFrame).toHaveBeenCalled();
  expect(f.callbacks.size).toBe(1);
});

it("does not treat repeated presentation of the same timestamp as progress", async () => {
  const f = await watchdogFixture();
  f.advance(6000, { sameFrame: true });
  expect(reload).toHaveBeenCalledOnce();
});

it("allows offscreen audio playback and resumes picture monitoring when visible", async () => {
  const f = await watchdogFixture();
  f.state.offscreen = true;
  f.advance(8000, { picture: false });
  expect(reload).not.toHaveBeenCalled();
  f.state.offscreen = false;
  f.advance(3000, { picture: false });
  expect(reload).not.toHaveBeenCalled();
  f.advance(2000, { picture: false });
  expect(reload).toHaveBeenCalledOnce();
});
