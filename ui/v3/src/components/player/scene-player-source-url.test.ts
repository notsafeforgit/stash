import { describe, expect, it } from "vitest";
import { injectReloadNonce } from "./scene-player-source-url";

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
