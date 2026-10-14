import { describe, expect, it } from "vitest";
import { StreamingResolutionEnum as Resolution } from "@/core/generated-graphql";
import {
  downloadQueryString,
  pickDownloadFormat,
  type PickFormatInput,
} from "./pick-download-format";

const input: PickFormatInput = {
  source: { width: 1920, height: 1080 },
  device: {
    videoAndAudioInMp4: true,
    videoInMp4: true,
    decodesHevc: true,
    decodesAv1: true,
  },
  server: { hevcAvailable: true, av1Available: true },
  maxResolution: Resolution.FullHd,
};

describe("offline resolution ceiling", () => {
  it.each([
    { width: 1280, height: 720 },
    { width: 720, height: 1280 },
    { width: 1080, height: 1920 },
  ])("copies a compatible original within the cap: %j", (source) => {
    expect(pickDownloadFormat({ ...input, source })).toEqual({
      mode: "copy",
      resolution: Resolution.Original,
      effectiveHeight: source.height,
    });
  });

  it("copies portrait video and converts only unsupported audio", () => {
    expect(
      pickDownloadFormat({
        ...input,
        source: { width: 1080, height: 1920 },
        device: { ...input.device, videoAndAudioInMp4: false },
      }),
    ).toEqual({
      mode: "copy-aac",
      resolution: Resolution.Original,
      effectiveHeight: 1920,
    });
  });

  it.each([
    [{ width: 3840, height: 2160 }, 1080],
    [{ width: 2160, height: 3840 }, 1920],
  ])("scales oversized video on its shorter edge: %j", (source, height) => {
    const result = pickDownloadFormat({ ...input, source });
    expect(result).toEqual({
      mode: "av1",
      resolution: Resolution.FullHd,
      effectiveHeight: height,
    });
    expect(downloadQueryString(result)).toBe("mode=av1&resolution=FULL_HD");
  });

  it("keeps a small portrait's dimensions when only its codec needs conversion", () => {
    expect(
      pickDownloadFormat({
        ...input,
        source: { width: 720, height: 1280 },
        device: {
          ...input.device,
          videoInMp4: false,
          videoAndAudioInMp4: false,
        },
        server: { ...input.server, av1Available: false },
      }),
    ).toEqual({
      mode: "hevc",
      resolution: Resolution.FullHd,
      effectiveHeight: 1280,
    });
  });

  it.each([
    { width: 0, height: 0 },
    { width: 0, height: 1920 },
    { width: Number.NaN, height: 1920 },
  ])(
    "uses a bounded transcode when dimensions cannot establish that the source fits: %j",
    (source) => {
      expect(pickDownloadFormat({ ...input, source })).toEqual({
        mode: "av1",
        resolution: Resolution.FullHd,
        effectiveHeight: 0,
      });
    },
  );

  it("allows original copies when no resolution ceiling is requested", () => {
    expect(
      pickDownloadFormat({
        ...input,
        source: { width: 2160, height: 3840 },
        maxResolution: Resolution.Original,
      }),
    ).toEqual({
      mode: "copy",
      resolution: Resolution.Original,
      effectiveHeight: 3840,
    });
  });
});
