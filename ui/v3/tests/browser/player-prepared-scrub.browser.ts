import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test("prepares broad previews before dragging and reuses them while a fresh fine seek waits for the network", async ({
  page,
}) => {
  await page.clock.install();
  await serveSceneMedia(page);
  await page.goto("/scene-detail?landscape&short&hls");
  const player = page.locator("[data-scene-player]");
  const main = player.locator("video[data-playback-video]");
  await player.locator("[data-player-native-button]").click();
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await main.evaluate((video: HTMLVideoElement) => {
    video.pause();
    // Model the ordinary partial main buffer of a longer streamed scene.
    Object.defineProperty(video, "buffered", {
      configurable: true,
      get: (): TimeRanges => ({ length: 1, start: () => 0, end: () => 3 }),
    });
  });
  const originalTime = await main.evaluate(
    (video: HTMLVideoElement) => video.currentTime,
  );
  const surface = player.locator("[data-scrub-surface]");
  await expect
    .poll(
      async () => Number(await surface.getAttribute("data-preview-frames")),
      { timeout: 15_000 },
    )
    .toBe(4);
  await expect(surface).toHaveCSS("opacity", "0");
  expect(
    await main.evaluate((video: HTMLVideoElement) => video.currentTime),
  ).toBe(originalTime);

  // Expire the decoder through its real idle policy. Compressed coarse frames
  // must survive, so the next gesture does not depend on another HLS startup.
  await page.clock.fastForward(31_000);
  const preview = player.locator("video[data-scrub-preview]");
  await expect(preview).not.toHaveAttribute("src");
  let allow: () => void = () => {};
  const blocked = new Promise<void>((resolve) => {
    allow = resolve;
  });
  await page.route("**/media/scrub/**", async (route) => {
    await blocked;
    await route.fallback();
  });
  try {
    const slider = player.getByRole("slider", { name: "Playback position" });
    await player.hover();
    await slider.focus();
    const box = await slider.boundingBox();
    if (!box) throw new Error("Missing timeline");
    await page.mouse.move(box.x + box.width * 0.5, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width * 0.75, box.y + box.height / 2);
    await expect(surface).toHaveAttribute("data-preview-kind", "still");
    await expect(surface).toHaveAttribute("data-preview-ready", "true");
    const still = surface.locator("img[data-scrub-still]");
    await expect(still).toHaveJSProperty("complete", true);
    await expect(still).toHaveAttribute("data-scrub-time", "9");
    await expect(main).toHaveJSProperty("paused", true);
    expect(
      await main.evaluate((video: HTMLVideoElement) => video.currentTime),
    ).toBe(originalTime);
    allow();
    await expect(surface).toHaveAttribute("data-preview-kind", "video");
    await expect
      .poll(async () => Number(await preview.getAttribute("data-scrub-time")))
      .toBeCloseTo(9, 1);
    await main.evaluate((video) => {
      Reflect.deleteProperty(video, "buffered");
    });
    await page.mouse.up();
    await expect(surface).toHaveCSS("opacity", "0");
    await expect(main).toHaveJSProperty("paused", true);
    await expect
      .poll(() => main.evaluate((video: HTMLVideoElement) => video.currentTime))
      .toBeCloseTo(9, 1);
  } finally {
    allow();
  }
});
