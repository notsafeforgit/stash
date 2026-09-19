import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";
import { dragInput } from "./scrubber-input";

for (const mode of [
  "scene detail",
  "scene viewer",
  "lightbox scene",
  "lightbox marker",
  "TV scene",
  "TV marker",
  "TV rotated",
]) {
  test(`${mode} holds to magnify the timeline and commits a precise touch seek`, async ({
    page,
    context,
    browserName,
  }, testInfo) => {
    await serveSceneMedia(page);
    const tv = mode.startsWith("TV");
    const lightbox = mode.startsWith("lightbox");
    const marker = mode.endsWith("marker");
    const rotated = mode === "TV rotated";
    await page.goto(
      tv
        ? `/tv-fixture/tv?paused${marker ? "&markers" : ""}`
        : lightbox
          ? `/scene-lightbox?paused${marker ? "&mode=markers" : ""}`
          : "/scene-detail?landscape&short",
    );
    if (lightbox)
      await page.getByRole("button", { name: "Open scenes" }).click();
    const player = page.locator("[data-scene-player]");
    const video = player.locator("video");
    if (!tv) await player.locator("[data-player-native-button]").click();
    if (mode === "scene viewer")
      await player
        .getByRole("button", { name: "Open scene viewer", exact: true })
        .click();
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await video.evaluate((element: HTMLVideoElement) => element.pause());
    if (rotated) {
      await page.keyboard.press("o");
      await expect(page.locator("[data-tv]")).toHaveAttribute(
        "data-tv-rotation",
        "clockwise",
      );
    }
    const scrubber = player.getByRole("slider", { name: "Playback position" });
    await player.hover();
    await scrubber.focus();
    await expect(scrubber).toHaveAttribute("aria-disabled", "false");
    const duration = Number(await scrubber.getAttribute("aria-valuemax"));
    const origin =
      (await video.evaluate((v: HTMLVideoElement) => v.currentTime)) -
      Number(await scrubber.getAttribute("aria-valuenow"));
    const bar = await scrubber.boundingBox();
    if (!bar) throw new Error("Missing scrubber");
    const point = (ratio: number) =>
      rotated
        ? { x: bar.x + 2, y: bar.y + bar.height * ratio }
        : { x: bar.x + bar.width * ratio, y: bar.y + bar.height - 2 };
    const drag = await dragInput(
      page,
      context,
      browserName === "chromium",
      true,
      point(0.5),
    );
    try {
      await expect(scrubber).toHaveAttribute("data-precision", "true");
      await expect(video).toHaveJSProperty("paused", true);
      // Let progressive dwelling reach its one-second window in these clips.
      await page.waitForTimeout(1500);
      expect(Number(await scrubber.getAttribute("aria-valuenow"))).toBeCloseTo(
        duration / 2,
        1,
      );
      await page.screenshot({
        path: testInfo.outputPath("precision-seeking.png"),
      });
      await drag.move(point(0.75));
      await expect
        .poll(async () => Number(await scrubber.getAttribute("aria-valuenow")))
        .toBeCloseTo(duration / 2 + 0.25, 1);
      await expect(
        scrubber.locator("[data-position-scrubber-precision]"),
      ).toContainText("Fine seeking");
    } finally {
      await drag.end();
    }
    await expect(scrubber).not.toHaveAttribute("data-precision");
    await expect(scrubber).not.toHaveAttribute("data-dragging");
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeCloseTo(origin + duration / 2 + 0.25, 1);
    await expect(video).toHaveJSProperty("paused", true);
    // A subsequent tap gets the full scene/marker scale back immediately.
    const next = point(0.25);
    await page.touchscreen.tap(next.x, next.y);
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeCloseTo(origin + duration / 4, 1);
  });
}
