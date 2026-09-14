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
            ? "/scene-lightbox?paused"
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
      // Use the normal paused state so the hover check does not interrupt
      // the lightbox's autoplay handoff or its pending media requests.
      await expect(page.locator("video")).toHaveJSProperty("paused", true);
      const surface = page.locator("[data-video-gesture-surface]");
      await expect(surface).toHaveAccessibleName("Play");
      await surface.hover({ position: { x: 100, y: 100 } });
      // Finish the hover transition before checking its final colour.
      await surface.screenshot({
        path: testInfo.outputPath("video-hover.png"),
        animations: "disabled",
      });
      await expect(surface).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
    });
  }
}
