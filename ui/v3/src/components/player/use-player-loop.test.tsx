// @vitest-environment jsdom
import { act, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { usePlayerLoop } from "./use-player-loop";

let container: HTMLDivElement;
let root: Root;
let loop: ReturnType<typeof vi.fn<(start: number) => void>>;
let video: HTMLVideoElement;
let media: {
  currentTime: number;
  duration: number;
  paused: boolean;
  seeking: boolean;
  ended: boolean;
  readyState: number;
  playbackRate: number;
};

function Player({
  enabled = true,
  start = 0,
  end,
  offsetStart = 0,
}: {
  enabled?: boolean;
  start?: number;
  end?: number;
  offsetStart?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  usePlayerLoop({
    enabled,
    rootRef: ref,
    source: "scene.mp4",
    start,
    end,
    offsetStart,
    frameRate: 30,
    onLoop: loop,
  });
  return (
    <div ref={ref}>
      {/* biome-ignore lint/a11y/useMediaCaption: synthetic media clock only. */}
      <video />
    </div>
  );
}

beforeEach(async () => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  loop = vi.fn(() => {
    media.currentTime = 0;
  });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () => root.render(<Player />));
  const element = container.querySelector("video");
  if (!element) throw new Error("Missing video");
  video = element;
  media = {
    currentTime: 9,
    duration: 10,
    paused: false,
    seeking: false,
    ended: false,
    readyState: 4,
    playbackRate: 1,
  };
  for (const property of Object.keys(media) as (keyof typeof media)[])
    Object.defineProperty(video, property, {
      configurable: true,
      get: () => media[property],
    });
  video.dispatchEvent(new Event("playing"));
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("keeps the final frame and loops while the media is still playing", () => {
  vi.advanceTimersByTime(994);
  expect(loop).not.toHaveBeenCalled();
  media.currentTime = 9.996;
  vi.advanceTimersByTime(2);
  expect(loop).toHaveBeenCalledExactlyOnceWith(0);
  expect(media.paused).toBe(false);
});

it("does not confuse elapsed wall time with media progress during a stall", () => {
  media.currentTime = 9.4;
  vi.advanceTimersByTime(2000);
  expect(loop).not.toHaveBeenCalled();
  video.dispatchEvent(new Event("waiting"));
  vi.advanceTimersByTime(5000);
  expect(loop).not.toHaveBeenCalled();
  media.currentTime = 9.996;
  video.dispatchEvent(new Event("playing"));
  expect(loop).toHaveBeenCalledExactlyOnceWith(0);
});

it.each(["pause", "seeking", "emptied"])(
  "cancels a pending loop on %s",
  (event) => {
    media.currentTime = 9.996;
    if (event === "pause") media.paused = true;
    if (event === "seeking") media.seeking = true;
    if (event === "emptied") media.readyState = 0;
    video.dispatchEvent(new Event(event));
    vi.advanceTimersByTime(2000);
    expect(loop).not.toHaveBeenCalled();
  },
);

it("recomputes the deadline after a seek and a playback-rate change", () => {
  media.currentTime = 8;
  video.dispatchEvent(new Event("seeked"));
  vi.advanceTimersByTime(1000);
  expect(loop).not.toHaveBeenCalled();
  media.currentTime = 9;
  media.playbackRate = 2;
  video.dispatchEvent(new Event("ratechange"));
  vi.advanceTimersByTime(490);
  expect(loop).not.toHaveBeenCalled();
  media.currentTime = 9.996;
  vi.advanceTimersByTime(10);
  expect(loop).toHaveBeenCalledExactlyOnceWith(0);
});

it("uses a clip boundary in media time and restarts in scene time", async () => {
  media.currentTime = 1;
  await act(async () =>
    root.render(<Player start={6} end={8} offsetStart={6} />),
  );
  media.currentTime = 1.996;
  vi.advanceTimersByTime(1000);
  expect(loop).toHaveBeenCalledExactlyOnceWith(6);
});

it("uses native duration when the file ends before the library duration", async () => {
  await act(async () => root.render(<Player end={12} />));
  media.currentTime = 9.996;
  vi.advanceTimersByTime(1000);
  expect(loop).toHaveBeenCalledExactlyOnceWith(0);
});

it("leaves a late native EOF to the completion handler without looping twice", () => {
  media.currentTime = 10;
  media.ended = true;
  media.paused = true;
  video.dispatchEvent(new Event("ended"));
  vi.advanceTimersByTime(2000);
  expect(loop).not.toHaveBeenCalled();
});

it("cancels when loop mode is disabled or the player is suspended", async () => {
  await act(async () => root.render(<Player enabled={false} />));
  media.currentTime = 9.996;
  video.dispatchEvent(new Event("timeupdate"));
  vi.advanceTimersByTime(2000);
  expect(loop).not.toHaveBeenCalled();
});

it("cancels on unmount", async () => {
  await act(async () => root.render(null));
  media.currentTime = 9.996;
  vi.advanceTimersByTime(2000);
  expect(loop).not.toHaveBeenCalled();
});

it("lets native completion handle background playback and reschedules on return", () => {
  vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  document.dispatchEvent(new Event("visibilitychange"));
  media.currentTime = 9.996;
  video.dispatchEvent(new Event("timeupdate"));
  vi.advanceTimersByTime(2000);
  expect(loop).not.toHaveBeenCalled();
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  document.dispatchEvent(new Event("visibilitychange"));
  expect(loop).toHaveBeenCalledExactlyOnceWith(0);
});
