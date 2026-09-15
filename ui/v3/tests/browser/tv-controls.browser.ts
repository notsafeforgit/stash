import { test, expect, expectTouchTargets } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const viewport of [
  { name: "mobile", width: 390, height: 844, touch: true, safeBottom: 34 },
  { name: "desktop", width: 1280, height: 800, touch: false, safeBottom: 0 },
]) {
  test.describe(`Minimal TV controls on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.touch,
      hasTouch: viewport.touch,
    });

    test("portrait video remains clear behind the icons and hidden-UI gaps accept gestures", async ({
      page,
    }, testInfo) => {
      await serveSceneMedia(page, "portrait");
      await page.goto("/tv-fixture/tv?paused&portrait");
      const tv = page.locator("[data-tv]");
      const dock = page.locator("[data-tv-dock]");
      const video = page.locator("video");
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      await expect(video).toHaveJSProperty("videoWidth", 180);
      await expect(video).toHaveJSProperty("videoHeight", 320);
      await tv.evaluate(
        (element, bottom) =>
          element.style.setProperty("--safe-area-bottom", `${bottom}px`),
        viewport.safeBottom,
      );
      await expectTouchTargets(dock);
      await expectTouchTargets(
        page.getByRole("complementary", { name: "TV actions" }),
      );

      for (const theme of ["light", "dark"]) {
        await page.evaluate(
          (dark) => document.documentElement.classList.toggle("dark", dark),
          theme === "dark",
        );
        await tv.screenshot({
          path: testInfo.outputPath(`portrait-${theme}-visible.png`),
          animations: "disabled",
        });
      }
      await page
        .getByRole("button", { name: "Show or hide controls", exact: true })
        .click();
      await expect(
        page.getByRole("complementary", { name: "TV actions" }),
      ).toHaveCount(0);
      await expect(dock.getByRole("slider")).toHaveCount(0);
      await expect(dock.getByRole("button")).toHaveCount(3);
      await tv.screenshot({
        path: testInfo.outputPath("portrait-hidden.png"),
        animations: "disabled",
      });
      await expect(dock).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
      await expect(dock).toHaveCSS("background-image", "none");
      await expectTouchTargets(dock);
      for (const button of await dock.getByRole("button").all()) {
        await expect(button).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
        await button.hover();
        await expect(button).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
      }
      const dockBounds = await dock.boundingBox();
      const mute = dock.getByRole("button", { name: "Unmute", exact: true });
      const muteBounds = await mute.boundingBox();
      if (!dockBounds || !muteBounds) throw new Error("Missing TV controls");
      expect(dockBounds.height).toBeLessThanOrEqual(
        48 + Math.max(8, viewport.safeBottom),
      );
      expect(muteBounds.y + muteBounds.height).toBeLessThanOrEqual(
        viewport.height - viewport.safeBottom,
      );

      // The empty middle of the hidden dock is part of the video gesture surface.
      const point = {
        x: viewport.width / 2,
        y: muteBounds.y + muteBounds.height / 2,
      };
      expect(
        await page.evaluate(
          ({ x, y }) =>
            document.elementFromPoint(x, y)?.matches("[data-tv-play-surface]"),
          point,
        ),
      ).toBe(true);
      if (viewport.touch) await page.touchscreen.tap(point.x, point.y);
      else await page.mouse.click(point.x, point.y);
      await expect(video).toHaveJSProperty("paused", false);
      await expect
        .poll(() =>
          video.evaluate((element: HTMLVideoElement) => element.currentTime),
        )
        .toBeGreaterThan(0.2);
      await mute.click();
      await expect(video).toHaveJSProperty("muted", false);
      await page.mouse.move(point.x, point.y);
      if (viewport.touch) {
        // WebKit exposes pointer dragging, but no native touch swipe API.
        await page.mouse.down();
        await page.mouse.move(point.x, point.y - 220, { steps: 10 });
        await page.mouse.up();
      } else {
        await page.mouse.wheel(0, 180);
      }
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-key",
        /scene:2$/,
      );
      await expect(video).toHaveJSProperty("muted", false);
      await page
        .getByRole("button", { name: "Show TV controls", exact: true })
        .click();
      await expect(dock.getByRole("slider")).toBeVisible();
      await expect(dock).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
      await page.getByRole("button", { name: "Playback", exact: true }).click();
      await expect(
        page.getByRole("menuitem", { name: "Quality", exact: true }),
      ).toBeVisible();
      await page.keyboard.press("Escape");
      await page
        .getByRole("button", { name: "Navigation", exact: true })
        .click();
      await expect(
        page.getByRole("link", { name: "Scenes", exact: true }),
      ).toBeVisible();
    });
  });
}
