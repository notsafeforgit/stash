import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const hls of [false, true]) {
  for (const mode of ["scene detail", "lightbox", "TV"] as const) {
    test(`${mode} ${hls ? "HLS" : "direct"} pauses and resumes without seeking or reloading`, async ({
      page,
    }) => {
      await serveSceneMedia(page);
      await page.goto(
        mode === "TV"
          ? `/tv-fixture/tv?paused${hls ? "&low" : ""}`
          : mode === "lightbox"
            ? `/scene-lightbox?paused${hls ? "&mode=hls" : ""}`
            : `/scene-detail?landscape&short${hls ? "&hls" : ""}`,
      );
      if (mode === "lightbox")
        await page.getByRole("button", { name: "Open scenes" }).click();
      const player = page.locator("[data-scene-player]");
      const video = player.locator("video");
      const surface = page.locator("[data-tv-play-surface]");
      const bar = player.locator("[data-player-control-bar]");
      if (mode === "TV") await surface.tap({ position: { x: 150, y: 250 } });
      else await player.locator("[data-player-native-button]").click();
      // Cold media startup has its own budget; pause/resume checks below
      // still use the normal interaction timeout once playback is ready.
      await expect(player).toHaveAttribute("data-playback-ready", "true", {
        timeout: 15_000,
      });
      await expect(video).toHaveJSProperty("seeking", false);
      await expect
        .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
        .toBeGreaterThan(0.5);
      const events = await video.evaluateHandle((v: HTMLVideoElement) => {
        const counts = { seeks: 0, reloads: 0 };
        v.addEventListener("seeking", () => counts.seeks++);
        v.addEventListener("emptied", () => counts.reloads++);
        return counts;
      });
      for (const pauseMs of [200, 1000]) {
        if (mode === "TV") await surface.tap({ position: { x: 150, y: 250 } });
        else
          await bar.getByRole("button", { name: "Pause", exact: true }).tap();
        await expect(video).toHaveJSProperty("paused", true);
        await page.waitForTimeout(pauseMs);
        await expect(video).toHaveJSProperty("paused", true);
        const pausedAt = await video.evaluate(
          (v: HTMLVideoElement) => v.currentTime,
        );
        if (mode === "TV") await surface.tap({ position: { x: 150, y: 250 } });
        else await bar.getByRole("button", { name: "Play", exact: true }).tap();
        await expect(video).toHaveJSProperty("paused", false);
        await expect
          .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
          .toBeGreaterThan(pausedAt + 0.25);
      }
      if (mode === "scene detail") {
        // Native intent changes synchronously, even before the event reaches
        // React. Each rapid command must apply without leaking AbortError.
        await bar
          .getByRole("button", { name: "Pause", exact: true })
          .evaluate((button: HTMLElement) => {
            for (let index = 0; index < 5; index++) button.click();
          });
        await expect(video).toHaveJSProperty("paused", true);
        await bar.getByRole("button", { name: "Play", exact: true }).tap();
        await expect(video).toHaveJSProperty("paused", false);
      }
      await expect(video).toHaveJSProperty("playbackRate", 1);
      expect(await events.jsonValue()).toEqual({ seeks: 0, reloads: 0 });
    });
  }
}
