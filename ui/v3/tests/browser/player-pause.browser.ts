import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const hls of [false, true]) {
  test(`TV ${hls ? "HLS" : "direct"} restores its pause frame after a late renderer clock update`, async ({
    page,
  }) => {
    await serveSceneMedia(page);
    await page.goto(`/tv-fixture/tv?paused${hls ? "&low" : ""}`);
    const video = page.locator("video");
    const surface = page.locator("[data-tv-play-surface]");
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
    const frames = await video.evaluateHandle((v: HTMLVideoElement) => {
      const state = { times: [] as number[] };
      const observe = (_now: number, metadata: VideoFrameCallbackMetadata) => {
        state.times.push(metadata.mediaTime);
        v.requestVideoFrameCallback(observe);
      };
      v.requestVideoFrameCallback(observe);
      return state;
    });
    await surface.tap({ position: { x: 150, y: 250 } });
    await expect
      .poll(() => frames.evaluate((s) => s.times.at(-1) ?? 0))
      .toBeGreaterThan(0.5);
    await surface.tap({ position: { x: 150, y: 250 } });
    await expect(video).toHaveJSProperty("paused", true);
    await expect(video).toHaveJSProperty("seeking", false);
    await page.waitForTimeout(250);
    const last = await frames.evaluate((s) => s.times.at(-1));
    if (last === undefined) throw new Error("Missing paused frame");
    const count = await frames.evaluate((s) => s.times.length);
    await video.evaluate((v: HTMLVideoElement) => {
      const descriptor = Object.getOwnPropertyDescriptor(
        HTMLMediaElement.prototype,
        "currentTime",
      );
      if (!descriptor?.get || !descriptor.set)
        throw new Error("Missing media clock accessors");
      const read = () => {
        const value: unknown = descriptor.get?.call(v);
        if (typeof value !== "number") throw new Error("Invalid media clock");
        return value;
      };
      const play = v.play.bind(v);
      let lateClock = 0.5;
      // Model the recorded iOS failure: the renderer finishes pausing half a
      // second later, while the paused picture remains unchanged. Unless the
      // next Play repositions it, the renderer resumes from that newer clock.
      Object.defineProperty(v, "currentTime", {
        configurable: true,
        get: () => read() + lateClock,
        set: (value: number) => {
          lateClock = 0;
          descriptor.set?.call(v, value);
        },
      });
      v.play = () => {
        if (lateClock) {
          const target = read() + lateClock;
          lateClock = 0;
          descriptor.set?.call(v, target);
        }
        return play();
      };
      v.dispatchEvent(new Event("timeupdate"));
    });
    await page.waitForTimeout(250);
    await surface.tap({ position: { x: 150, y: 250 } });
    await expect(video).toHaveJSProperty("paused", false);
    await expect
      .poll(() => frames.evaluate((s) => s.times.length))
      .toBeGreaterThan(count + 4);
    const resumed = await frames.evaluate(
      (s, n) => s.times.slice(n, n + 5),
      count,
    );
    expect(resumed[0]).toBeGreaterThanOrEqual(last - 0.001);
    expect(resumed[0]).toBeLessThanOrEqual(last + 1 / 30 + 0.001);
  });

  test(`scene detail ${hls ? "HLS" : "direct"} resumes from the paused frame before and after a seek`, async ({
    page,
  }) => {
    await serveSceneMedia(page);
    await page.goto(`/scene-detail?landscape&short${hls ? "&hls" : ""}`);
    const video = page.locator("video");
    const frames = await video.evaluateHandle((v: HTMLVideoElement) => {
      const state = {
        frames: [] as { time: number; count: number }[],
        reloads: 0,
      };
      const observe = (_now: number, data: VideoFrameCallbackMetadata) => {
        state.frames.push({
          time: data.mediaTime,
          count: data.presentedFrames,
        });
        v.requestVideoFrameCallback(observe);
      };
      v.requestVideoFrameCallback(observe);
      v.addEventListener("emptied", () => state.reloads++);
      return state;
    });
    const player = page.locator("[data-scene-player]");
    const bar = player.locator("[data-player-control-bar]");
    await player.locator("[data-player-native-button]").click();
    await expect
      .poll(() => frames.evaluate((s) => s.frames.at(-1)?.time ?? 0))
      .toBeGreaterThan(0.5);
    const reloads = await frames.evaluate((s) => s.reloads);

    for (const seek of [false, true]) {
      if (seek) {
        await player.hover();
        const scrubber = page.getByRole("slider", {
          name: "Playback position",
        });
        await scrubber.focus();
        const bounds = await scrubber.boundingBox();
        if (!bounds) throw new Error("Missing scrubber");
        await page.mouse.click(
          bounds.x + bounds.width * 0.6,
          bounds.y + bounds.height / 2,
        );
        await expect
          .poll(() => frames.evaluate((s) => s.frames.at(-1)?.time ?? 0))
          .toBeGreaterThan(7);
      }
      for (const duration of [200, 2000]) {
        await player.hover();
        await bar.getByRole("button", { name: "Pause", exact: true }).click();
        await expect(video).toHaveJSProperty("paused", true);
        await expect(video).toHaveJSProperty("seeking", false);
        await page.waitForTimeout(duration);
        // Observe continuously across pause. Comparing to currentTime alone
        // misses frames skipped when the audio clock leads the visible image.
        const last = await frames.evaluate((s) => s.frames.at(-1));
        const count = await frames.evaluate((s) => s.frames.length);
        if (!last) throw new Error("Missing paused frame");
        const pausedAt = await video.evaluate(
          (v: HTMLVideoElement) => v.currentTime,
        );
        expect(pausedAt).toBeCloseTo(last.time, 3);
        await bar.getByRole("button", { name: "Play", exact: true }).click();
        await expect
          .poll(() => frames.evaluate((s) => s.frames.length))
          .toBeGreaterThan(count + 4);
        const resumed = await frames.evaluate(
          (s, n) => s.frames.slice(n, n + 5),
          count,
        );
        let previous = last;
        for (const next of resumed) {
          // 30fps fixture. presentedFrames distinguishes a missed JS callback
          // from the decoder actually skipping presentation timestamps.
          expect(next.time - previous.time).toBeGreaterThanOrEqual(-0.001);
          expect(next.time - previous.time).toBeLessThanOrEqual(
            (next.count - previous.count) / 30 + 0.001,
          );
          previous = next;
        }
        await expect(video).toHaveJSProperty("playbackRate", 1);
        await page.waitForTimeout(400);
        const resumedAt = await video.evaluate(
          (v: HTMLVideoElement) => v.currentTime,
        );
        expect(resumedAt - pausedAt).toBeGreaterThan(0.4);
        expect(resumedAt - pausedAt).toBeLessThan(1.4);
      }
    }
    // Rapid toggles can precede both the native pause event and React's commit.
    // All commands must apply, and a canceled play promise must not leak.
    await player.hover();
    await bar
      .getByRole("button", { name: "Pause", exact: true })
      .evaluate((button: HTMLElement) => {
        for (let index = 0; index < 5; index++) button.click();
      });
    await expect(video).toHaveJSProperty("paused", true);
    await expect(video).toHaveJSProperty("seeking", false);
    const stoppedAt = await video.evaluate(
      (v: HTMLVideoElement) => v.currentTime,
    );
    await page.waitForTimeout(250);
    expect(
      await video.evaluate((v: HTMLVideoElement) => v.currentTime),
    ).toBeCloseTo(stoppedAt, 3);
    await bar.getByRole("button", { name: "Play", exact: true }).click();
    await expect(video).toHaveJSProperty("paused", false);
    expect(await frames.evaluate((s) => s.reloads)).toBe(reloads);
  });
}
