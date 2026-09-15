import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test("a quality change loads at the marker playhead without narrowing its range", async ({
  page,
}) => {
  await serveSceneMedia(page);
  const fragments: number[] = [];
  await page.route("**/media/hls/segment-*.m4s", async (route) => {
    const match = /segment-(\d+)\.m4s/.exec(route.request().url());
    if (!match) throw new Error("Missing fragment index");
    fragments.push(Number(match[1]));
    await route.fallback();
  });
  await page.goto("/tv-fixture/tv?paused&markers&long-marker");
  const video = page.locator("video");
  const slider = page.getByRole("slider", { name: "Playback position" });
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  const bounds = await slider.boundingBox();
  if (!bounds) throw new Error("Missing scrubber");
  await page.touchscreen.tap(
    bounds.x + bounds.width * 0.8,
    bounds.y + bounds.height / 2,
  );
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(8);
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await page.getByRole("menuitem", { name: "Quality", exact: true }).click();
  await page.getByRole("combobox", { name: "Current item quality" }).click();
  await page.getByRole("option", { name: "HLS (240p)", exact: true }).click();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  expect(fragments[0]).toBe(4);
  await expect(slider).toHaveAttribute("aria-valuemax", "8");
  await expect(video).toHaveJSProperty("paused", true);
  // The marker still starts at scene time 2, even though loading began at 8.
  await page.touchscreen.tap(
    bounds.x + bounds.width * 0.1,
    bounds.y + bounds.height / 2,
  );
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeCloseTo(0.8, 0);
});

for (const marker of [false, true]) {
  test(`a stalled TV ${marker ? "marker" : "scene"} reloads at the playhead and resumes`, async ({
    page,
  }) => {
    test.setTimeout(45000);
    await serveSceneMedia(page);
    let recovering = false;
    let release: (() => void) | undefined;
    const recoveredFragments: number[] = [];
    await page.route("**/scene/*/stream.master.m3u8**", async (route) => {
      if (new URL(route.request().url()).searchParams.has("_r")) {
        recovering = true;
        release?.();
      }
      await route.fallback();
    });
    await page.route("**/media/hls/segment-*.m4s", async (route) => {
      const match = /segment-(\d+)\.m4s/.exec(route.request().url());
      if (!match) throw new Error("Missing fragment index");
      const index = Number(match[1]);
      if (recovering) recoveredFragments.push(index);
      else if (index === 3)
        await new Promise<void>((resolve) => {
          release = resolve;
        });
      await route.fallback();
    });
    try {
      await page.goto(
        `/tv-fixture/tv?low${marker ? "&markers&long-marker" : ""}`,
      );
      const video = page.locator("video");
      await expect(video).toHaveJSProperty("paused", false);
      // Withhold the next real fragment until the existing stall watchdog
      // reloads. Recovery must request the current region, not clip segment 0.
      await expect.poll(() => recovering, { timeout: 25000 }).toBe(true);
      await expect
        .poll(() => recoveredFragments.length, { timeout: 15000 })
        .toBeGreaterThan(0);
      expect(recoveredFragments[0]).toBeGreaterThanOrEqual(2);
      await expect
        .poll(() =>
          video.evaluate(
            (v: HTMLVideoElement, offset) =>
              !v.paused && v.currentTime + offset > 6.5,
            marker ? 2 : 0,
          ),
        )
        .toBe(true);
      await expect(
        page.getByRole("slider", { name: "Playback position" }),
      ).toHaveAttribute("aria-valuemax", marker ? "8" : "12");
    } finally {
      release?.();
    }
  });
}
