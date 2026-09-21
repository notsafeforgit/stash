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
    currentTime: { value: 10 },
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
