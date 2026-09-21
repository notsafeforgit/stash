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
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
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
    await page.clock.pauseAt(new Date("2026-01-01T00:01:00Z"));
    const drag = await dragInput(
      page,
      context,
      browserName === "chromium",
      true,
      point(0.5),
    );
    const clock = player.locator("[data-player-time-display]");
    const displayTime = (time: number) =>
      `${Math.floor(time / 60)}:${String(Math.floor(time) % 60).padStart(2, "0")}`;
    try {
      // The draft reaches both readouts before any preview seek runs.
      await expect(clock).toHaveText(
        new RegExp(
          `^${displayTime(duration / 2)}\\s*/\\s*${displayTime(duration)}$`,
        ),
      );
      await page.clock.runFor(650);
      await expect(scrubber).toHaveAttribute("data-precision", "true");
      await expect(video).toHaveJSProperty("paused", true);
      const initialSpan = Math.max(1, Math.min(60, duration / 4));
      const readout = scrubber.locator("[data-position-scrubber-precision]");
      const label = await readout.boundingBox();
      const thumb = await scrubber
        .locator("[data-position-scrubber-thumb]")
        .boundingBox();
      if (!label || !thumb) throw new Error("Missing precision readout");
      // Keep the time and zoom clear of the thumb, including rotated TV.
      expect(
        rotated
          ? label.x - (thumb.x + thumb.width / 2)
          : thumb.y + thumb.height / 2 - (label.y + label.height),
      ).toBeGreaterThanOrEqual(60);
      // Small, slow seeking movements must keep the initial zoom level. Use
      // explicit gesture time so CI input latency cannot become another dwell.
      for (let step = 1; step <= 4; step++) {
        await page.clock.runFor(300);
        const ratio = 0.5 + (step * 3) / (rotated ? bar.height : bar.width);
        await drag.move(point(ratio));
        await expect(readout).toContainText(
          new RegExp(` · ${Math.round(duration / initialSpan)}×$`),
        );
      }
      await drag.move(point(0.5));
      // A deliberate fresh pause reaches the one-second window in these clips.
      await page.clock.runFor(1200);
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
      await expect(clock).toHaveText(
        new RegExp(
          `^${displayTime(duration / 2 + 0.25)}\\s*/\\s*${displayTime(duration)}$`,
        ),
      );
      await expect(readout).toContainText("Fine seeking");
    } finally {
      await drag.end();
      await page.clock.resume();
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

for (const condition of ["buffer changes", "preview still seeking"] as const) {
  test(`precision HLS release retains its buffered preview when ${condition}`, async ({
    page,
    context,
    browserName,
  }) => {
    // Exercise the application's iOS reload policy on both browser engines.
    // Native Apple buffering is represented by the changing range snapshot.
    await page.addInitScript(() => {
      Object.defineProperty(navigator, "userAgent", {
        value: `${navigator.userAgent} iPhone`,
      });
    });
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await serveSceneMedia(page);
    await page.goto("/scene-detail?landscape&short&hls");
    const player = page.locator("[data-scene-player]");
    const video = player.locator("video");
    await player.locator("[data-player-native-button]").click();
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await expect
      .poll(() =>
        video.evaluate((v: HTMLVideoElement) =>
          v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0,
        ),
      )
      .toBeGreaterThan(10);
    await video.evaluate((v: HTMLVideoElement) => v.pause());
    const observation = await video.evaluateHandle((v: HTMLVideoElement) => {
      const state = { seeks: [] as number[], loads: [] as string[] };
      const time = Object.getOwnPropertyDescriptor(
        HTMLMediaElement.prototype,
        "currentTime",
      );
      if (!time?.get || !time.set) throw new Error("Missing media time");
      Object.defineProperty(v, "currentTime", {
        configurable: true,
        get: () => time.get?.call(v),
        set: (value: number) => {
          state.seeks.push(value);
          time.set?.call(v, value);
        },
      });
      for (const event of ["emptied", "loadstart"])
        v.addEventListener(event, () => state.loads.push(event));
      return state;
    });
    const scrubber = player.getByRole("slider", { name: "Playback position" });
    await player.hover();
    await scrubber.focus();
    const bounds = await scrubber.boundingBox();
    if (!bounds) throw new Error("Missing scrubber");
    await page.clock.pauseAt(new Date("2026-01-01T00:01:00Z"));
    const drag = await dragInput(
      page,
      context,
      browserName === "chromium",
      true,
      { x: bounds.x + bounds.width / 2, y: bounds.y + bounds.height - 2 },
    );
    try {
      await page.clock.runFor(700);
      await expect(scrubber).toHaveAttribute("data-precision", "true");
      await expect(video).toHaveJSProperty("seeking", false);
      await expect
        .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
        .toBeCloseTo(6, 2);
      if (condition === "buffer changes") {
        // MMS can change ranges during a long held gesture. A preview's
        // native seeking event must not become an independent seek command.
        await video.evaluate((v: HTMLVideoElement) => {
          Object.defineProperty(v, "buffered", {
            configurable: true,
            get: (): TimeRanges => ({
              length: 1,
              start: () => 8,
              end: () => 12,
            }),
          });
        });
        await page.clock.runFor(300);
        await video.evaluate((v: HTMLVideoElement) => {
          Reflect.deleteProperty(v, "buffered");
          v.dispatchEvent(new Event("progress"));
        });
      } else {
        // Delay the completion signal while retaining the preview's native
        // target, as when the last frame is still being decoded on release.
        await video.evaluate((v: HTMLVideoElement) => {
          Object.defineProperty(v, "seeking", {
            configurable: true,
            get: () => true,
          });
        });
      }
    } finally {
      await drag.end();
      await video.evaluate((v: HTMLVideoElement) => {
        Reflect.deleteProperty(v, "buffered");
        Reflect.deleteProperty(v, "seeking");
        v.dispatchEvent(new Event("seeked"));
      });
      await page.clock.resume();
    }
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await expect(video).toHaveJSProperty("paused", true);
    await expect(scrubber).not.toHaveAttribute("data-precision");
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeCloseTo(6, 2);
    expect(await observation.evaluate((state) => state.loads)).toEqual([]);
    expect(await observation.evaluate((state) => state.seeks)).toHaveLength(1);
  });
}
