// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { parseStartPosition, suspendHlsBuffering } from "./hls";

const scene = "https://stash.test/scene/1/stream.master.m3u8";

afterEach(() => vi.unstubAllGlobals());

describe("HLS scrub loading", () => {
  const engine = (bufferingEnabled = true) => ({
    bufferingEnabled,
    stopLoad: vi.fn(),
    startLoad: vi.fn(),
    trigger: vi.fn(),
    pauseBuffering: vi.fn(),
    resumeBuffering: vi.fn(),
  });

  it("suspends new fragments without aborting or flushing existing media", () => {
    const media = { engine: engine() };
    const resume = suspendHlsBuffering(media);
    expect(media.engine.pauseBuffering).toHaveBeenCalledOnce();
    expect(media.engine.stopLoad).not.toHaveBeenCalled();
    expect(media.engine.trigger).not.toHaveBeenCalled();
    resume();
    expect(media.engine.resumeBuffering).toHaveBeenCalledOnce();
  });

  it("does not start a loader that was already suspended or has been replaced", () => {
    const old = engine();
    const media = { engine: old };
    const resume = suspendHlsBuffering(media);
    media.engine = engine(false);
    resume();
    suspendHlsBuffering(media)();
    expect(old.resumeBuffering).not.toHaveBeenCalled();
    expect(media.engine.pauseBuffering).not.toHaveBeenCalled();
    expect(media.engine.resumeBuffering).not.toHaveBeenCalled();
    expect(() => suspendHlsBuffering(null)()).not.toThrow();
  });

  it("leaves ManagedMediaSource in control of loading throughout a drag", () => {
    vi.stubGlobal("ManagedMediaSource", class {});
    const media = { engine: engine() };
    const resume = suspendHlsBuffering(media);
    expect(media.engine.pauseBuffering).not.toHaveBeenCalled();
    // Safari can withdraw its loading window before the drag is released.
    media.engine.bufferingEnabled = false;
    resume();
    expect(media.engine.resumeBuffering).not.toHaveBeenCalled();
  });
});

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
