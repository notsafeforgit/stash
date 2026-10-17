import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test("prepares broad previews before dragging and reuses them while a fresh fine seek waits for the network", async ({
  page,
}, testInfo) => {
  await page.addInitScript(() => {
    const errors: unknown[] = [];
    Object.assign(window, { preparationErrors: errors });
    for (const name of [
      "loadedmetadata",
      "loadeddata",
      "seeking",
      "seeked",
      "canplay",
      "error",
      "emptied",
    ])
      document.addEventListener(
        name,
        (event) => {
          const video = event.target;
          if (
            video instanceof HTMLVideoElement &&
            video.hasAttribute("data-scrub-preview")
          )
            errors.push({
              event: event.type,
              preview: video.hasAttribute("data-scrub-preview"),
              error: video.error?.message,
              code: video.error?.code,
              time: video.currentTime,
              seeking: video.seeking,
              readyState: video.readyState,
            });
        },
        true,
      );
  });
  await page.clock.install();
  await serveSceneMedia(page);
  await page.goto("/scene-detail?landscape&short&hls");
  const player = page.locator("[data-scene-player]");
  const main = player.locator("video[data-playback-video]");
  await player.locator("[data-player-native-button]").click();
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await main.evaluate((video: HTMLVideoElement) => {
    video.pause();
    // Begin between frames, where WebKit used to fail during initial preroll.
    video.currentTime = 0.085874895;
    // Model the ordinary partial main buffer of a longer streamed scene.
    Object.defineProperty(video, "buffered", {
      configurable: true,
      get: (): TimeRanges => ({ length: 1, start: () => 0, end: () => 3 }),
    });
  });
  await expect(main).toHaveJSProperty("seeking", false);
  const originalTime = await main.evaluate(
    (video: HTMLVideoElement) => video.currentTime,
  );
  const surface = player.locator("[data-scrub-surface]");
  const preparationState = () =>
    player.evaluate((root) => {
      const surface = root.querySelector<HTMLElement>("[data-scrub-surface]");
      const main = root.querySelector<HTMLVideoElement>(
        "[data-playback-video]",
      )!;
      const preview = root.querySelector<HTMLVideoElement>(
        "[data-scrub-preview]",
      );
      return {
        frames: Number(surface?.dataset.previewFrames ?? 0),
        mainReady: main.readyState,
        mainSeeking: main.seeking,
        mainPaused: main.paused,
        previewReady: preview?.readyState,
        previewTime: preview?.currentTime,
        previewSeeking: preview?.seeking,
        previewWidth: preview?.videoWidth,
        previewError: preview?.error?.message,
        errors: (window as Window & { preparationErrors?: unknown[] })
          .preparationErrors,
      };
    });
  await expect
    .poll(preparationState, { timeout: 15_000 })
    .toMatchObject({ frames: 4 })
    .catch(async (error: unknown) => {
      await testInfo.attach("preview-state", {
        body: JSON.stringify(await preparationState()),
        contentType: "application/json",
      });
      throw error;
    });
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
