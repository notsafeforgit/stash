import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

const lightboxChunk = /\/components\/lightbox\/scene-lightbox\.tsx(?:\?.*)?$/;

test("startup preloads the player before selection and opens without a loading overlay", async ({
  page,
}) => {
  await serveSceneMedia(page);
  let chunks = 0;
  let mediaRequests = 0;
  page.on("request", (request) => {
    if (lightboxChunk.test(request.url())) chunks++;
    if (/\/scene\/[^/]+\/stream/.test(request.url())) mediaRequests++;
  });
  await page.goto("/lightbox-startup.html?preload&paused");
  await expect(page.getByTestId("player-preloaded")).toHaveText("true");
  expect(chunks).toBe(1);
  expect(mediaRequests).toBe(0);
  await expect(page.locator("video")).toHaveCount(0);
  await page.evaluate(() => {
    const observer = new MutationObserver((records) => {
      for (const record of records)
        for (const node of record.addedNodes) {
          if (
            node instanceof Element &&
            (node.matches("[data-lightbox-pending]") ||
              node.querySelector("[data-lightbox-pending]"))
          )
            document.documentElement.dataset.loadingOverlaySeen = "true";
        }
    });
    observer.observe(document.body, { childList: true, subtree: true });
  });
  await page.getByRole("button", { name: "Open scenes", exact: true }).click();
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  expect(mediaRequests).toBeGreaterThan(0);
  expect(
    await page.locator("html").getAttribute("data-loading-overlay-seen"),
  ).toBeNull();
  await page.reload();
  await expect(page.getByTestId("player-preloaded")).toHaveText("true");
  expect(chunks).toBe(2);
  await expect(page.locator("video")).toHaveCount(0);
});

for (const cancel of [false, true]) {
  test(`an early open uses a full-screen loading surface and ${cancel ? "stays closed after cancellation" : "becomes the player when ready"}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 430, height: 932 });
    await serveSceneMedia(page);
    let requested = false;
    let release: (() => void) | undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(lightboxChunk, async (route) => {
      requested = true;
      await held;
      await route.fallback();
    });
    try {
      // A held module must not stop the rest of the app from mounting.
      await page.goto("/lightbox-startup.html?preload&paused", {
        waitUntil: "domcontentloaded",
      });
      const open = page.getByRole("button", {
        name: "Open scenes",
        exact: true,
      });
      await expect(open).toBeVisible();
      await expect.poll(() => requested).toBe(true);
      await page.addStyleTag({
        content: ":root { --safe-area-top: 59px; --safe-area-bottom: 34px; }",
      });
      await open.click();
      const pending = page.locator("[data-lightbox-pending]");
      await expect(pending).toBeVisible();
      expect(await pending.boundingBox()).toEqual({
        x: 0,
        y: 0,
        width: 430,
        height: 932,
      });
      const close = pending.getByRole("button", { name: "Close", exact: true });
      const box = await close.boundingBox();
      if (!box) throw new Error("Missing loading close control");
      expect(box.height).toBeGreaterThanOrEqual(44);
      expect(box.y).toBeGreaterThanOrEqual(932 - 34 - 44 - 1);
      expect(box.x).toBeGreaterThanOrEqual(430 - 16 - 44 - 1);
      expect(box.y + box.height).toBeLessThanOrEqual(932 - 34);
      expect(box.x + box.width).toBeLessThanOrEqual(430 - 16);
      await page.screenshot({
        path: test.info().outputPath("lightbox-loading.png"),
      });
      if (cancel) await close.click();
      release?.();
      await expect(page.getByTestId("player-preloaded")).toHaveText("true");
      await expect(pending).toHaveCount(0);
      if (cancel) {
        await expect(page.locator("video")).toHaveCount(0);
        await open.click();
      }
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      await expect(page.locator("video")).toHaveCount(1);
    } finally {
      release?.();
    }
  });
}
