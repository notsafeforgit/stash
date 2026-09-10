// @vitest-environment jsdom
import { act, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { usePlayDelay } from "./use-play-delay";

let container: HTMLDivElement;
let root: Root;
let autoplayIntent: { current: boolean };
let userIntent: { current: boolean };

function Gate() {
  const ref = useRef<HTMLDivElement>(null);
  usePlayDelay(ref, 500, autoplayIntent, userIntent);
  return (
    <div ref={ref}>
      {/* biome-ignore lint/a11y/useMediaCaption: no media source; tests mocked play promises only. */}
      <video />
    </div>
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  autoplayIntent = { current: true };
  userIntent = { current: false };
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function mount() {
  await act(async () => root.render(<Gate />));
  const video = container.querySelector("video");
  if (!video) throw new Error("Missing video");
  return video;
}

it.each([
  "AbortError",
  "NotSupportedError",
])("does not mute or retry after %s", async (name) => {
  const video = await mount();
  const play = vi
    .spyOn(video, "play")
    .mockRejectedValue(new DOMException("Playback failed", name));
  await act(async () => vi.advanceTimersByTime(500));
  expect(play).toHaveBeenCalledOnce();
  expect(video.muted).toBe(false);
});

it("retries muted only when audible autoplay is denied", async () => {
  const video = await mount();
  const play = vi
    .spyOn(video, "play")
    .mockRejectedValueOnce(
      new DOMException("Autoplay denied", "NotAllowedError"),
    )
    .mockResolvedValueOnce();
  await act(async () => vi.advanceTimersByTime(500));
  expect(play).toHaveBeenCalledTimes(2);
  expect(video.muted).toBe(true);
});

it.each([
  "source changed",
  "user took control",
  "unmounted",
])("ignores a pending autoplay rejection after %s", async (change) => {
  const video = await mount();
  const pending = Promise.withResolvers<void>();
  const play = vi.spyOn(video, "play").mockReturnValue(pending.promise);
  await act(async () => vi.advanceTimersByTime(500));
  await act(async () => {
    if (change === "source changed") {
      Object.defineProperty(video, "currentSrc", { value: "next.mp4" });
    } else if (change === "user took control") {
      userIntent.current = true;
    } else {
      root.render(null);
    }
  });
  await act(async () => {
    pending.reject(new DOMException("Autoplay denied", "NotAllowedError"));
  });
  expect(play).toHaveBeenCalledOnce();
  expect(video.muted).toBe(false);
});
