import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

const HEVC = 'video/mp4; codecs="hvc1.1.6.L153.B0"';

for (const overReported of [false, true]) {
  test(`an HEVC file plays a picture, never audio alone${overReported ? ", when the browser over-reports HEVC" : ""}`, async ({
    page,
  }) => {
    if (overReported) {
      // Model a browser whose `canPlayType` claims a codec its decoder lacks,
      // so the player must notice the missing picture after loading.
      await page.addInitScript((hevc) => {
        const canPlayType = HTMLMediaElement.prototype.canPlayType;
        HTMLMediaElement.prototype.canPlayType = function (type: string) {
          return type === hevc ? "probably" : canPlayType.call(this, type);
        };
      }, HEVC);
    }
    await serveSceneMedia(page);
    await page.route(
      (url) => url.pathname.endsWith("/scene/detail/stream"),
      (route) =>
        route.continue({
          url: new URL("/media/hevc.mp4", route.request().url()).href,
        }),
    );
    const requested = { direct: false, transcode: false };
    page.on("request", (request) => {
      const path = new URL(request.url()).pathname;
      if (path.endsWith("/scene/detail/stream")) requested.direct = true;
      if (path.endsWith("/scene/detail/stream.master.m3u8"))
        requested.transcode = true;
    });

    await page.goto("/scene-detail?hevc");
    // Whether this engine really decodes HEVC; the over-report above only
    // replaces `canPlayType`.
    const decodesHevc = await page.evaluate(
      (hevc) => MediaSource.isTypeSupported(hevc),
      HEVC,
    );
    const player = page.locator("[data-scene-player]");
    const video = player.locator("video[data-playback-video]");
    await player.locator("[data-player-native-button]").click();
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.videoWidth))
      .toBe(160);
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeGreaterThan(0.5);
    if (!decodesHevc) {
      expect(requested.transcode).toBe(true);
      // An honest browser never loads the undecodable direct stream.
      if (!overReported) expect(requested.direct).toBe(false);
    }
  });
}
