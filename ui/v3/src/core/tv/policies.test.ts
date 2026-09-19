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
import {
  createTvAction,
  defaultTvRail,
  type TvRailEntry,
} from "./action-config";
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
  it("defaults existing settings to five prepared items and accepts only bounded capacities", () => {
    const { preloadCount: _, ...old } = defaultTvSettings;
    expect(decodeTvSettings(old)).toMatchObject({
      kind: "ready",
      settings: { preloadCount: 5 },
    });
    for (const count of [1, 3, 5])
      expect(
        tvSettingsSchema.safeParse({ ...old, preloadCount: count }).success,
      ).toBe(true);
    for (const count of [0, 2, 4, 6, 100, "5"])
      expect(
        tvSettingsSchema.safeParse({ ...old, preloadCount: count }).success,
      ).toBe(false);
  });
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
    if (first.kind !== "ready") throw new Error("Expected a playable window");
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
    ).toEqual({ kind: "ready", range: { start: 80, end: 100 } });
  });

  it.each(["beginning", "resume", "random-marker", "random-position"] as const)(
    "bounds the full window from the %s start to scene end",
    (start) => {
      const plan = tvPlaybackPlan(
        scene,
        { ...defaultTvSettings, start },
        42,
        "scene:1",
      );
      if (plan.kind !== "ready") throw new Error("Expected a playable segment");
      expect(plan.range.end).toBe(100);
      if (start === "beginning") expect(plan.range.start).toBe(0);
      else if (start === "resume") expect(plan.range.start).toBe(80);
      else if (start === "random-marker")
        expect([10, 40]).toContain(plan.range.start);
      else {
        expect(plan.range.start).toBeGreaterThan(0);
        expect(plan.range.start).toBeLessThan(99);
      }
      expect(plan).toEqual(
        tvPlaybackPlan(scene, { ...defaultTvSettings, start }, 42, "scene:1"),
      );
    },
  );

  it.each([NaN, -1, 100])(
    "resets an invalid resume position %s to scene start",
    (resume_time) => {
      expect(
        tvPlaybackPlan(
          { ...scene, resume_time },
          defaultTvSettings,
          42,
          "scene:1",
        ),
      ).toEqual({ kind: "ready", range: { start: 0, end: 100 } });
    },
  );

  it("preserves marker bounds independently of scene start and window settings", () => {
    expect(
      tvPlaybackPlan(
        scene,
        {
          ...defaultTvSettings,
          start: "random-position",
          window: { kind: "fixed", seconds: 1 },
        },
        42,
        "marker:3",
        { id: "3", seconds: 40, end_seconds: 45 },
      ),
    ).toEqual({ kind: "ready", range: { start: 40, end: 45 } });
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
  const legacySettingsEntry: TvRailEntry = {
    type: "action",
    pinned: true,
    action: createTvAction("settings", "settings"),
  };

  it.each([1, 2, 3])(
    "retires the default gear from version %i without resetting the rail or audio",
    (version) => {
      const settings = {
        ...defaultTvSettings,
        startMuted: false,
        rail: [...defaultTvRail].reverse(),
      };
      const result = decodeTvSettings({
        ...settings,
        version,
        shuffle: false,
        rail: [legacySettingsEntry, ...settings.rail],
      });
      expect(result).toEqual({ kind: "ready", settings });
      if (result.kind === "ready")
        expect(decodeTvSettings(result.settings)).toEqual(result);
    },
  );

  it.each([
    { ...legacySettingsEntry, pinned: false },
    {
      ...legacySettingsEntry,
      action: { ...legacySettingsEntry.action, label: "Preferences" },
    },
    {
      ...legacySettingsEntry,
      action: { ...legacySettingsEntry.action, icon: "star" as const },
    },
    {
      ...legacySettingsEntry,
      action: { ...legacySettingsEntry.action, id: "custom-settings" },
    },
  ])("preserves a customized legacy settings shortcut: %j", (entry) => {
    const settings = { ...defaultTvSettings, rail: [...defaultTvRail, entry] };
    expect(decodeTvSettings({ ...settings, version: 3 })).toEqual({
      kind: "ready",
      settings,
    });
  });

  it.each([4, 5])(
    "keeps explicitly added settings shortcuts from version %i",
    (version) => {
      const settings = {
        ...defaultTvSettings,
        rail: [legacySettingsEntry, ...defaultTvRail],
      };
      expect(decodeTvSettings({ ...settings, version })).toEqual({
        kind: "ready",
        settings,
      });
    },
  );

  it.each([1, 2, 3, 4])(
    "adds the startup mute default to version %i without resetting preferences",
    (version) => {
      const previous = {
        ...defaultTvSettings,
        version,
        shuffle: false,
        startMuted: undefined,
        autoplay: false,
        sceneFilter: { kind: "saved", id: "12" },
      };
      const expected = {
        ...defaultTvSettings,
        autoplay: false,
        sceneFilter: { kind: "saved", id: "12" },
      };
      expect(decodeTvSettings(previous)).toEqual({
        kind: "ready",
        settings: expected,
      });
      expect(decodeTvSettings({ ...previous, startMuted: false })).toEqual({
        kind: "ready",
        settings: { ...expected, startMuted: false },
      });
      expect(decodeTvSettings({ ...previous, startMuted: "false" }).kind).toBe(
        "invalid",
      );
    },
  );

  it.each([
    { shuffle: true, sort: "created_at", expected: "random" },
    { shuffle: true, sort: null, expected: "random" },
    { shuffle: false, sort: "created_at", expected: "created_at" },
    { shuffle: false, sort: "random", expected: "random" },
    { shuffle: false, sort: null, expected: null },
  ])(
    "migrates legacy sort $sort with shuffle $shuffle",
    ({ shuffle, sort, expected }) => {
      const result = decodeTvSettings({
        ...defaultTvSettings,
        version: 1,
        shuffle,
        sort,
        rules: [],
      });
      expect(result).toEqual({
        kind: "ready",
        settings: { ...defaultTvSettings, sort: expected },
      });
      if (result.kind === "ready") {
        expect(result.settings).not.toHaveProperty("shuffle");
        expect(decodeTvSettings(result.settings)).toEqual(result);
      }
    },
  );

  it("removes legacy rules while preserving saved filters and playback preferences", () => {
    const settings = {
      ...defaultTvSettings,
      sceneFilter: { kind: "saved", id: "12" },
      markerFilter: { kind: "saved", id: "34" },
      sort: "random",
      autoplay: false,
    };
    const result = decodeTvSettings({
      ...settings,
      version: 2,
      rules: [{ kind: "filter", mode: "scenes", filterId: "56" }],
    });
    expect(result).toEqual({ kind: "ready", settings });
    if (result.kind === "ready") {
      expect(result.settings).not.toHaveProperty("rules");
      expect(decodeTvSettings(result.settings)).toEqual(result);
    }
  });

  it.each([1, 2, 3, 4])(
    "retires paging and session caps from version %i without resetting preferences",
    (version) => {
      const settings = {
        ...defaultTvSettings,
        sceneFilter: { kind: "saved", id: "12" },
        markerFilter: { kind: "saved", id: "34" },
        startMuted: false,
        autoplay: false,
        sort: "random",
      };
      expect(
        decodeTvSettings({
          ...settings,
          version,
          shuffle: false,
          pageSize: 5,
          prefetch: 5,
          itemLimit: 1,
        }),
      ).toEqual({ kind: "ready", settings });
    },
  );

  it("retains invalid and future envelopes without silently resetting them", () => {
    expect(decodeTvSettings(undefined)).toEqual({
      kind: "ready",
      settings: defaultTvSettings,
    });
    const future = {
      ...defaultTvSettings,
      version: 6,
      unknownSetting: "retain me",
    };
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

  it("allows an optional settings shortcut inside a folder", () => {
    const settings = {
      ...defaultTvSettings,
      rail: defaultTvRail.map((entry) =>
        entry.type === "folder" && entry.id === "playback"
          ? {
              ...entry,
              actions: [
                ...entry.actions,
                createTvAction("settings", "settings"),
              ],
            }
          : entry,
      ),
    };
    expect(decodeTvSettings(settings)).toEqual({ kind: "ready", settings });
  });

  it("requires reachable visibility and valid quick presets without a settings action", () => {
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
    );
    expect(next.items.map((item) => item.id)).toEqual(["1", "3"]);
    expect(next.selected).toBe(0);
    expect(next.nextPage).toBe(3);
    expect(next.exhausted).toBe(false);
  });
  it("admits the full batch and stops only at the end of the matching feed", () => {
    expect(
      appendTvPage(initial, [item("3"), item("4"), item("5")], 20, 3),
    ).toMatchObject({
      items: [item("1"), item("3"), item("4"), item("5")],
      exhausted: false,
    });
    expect(appendTvPage(initial, [], 20, 3).exhausted).toBe(true);
    expect(
      appendTvPage(initial, [item("3"), item("4"), item("5")], 6, 3).exhausted,
    ).toBe(true);
  });
});
