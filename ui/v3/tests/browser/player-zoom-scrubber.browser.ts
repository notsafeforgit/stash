import type { Locator } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

import { dragInput, type Point } from "./scrubber-input";

const scale = (frame: Locator) =>
  frame.evaluate(
    (element) => new DOMMatrixReadOnly(getComputedStyle(element).transform).a,
  );

for (const touch of [true, false]) {
  test.describe(touch ? "touch zoomed seeking" : "mouse zoomed seeking", () => {
    test.use({
      isMobile: touch,
      hasTouch: touch,
      viewport: touch
        ? { width: 390, height: 844 }
        : { width: 1280, height: 800 },
    });
    for (const mode of [
      "TV scene",
      "TV marker",
      "lightbox scene",
      "lightbox marker",
      "scene detail",
    ]) {
      test(`${mode} keeps the drag on the scrubber without panning`, async ({
        page,
        context,
        browserName,
      }) => {
        await serveSceneMedia(page);
        const tv = mode.startsWith("TV");
        const detail = mode === "scene detail";
        const marker = mode.endsWith("marker");
        await page.goto(
          tv
            ? `/tv-fixture/tv?paused${marker ? "&markers" : ""}`
            : detail
              ? "/scene-detail?landscape"
              : `/scene-lightbox?paused${marker ? "&mode=markers" : ""}`,
        );
        if (!tv && !detail)
          await page.getByRole("button", { name: "Open scenes" }).click();
        const player = page.locator("[data-scene-player]");
        const video = player.locator("video");
        if (detail) {
          await player.locator("[data-player-native-button]").click();
          if (touch)
            await player
              .getByRole("button", { name: "Open scene viewer", exact: true })
              .click();
        }
        await expect(player).toHaveAttribute("data-playback-ready", "true");
        // Start the pre-play lightbox before zooming. Its initial play overlay
        // otherwise handles the first tap of the zoom gesture as a play click.
        if (!tv && !detail)
          await player.locator("[data-player-native-button]").click();
        await video.evaluate((element: HTMLVideoElement) => element.pause());
        await expect(video).toHaveJSProperty("paused", true);
        const bounds = await player.boundingBox();
        if (!bounds) throw new Error("Missing player");
        const point = {
          x: bounds.x + bounds.width / 2,
          y: bounds.y + bounds.height / 3,
        };
        if (touch) {
          await page.touchscreen.tap(point.x, point.y);
          await page.touchscreen.tap(point.x, point.y);
        } else {
          await page.mouse.move(point.x, point.y);
          await page.keyboard.down("Control");
          await page.mouse.wheel(0, -92);
          await page.keyboard.up("Control");
        }
        const frame = player.locator("[data-video-frame-zoom] > div");
        await expect.poll(() => scale(frame)).toBeCloseTo(2.5, 1);
        const transform = await frame.evaluate(
          (element) => element.style.transform,
        );
        const scrubber = player.getByRole("slider", {
          name: "Playback position",
        });
        await player.hover();
        await scrubber.focus();
        await expect(video).toHaveJSProperty("paused", true);
        await expect(scrubber).toHaveAttribute("aria-disabled", "false");
        const duration = Number(await scrubber.getAttribute("aria-valuemax"));
        const origin =
          (await video.evaluate((v: HTMLVideoElement) => v.currentTime)) -
          Number(await scrubber.getAttribute("aria-valuenow"));
        const bar = await scrubber.boundingBox();
        if (!bar) throw new Error("Missing scrubber");
        // Hit the thin bar's child, not only the surrounding slider element.
        const position = (fraction: number): Point => ({
          x: bar.x + bar.width * fraction,
          y: bar.y + bar.height - 2,
        });
        const drag = await dragInput(
          page,
          context,
          touch && browserName === "chromium",
          touch,
          position(0.2),
        );
        try {
          for (const fraction of [0.4, 0.75, 0.5]) {
            await drag.move(position(fraction));
            await expect
              .poll(async () =>
                Number(await scrubber.getAttribute("aria-valuenow")),
              )
              .toBeCloseTo(duration * fraction, 1);
            expect(
              await frame.evaluate((element) => element.style.transform),
            ).toBe(transform);
          }
        } finally {
          await drag.end();
        }
        await expect
          .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
          .toBeCloseTo(origin + duration / 2, 1);
        await expect(video).toHaveJSProperty("paused", true);
        await expect(scrubber).not.toHaveAttribute("data-dragging");
        expect(await frame.evaluate((element) => element.style.transform)).toBe(
          transform,
        );
      });
    }
  });
}
