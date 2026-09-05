import { describe, expect, it } from "vitest";
import { planSceneSeek, planSourceResume } from "./scene-player-transitions";
import { makeHlsStrategy } from "./hls";

const fullScene = {
  intent: "seek" as const,
  targetTime: 20,
  duration: 600,
  offsetStart: 0,
  src: "https://stash.test/scene/1/stream.master.m3u8",
  frameRate: 30,
  mediaState: { buffered: [[10, 40]] as [number, number][] },
  ios: false,
  hasHlsEngine: true,
};

describe("scene seek transitions", () => {
  it("seeks buffered positions in place on both iOS and desktop", () => {
    for (const ios of [true, false]) {
      expect(planSceneSeek({ ...fullScene, ios })).toEqual({
        kind: "seek",
        sceneTime: 20,
        mediaTime: 20,
      });
    }
  });

  it("restarts a desktop engine for a far seek, but reloads the source on iOS", () => {
    expect(planSceneSeek({ ...fullScene, targetTime: 200 }).kind).toBe(
      "restart-engine",
    );
    expect(planSceneSeek({ ...fullScene, targetTime: 200, ios: true })).toEqual(
      {
        kind: "reload-source",
        sceneTime: 200,
        resume: { offset: 0, seekTo: 200, fragmentTime: 200 },
      },
    );
  });

  it("reloads when there is no engine to flush", () => {
    expect(
      planSceneSeek({ ...fullScene, targetTime: 200, hasHlsEngine: false })
        .kind,
    ).toBe("reload-source");
  });

  it("keeps a clip's segment-aligned origin across source reloads", () => {
    expect(
      planSceneSeek({
        ...fullScene,
        clipRange: { start: 101, end: 220 },
        offsetStart: 100,
        targetTime: 200,
        mediaState: { buffered: [[0, 10]], seekable: [[0, 120]] },
      }),
    ).toEqual({
      kind: "reload-source",
      sceneTime: 200,
      resume: { offset: 100, seekTo: 200, fragmentTime: 200 },
    });
    expect(
      planSceneSeek({
        ...fullScene,
        clipRange: { start: 101, end: 220 },
        offsetStart: 100,
        targetTime: 120,
        mediaState: { buffered: [[0, 30]], seekable: [[0, 120]] },
      }),
    ).toEqual({ kind: "seek", sceneTime: 120, mediaTime: 20 });
  });

  it("seeks direct files without an HLS reset and clamps to file bounds", () => {
    const direct = { ...fullScene, src: "https://stash.test/scene/1/stream" };
    expect(planSceneSeek({ ...direct, targetTime: 900 })).toEqual({
      kind: "seek",
      sceneTime: 600,
      mediaTime: 600,
    });
    expect(planSceneSeek({ ...direct, targetTime: -20 })).toEqual({
      kind: "seek",
      sceneTime: 0,
      mediaTime: 0,
    });
  });

  it("restarts before a clip's seekable origin with a source reload", () => {
    expect(
      planSceneSeek({
        ...fullScene,
        intent: "restart",
        clipRange: { start: 101, end: 220 },
        targetTime: 80,
        offsetStart: 100,
        mediaState: { buffered: [[0, 30]], seekable: [[0, 120]] },
      }).kind,
    ).toBe("reload-source");
  });

  it("allows restart before duration metadata is available", () => {
    expect(
      planSceneSeek({ ...fullScene, intent: "restart", duration: 0 }).sceneTime,
    ).toBe(20);
  });
});

describe("source resume plans", () => {
  it("preserves absolute time when switching between direct and clipped HLS", () => {
    expect(planSourceResume(null, 125)).toEqual({
      offset: 0,
      seekTo: 125,
      fragmentTime: 125,
    });
    expect(
      planSourceResume(
        makeHlsStrategy(30, true, { start: 101, end: 220 }),
        125,
      ),
    ).toEqual({ offset: 100, seekTo: 125, fragmentTime: 125 });
  });

  it("does not inject a fragment seek when restarting at zero", () => {
    expect(planSourceResume(null, 0)).toEqual({
      offset: 0,
      seekTo: 0,
      fragmentTime: null,
    });
    expect(planSourceResume(makeHlsStrategy(30, false), 0)).toEqual({
      offset: 0,
      seekTo: null,
      fragmentTime: null,
    });
  });
});
