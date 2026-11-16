// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ScrubTimingReadout } from "./scrub-timing-readout";
import { clearScrubTimings, recordScrubTiming } from "./scrub-timings";

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  clearScrubTimings();
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  localStorage.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const render = () =>
  act(async () => {
    root.render(
      <IntlProvider locale="en-GB">
        <ScrubTimingReadout />
      </IntlProvider>,
    );
  });

it("renders nothing unless the debug readout is enabled", async () => {
  recordScrubTiming("still-cache", 4);
  await render();
  expect(container.innerHTML).toBe("");
});

it("renders nothing when storage access is blocked", async () => {
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new DOMException("blocked", "SecurityError");
  });
  await render();
  expect(container.innerHTML).toBe("");
});

it("shows selectable per-tier summaries that follow new measurements", async () => {
  localStorage.setItem("stash.scrubDebug", "1");
  recordScrubTiming("still-network", 120);
  await render();
  const row = (tier: string) =>
    [
      ...container.querySelectorAll(`[data-scrub-timing-tier="${tier}"] td`),
    ].map((cell) => cell.textContent);
  expect(row("still-network")).toEqual([
    "Fetched still",
    "1",
    "120ms",
    "120ms",
  ]);
  expect(row("exact-frame")).toEqual(["Exact frame", "0", "–", "–"]);
  expect(container.querySelectorAll("[data-selectable-text]").length).toBe(12);
  await act(async () => {
    recordScrubTiming("exact-frame", 640.4);
  });
  expect(row("exact-frame")).toEqual(["Exact frame", "1", "640ms", "640ms"]);
});
