import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";
import { dragInput } from "./scrubber-input";

for (const marker of [false, true]) {
  test(`${marker ? "TV marker" : "scene detail"} recovers a video-only stall after precision scrubbing`, async ({
    page,
    context,
    browserName,
  }) => {
    test.setTimeout(45000);
    // Keep real HLS playback and its advancing clock, but withhold frame
    // presentation notifications to reproduce the watchdog's video-only stall.
    // This tests recovery; Linux WebKit cannot reproduce Apple's decoder.
    await page.addInitScript(() => {
      const requestFrame = HTMLVideoElement.prototype.requestVideoFrameCallback;
      HTMLVideoElement.prototype.requestVideoFrameCallback = function (
        callback,
      ) {
        return requestFrame.call(this, (now, metadata) => {
          if (this.dataset.stallVideoFrames !== "true") callback(now, metadata);
        });
      };
    });
    await serveSceneMedia(page);
    let reloads = 0;
    await page.route("**/scene/*/stream.master.m3u8**", async (route) => {
      if (new URL(route.request().url()).searchParams.has("_r")) {
        reloads++;
      }
      await route.fallback();
    });
    await page.goto(
      marker
        ? "/tv-fixture/tv?low&markers&long-marker"
        : "/scene-detail?landscape&short&hls",
    );
    const player = page.locator("[data-scene-player]");
    const video = player.locator("video");
    const original = await video.elementHandle();
    if (!original) throw new Error("Missing video");
    if (!marker) await player.locator("[data-player-native-button]").click();
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await expect(video).toHaveJSProperty("paused", false);
    await expect
      .poll(() =>
        video.evaluate((v: HTMLVideoElement) =>
          v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0,
        ),
      )
      .toBeGreaterThan(marker ? 7 : 10);

    const scrubber = player.getByRole("slider", { name: "Playback position" });
    await player.hover();
    await scrubber.focus();
    const bounds = await scrubber.boundingBox();
    if (!bounds) throw new Error("Missing scrubber");
    const point = {
      x: bounds.x + bounds.width * 0.125,
      y: bounds.y + bounds.height - 2,
    };
    const drag = await dragInput(
      page,
      context,
      browserName === "chromium",
      true,
      point,
    );
    try {
      await expect(scrubber).toHaveAttribute("data-precision", "true");
      await expect(video).toHaveJSProperty("paused", true);
      await video.evaluate((v: HTMLVideoElement) => {
        v.dataset.stallVideoFrames = "true";
        v.addEventListener(
          "loadstart",
          () => {
            delete v.dataset.stallVideoFrames;
          },
          { once: true },
        );
        v.muted = false;
        v.volume = 0.4;
        v.playbackRate = 0.75;
      });
      for (const delta of [2, -1, 3, 0]) {
        await drag.move({ ...point, x: point.x + delta });
        await expect(video).toHaveJSProperty("seeking", false);
      }
      // A long dwell must not trigger recovery while the user holds a frame.
      await page.waitForTimeout(4200);
      expect(reloads).toBe(0);
    } finally {
      await drag.end();
    }
    await expect(video).toHaveJSProperty("paused", false);
    const releasedAt = await video.evaluate(
      (v: HTMLVideoElement) => v.currentTime,
    );
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeGreaterThan(releasedAt + 0.5);
    await expect.poll(() => reloads, { timeout: 10000 }).toBe(1);
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await expect(video).toHaveJSProperty("paused", false);
    expect(await original.evaluate((v) => v.isConnected)).toBe(true);
    await expect(video).toHaveJSProperty("muted", false);
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.volume))
      .toBeCloseTo(0.4, 5);
    await expect(video).toHaveJSProperty("playbackRate", 0.75);
    const frame = await video.evaluate(
      (v: HTMLVideoElement) =>
        new Promise<number>((resolve) =>
          v.requestVideoFrameCallback((_now, metadata) =>
            resolve(metadata.mediaTime),
          ),
        ),
    );
    expect(frame).toBeGreaterThan(releasedAt + 2);
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeGreaterThan(frame + 0.2);
    expect(reloads).toBe(1);
  });
}
