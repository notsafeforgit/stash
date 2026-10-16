import { afterEach, expect, it, vi } from "vitest";
import { StreamingResolutionEnum } from "@/core/generated-graphql";
import {
  filterSources,
  getCompatibilitySource,
  selectScenePlayerSource,
} from "./scene-player-sources";

// Source eligibility has already run before these selectors; avoid creating a
// DOM codec probe in this test of preference selection and offline playback.
vi.mock("./player-utils", () => ({
  canPlaySource: () => true,
  canPlayHDRHls: () => false,
}));
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

it("offers HDR only to capable displays and prefers it within the resolution ceiling", () => {
  const hdrOriginal = {
    src: original.src.replace("stream.master", "stream.hevc.master"),
  };
  const hdrHigh = {
    src: high.src.replace("stream.master", "stream.hevc.master"),
  };
  const hd = { src: original.src.replace("ORIGINAL", "FULL_HD") };
  const hdrHd = { src: hd.src.replace("stream.master", "stream.hevc.master") };
  const raw = [original, high, hd, hdrOriginal, hdrHigh, hdrHd].map((s) => ({
    url: s.src,
  }));
  const sources = filterSources(raw, false, false, true);
  const large = { width: 2160, height: 3840 };
  expect(selectScenePlayerSource(sources, { kind: "best" }, large)?.src).toBe(
    hdrOriginal.src,
  );
  expect(
    selectScenePlayerSource(
      sources,
      { kind: "fixed", resolution: StreamingResolutionEnum.FullHd },
      large,
    )?.src,
  ).toBe(hdrHd.src);
  expect(
    getCompatibilitySource(
      sources,
      { kind: "fixed", resolution: StreamingResolutionEnum.FullHd },
      large,
    )?.src,
  ).toBe(hd.src);
  expect(
    selectScenePlayerSource(
      filterSources(raw, false, false, false),
      { kind: "best" },
      large,
    )?.src,
  ).toBe(original.src);
});
