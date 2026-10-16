import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StreamingResolutionEnum as Resolution } from "./generated-graphql";
import {
  loadPlayerQualityPreference,
  QUALITY_STORAGE_KEY,
  savePlayerQualityPreference,
} from "./player-quality-preference";

describe("online quality preference", () => {
  beforeEach(() => {
    const values = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it.each([
    ["HLS (240p)", Resolution.Low],
    ["HLS Standard (480p)", Resolution.Standard],
    ["HLS Standard HD (720p)", Resolution.StandardHd],
    ["HLS Full HD (1080p)", Resolution.FullHd],
    ["HLS 4K (2160p)", Resolution.FourK],
  ])("upgrades the older v3 label %s to a ceiling", (label, resolution) => {
    localStorage.setItem(QUALITY_STORAGE_KEY, label);
    expect(loadPlayerQualityPreference()).toEqual({
      kind: "fixed",
      resolution,
    });
  });

  it.each([
    null,
    "Direct stream",
    "HLS (remux)",
    "HLS Original",
    "broken",
    '{"kind":"fixed","resolution":"unknown"}',
  ])(
    "uses best available for unset, original or invalid preferences: %s",
    (saved) => {
      if (saved) localStorage.setItem(QUALITY_STORAGE_KEY, saved);
      expect(loadPlayerQualityPreference()).toEqual({ kind: "best" });
    },
  );

  it.each(["stream.master.m3u8", "stream.hevc.master.m3u8"])(
    "stores the source resolution independently of its display label: %s",
    (path) => {
      savePlayerQualityPreference({
        src: `https://stash.test/scene/1/${path}?resolution=FULL_HD&start=15`,
      });
      expect(
        JSON.parse(localStorage.getItem(QUALITY_STORAGE_KEY) ?? "null"),
      ).toEqual({
        kind: "fixed",
        resolution: Resolution.FullHd,
      });
      expect(loadPlayerQualityPreference()).toEqual({
        kind: "fixed",
        resolution: Resolution.FullHd,
      });
    },
  );

  it.each([
    "stream",
    "stream.fmp4.master.m3u8",
    "stream.fmp4.aac.master.m3u8",
    "stream.master.m3u8?resolution=ORIGINAL",
    "stream.hevc.master.m3u8?resolution=ORIGINAL",
  ])("remembers original-video choices as best available: %s", (path) => {
    localStorage.setItem(QUALITY_STORAGE_KEY, "HLS (240p)");
    savePlayerQualityPreference({ src: `https://stash.test/scene/1/${path}` });
    expect(loadPlayerQualityPreference()).toEqual({ kind: "best" });
  });

  it.each([
    "blob:https://stash.test/offline",
    "https://stash.test/scene/1/stream.master.m3u8?resolution=UNKNOWN",
  ])(
    "does not replace the online ceiling for an unrelated source: %s",
    (src) => {
      localStorage.setItem(QUALITY_STORAGE_KEY, "HLS (240p)");
      savePlayerQualityPreference({ src });
      expect(loadPlayerQualityPreference()).toEqual({
        kind: "fixed",
        resolution: Resolution.Low,
      });
    },
  );

  it("does not prevent playback when browser storage is unavailable", () => {
    vi.stubGlobal("localStorage", {
      getItem() {
        throw new Error("Storage blocked");
      },
      setItem() {
        throw new Error("Storage blocked");
      },
    });
    expect(loadPlayerQualityPreference()).toEqual({ kind: "best" });
    expect(() =>
      savePlayerQualityPreference({ src: "https://stash.test/scene/1/stream" }),
    ).not.toThrow();
  });
});
