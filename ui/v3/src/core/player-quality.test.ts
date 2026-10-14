import { describe, expect, it } from "vitest";
import { StreamingResolutionEnum } from "./generated-graphql";
import { selectFixedQuality } from "./player-quality";

const source = (path: string) => ({
  src: `https://media.test/scene/1/${path}`,
});
const direct = source("stream");
const remux = source("stream.fmp4.master.m3u8");
const remuxAac = source("stream.fmp4.aac.master.m3u8");
const original = source("stream.master.m3u8?resolution=ORIGINAL");
const hd = source("stream.master.m3u8?resolution=STANDARD_HD");
const fullHd = source("stream.master.m3u8?resolution=FULL_HD");
const fourK = source("stream.master.m3u8?resolution=FOUR_K");
const quality = {
  kind: "fixed",
  resolution: StreamingResolutionEnum.FullHd,
} as const;

describe("player resolution ceiling", () => {
  it.each([
    { width: 1280, height: 720 },
    { width: 720, height: 1280 },
    { width: 1920, height: 1080 },
    { width: 1080, height: 1920 },
  ])("prefers compatible originals at $width × $height", (dimensions) => {
    expect(
      selectFixedQuality(
        [hd, fullHd, original, remuxAac, remux, direct],
        quality,
        dimensions,
      ),
    ).toBe(direct);
  });

  it("preserves video through the best compatible remux when direct is unavailable", () => {
    const dimensions = { width: 1280, height: 720 };
    expect(
      selectFixedQuality([original, remuxAac, remux], quality, dimensions),
    ).toBe(remux);
    expect(
      selectFixedQuality([hd, original, remuxAac], quality, dimensions),
    ).toBe(remuxAac);
    expect(selectFixedQuality([original, hd], quality, dimensions)).toBe(
      original,
    );
  });

  it.each([
    { width: 3840, height: 2160 },
    { width: 2160, height: 3840 },
  ])("downscales larger originals at $width × $height", (dimensions) => {
    expect(
      selectFixedQuality(
        [direct, remux, remuxAac, original, fourK, hd, fullHd],
        quality,
        dimensions,
      ),
    ).toBe(fullHd);
    expect(
      selectFixedQuality([direct, remux, original, fourK], quality, dimensions),
    ).toBeNull();
  });

  it.each([
    {},
    { width: 720 },
    { width: 0, height: 1280 },
    { width: -1, height: 1280 },
    { width: 720, height: Number.NaN },
    { width: Number.POSITIVE_INFINITY, height: 720 },
  ])(
    "does not guess original resolution from invalid dimensions %o",
    (dimensions) => {
      const originals = [direct, remux, remuxAac, original];
      expect(selectFixedQuality(originals, quality, dimensions)).toBeNull();
      expect(selectFixedQuality([...originals, hd], quality, dimensions)).toBe(
        hd,
      );
    },
  );

  it("uses only recognized, advertised sources instead of trusting labels or fake tiers", () => {
    expect(
      selectFixedQuality(
        [
          { src: "not a URL", label: "1080p" },
          source("stream?resolution=LOW"),
          source("stream.mp4?resolution=LOW"),
          source("stream.master.m3u8?resolution=unknown"),
        ],
        quality,
        { width: 720, height: 1280 },
      ),
    ).toBeNull();
    expect(
      selectFixedQuality([], quality, { width: 720, height: 1280 }),
    ).toBeNull();
  });
});
