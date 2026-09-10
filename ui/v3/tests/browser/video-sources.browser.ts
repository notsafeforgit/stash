import { test, expect } from "./test";

test("direct, HLS, quality and seek reloads retain the authorized video element", async ({
  page,
}) => {
  await page.goto("/video-sources");
  const video = page.locator("video");
  await expect(video).toHaveJSProperty("muted", true);
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video element");
  await expect
    .poll(() =>
      video.evaluate((element: HTMLVideoElement) => element.readyState),
    )
    .toBeGreaterThanOrEqual(3);
  await page.getByRole("button", { name: "Toggle sound" }).click();
  await expect(video).toHaveJSProperty("paused", false);

  for (const [button, engine, minimumTime] of [
    ["HLS at 4", "hlsjs", 4],
    ["HLS at 6", "hlsjs", 6],
    ["Marker clip", "hlsjs", 0],
    ["Reload HLS at zero", "hlsjs", 0],
    ["Direct file", "native", 0],
  ] as const) {
    const loads = Number(await page.getByTestId("loads").textContent());
    await page.getByRole("button", { name: button, exact: true }).click();
    await expect
      .poll(async () => Number(await page.getByTestId("loads").textContent()))
      .toBeGreaterThan(loads);
    await expect(page.getByTestId("playback-state")).toHaveAttribute(
      "data-engine",
      engine,
    );
    await expect(video).toHaveJSProperty("paused", false);
    await expect
      .poll(() =>
        video.evaluate((element: HTMLVideoElement) => element.currentTime),
      )
      .toBeGreaterThan(minimumTime + 0.1);
    await expect(video).toHaveJSProperty("muted", false);
    expect(
      await video.evaluate(
        (element, previous) => element === previous,
        original,
      ),
    ).toBe(true);
    // Reloads at zero must actually restart, not just leave the old source
    // playing and satisfy the lower-bound check with its old timestamp.
    if (minimumTime === 0) {
      expect(
        await video.evaluate(
          (element: HTMLVideoElement) => element.currentTime,
        ),
      ).toBeLessThan(4);
    }
  }
  await page.getByRole("button", { name: "Toggle sound" }).click();
  await page.getByRole("button", { name: "HLS at 4", exact: true }).click();
  await expect(page.getByTestId("playback-state")).toHaveAttribute(
    "data-engine",
    "hlsjs",
  );
  await expect(video).toHaveJSProperty("paused", false);
  await expect(video).toHaveJSProperty("muted", true);
  await page.getByRole("button", { name: "Unmount", exact: true }).click();
  await expect(video).toHaveCount(0);
});

test("ended-driven direct/HLS changes keep audio and the same element across several videos", async ({
  page,
}) => {
  await page.goto("/video-sources?short");
  const video = page.locator("video");
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video element");
  await expect
    .poll(() =>
      video.evaluate((element: HTMLVideoElement) => element.readyState),
    )
    .toBeGreaterThanOrEqual(3);
  await page.getByRole("button", { name: "Toggle sound" }).click();
  await expect(video).toHaveJSProperty("paused", false);
  await page.getByRole("button", { name: "Enable auto advance" }).click();
  for (let sequence = 1; sequence <= 4; sequence += 1) {
    // Let the short clips finish naturally: this exercises real autoplay
    // without making its outcome depend on a seek right up to native EOF.
    await expect(page.getByTestId("sequence")).toHaveText(String(sequence), {
      timeout: 8000,
    });
    await expect(video).toHaveJSProperty("paused", false);
    await expect(video).toHaveJSProperty("muted", false);
    expect(
      await video.evaluate(
        (element, previous) => element === previous,
        original,
      ),
    ).toBe(true);
    await expect
      .poll(() =>
        video.evaluate((element: HTMLVideoElement) => element.currentTime),
      )
      .toBeLessThan(1.5);
  }
});
