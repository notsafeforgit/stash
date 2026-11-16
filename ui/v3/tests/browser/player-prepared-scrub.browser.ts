import { test, expect } from "./test";
import { FIXTURE_KEYFRAME_VERSION, serveSceneMedia } from "./scene-media";

test("prefetches keyframe stills before dragging and shows one while the exact frame waits for the network", async ({
  page,
}) => {
  await page.clock.install();
  await serveSceneMedia(page);
  const keyframes: URL[] = [];
  const scrubManifests: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (/\/keyframes?(?:\/stills)?$/.test(url.pathname)) keyframes.push(url);
    if (url.pathname.endsWith("/stream.scrub.master.m3u8"))
      scrubManifests.push(request.url());
  });
  await page.goto("/scene-detail?landscape&short&hls");
  const player = page.locator("[data-scene-player]");
  const main = player.locator("video[data-playback-video]");
  await player.locator("[data-player-native-button]").click();
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await main.evaluate((video: HTMLVideoElement) => {
    video.pause();
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
  // Every keyframe of the twelve-second scene from one batch request, sliced
  // into separate stills, without an HLS session.
  await expect(surface).toHaveAttribute("data-preview-frames", "6", {
    timeout: 15_000,
  });
  await expect(surface).toHaveAttribute("data-keyframe-index", "ready");
  await expect(surface).toHaveCSS("opacity", "0");
  expect(scrubManifests).toEqual([]);
  await expect(player.locator("video[data-scrub-preview]")).toHaveCount(0);
  expect(keyframes.filter((url) => url.pathname.endsWith("/keyframe"))).toEqual(
    [],
  );
  const batches = keyframes.filter((url) =>
    url.pathname.endsWith("/keyframes/stills"),
  );
  expect(batches.map((url) => url.search)).toEqual([
    `?v=${FIXTURE_KEYFRAME_VERSION}&t=0,2,4,6,8,10`,
  ]);
  expect(
    keyframes.filter((url) => url.pathname.endsWith("/keyframes")),
  ).toHaveLength(1);
  expect(
    await main.evaluate((video: HTMLVideoElement) => video.currentTime),
  ).toBe(originalTime);

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
    const requested = keyframes.length;
    await page.mouse.move(box.x + box.width * 0.5, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width * 0.75, box.y + box.height / 2);
    await expect(surface).toHaveAttribute("data-preview-kind", "still");
    await expect(surface).toHaveAttribute("data-preview-ready", "true");
    const still = surface.locator("img[data-scrub-still]");
    await expect(still).toHaveJSProperty("complete", true);
    // The keyframe at or before 9 s, from the prepared cache.
    await expect(still).toHaveAttribute("data-scrub-time", "8");
    expect(keyframes).toHaveLength(requested);
    await expect(slider).toHaveAttribute("aria-busy", "true");
    await expect(main).toHaveJSProperty("paused", true);
    expect(
      await main.evaluate((video: HTMLVideoElement) => video.currentTime),
    ).toBe(originalTime);
    allow();
    await expect(surface).toHaveAttribute("data-preview-kind", "video");
    const preview = player.locator("video[data-scrub-preview]");
    await expect
      .poll(async () => Number(await preview.getAttribute("data-scrub-time")))
      .toBeCloseTo(9, 1);
    await expect(slider).not.toHaveAttribute("aria-busy");
    const timings = await page.evaluate(() =>
      window.__stashScrubTimings?.summary(),
    );
    expect(timings?.["still-cache"].count).toBeGreaterThan(0);
    expect(timings?.["exact-frame"].count).toBeGreaterThan(0);
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

test("fetches an uncached keyframe during a drag and shows the debug readout when enabled", async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem("stash.scrubDebug", "1"));
  await serveSceneMedia(page);
  // Hold every still and batch until the drag, so none is prepared in the
  // background.
  let release: () => void = () => {};
  const stills = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(
    (url) => /\/keyframe$|\/keyframes\/stills$/.test(url.pathname),
    async (route) => {
      await stills;
      // Background requests superseded by the drag are already cancelled.
      await route.fallback().catch(() => {});
    },
  );
  await page.goto("/scene-detail?landscape&short&hls");
  const player = page.locator("[data-scene-player]");
  const main = player.locator("video[data-playback-video]");
  await player.locator("[data-player-native-button]").click();
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  const readout = player.locator("[data-scrub-timings]");
  await expect(readout).toBeVisible();
  await main.evaluate((video: HTMLVideoElement) => {
    video.pause();
    // No main-buffer coverage, and no time for background preparation.
    Object.defineProperty(video, "buffered", {
      configurable: true,
      get: (): TimeRanges => ({ length: 0, start: () => 0, end: () => 0 }),
    });
  });
  const surface = player.locator("[data-scrub-surface]");
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
    await page.mouse.move(box.x + box.width * 0.2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width * 0.4, box.y + box.height / 2);
    await expect(surface).toHaveAttribute("data-preview-frames", "0");
    release();
    await expect(surface).toHaveAttribute("data-preview-kind", "still");
    await expect(surface.locator("img[data-scrub-still]")).toHaveAttribute(
      "data-scrub-time",
      "4",
    );
    await expect(
      readout.locator('[data-scrub-timing-tier="still-network"] td').nth(1),
    ).toHaveText("1");
    allow();
    await expect(surface).toHaveAttribute("data-preview-kind", "video");
    await expect(
      readout.locator('[data-scrub-timing-tier="exact-frame"] td').nth(1),
    ).toHaveText("1");
    await main.evaluate((video) => {
      Reflect.deleteProperty(video, "buffered");
    });
    await page.mouse.up();
    await expect(surface).toHaveCSS("opacity", "0");
  } finally {
    release();
    allow();
  }
});

test.describe("without keyframe endpoints", () => {
  test.use({ expectedConsoleErrors: ["status of 404"] });

  test("keeps the exact-frame preview and commits normally", async ({
    page,
  }) => {
    await serveSceneMedia(page, "landscape", { keyframes: false });
    await page.goto("/scene-detail?landscape&short&hls");
    const player = page.locator("[data-scene-player]");
    const main = player.locator("video[data-playback-video]");
    await player.locator("[data-player-native-button]").click();
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await main.evaluate((video: HTMLVideoElement) => {
      video.pause();
      Object.defineProperty(video, "buffered", {
        configurable: true,
        get: (): TimeRanges => ({ length: 0, start: () => 0, end: () => 0 }),
      });
    });
    const slider = player.getByRole("slider", { name: "Playback position" });
    await player.hover();
    await slider.focus();
    const box = await slider.boundingBox();
    if (!box) throw new Error("Missing timeline");
    await page.mouse.move(box.x + box.width * 0.2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width * 0.6, box.y + box.height / 2);
    const surface = player.locator("[data-scrub-surface]");
    await expect(surface).toHaveAttribute("data-preview-kind", "video");
    await expect(surface).toHaveAttribute("data-preview-frames", "0");
    await expect
      .poll(async () =>
        Number(
          await player
            .locator("video[data-scrub-preview]")
            .getAttribute("data-scrub-time"),
        ),
      )
      .toBeCloseTo(7.2, 1);
    await main.evaluate((video) => {
      Reflect.deleteProperty(video, "buffered");
    });
    await page.mouse.up();
    await expect(surface).toHaveCSS("opacity", "0");
    await expect
      .poll(() => main.evaluate((video: HTMLVideoElement) => video.currentTime))
      .toBeCloseTo(7.2, 1);
  });
});
