import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const hls of [false, true]) {
  for (const completion of ["commit", "cancel"] as const) {
    test(`scene detail ${hls ? "HLS" : "direct"} drag ${completion} restores playback without reloading`, async ({
      page,
    }) => {
      await serveSceneMedia(page);
      await page.goto(`/scene-detail?landscape&short${hls ? "&hls" : ""}`);
      const video = page.locator("video");
      const player = page.locator("[data-scene-player]");
      await player.locator("[data-player-native-button]").click();
      await expect(player).toHaveAttribute("data-playback-ready", "true");
      await expect
        .poll(() =>
          video.evaluate((v: HTMLVideoElement) =>
            v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0,
          ),
        )
        .toBeGreaterThan(10);
      const observation = await video.evaluateHandle((v: HTMLVideoElement) => {
        const state = { frame: -1, events: [] as string[] };
        const observe = (
          _now: number,
          metadata: VideoFrameCallbackMetadata,
        ) => {
          state.frame = metadata.mediaTime;
          v.requestVideoFrameCallback(observe);
        };
        v.requestVideoFrameCallback(observe);
        for (const event of [
          "pause",
          "play",
          "emptied",
          "loadstart",
          "ratechange",
        ])
          v.addEventListener(event, () => state.events.push(event));
        return state;
      });
      const scrubber = page.getByRole("slider", { name: "Playback position" });
      // Focus exposes controls after asynchronous loading without a reveal tap.
      await player.hover();
      await scrubber.focus();
      const bounds = await scrubber.boundingBox();
      if (!bounds) throw new Error("Missing scrubber");
      const y = bounds.y + bounds.height / 2;
      await page.mouse.move(bounds.x + bounds.width / 3, y);
      await page.mouse.down();
      await page.mouse.move(bounds.x + (bounds.width * 2) / 3, y, { steps: 4 });
      await expect(video).toHaveJSProperty("paused", true);
      await expect
        .poll(() => observation.evaluate((state) => state.frame))
        .toBeCloseTo(8, 1);
      await page.waitForTimeout(300);
      expect(
        await video.evaluate((v: HTMLVideoElement) => v.currentTime),
      ).toBeCloseTo(8, 1);
      if (completion === "cancel") {
        await scrubber.dispatchEvent("pointercancel", { pointerId: 1 });
        await expect(video).toHaveJSProperty("paused", false);
        expect(
          await video.evaluate((v: HTMLVideoElement) => v.currentTime),
        ).toBeLessThan(4);
      }
      await page.mouse.up();
      await expect(video).toHaveJSProperty("paused", false);
      if (completion === "commit") {
        await expect
          .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
          .toBeGreaterThan(8.2);
      }
      expect(await observation.evaluate((state) => state.events)).toEqual([
        "pause",
        "play",
      ]);
    });
  }

  test(`scene detail ${hls ? "HLS" : "direct"} pause keeps the frame and resumes at normal speed`, async ({
    page,
  }) => {
    await serveSceneMedia(page);
    await page.goto(`/scene-detail?landscape&short${hls ? "&hls" : ""}`);
    const video = page.locator("video");
    const player = page.locator("[data-scene-player]");
    await player.locator("[data-player-native-button]").click();
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeGreaterThan(0.5);
    await player.hover();
    const bar = player.locator("[data-player-control-bar]");
    await bar.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(video).toHaveJSProperty("paused", true);
    const pausedAt = await video.evaluate(
      (v: HTMLVideoElement) => v.currentTime,
    );
    await page.waitForTimeout(4000);
    expect(
      await video.evaluate((v: HTMLVideoElement) => v.currentTime),
    ).toBeCloseTo(pausedAt, 1);
    const frame = await video.evaluateHandle((v: HTMLVideoElement) => {
      const state = { time: -1 };
      v.requestVideoFrameCallback((_now, metadata) => {
        state.time = metadata.mediaTime;
      });
      return state;
    });
    await bar.getByRole("button", { name: "Play", exact: true }).click();
    await expect
      .poll(() => frame.evaluate((state) => state.time))
      .toBeGreaterThanOrEqual(0);
    expect(await frame.evaluate((state) => state.time)).toBeLessThan(
      pausedAt + 0.15,
    );
    await page.waitForTimeout(1000);
    const resumedAt = await video.evaluate(
      (v: HTMLVideoElement) => v.currentTime,
    );
    expect(resumedAt).toBeGreaterThan(pausedAt + 0.5);
    expect(resumedAt).toBeLessThan(pausedAt + 1.7);
    await expect(video).toHaveJSProperty("playbackRate", 1);
  });
}
