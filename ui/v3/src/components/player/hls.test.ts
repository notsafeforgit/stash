// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { parseStartPosition } from "./hls";

const scene = "https://stash.test/scene/1/stream.master.m3u8";

describe("HLS initial playback position", () => {
  it("keeps scene-time startup hints for full-scene playlists", () => {
    expect(parseStartPosition(`${scene}?start=18`)).toBe(18);
    expect(parseStartPosition(`${scene}?start=18.7&resolution=720`)).toBe(18.7);
  });

  it("loads the first listed segment of a marker in clip-relative time", () => {
    for (const start of [0, 18, 18.7]) {
      expect(parseStartPosition(`${scene}?start=${start}&end=60`)).toBe(0);
    }
    expect(parseStartPosition(`${scene}?end=60`)).toBe(0);
  });

  it("uses the clip origin for both codec-copy variants and source reloads", () => {
    for (const suffix of ["fmp4", "fmp4.aac"]) {
      expect(
        parseStartPosition(
          `https://stash.test/scene/1/stream.${suffix}.master.m3u8?start=18&end=60&_r=2`,
        ),
      ).toBe(0);
    }
  });

  it("uses the HLS default when a full-scene startup hint is unavailable", () => {
    for (const src of [
      undefined,
      null,
      "",
      scene,
      `${scene}?start=0`,
      `${scene}?start=-1`,
      `${scene}?start=invalid`,
      `${scene}?start=Infinity`,
    ]) {
      expect(parseStartPosition(src)).toBe(-1);
    }
  });
});
