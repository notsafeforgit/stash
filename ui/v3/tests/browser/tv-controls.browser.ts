import type { Page } from "@playwright/test";
import { test, expect, expectTouchTargets } from "./test";
import { serveSceneMedia } from "./scene-media";

async function expectDockPaddingInert(page: Page, touch: boolean) {
  const video = page.locator("video");
  const paused = await video.evaluate(
    (video: HTMLVideoElement) => video.paused,
  );
  const events = await video.evaluateHandle((video: HTMLVideoElement) => {
    const events: string[] = [];
    for (const type of ["play", "pause", "ratechange"])
      video.addEventListener(type, () => events.push(type));
    return events;
  });
  const points = await page.locator("[data-tv-dock]").evaluate((dock) => {
    const bounds = dock.getBoundingClientRect();
    const rotation = dock
      .closest("[data-tv]")
      ?.getAttribute("data-tv-rotation");
    const width = dock.clientWidth;
    const height = dock.clientHeight;
    // Side gutters beside the scrubber, the gap above it, and bottom padding
    // both below Navigation and in the middle of the safe area.
    return [
      { x: 2, y: height / 2 },
      { x: width - 2, y: height / 2 },
      { x: width / 2, y: 1 },
      { x: 30, y: height - 2 },
      { x: width / 2, y: height - 2 },
    ].map(({ x, y }) =>
      rotation === "clockwise"
        ? { x: bounds.right - y, y: bounds.top + x }
        : rotation === "counterclockwise"
          ? { x: bounds.left + y, y: bounds.bottom - x }
          : { x: bounds.left + x, y: bounds.top + y },
    );
  });
  for (const point of points) {
    if (touch) await page.touchscreen.tap(point.x, point.y);
    else await page.mouse.click(point.x, point.y);
    // Include delayed single-tap recognition, and detect even a brief toggle.
    await page.waitForTimeout(350);
    expect(await events.jsonValue()).toEqual([]);
    await expect(video).toHaveJSProperty("paused", paused);
  }
  await events.dispose();
}

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

    test("portrait controls stay transparent and their padding cannot toggle playback", async ({
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
      await expectDockPaddingInert(page, viewport.touch);
      const point = { x: viewport.width / 2, y: viewport.height / 2 };
      if (viewport.touch) await page.touchscreen.tap(point.x, point.y);
      else await page.mouse.click(point.x, point.y);
      await expect(video).toHaveJSProperty("paused", false);
      await expect
        .poll(() =>
          video.evaluate((element: HTMLVideoElement) => element.currentTime),
        )
        .toBeGreaterThan(0.2);
      await expectDockPaddingInert(page, viewport.touch);

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

      // Hiding the UI must not turn its gaps or safe-area padding into play/pause.
      await expectDockPaddingInert(page, viewport.touch);
      const gap = {
        x: viewport.width / 2,
        y: muteBounds.y + muteBounds.height / 2,
      };
      if (viewport.touch) await page.touchscreen.tap(gap.x, gap.y);
      else await page.mouse.click(gap.x, gap.y);
      await page.waitForTimeout(350);
      await expect(video).toHaveJSProperty("paused", false);

      // WebKit can expand the nearby eye button's touch target into a side
      // gutter. That may reveal controls, but must never toggle playback.
      const restore = page.getByRole("button", {
        name: "Show TV controls",
        exact: true,
      });
      if (await restore.isVisible()) await restore.click();
      await expect(dock.getByRole("slider")).toBeVisible();

      // The video above the dock still accepts taps and navigation gestures.
      if (viewport.touch) await page.touchscreen.tap(point.x, point.y);
      else await page.mouse.click(point.x, point.y);
      await expect(video).toHaveJSProperty("paused", true);
      // Keep two single taps distinct from the double-tap zoom gesture.
      await page.waitForTimeout(350);
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

for (const rotation of ["clockwise", "counterclockwise"]) {
  test(`rotating ${rotation} keeps the dock padding outside playback gestures`, async ({
    page,
  }) => {
    await serveSceneMedia(page, "portrait");
    await page.goto("/tv-fixture/tv?paused&portrait");
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
    await page
      .locator("[data-tv]")
      .evaluate((element) =>
        element.style.setProperty("--safe-area-bottom", "34px"),
      );
    await page.keyboard.press("o");
    if (rotation === "counterclockwise") {
      // The shortcut toggles normal/clockwise. Exercise the other persisted
      // presentation direction through the existing rotation preference.
      await page.evaluate(() => {
        const key = Object.keys(localStorage).find((key) =>
          key.startsWith("stash:tv:rotation:v1:"),
        );
        if (!key) throw new Error("Missing TV rotation preference");
        const newValue = JSON.stringify("counterclockwise");
        localStorage.setItem(key, newValue);
        window.dispatchEvent(new StorageEvent("storage", { key, newValue }));
      });
    }
    await expect(page.locator("[data-tv]")).toHaveAttribute(
      "data-tv-rotation",
      rotation,
    );
    await expectDockPaddingInert(page, true);
    await page
      .getByRole("button", { name: "Show or hide controls", exact: true })
      .click();
    await expectDockPaddingInert(page, true);
    await page.touchscreen.tap(195, 422);
    await expect(page.locator("video")).toHaveJSProperty("paused", false);
  });
}
