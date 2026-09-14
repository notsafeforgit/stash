import { describe, expect, it, vi } from "vitest";
import {
  SceneActivityVisit,
  SceneActivityWriter,
  type ActivityObservation,
  type ActivityWrite,
} from "./scene-activity";

function observation(
  position: number,
  changes: Partial<ActivityObservation> = {},
): ActivityObservation {
  return {
    now: position * 1000,
    position,
    duration: 100,
    source: "scene",
    rate: 1,
    ready: true,
    playing: true,
    seeking: false,
    ended: false,
    ...changes,
  };
}

describe("scene activity accounting", () => {
  it("counts observed playback through pause and ignores UI-only updates", () => {
    const visit = new SceneActivityVisit();
    visit.observe(observation(0), 1);
    visit.observe(observation(0, { now: 900 }), 1);
    expect(visit.observe(observation(1, { playing: false }), 1)).toEqual({
      watched: 1,
      resume: 1,
      qualified: true,
    });
    expect(
      visit.observe(observation(1, { now: 10000, playing: false }), 1).watched,
    ).toBe(1);
  });

  it("excludes seeks, source transitions, buffering and suspended timer gaps", () => {
    const visit = new SceneActivityVisit();
    visit.observe(observation(0), 0);
    visit.observe(observation(30, { now: 1000, seeking: true }), 0);
    visit.observe(observation(30, { now: 1100 }), 0);
    visit.observe(observation(31, { now: 2100, source: "new-quality" }), 0);
    visit.observe(
      observation(32, { now: 3100, source: "new-quality", ready: false }),
      0,
    );
    visit.observe(observation(33, { now: 4100, source: "new-quality" }), 0);
    visit.observe(observation(34, { now: 20000, source: "new-quality" }), 0);
    expect(visit.snapshot()).toEqual({
      watched: 0,
      resume: 0,
      qualified: false,
    });
    expect(
      visit.observe(observation(35, { now: 21000, source: "new-quality" }), 0),
    ).toEqual({ watched: 1, resume: 35, qualified: true });
    visit.suspend();
    expect(
      visit.observe(observation(36, { now: 22000, source: "new-quality" }), 0)
        .watched,
    ).toBe(1);
  });

  it("counts actual media progress at the chosen rate and resets resume only at scene EOF", () => {
    const visit = new SceneActivityVisit();
    visit.observe(observation(90, { now: 0, rate: 2 }), 10);
    expect(
      visit.observe(
        observation(94, { now: 2000, rate: 2, playing: false }),
        10,
      ),
    ).toEqual({ watched: 4, resume: 94, qualified: false });
    visit.observe(observation(94, { now: 2500, rate: 2 }), 10);
    expect(
      visit.observe(
        observation(100, { now: 5500, rate: 2, ended: true, playing: false }),
        10,
      ),
    ).toEqual({ watched: 10, resume: 0, qualified: true });
  });
});

describe("shared scene activity writes", () => {
  it("serializes surfaces for the same scene and sends deltas with one play per visit", async () => {
    const writer = new SceneActivityWriter();
    const writes: ActivityWrite[] = [];
    let release = () => {};
    const firstWrite = new Promise<void>((resolve) => {
      release = resolve;
    });
    const save = vi.fn(async (write: ActivityWrite) => {
      writes.push(write);
      if (writes.length === 1) await firstWrite;
    });
    const play = vi.fn(async () => {});
    const report = vi.fn();
    const first = writer.createVisit("1", save, play, () => true, report);
    const nextSurface = writer.createVisit("1", save, play, () => true, report);
    const a = first({ watched: 2, resume: 12, qualified: true });
    const b = first({ watched: 5, resume: 15, qualified: true });
    const c = nextSurface({ watched: 1, resume: 81, qualified: true });
    await Promise.resolve();
    expect(writes).toEqual([{ sceneId: "1", delta: 2, resume: 12 }]);
    release();
    await Promise.all([a, b, c]);
    expect(writes).toEqual([
      { sceneId: "1", delta: 2, resume: 12 },
      { sceneId: "1", delta: 3, resume: 15 },
      { sceneId: "1", delta: 1, resume: 81 },
    ]);
    expect(play).toHaveBeenCalledTimes(2);
    await first({ watched: 5, resume: 15, qualified: true });
    expect(save).toHaveBeenCalledTimes(3);
    expect(report).not.toHaveBeenCalled();
  });

  it("quarantines uncertain increments and does not blindly retry play count", async () => {
    const save = vi
      .fn<(write: ActivityWrite) => Promise<void>>()
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValue(undefined);
    const play = vi.fn(async () => {
      throw new Error("play response lost");
    });
    const report = vi.fn();
    const flush = new SceneActivityWriter().createVisit(
      "1",
      save,
      play,
      () => true,
      report,
    );
    await flush({ watched: 5, resume: 5, qualified: true });
    await flush({ watched: 5, resume: 5, qualified: true });
    expect(save).toHaveBeenCalledTimes(1);
    expect(play).not.toHaveBeenCalled();
    await flush({ watched: 7, resume: 7, qualified: true });
    await flush({ watched: 8, resume: 8, qualified: true });
    expect(save.mock.calls.map(([write]) => write.delta)).toEqual([5, 2, 1]);
    expect(play).toHaveBeenCalledTimes(1);
    expect(report).toHaveBeenCalledTimes(2);
  });

  it("rechecks the tracking preference before queued work and excludes zero-watch visits", async () => {
    let enabled = true;
    const save = vi.fn(async () => {});
    const play = vi.fn(async () => {});
    const flush = new SceneActivityWriter().createVisit(
      "1",
      save,
      play,
      () => enabled,
      vi.fn(),
    );
    await flush({ watched: 0, resume: 50, qualified: true });
    const queued = flush({ watched: 2, resume: 2, qualified: true });
    enabled = false;
    await queued;
    expect(save).not.toHaveBeenCalled();
    expect(play).not.toHaveBeenCalled();
  });
});
