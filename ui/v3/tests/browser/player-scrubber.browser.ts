import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const mode of ["tv", "lightbox"]) {
  for (const segment of [false, true]) {
    test(`${mode} ${segment ? "segment" : "scene"} paints separate buffered intervals and clears evicted data`, async ({
      page,
    }) => {
      await serveSceneMedia(page);
      await page.goto(
        mode === "tv"
          ? `/tv-fixture/tv?paused${segment ? "&resume" : ""}`
          : `/scene-lightbox?paused${segment ? "&mode=markers" : ""}`,
      );
      if (mode === "lightbox")
        await page.getByRole("button", { name: "Open scenes" }).click();
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      const scrubber = page.getByRole("slider", { name: "Playback position" });
      const duration = Number(await scrubber.getAttribute("aria-valuemax"));
      const video = page.locator("video");
      const start = await video.evaluate(
        (element: HTMLVideoElement) => element.currentTime,
      );
      // Supply native range snapshots (not player state) to exercise the real
      // store, semantic subscription and both timeline coordinate systems.
      const replaceBuffer = (ranges: { start: number; end: number }[]) =>
        video.evaluate((element: HTMLVideoElement, ranges) => {
          Object.defineProperty(element, "buffered", {
            configurable: true,
            get: (): TimeRanges => ({
              length: ranges.length,
              start: (index) => {
                const range = ranges[index];
                if (!range) throw new Error("Invalid buffer index");
                return range.start;
              },
              end: (index) => {
                const range = ranges[index];
                if (!range) throw new Error("Invalid buffer index");
                return range.end;
              },
            }),
          });
          element.dispatchEvent(new Event("progress"));
        }, ranges);
      await replaceBuffer([
        { start: Math.max(0, start - 1), end: start + duration * 0.2 },
        { start: start + duration * 0.5, end: start + duration * 0.7 },
        { start: start + duration * 0.9, end: start + duration + 1 },
      ]);
      const spans = scrubber.locator("[data-position-scrubber-buffer]");
      const paintedRanges = () =>
        spans.evaluateAll((elements) =>
          elements.map((element) => {
            if (!(element instanceof HTMLElement))
              throw new Error("Missing buffered span");
            return {
              left: Math.round(parseFloat(element.style.left)),
              width: Math.round(parseFloat(element.style.width)),
            };
          }),
        );
      await expect.poll(paintedRanges).toEqual([
        { left: 0, width: 20 },
        { left: 50, width: 20 },
        { left: 90, width: 10 },
      ]);
      await replaceBuffer([
        { start: start + duration * 0.6, end: start + duration * 0.7 },
      ]);
      await expect(spans).toHaveCount(1);
      await expect.poll(paintedRanges).toEqual([{ left: 60, width: 10 }]);
      await replaceBuffer([]);
      await expect(spans).toHaveCount(0);
    });
  }
}

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
                const styles = ["track", "progress", "thumb"].map((part) => {
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
                });
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
