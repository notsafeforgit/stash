import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const mobile of [true, false]) {
  test.describe(
    mobile ? "touch video scrubber" : "desktop video scrubber",
    () => {
      test.use({
        isMobile: mobile,
        hasTouch: mobile,
        viewport: mobile
          ? { width: 390, height: 844 }
          : { width: 1280, height: 800 },
      });
      for (const marker of [false, true]) {
        test(`${marker ? "marker" : "scene"} TV and lightbox share the bar and seek interaction`, async ({
          page,
        }, testInfo) => {
          await serveSceneMedia(page);
          const appearances = [];
          for (const mode of ["tv", "lightbox"] as const) {
            await page.goto(
              mode === "tv"
                ? `/tv-fixture/tv?paused${marker ? "&markers" : ""}`
                : `/scene-lightbox?paused${marker ? "&mode=markers" : ""}`,
            );
            if (mode === "lightbox")
              await page.getByRole("button", { name: "Open scenes" }).click();
            await expect(page.locator("[data-scene-player]")).toHaveAttribute(
              "data-playback-ready",
              "true",
            );
            const video = page.locator("video");
            // HLS clips rebase their native timeline; display coordinates
            // remain 0..duration in both players.
            const mediaStart = await video.evaluate(
              (element: HTMLVideoElement) => element.currentTime,
            );
            const scrubber = page.getByRole("slider", {
              name: "Playback position",
            });
            await expect(scrubber).toHaveAttribute("aria-disabled", "false");
            const duration = marker ? 2 : 12;
            await expect(scrubber).toHaveAttribute(
              "aria-valuemax",
              String(duration),
            );
            appearances.push(
              await scrubber.evaluate((element) => {
                const styles = ["track", "buffer", "progress", "thumb"].map(
                  (part) => {
                    const node = element.querySelector(
                      `[data-position-scrubber-${part}]`,
                    );
                    if (!node) throw new Error(`Missing scrubber ${part}`);
                    const style = getComputedStyle(node);
                    return {
                      height: style.height,
                      color: style.backgroundColor,
                      radius: style.borderRadius,
                      opacity: style.opacity,
                    };
                  },
                );
                return {
                  hitArea: element.getBoundingClientRect().height,
                  styles,
                };
              }),
            );
            await page.screenshot({
              path: testInfo.outputPath(`${mode}-scrubber.png`),
            });
            const bounds = await scrubber.boundingBox();
            if (!bounds) throw new Error("Missing scrubber");
            const point = (fraction: number) => ({
              x: bounds.x + bounds.width * fraction,
              y: bounds.y + bounds.height / 2,
            });
            const middle = point(0.5);
            if (mobile) await page.touchscreen.tap(middle.x, middle.y);
            else await page.mouse.click(middle.x, middle.y);
            await expect
              .poll(async () =>
                Number(await scrubber.getAttribute("aria-valuenow")),
              )
              .toBeCloseTo(duration / 2, 1);
            const settled = await video.evaluate(
              (element: HTMLVideoElement) => element.currentTime,
            );
            const start = point(0.25);
            const end = point(0.75);
            await page.mouse.move(start.x, start.y);
            await page.mouse.down();
            await page.mouse.move(end.x, end.y, { steps: 5 });
            await expect
              .poll(async () =>
                Number(await scrubber.getAttribute("aria-valuenow")),
              )
              .toBeCloseTo(duration * 0.75, 1);
            // Dragging previews a position; the media seeks once on release.
            expect(
              await video.evaluate(
                (element: HTMLVideoElement) => element.currentTime,
              ),
            ).toBeCloseTo(settled, 1);
            await page.mouse.up();
            await expect
              .poll(() =>
                video.evaluate(
                  (element: HTMLVideoElement) => element.currentTime,
                ),
              )
              .toBeCloseTo(mediaStart + duration * 0.75, 1);
            await scrubber.press("Home");
            await expect(scrubber).toHaveAttribute("aria-valuenow", "0");
            if (!marker) {
              await scrubber.press("ArrowRight");
              await expect(scrubber).toHaveAttribute("aria-valuenow", "5");
            }
            await expect(video).toHaveJSProperty("paused", true);
          }
          expect(appearances[0]).toEqual(appearances[1]);
        });
      }
    },
  );
}
