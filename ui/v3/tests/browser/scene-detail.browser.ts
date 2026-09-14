import { test, expect, detailFooter, chooseSection } from "./test";

declare global {
  interface Window {
    freezeFrameWarmups: number[];
  }
}

test.beforeEach(async ({ page }) => {
  await page.route("**/scene/detail/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/stream")) {
      const response = await route.fetch({
        url: new URL("/media/audio.mp4", url).href,
      });
      await route.fulfill({ response });
    } else if (/\/streams\.(stop|keepalive)$/.test(url.pathname)) {
      await route.fulfill({ status: 204 });
    } else {
      throw new Error(`Unexpected scene request: ${url}`);
    }
  });
});

for (const landscape of [false, true]) {
  test(`${landscape ? "landscape" : "portrait"} video controls fit above the mobile footer`, async ({
    page,
  }) => {
    await page.goto(`/scene-detail${landscape ? "?landscape" : ""}`);
    // Model an iPhone home-indicator inset as part of the real footer layout.
    await page.addStyleTag({
      content: "[data-mobile-detail-footer] { padding-bottom: 34px; }",
    });
    const player = page.locator("[data-scene-player]");
    const row = player.locator("[data-player-control-row]");
    const footer = detailFooter(page);
    const video = await player.locator("video").elementHandle();
    for (const viewport of [
      { width: 320, height: 568 },
      { width: 390, height: 740 },
      { width: 390, height: 844 },
      { width: 844, height: 390 },
      { width: 768, height: 1024 },
    ]) {
      await page.setViewportSize(viewport);
      await expect(row).toBeVisible();
      await expect(footer).toBeVisible();
      await expect
        .poll(() =>
          player.evaluate((element) => {
            const footer = document.querySelector(
              "[data-mobile-detail-footer]",
            );
            const row = element.querySelector("[data-player-control-row]");
            if (!footer || !row) return false;
            const bounds = element.getBoundingClientRect();
            const controls = row.getBoundingClientRect();
            return (
              bounds.top >= 0 &&
              bounds.bottom <= footer.getBoundingClientRect().top + 1 &&
              controls.top >= bounds.top &&
              controls.bottom <= bounds.bottom + 1
            );
          }),
        )
        .toBe(true);
      const bounds = await player.boundingBox();
      const footerBounds = await footer.boundingBox();
      if (!bounds || !footerBounds) throw new Error("Missing detail layout");
      const naturalHeight = viewport.width * (landscape ? 9 / 16 : 16 / 9);
      expect(bounds.height).toBeCloseTo(
        Math.min(naturalHeight, footerBounds.y - bounds.y),
        0,
      );
      expect(await video?.evaluate((element) => element.isConnected)).toBe(
        true,
      );
    }
    await page.setViewportSize({ width: 390, height: 740 });
    await page.screenshot({
      path: test
        .info()
        .outputPath(`${landscape ? "landscape" : "portrait"}-detail.png`),
    });
    await chooseSection(page, "Details");
    await expect(
      page.getByText("Scene details below the player"),
    ).toBeInViewport();
  });
}

test("the focused viewer fills the viewport and restores the same inline player", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 740 });
  await page.goto("/scene-detail");
  const player = page.locator("[data-scene-player]");
  const video = await player.locator("video").elementHandle();
  const open = player.getByRole("button", {
    name: "Open scene viewer",
    exact: true,
  });
  await open.tap();
  const viewer = page.getByRole("dialog", {
    name: "Scene viewer",
    exact: true,
  });
  await expect(viewer).toBeVisible();
  await expect(detailFooter(page)).toHaveCount(0);
  const bounds = await viewer.boundingBox();
  expect(bounds).toEqual({ x: 0, y: 0, width: 390, height: 740 });
  expect(await video?.evaluate((element) => element.isConnected)).toBe(true);
  await viewer.getByRole("button", { name: "Close scene viewer" }).tap();
  await expect(viewer).toHaveCount(0);
  await expect(open).toBeFocused();
  await expect(detailFooter(page)).toBeVisible();
  expect(await video?.evaluate((element) => element.isConnected)).toBe(true);
});

test("a scene scrolls while media is loading without warming a full-size canvas", async ({
  page,
  context,
  browserName,
}) => {
  let release: () => void = () => {};
  const mediaReady = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/scene/detail/stream", async (route) => {
    await mediaReady;
    await route.fallback();
  });
  await page.addInitScript(() => {
    window.freezeFrameWarmups = [];
    const read = CanvasRenderingContext2D.prototype.getImageData;
    CanvasRenderingContext2D.prototype.getImageData = function (...args) {
      window.freezeFrameWarmups.push(
        document.querySelector("video")?.readyState ?? -1,
      );
      return read.apply(this, args);
    };
  });
  try {
    await page.goto("/scene-detail", { waitUntil: "domcontentloaded" });
    const player = page.locator("[data-scene-player]");
    await expect(player).toBeVisible();
    expect(await page.evaluate(() => window.freezeFrameWarmups)).toEqual([]);
    const scroller = page.locator(".overflow-y-auto").filter({ has: player });
    if (browserName === "chromium") {
      const input = await context.newCDPSession(page);
      await input.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [{ x: 100, y: 300 }],
      });
      for (let y = 280; y >= 100; y -= 20) {
        await input.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [{ x: 100, y }],
        });
      }
      await input.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
      await input.detach();
    } else {
      // Playwright cannot synthesize a swipe/wheel in mobile WebKit. Exercise
      // the actual section control's native scroll path while media is held.
      await chooseSection(page, "Details");
    }
    await expect
      .poll(() => scroller.evaluate((element) => element.scrollTop))
      .toBeGreaterThan(30);
    expect(await page.evaluate(() => window.freezeFrameWarmups)).toEqual([]);
    release();
    await expect
      .poll(() => page.evaluate(() => window.freezeFrameWarmups.length))
      .toBeGreaterThan(0);
    expect(
      await page.evaluate(() =>
        window.freezeFrameWarmups.every((ready) => ready >= 2),
      ),
    ).toBe(true);
  } finally {
    release();
  }
});
