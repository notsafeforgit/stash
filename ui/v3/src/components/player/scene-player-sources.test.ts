import { afterEach, expect, it, vi } from "vitest";
import {
  getCompatibilitySource,
  selectScenePlayerSource,
} from "./scene-player-sources";

// Source eligibility has already run before these selectors; avoid creating a
// DOM codec probe in this test of preference selection and offline playback.
vi.mock("./player-utils", () => ({ canPlaySource: () => true }));
afterEach(() => vi.unstubAllGlobals());

const direct = { src: "https://stash.test/scene/1/stream" };
const original = {
  src: "https://stash.test/scene/1/stream.master.m3u8?resolution=ORIGINAL",
};
const low = {
  src: "https://stash.test/scene/1/stream.master.m3u8?resolution=LOW",
};
const high = {
  src: "https://stash.test/scene/1/stream.master.m3u8?resolution=FOUR_K",
};
const dimensions = { width: 720, height: 1280 };

function rememberCeiling() {
  vi.stubGlobal("localStorage", {
    getItem: () => '{"kind":"fixed","resolution":"FULL_HD"}',
  });
}

it("plays a saved offline blob independently of the online ceiling", () => {
  rememberCeiling();
  const blob = { src: "blob:https://stash.test/offline" };
  expect(
    selectScenePlayerSource([blob], undefined, { width: 3840, height: 2160 }),
  ).toBe(blob);
});

it("does not silently exceed a remembered ceiling when the needed transcode is absent", () => {
  rememberCeiling();
  expect(
    selectScenePlayerSource([direct, original, high], undefined, {
      width: 3840,
      height: 2160,
    }),
  ).toBeNull();
});

it("recovers codec errors within the ordinary player's remembered ceiling", () => {
  rememberCeiling();
  expect(
    getCompatibilitySource([direct, high, low], undefined, dimensions),
  ).toBe(low);
  expect(
    getCompatibilitySource([direct, high, original], undefined, dimensions),
  ).toBe(original);
});

it("keeps an explicit TV best-quality preference separate from the ordinary ceiling", () => {
  rememberCeiling();
  expect(
    selectScenePlayerSource(
      [direct, low],
      { kind: "best" },
      { width: 3840, height: 2160 },
    ),
  ).toBe(direct);
});
