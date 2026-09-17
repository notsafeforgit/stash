import { readFile } from "node:fs/promises";
import type { Page } from "@playwright/test";
import { test, expect } from "./test";

/** Keep the first four seconds playable; hold every later fragment until
 * all inputs have been accepted. No real library or transcoder is involved. */
async function holdSeekMedia(page: Page) {
  const media = new URL("./fixture/media/seek/", import.meta.url);
  const [playlist, fragments] = await Promise.all([
    readFile(new URL("stream.m3u8", media), "utf8"),
    readFile(new URL("stream.m4s", media)),
  ]);
  let release = () => {};
  let released = false;
  const waiting = new Promise<void>((resolve) => {
    release = () => {
      released = true;
      resolve();
    };
  });
  await page.route("**/scene/detail/**", async (route) => {
    const url = new URL(route.request().url());
    if (/\/streams\.(stop|keepalive)$/.test(url.pathname)) {
      await route.fulfill({ status: 204 });
    } else if (url.pathname.endsWith("/stream.master.m3u8")) {
      await route.fulfill({
        contentType: "application/vnd.apple.mpegurl",
        body: playlist,
      });
    } else if (url.pathname.endsWith("/stream.m4s")) {
      const range = /^bytes=(\d+)-(\d+)$/.exec(
        route.request().headers().range ?? "",
      );
      if (!range) throw new Error("Expected an HLS byte-range request");
      const start = Number(range[1]);
      const end = Number(range[2]);
      // Init + first two segments. Derive the cutoff from the real playlist.
      const ranges = [...playlist.matchAll(/#EXT-X-BYTERANGE:\d+@(\d+)/g)];
      const cutoff = Number(ranges[2]?.[1]);
      if (!released && start >= cutoff) await waiting;
      await route.fulfill({
        status: 206,
        contentType: "video/mp4",
        headers: {
          "Content-Range": `bytes ${start}-${end}/${fragments.length}`,
        },
        body: fragments.subarray(start, end + 1),
      });
    } else {
      throw new Error(`Unexpected seek fixture request: ${url.pathname}`);
    }
  });
  return release;
}

for (const mobile of [true, false]) {
  test.describe(
    mobile ? "touch source reloads" : "desktop engine seeks",
    () => {
      test.use({
        isMobile: mobile,
        hasTouch: mobile,
        viewport: mobile
          ? { width: 390, height: 844 }
          : { width: 1280, height: 800 },
        // Exercise the iOS source-reload policy in both test engines. Headless
        // WebKit still cannot establish physical iPhone MMS behavior.
        ...(mobile && {
          userAgent:
            "Mozilla/5.0 (iPhone; CPU iPhone OS 27_0 like Mac OS X) AppleWebKit/605.1.15 Version/27.0 Mobile/15E148 Safari/604.1",
        }),
      });

      for (const paused of [false, true]) {
        test(`repeated seeks accumulate while buffering and stay ${paused ? "paused" : "playing"}`, async ({
          page,
        }) => {
          const release = await holdSeekMedia(page);
          try {
            await page.goto("/scene-detail?hls");
            const player = page.locator("[data-scene-player]");
            const video = player.locator("video");
            const originalVideo = await video.elementHandle();
            await player.locator("[data-player-native-button]").click();
            await expect(video).toHaveJSProperty("paused", false);
            await expect(player).toHaveAttribute("data-playback-ready", "true");
            // The expanded viewer enables the same zoom recognizer as the
            // lightbox. Rapid button taps must remain seeks, never zooms.
            if (mobile)
              await player
                .getByRole("button", { name: "Open scene viewer" })
                .tap();
            if (paused) {
              await player
                .locator("[data-player-control-bar]")
                .getByRole("button", { name: "Pause", exact: true })
                .click();
              await expect(video).toHaveJSProperty("paused", true);
            }
            const position = page.getByRole("slider", {
              name: "Playback position",
            });
            const forward = page.getByRole("button", {
              name: "Skip forward 10 seconds",
            });
            const backward = page.getByRole("button", {
              name: "Skip back 10 seconds",
            });
            const activate = async (direction: "forward" | "backward") => {
              const button = direction === "forward" ? forward : backward;
              if (mobile) await button.tap({ timeout: 2000 });
              else await button.click({ timeout: 2000 });
            };

            await activate("forward");
            await expect
              .poll(async () =>
                Number(await position.getAttribute("aria-valuenow")),
              )
              .toBeGreaterThanOrEqual(10);
            let target = Number(await position.getAttribute("aria-valuenow"));
            for (let index = 0; index < 24; index++) {
              await activate("forward");
              target += 10;
              await expect
                .poll(async () =>
                  Number(await position.getAttribute("aria-valuenow")),
                )
                .toBeCloseTo(target, 1);
            }
            for (let index = 0; index < 3; index++) {
              await activate("backward");
              target -= 10;
              await expect
                .poll(async () =>
                  Number(await position.getAttribute("aria-valuenow")),
                )
                .toBeCloseTo(target, 1);
            }
            // All events in one browser task: React has not committed between
            // inputs, and native currentTime is still unavailable after reload.
            await page.evaluate(() => {
              for (let index = 0; index < 12; index++)
                window.dispatchEvent(
                  new KeyboardEvent("keydown", { key: "ArrowRight" }),
                );
            });
            target += 120;
            await expect
              .poll(async () =>
                Number(await position.getAttribute("aria-valuenow")),
              )
              .toBeCloseTo(target, 1);
            if (mobile)
              expect(
                await player
                  .locator("[data-video-frame-zoom] > div")
                  .evaluate(
                    (element) =>
                      new DOMMatrixReadOnly(getComputedStyle(element).transform)
                        .a,
                  ),
              ).toBe(1);

            release();
            await expect
              .poll(() =>
                video.evaluate((element: HTMLVideoElement) => ({
                  paused: element.paused,
                  ready:
                    element.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA,
                  seeking: element.seeking,
                })),
              )
              .toEqual({ paused, ready: true, seeking: false });
            const landed = await video.evaluate(
              (element: HTMLVideoElement) => element.currentTime,
            );
            expect(landed).toBeGreaterThanOrEqual(target - 0.05);
            expect(landed).toBeLessThan(target + 3);
            expect(
              await originalVideo?.evaluate((element) => element.isConnected),
            ).toBe(true);
            if (!paused) {
              await expect
                .poll(() =>
                  video.evaluate(
                    (element: HTMLVideoElement) => element.currentTime,
                  ),
                )
                .toBeGreaterThan(landed + 0.3);
            }
          } finally {
            release();
          }
        });
      }
    },
  );
}
