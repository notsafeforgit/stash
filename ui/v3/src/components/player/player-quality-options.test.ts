import { expect, it } from "vitest";
import { getQualityMenuSources } from "./player-quality-options";
import type { PlayerSource } from "./player-utils";

const direct = {
  src: "https://stash.test/scene/1/stream",
  label: "Direct stream",
};
const remux = {
  src: "https://stash.test/scene/1/stream.fmp4.master.m3u8",
  label: "HLS (remux)",
};
const sdr = ["ORIGINAL", "FULL_HD", "STANDARD_HD"].map((resolution) => ({
  src: `https://stash.test/scene/1/stream.master.m3u8?resolution=${resolution}&signature=sdr`,
  label: resolution === "ORIGINAL" ? "HLS (transcode)" : `HLS ${resolution}`,
}));
const hdr = sdr.map((source, index) => ({
  src: source.src
    .replace("stream.master", "stream.hevc.master")
    .replace("=sdr", "=hdr"),
  label: index === 0 ? "HLS (HDR)" : `HLS (HDR) ${index}`,
}));

it.each([false, true])(
  "offers one HDR option per tier regardless of catalog order (HDR first: %s)",
  (hdrFirst) => {
    const catalog = hdrFirst ? [...hdr, ...sdr] : [...sdr, ...hdr];
    expect(getQualityMenuSources(catalog, hdr[0] ?? null)).toEqual(hdr);
    expect(getQualityMenuSources(catalog, null)).toEqual(hdr);
    // Preserve endpoint signatures and the recovery catalog, without mutation.
    expect(catalog).toHaveLength(6);
    expect(getQualityMenuSources(catalog, null)[0]).toBe(hdr[0]);
  },
);

it("keeps every SDR tier when capability filtering excludes HDR or the source is SDR", () => {
  expect(getQualityMenuSources(sdr, sdr[0] ?? null)).toEqual(sdr);
});

it("shows the active SDR recovery choice once while retaining HDR at other tiers", () => {
  expect(getQualityMenuSources([...hdr, ...sdr], sdr[1] ?? null)).toEqual([
    hdr[0],
    sdr[1],
    hdr[2],
  ]);
});

it("keeps an SDR quality when the server has no equivalent HDR rendition", () => {
  expect(getQualityMenuSources([...sdr, hdr[0]!], null)).toEqual([
    hdr[0],
    sdr[1],
    sdr[2],
  ]);
});

it("hides original-resolution transcodes beside direct/remux, except the active recovery stream", () => {
  expect(
    getQualityMenuSources([direct, remux, ...sdr, ...hdr], direct),
  ).toEqual([direct, ...hdr.slice(1)]);
  expect(getQualityMenuSources([remux, ...sdr, ...hdr], remux)).toEqual([
    remux,
    ...hdr.slice(1),
  ]);
  expect(
    getQualityMenuSources([direct, ...sdr, ...hdr], sdr[0] ?? null),
  ).toEqual([direct, sdr[0], ...hdr.slice(1)]);
});

it("groups by scene, clip and resolution, not labels or query-string order", () => {
  const sources: PlayerSource[] = [
    { src: "https://stash.test/scene/1/stream.master.m3u8", label: "Original" },
    {
      src: "https://stash.test/scene/1/stream.hevc.master.m3u8?signature=x&resolution=ORIGINAL",
      label: "Original",
    },
    { src: "https://stash.test/scene/2/stream.master.m3u8", label: "Original" },
    {
      src: "https://stash.test/scene/1/stream.master.m3u8?start=4&end=8",
      label: "Original",
    },
    { src: "blob:https://stash.test/offline", label: "Saved" },
  ];
  expect(getQualityMenuSources(sources, null)).toEqual(sources.slice(1));
});
