import { describe, expect, it } from "vitest";
import {
  injectReloadNonce,
  scenePlayerSourceURL,
} from "./scene-player-source-url";

describe("scene player source URLs", () => {
  it("retains the full clip after a mid-clip quality switch", () => {
    const src = "https://stash.test/scene/1/stream.master.m3u8?resolution=720";
    const url = new URL(
      scenePlayerSourceURL(src, 125, { start: 101, end: 220 }, 0)!,
    );
    expect(url.searchParams.get("start")).toBe("101");
    expect(url.searchParams.get("end")).toBe("220");
    expect(url.searchParams.get("resolution")).toBe("720");
  });

  it("keeps direct-stream clip fragments valid while forcing a reload", () => {
    expect(
      scenePlayerSourceURL(
        "https://stash.test/scene/1/stream",
        125,
        { start: 101, end: 220 },
        2,
      ),
    ).toBe("https://stash.test/scene/1/stream?_r=2#t=101,220");
  });

  it("changes the URL even when a forced reload repeats scene time zero", () => {
    const src = "https://stash.test/scene/1/stream.master.m3u8";
    expect(scenePlayerSourceURL(src, null, undefined, 2)).not.toBe(
      scenePlayerSourceURL(src, null, undefined, 1),
    );
  });
});

describe("injectReloadNonce", () => {
  it("leaves the source unchanged before a reload", () => {
    expect(injectReloadNonce("https://stash.test/scene/1/stream", 0)).toBe(
      "https://stash.test/scene/1/stream",
    );
  });

  it("adds a query parameter to a stable source URL", () => {
    expect(injectReloadNonce("https://stash.test/scene/1/stream", 2)).toBe(
      "https://stash.test/scene/1/stream?_r=2",
    );
  });

  it("preserves existing query parameters", () => {
    expect(
      injectReloadNonce(
        "https://stash.test/scene/1/stream.master.m3u8?start=12",
        3,
      ),
    ).toBe("https://stash.test/scene/1/stream.master.m3u8?start=12&_r=3");
  });

  it("inserts the query before a media fragment", () => {
    expect(
      injectReloadNonce("https://stash.test/scene/1/stream#t=12.5", 4),
    ).toBe("https://stash.test/scene/1/stream?_r=4#t=12.5");
  });
});
