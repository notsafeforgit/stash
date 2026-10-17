import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";
import { observeVideoFrames } from "./video-frames";

for (const mode of ["detail", "lightbox", "TV marker"] as const) {
  test(`${mode} reuses silent remote preview frames and commits once while paused`, async ({
    page,
  }) => {
    await observeVideoFrames(page);
    await serveSceneMedia(page);
    const manifests: string[] = [];
    page.on("request", (request) => {
      if (new URL(request.url()).pathname.endsWith("/stream.scrub.master.m3u8"))
        manifests.push(request.url());
    });
    await page.goto(
      mode === "detail"
        ? "/scene-detail?landscape&short&hls"
        : mode === "lightbox"
          ? "/scene-lightbox?paused"
          : "/tv-fixture/tv?paused&markers",
    );
    if (mode === "lightbox")
      await page.getByRole("button", { name: "Open scenes" }).click();
    const player = page.locator("[data-scene-player]");
    const main = player.locator("[data-playback-video]");
    if (mode !== "TV marker")
      await player.locator("[data-player-native-button]").click();
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await main.evaluate((v: HTMLVideoElement) => v.pause());
    const originalTime = await main.evaluate(
      (v: HTMLVideoElement) => v.currentTime,
    );
    const scrubber = player.getByRole("slider", { name: "Playback position" });
    await player.hover();
    await scrubber.focus();
    const origin = mode === "TV marker" ? 6 : 0;
    const duration = Number(await scrubber.getAttribute("aria-valuemax"));
    await main.evaluate((v: HTMLVideoElement) => {
      Object.defineProperty(v, "buffered", {
        configurable: true,
        get: (): TimeRanges => ({ length: 0, start: () => 0, end: () => 0 }),
      });
    });
    const bounds = await scrubber.boundingBox();
    if (!bounds) throw new Error("Missing scrubber");
    const point = (ratio: number) => ({
      x: bounds.x + ratio * bounds.width,
      y: bounds.y + bounds.height / 2,
    });
    await page.mouse.move(point(0.2).x, point(0.2).y);
    await page.mouse.down();
    const preview = player.locator("[data-scrub-preview]");
    for (const ratio of [0.75, 0.25, 0.7]) {
      await page.mouse.move(point(ratio).x, point(ratio).y, { steps: 3 });
      await expect(player.locator("[data-scrub-surface]")).toHaveAttribute(
        "data-preview-ready",
        "true",
      );
      await expect
        .poll(async () => Number(await preview.getAttribute("data-scrub-time")))
        .toBeCloseTo(origin + duration * ratio, 1);
      await expect
        .poll(async () =>
          Number(await preview.getAttribute("data-decoded-time")),
        )
        .toBeCloseTo(origin + duration * ratio, 1);
      await expect(main).toHaveJSProperty("paused", true);
      expect(
        await main.evaluate((v: HTMLVideoElement) => v.currentTime),
      ).toBeCloseTo(originalTime, 3);
      await expect(preview).toHaveJSProperty("muted", true);
      await expect(preview).toHaveJSProperty("paused", true);
    }
    expect(manifests).toHaveLength(1);
    const captured = await page.evaluateHandle(() => {
      const frames: { preview: boolean; time: number }[] = [];
      const draw = CanvasRenderingContext2D.prototype.drawImage;
      CanvasRenderingContext2D.prototype.drawImage = function (
        ...args: [CanvasImageSource, ...number[]]
      ) {
        const source = args[0];
        if (source instanceof HTMLVideoElement)
          frames.push({
            preview: source.hasAttribute("data-scrub-preview"),
            time: source.currentTime,
          });
        return Reflect.apply(draw, this, args);
      };
      return frames;
    });
    // Detail commits with an unbuffered main target to exercise the transition
    // overlay. The other surfaces commit inside their existing main buffers.
    if (mode !== "detail")
      await main.evaluate((v) => {
        Reflect.deleteProperty(v, "buffered");
      });
    await page.mouse.up();
    await main.evaluate((v) => {
      Reflect.deleteProperty(v, "buffered");
    });
    if (mode === "detail") {
      const frames = await captured.evaluate((value) => value);
      expect(
        frames.some(
          (frame) =>
            frame.preview && Math.abs(frame.time - duration * 0.7) < 0.05,
        ),
      ).toBe(true);
    }
    await expect(player.locator("[data-scrub-surface]")).toHaveCSS(
      "opacity",
      "0",
    );
    await expect(main).toHaveJSProperty("paused", true);
    await expect
      .poll(() => main.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeCloseTo(origin + duration * 0.7, 1);
  });
}
