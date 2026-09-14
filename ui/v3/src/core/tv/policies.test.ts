import { describe, expect, it } from "vitest";
import { StreamingResolutionEnum } from "../generated-graphql";
import { markerRange, type SceneTiming } from "../marker-range";
import { selectFixedQuality } from "../player-quality";
import { tvPlaybackPlan, markerAwareSeek } from "./playback-policy";
import {
  defaultTvSettings,
  decodeTvSettings,
  tvSettingsSchema,
} from "./settings";
import { createTvAction, defaultTvRail } from "./action-config";
import {
  appendTvPage,
  type TvFeedSnapshot,
  type TvFeedItem,
} from "./feed-state";

const scene: SceneTiming = {
  files: [{ duration: 100 }],
  resume_time: 80,
  scene_markers: [
    { id: "1", seconds: 10 },
    { id: "2", seconds: 10 },
    { id: "3", seconds: 40, end_seconds: 45 },
  ],
};

describe("TV media policies", () => {
  it("bounds marker playback by explicit end, next distinct start or real scene duration", () => {
    expect(markerRange(scene, { id: "1", seconds: 0 })).toEqual({
      start: 10,
      end: 40,
    });
    expect(markerRange(scene, { id: "3", seconds: 0 })).toEqual({
      start: 40,
      end: 45,
    });
    expect(
      markerRange(scene, { id: "4", seconds: 90, end_seconds: 110 }),
    ).toEqual({ start: 90, end: 100 });
    expect(markerRange(scene, { id: "4", seconds: 90 })).toEqual({
      start: 90,
      end: 100,
    });
    expect(markerRange(scene, { id: "4", seconds: 100 })).toBeUndefined();
    expect(
      markerRange({ ...scene, files: [] }, { id: "1", seconds: 0 }),
    ).toBeUndefined();
    expect(
      markerRange(
        { ...scene, files: [{ duration: Infinity }] },
        { id: "1", seconds: 0 },
      ),
    ).toBeUndefined();
  });

  it("samples windows deterministically and preserves original scene data", () => {
    const settings = {
      ...defaultTvSettings,
      start: "random-position",
      window: { kind: "random", min: 5, max: 10 },
    } satisfies typeof defaultTvSettings;
    const before = structuredClone(scene);
    const first = tvPlaybackPlan(scene, settings, 42, "scene:1");
    expect(first).toEqual(tvPlaybackPlan(scene, settings, 42, "scene:1"));
    expect(first).not.toEqual(tvPlaybackPlan(scene, settings, 43, "scene:1"));
    if (first.kind !== "ready" || !first.range)
      throw new Error("Expected a playable window");
    expect(first.range.end).toBeLessThanOrEqual(100);
    expect(first.range.end - first.range.start).toBeGreaterThan(0);
    expect(first.range.end - first.range.start).toBeLessThanOrEqual(10);
    expect(scene).toEqual(before);
    expect(
      tvPlaybackPlan(
        scene,
        { ...defaultTvSettings, window: { kind: "fixed", seconds: 50 } },
        1,
        "scene:1",
      ),
    ).toEqual({ kind: "ready", start: 80, range: { start: 80, end: 100 } });
  });

  it("keeps marker-aware seeks inside the active scene range", () => {
    expect(
      markerAwareSeek(12, 1, scene.scene_markers, { start: 10, end: 45 }),
    ).toBe(40);
    expect(
      markerAwareSeek(39, -1, scene.scene_markers, { start: 10, end: 45 }),
    ).toBe(10);
    expect(
      markerAwareSeek(44, 1, scene.scene_markers, { start: 10, end: 45 }),
    ).toBe(45);
  });

  it("uses only advertised transcodes at or below the quality ceiling", () => {
    const source = (path: string) => ({
      src: `https://media.test/scene/1/${path}`,
    });
    const direct = source("stream?resolution=LOW");
    const low = source("stream.master.m3u8?resolution=LOW");
    const hd = source("stream.master.m3u8?resolution=STANDARD_HD");
    const original = source("stream.master.m3u8?resolution=ORIGINAL");
    const quality = {
      kind: "fixed",
      resolution: StreamingResolutionEnum.Standard,
    } as const;
    expect(
      selectFixedQuality([direct, hd, low], quality, {
        width: 1920,
        height: 1080,
      }),
    ).toBe(low);
    expect(
      selectFixedQuality([direct, hd], quality, { width: 1920, height: 1080 }),
    ).toBeNull();
    expect(
      selectFixedQuality([original], quality, { width: 480, height: 640 }),
    ).toBe(original);
    expect(selectFixedQuality([original], quality, {})).toBeNull();
  });
});

describe("TV settings validation", () => {
  it("retains invalid and future envelopes without silently resetting them", () => {
    expect(decodeTvSettings(undefined)).toEqual({
      kind: "ready",
      settings: defaultTvSettings,
    });
    const future = { version: 5, unknownSetting: "retain me" };
    expect(decodeTvSettings(future)).toMatchObject({
      kind: "invalid",
      raw: future,
    });
    expect(
      tvSettingsSchema.safeParse({
        ...defaultTvSettings,
        window: { kind: "random", min: 10, max: 5 },
      }).success,
    ).toBe(false);
  });

  it("requires reachable settings and visibility actions and valid quick presets", () => {
    expect(tvSettingsSchema.safeParse(defaultTvSettings).success).toBe(true);
    expect(
      tvSettingsSchema.safeParse({ ...defaultTvSettings, rail: [] }).success,
    ).toBe(false);
    expect(
      tvSettingsSchema.safeParse({
        ...defaultTvSettings,
        rail: [
          ...defaultTvRail,
          {
            type: "action",
            pinned: false,
            action: createTvAction("quick-tag", "new"),
          },
        ],
      }).success,
    ).toBe(false);
  });
});

describe("bounded TV pages", () => {
  const item = (id: string): TvFeedItem => ({
    kind: "scene",
    key: `scene:${id}`,
    id,
    sceneId: id,
  });
  const initial: TvFeedSnapshot = {
    items: [item("1")],
    selected: 0,
    nextPage: 2,
    total: 10,
    exhausted: false,
    status: "loading",
    tombstones: ["scene:2"],
  };
  it("deduplicates offset pages and tombstones without changing current selection", () => {
    const next = appendTvPage(
      initial,
      [item("1"), item("2"), item("3")],
      10,
      3,
      null,
    );
    expect(next.items.map((item) => item.id)).toEqual(["1", "3"]);
    expect(next.selected).toBe(0);
    expect(next.nextPage).toBe(3);
    expect(next.exhausted).toBe(false);
  });
  it("enforces the item limit and terminates on short pages", () => {
    expect(
      appendTvPage(initial, [item("3"), item("4"), item("5")], 20, 3, 2),
    ).toMatchObject({ items: [item("1"), item("3")], exhausted: true });
    expect(appendTvPage(initial, [], 20, 3, null).exhausted).toBe(true);
  });
});
