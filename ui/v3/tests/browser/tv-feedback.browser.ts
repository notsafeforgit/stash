import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const marker of [false, true]) {
  test(`TV ${marker ? "marker" : "scene"} loading never flashes a paused indicator`, async ({
    page,
  }) => {
    await serveSceneMedia(page);
    let release = () => {};
    const holdSource = () =>
      new Promise<void>((resolve) => {
        release = resolve;
      });
    let pending = holdSource();
    await page.route("**/scene/*/stream**", async (route) => {
      if (new URL(route.request().url()).pathname.endsWith("/stream"))
        await pending;
      await route.fallback();
    });
    try {
      await page.goto(`/tv-fixture/tv?long-marker${marker ? "&markers" : ""}`);
      const controls = page.locator("[data-tv-controls]");
      const indicator = controls.locator("svg.lucide-play");
      const video = page.locator("video");
      const player = page.locator("[data-scene-player]");
      await expect(
        controls.getByRole("status", { name: "Loading" }),
      ).toBeVisible();
      await expect(indicator).toHaveCount(0);
      release();
      await expect(player).toHaveAttribute("data-playback-ready", "true");
      await expect(video).toHaveJSProperty("paused", false);
      await expect(indicator).toHaveCount(0);

      await page
        .locator("[data-tv-play-surface]")
        .tap({ position: { x: 150, y: 250 } });
      await expect(video).toHaveJSProperty("paused", true);
      await expect(indicator).toBeVisible();

      // A deliberately paused previous item must not lend its play icon to
      // the next item's media load, including a marker in the same scene.
      pending = holdSource();
      await page.evaluate(() => {
        if (document.activeElement instanceof HTMLElement)
          document.activeElement.blur();
      });
      await page.keyboard.press("ArrowDown");
      await expect(player).toHaveAttribute(
        "data-playback-key",
        marker ? /marker:11$/ : /scene:2$/,
      );
      await expect(
        controls.getByRole("status", { name: "Loading" }),
      ).toBeVisible();
      await expect(indicator).toHaveCount(0);
      release();
      await expect(player).toHaveAttribute("data-playback-ready", "true");
      await expect(video).toHaveJSProperty("paused", false);
      await expect(indicator).toHaveCount(0);
      await page
        .locator("[data-tv-play-surface]")
        .tap({ position: { x: 150, y: 250 } });
      await expect(video).toHaveJSProperty("paused", true);
      await expect(indicator).toBeVisible();
    } finally {
      release();
    }
  });
}
