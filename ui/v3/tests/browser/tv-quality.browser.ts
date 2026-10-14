import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test("a resolution ceiling still excludes a direct format rejected by the browser", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const canPlayType = HTMLMediaElement.prototype.canPlayType;
    HTMLMediaElement.prototype.canPlayType = function (type) {
      // Reject the advertised original MIME without changing the codec probe
      // for the H.264/AAC HLS pipeline or the synthetic media decoder.
      return type === "video/mp4" ? "" : canPlayType.call(this, type);
    };
  });
  await serveSceneMedia(page);
  const media: string[] = [];
  page.on("request", (request) => {
    if (/\/scene\/\d+\/stream/.test(request.url())) media.push(request.url());
  });
  await page.goto("/tv-fixture/tv?paused&full-hd&preload=3");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect
    .poll(() =>
      media.some((url) => url.includes("/scene/2/stream.master.m3u8")),
    )
    .toBe(true);
  expect(media.every((url) => url.includes("resolution=LOW"))).toBe(true);
});

test("a direct decoding failure falls back to an eligible transcode without changing the TV limit", async ({
  page,
}) => {
  await serveSceneMedia(page);
  await page.goto("/tv-fixture/tv?paused&full-hd");
  const player = page.locator("[data-scene-player]");
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  const video = page.locator("video");
  await expect(video).toHaveAttribute("src", /\/stream(?:[?#]|$)/);
  const fallback = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname.endsWith("/stream.master.m3u8") &&
      url.searchParams.get("resolution") === "LOW"
    );
  });
  await video.evaluate((element: HTMLVideoElement) => {
    Object.defineProperty(element, "error", {
      configurable: true,
      value: { code: 3, message: "Synthetic native decoding failure" },
    });
    element.dispatchEvent(new Event("error"));
    Reflect.deleteProperty(element, "error");
  });
  await fallback;
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await page.keyboard.press("ArrowDown");
  await expect(player).toHaveAttribute("data-playback-key", /scene:2$/);
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await expect(video).toHaveAttribute("src", /\/scene\/2\/stream(?:[?#]|$)/);
  expect(
    await page.evaluate(() => localStorage.getItem("stash-player-quality")),
  ).toBeNull();
});
