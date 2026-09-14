import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test.use({
  viewport: { width: 1280, height: 800 },
  isMobile: false,
  hasTouch: false,
});

for (const theme of ["light", "dark"]) {
  for (const context of ["scene detail", "lightbox", "TV"]) {
    test(`${context} keeps desktop video hover transparent in ${theme} theme`, async ({
      page,
    }, testInfo) => {
      await serveSceneMedia(page);
      await page.goto(
        context === "TV"
          ? "/tv-fixture/tv?paused"
          : context === "lightbox"
            ? "/scene-lightbox"
            : "/scene-detail",
      );
      if (context === "lightbox")
        await page
          .getByRole("button", { name: "Open scenes", exact: true })
          .click();
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      await page.evaluate(
        (dark) => document.documentElement.classList.toggle("dark", dark),
        theme === "dark",
      );
      const video = page.locator("video");
      if (context === "lightbox") {
        // The lightbox gates autoplay on its entrance transition. Wait for that
        // handoff before pausing so it cannot race the click assertions below.
        await expect
          .poll(() =>
            video.evaluate((element: HTMLVideoElement) => element.currentTime),
          )
          .toBeGreaterThan(0.2);
      }
      await video.evaluate((element: HTMLVideoElement) => element.pause());
      const surface = page.locator("[data-video-gesture-surface]");
      await expect(surface).toHaveAccessibleName("Play");
      await surface.hover({ position: { x: 100, y: 100 } });
      // Finish the hover transition before checking its final colour.
      await surface.screenshot({
        path: testInfo.outputPath("video-hover.png"),
        animations: "disabled",
      });
      await expect(surface).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
      await surface.click({ position: { x: 100, y: 100 } });
      await expect(video).toHaveJSProperty("paused", false);
      await surface.click({ position: { x: 100, y: 100 } });
      await expect(video).toHaveJSProperty("paused", true);
    });
  }
}
