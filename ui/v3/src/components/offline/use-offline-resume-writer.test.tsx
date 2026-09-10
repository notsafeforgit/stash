// @vitest-environment jsdom
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useOfflineResumeWriter } from "./use-offline-resume-writer";

const { patchEntry } = vi.hoisted(() => ({
  patchEntry: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("./offline-db", () => ({ patchEntry }));
let container: HTMLDivElement;
let root: Root;
let playhead: number;

function Playback({ sceneId, time }: { sceneId?: string; time: number }) {
  const { sendGetCurrentTime } = useOfflineResumeWriter(sceneId, 0);
  useEffect(() => {
    sendGetCurrentTime(() => playhead);
    playhead = time;
  }, [sendGetCurrentTime, time]);
  return null;
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  root = createRoot(container);
  playhead = 0;
  patchEntry.mockClear();
});
afterEach(async () => {
  await act(async () => root.unmount());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("flushes the outgoing playhead before reusing the player for the next scene", async () => {
  await act(async () => root.render(<Playback sceneId="first" time={8} />));
  await act(async () => root.render(<Playback sceneId="second" time={2} />));
  expect(patchEntry).toHaveBeenNthCalledWith(1, "first", {
    last_position_seconds: 8,
  });
  await act(async () => root.render(null));
  expect(patchEntry).toHaveBeenNthCalledWith(2, "second", {
    last_position_seconds: 2,
  });
});

it("flushes once on a loading gap and never writes online playback to an offline row", async () => {
  await act(async () => root.render(<Playback sceneId="offline" time={6} />));
  await act(async () => root.render(<Playback time={9} />));
  await act(async () => vi.advanceTimersByTime(10000));
  expect(patchEntry).toHaveBeenCalledExactlyOnceWith("offline", {
    last_position_seconds: 6,
  });
});
