import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const repeatedFailure of [false, true]) {
  test(`a stuck seek reload ${repeatedFailure ? "offers Retry after one failed recovery" : "recovers with a new video at the accepted position"}`, async ({
    page,
  }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, "userAgent", {
        value: `${navigator.userAgent} iPhone`,
      });
    });
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await serveSceneMedia(page);
    const reloads: URL[] = [];
    let release: (() => void) | undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route("**/scene/*/stream.master.m3u8**", async (route) => {
      const url = new URL(route.request().url());
      const attempt = Number(url.searchParams.get("_r"));
      if (attempt > 0) reloads.push(url);
      if (attempt === 1 || (repeatedFailure && attempt === 2)) await held;
      await route.fallback();
    });
    try {
      await page.goto("/scene-detail?landscape&short&hls");
      const player = page.locator("[data-scene-player]");
      const video = player.locator("video");
      await player.locator("[data-player-native-button]").click();
      await expect(player).toHaveAttribute("data-playback-ready", "true");
      await video.evaluate((v: HTMLVideoElement) => {
        v.pause();
        v.muted = false;
        v.volume = 0.4;
        v.playbackRate = 0.75;
        v.currentTime = 9;
      });
      await expect(video).toHaveJSProperty("seeking", false);
      const original = await video.elementHandle();
      if (!original) throw new Error("Missing video");
      // Model Safari evicting the early buffer. Seeking back to 2s requires
      // a source reload; the synthetic 12s media remains valid on recovery.
      await video.evaluate((v: HTMLVideoElement) => {
        Object.defineProperty(v, "buffered", {
          configurable: true,
          get: (): TimeRanges => ({
            length: 1,
            start: () => 8,
            end: () => 12,
          }),
        });
        v.dispatchEvent(new Event("progress"));
      });
      await page.clock.pauseAt(new Date("2026-01-01T00:01:00Z"));
      await player.hover();
      const scrubber = player.getByRole("slider", {
        name: "Playback position",
      });
      await scrubber.focus();
      const bounds = await scrubber.boundingBox();
      if (!bounds) throw new Error("Missing timeline");
      await page.touchscreen.tap(
        bounds.x + bounds.width / 6,
        bounds.y + bounds.height - 2,
      );
      await expect.poll(() => reloads.length).toBe(1);
      const target = Number(reloads[0]?.searchParams.get("start"));
      expect(target).toBeGreaterThan(1);
      expect(target).toBeLessThan(3);
      await expect(player).toHaveAttribute("data-playback-ready", "false");
      await page.clock.fastForward(30_001);
      await expect.poll(() => reloads.length).toBe(2);
      expect(await original.evaluate((v) => v.isConnected)).toBe(false);
      expect(Number(reloads[1]?.searchParams.get("start"))).toBeCloseTo(
        target,
        2,
      );

      if (repeatedFailure) {
        await page.clock.fastForward(30_001);
        await expect(player.getByRole("alert")).toContainText(
          "Playback could not resume",
        );
        await page.clock.fastForward(60_000);
        expect(reloads).toHaveLength(2);
        await player
          .getByRole("button", { name: "Retry", exact: true })
          .click();
        await expect.poll(() => reloads.length).toBe(3);
        expect(Number(reloads[2]?.searchParams.get("start"))).toBeCloseTo(
          target,
          2,
        );
      }
      // Resume real scheduling after advancing the recovery deadlines so the
      // real HLS engine can finish any queued fragment/demux work.
      await page.clock.resume();
      await expect(player).toHaveAttribute("data-playback-ready", "true");
      await expect(player.getByRole("alert")).toHaveCount(0);
      await expect(video).toHaveJSProperty("paused", true);
      await expect(video).toHaveJSProperty("muted", false);
      await expect
        .poll(() => video.evaluate((v: HTMLVideoElement) => v.volume))
        .toBeCloseTo(0.4, 5);
      await expect(video).toHaveJSProperty("playbackRate", 0.75);
      await expect
        .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
        .toBeCloseTo(target, 1);
    } finally {
      release?.();
    }
  });
}
