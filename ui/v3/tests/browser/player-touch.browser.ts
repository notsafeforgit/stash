import type { Locator, Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

async function openLightbox(page: Page) {
  await serveSceneMedia(page);
  await page.goto("/scene-lightbox");
  await page.getByRole("button", { name: "Open scenes" }).tap();
  const player = page.locator("[data-scene-player]");
  const video = player.locator("video");
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  await expect(video).toHaveJSProperty("paused", false);
  await expect(video).toHaveJSProperty("seeking", false);
  const controls = player.locator("[data-player-touch-controls]");
  const surface = player.locator("[data-player-touch-surface]");
  const transform = player.locator("[data-video-frame-zoom] > div");
  return { player, video, controls, surface, transform };
}

async function center(element: Locator) {
  const bounds = await element.boundingBox();
  if (!bounds) throw new Error("Missing touch target");
  return { x: bounds.x + bounds.width / 2, y: bounds.y + bounds.height / 2 };
}

async function doubleTap(page: Page, point: { x: number; y: number }) {
  // Real touch sequences with no Playwright actionability wait between taps.
  await page.touchscreen.tap(point.x, point.y);
  await page.touchscreen.tap(point.x, point.y);
}

async function scale(transform: Locator) {
  return transform.evaluate(
    (element) => new DOMMatrixReadOnly(getComputedStyle(element).transform).a,
  );
}

test("hidden central and bottom controls only reveal on a single tap", async ({
  page,
}) => {
  const { player, video, controls, surface } = await openLightbox(page);
  await video.evaluate((v: HTMLVideoElement) => {
    v.muted = true;
  });
  const targets = [
    controls.locator("button").nth(1),
    controls.locator("button").nth(0),
    controls.locator("button").nth(2),
    player.locator("[data-player-playback-controls] button").first(),
  ];
  const events = await video.evaluateHandle((v: HTMLVideoElement) => {
    const counts = { pauses: 0, seeks: 0 };
    v.addEventListener("pause", () => counts.pauses++);
    v.addEventListener("seeking", () => counts.seeks++);
    return counts;
  });
  await expect(controls).toHaveAttribute("inert", "", { timeout: 10000 });
  for (const [index, target] of targets.entries()) {
    const point = await center(target);
    await page.touchscreen.tap(point.x, point.y);
    await expect(controls).not.toHaveAttribute("inert");
    await expect(video).toHaveJSProperty("paused", false);
    await expect(video).toHaveJSProperty("muted", true);
    expect(await events.jsonValue()).toEqual({ pauses: 0, seeks: 0 });
    if (index < targets.length - 1) {
      await surface.tap({ position: { x: 180, y: 250 } });
      await expect(controls).toHaveAttribute("inert", "");
    }
  }
});

for (const [index, label] of [
  "skip backward",
  "play/pause",
  "skip forward",
  "bottom playback",
].entries()) {
  test(`double tap over hidden ${label} control zooms without playback or visibility changes`, async ({
    page,
  }) => {
    const { player, video, controls, transform } = await openLightbox(page);
    const point = await center(
      index === 3
        ? player.locator("[data-player-playback-controls] button").first()
        : controls.locator("button").nth(index),
    );
    await expect(controls).toHaveAttribute("inert", "", { timeout: 10000 });
    const events = await video.evaluateHandle((v: HTMLVideoElement) => {
      const counts = { pauses: 0, seeks: 0 };
      v.addEventListener("pause", () => counts.pauses++);
      v.addEventListener("seeking", () => counts.seeks++);
      return counts;
    });
    await doubleTap(page, point);
    await expect.poll(() => scale(transform)).toBeCloseTo(2.5, 1);
    await expect(controls).toHaveAttribute("inert", "");
    await doubleTap(page, point);
    await expect.poll(() => scale(transform)).toBeCloseTo(1, 2);
    await expect(controls).toHaveAttribute("inert", "");
    expect(await events.jsonValue()).toEqual({ pauses: 0, seeks: 0 });
    await expect(video).toHaveJSProperty("paused", false);
  });
}

test("visible controls accept rapid pause taps and leave their gaps available for zoom", async ({
  page,
}) => {
  await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
  const { player, video, controls, surface, transform } =
    await openLightbox(page);
  // Start with a fresh visibility interval, then advance gesture time ourselves.
  // Protocol latency must not turn the visible-control case into an idle hide.
  await page.clock.pauseAt(new Date("2026-01-01T00:01:00Z"));
  await expect(controls).toHaveAttribute("inert", "");
  await surface.tap({ position: { x: 180, y: 250 } });
  await page.clock.runFor(350);
  await expect(controls).not.toHaveAttribute("inert");
  const pause = controls.getByRole("button", { name: "Pause", exact: true });
  const point = await center(pause);
  const events = await video.evaluateHandle((v: HTMLVideoElement) => {
    const counts = { pauses: 0, plays: 0, seeks: 0 };
    v.addEventListener("pause", () => counts.pauses++);
    v.addEventListener("play", () => counts.plays++);
    v.addEventListener("seeking", () => counts.seeks++);
    return counts;
  });
  // An even number of taps toggles every time and keeps playback running.
  await doubleTap(page, point);
  await expect
    .poll(() => events.jsonValue())
    .toEqual({ pauses: 1, plays: 1, seeks: 0 });
  await expect(video).toHaveJSProperty("paused", false);
  await expect.poll(() => scale(transform)).toBe(1);
  const bar = player.locator("[data-player-playback-controls]");
  const left = await controls.locator("button").nth(0).boundingBox();
  const right = await controls.locator("button").nth(1).boundingBox();
  const bottom = await bar.boundingBox();
  if (!left || !right || !bottom) throw new Error("Missing player controls");
  for (const gap of [
    {
      x: (left.x + left.width + right.x) / 2,
      y: right.y + right.height / 2,
    },
    // Bottom buttons fill the row with contiguous touch targets. Test the
    // padding next to them, rather than the boundary between two buttons.
    { x: bottom.x - 4, y: bottom.y + bottom.height / 2 },
  ]) {
    await doubleTap(page, gap);
    await page.clock.runFor(350);
    await expect.poll(() => scale(transform)).toBeCloseTo(2.5, 1);
    await expect(controls).not.toHaveAttribute("inert");
    await doubleTap(page, gap);
    await page.clock.runFor(350);
    await expect.poll(() => scale(transform)).toBeCloseTo(1, 2);
    await expect(controls).not.toHaveAttribute("inert");
  }
  expect(await events.jsonValue()).toEqual({ pauses: 1, plays: 1, seeks: 0 });
  await expect(video).toHaveJSProperty("paused", false);
  // Empty-space singles still dismiss and reveal without activating buttons.
  await surface.tap({ position: { x: 180, y: 250 } });
  await page.clock.runFor(350);
  await expect(controls).toHaveAttribute("inert", "");
});
