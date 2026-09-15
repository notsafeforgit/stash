import type { Locator } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const mobile of [true, false]) {
  test.describe(
    mobile ? "touch counter playback" : "mouse counter playback",
    () => {
      test.use({
        isMobile: mobile,
        hasTouch: mobile,
        viewport: mobile
          ? { width: 390, height: 844 }
          : { width: 1280, height: 800 },
      });
      for (const hls of [false, true]) {
        for (const marker of [false, true]) {
          test(`${hls ? "HLS" : "direct"} ${marker ? "marker" : "scene"} keeps playing through counter taps and refreshes`, async ({
            page,
          }) => {
            await serveSceneMedia(page);
            await page.goto(
              `/tv-fixture/tv?slow&slow-counter&slow-feed&count=2${hls ? "&low" : ""}${marker ? "&markers&long-marker" : ""}`,
            );
            const video = page.locator("video");
            await expect(page.locator("[data-scene-player]")).toHaveAttribute(
              "data-playback-ready",
              "true",
            );
            await expect(video).toHaveJSProperty("paused", false);
            const activate = (control: Locator) =>
              mobile ? control.tap() : control.click();
            await activate(
              page.getByRole("button", { name: "Unmute", exact: true }),
            );
            // Wait past initial source positioning before observing interruptions.
            const initialTime = await video.evaluate(
              (element: HTMLVideoElement) => element.currentTime,
            );
            await expect
              .poll(() =>
                video.evaluate(
                  (element: HTMLVideoElement) => element.currentTime,
                ),
              )
              .toBeGreaterThan(initialTime + 0.2);
            const observed = await video.evaluateHandle(
              (element: HTMLVideoElement) => {
                const events: string[] = [];
                for (const event of [
                  "pause",
                  "play",
                  "seeking",
                  "emptied",
                  "loadstart",
                ])
                  element.addEventListener(event, () => events.push(event));
                return { element, events, start: element.currentTime };
              },
            );
            await activate(
              page.getByRole("button", { name: "O-counter", exact: true }),
            );
            const counter = page.locator("[data-tv-counter]");
            await expect(counter).toBeVisible();
            for (const [name, count] of [
              ["Add O", "3"],
              ["Decrement O", "2"],
              ["Reset", "0"],
            ] as const) {
              await activate(
                counter.getByRole("button", { name, exact: true }),
              );
              await expect(counter.locator('[aria-live="polite"]')).toHaveText(
                count,
              );
              await expect(counter).toHaveAttribute("aria-busy", "false");
            }
            await counter.press("Escape");
            await expect(counter).toHaveCount(0);
            // Each write still refreshes filter membership. Wait for the final
            // response so this also catches media invalidation by feed summaries.
            await expect
              .poll(() =>
                page.evaluate(
                  (name) =>
                    window.tvFixtureRequests.filter(
                      (request) => request.name === name,
                    ).length,
                  marker ? "TvMarkers" : "TvScenes",
                ),
              )
              .toBeGreaterThanOrEqual(4);
            await expect(video).toHaveJSProperty("paused", false);
            const result = await observed.evaluate(
              ({ element, events, start }) => ({
                events,
                sameVideo: document.querySelector("video") === element,
                elapsed: element.currentTime - start,
              }),
            );
            expect(result.events).toEqual([]);
            expect(result.sameVideo).toBe(true);
            expect(result.elapsed).toBeGreaterThan(1);
            expect(
              await page.evaluate(
                () =>
                  window.tvFixtureRequests.filter(
                    (request) =>
                      request.name === "FindScene" &&
                      JSON.stringify(request.variables) === '{"id":"1"}',
                  ).length,
              ),
            ).toBe(1);
          });
        }
      }
    },
  );
}
