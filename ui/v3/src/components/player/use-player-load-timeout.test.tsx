// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  PLAYER_LOAD_TIMEOUT_MS,
  usePlayerLoadTimeout,
} from "./use-player-load-timeout";

let container: HTMLDivElement;
let root: Root;
let hidden: boolean;
const timeout = vi.fn();
function Fixture({ load, pending }: { load: object; pending: boolean }) {
  usePlayerLoadTimeout({ load, pending, onTimeout: timeout });
  return null;
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  hidden = false;
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
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

it("times out a source that never reaches readiness, once per load", async () => {
  const load = {};
  await act(async () => root.render(<Fixture load={load} pending />));
  vi.advanceTimersByTime(PLAYER_LOAD_TIMEOUT_MS - 1);
  expect(timeout).not.toHaveBeenCalled();
  vi.advanceTimersByTime(1);
  expect(timeout).toHaveBeenCalledOnce();
  vi.advanceTimersByTime(PLAYER_LOAD_TIMEOUT_MS * 3);
  expect(timeout).toHaveBeenCalledOnce();
});

it("gives a superseding seek a new deadline and cancels on readiness", async () => {
  await act(async () => root.render(<Fixture load={{}} pending />));
  vi.advanceTimersByTime(PLAYER_LOAD_TIMEOUT_MS - 1000);
  const next = {};
  await act(async () => root.render(<Fixture load={next} pending />));
  vi.advanceTimersByTime(2000);
  expect(timeout).not.toHaveBeenCalled();
  await act(async () => root.render(<Fixture load={next} pending={false} />));
  vi.advanceTimersByTime(PLAYER_LOAD_TIMEOUT_MS);
  expect(timeout).not.toHaveBeenCalled();
});

it("does not count background time and restarts the grace period on return", async () => {
  await act(async () => root.render(<Fixture load={{}} pending />));
  vi.advanceTimersByTime(PLAYER_LOAD_TIMEOUT_MS - 1000);
  hidden = true;
  document.dispatchEvent(new Event("visibilitychange"));
  vi.advanceTimersByTime(PLAYER_LOAD_TIMEOUT_MS * 2);
  expect(timeout).not.toHaveBeenCalled();
  hidden = false;
  document.dispatchEvent(new Event("visibilitychange"));
  vi.advanceTimersByTime(PLAYER_LOAD_TIMEOUT_MS - 1);
  expect(timeout).not.toHaveBeenCalled();
  vi.advanceTimersByTime(1);
  expect(timeout).toHaveBeenCalledOnce();
});
