import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

const cases = [
  {
    mode: "tv",
    name: "scene",
    path: "/tv-fixture/tv?low&paused",
    first: 0,
    duration: 12,
  },
  {
    mode: "tv",
    name: "marker",
    path: "/tv-fixture/tv?low&paused&markers&long-marker",
    first: 1,
    duration: 8,
  },
  {
    mode: "lightbox",
    name: "scene",
    path: "/scene-lightbox?mode=hls&paused",
    first: 0,
    duration: 12,
  },
  {
    mode: "lightbox",
    name: "marker",
    path: "/scene-lightbox?mode=long-marker&paused",
    first: 1,
    duration: 8,
  },
] as const;

for (const mobile of [true, false]) {
  test.describe(mobile ? "mobile buffer fill" : "desktop buffer fill", () => {
    test.use({
      isMobile: mobile,
      hasTouch: mobile,
      viewport: mobile
        ? { width: 390, height: 844 }
        : { width: 1280, height: 800 },
    });
    for (const item of cases) {
      test(`${item.mode} ${item.name} paints HLS buffers as fragments arrive`, async ({
        page,
      }, testInfo) => {
        await serveSceneMedia(page);
        let allowed = item.first + 1;
        const pending = new Set<{ index: number; resolve: () => void }>();
        const release = (end: number) => {
          allowed = end;
          for (const request of pending) {
            if (request.index < allowed) {
              pending.delete(request);
              request.resolve();
            }
          }
        };
        await page.route("**/media/hls/segment-*.m4s", async (route) => {
          const match = /segment-(\d+)\.m4s/.exec(route.request().url());
          if (!match) throw new Error("Missing HLS fragment index");
          const index = Number(match[1]);
          if (index >= allowed)
            await new Promise<void>((resolve) =>
              pending.add({ index, resolve }),
            );
          await route.fallback();
        });
        try {
          await page.goto(item.path);
          if (item.mode === "lightbox")
            await page.getByRole("button", { name: "Open scenes" }).click();
          const video = page.locator("video");
          const scrubber = page.getByRole("slider", {
            name: "Playback position",
          });
          await expect(scrubber).toHaveAttribute(
            "aria-valuemax",
            String(item.duration),
          );
          // Observe real native buffers throughout staged downloads. In WebKit,
          // metadata can precede the first append and progress may never fire.
          // Playback continuity is exercised separately without withheld media.
          for (let count = 1; count <= item.duration / 2; count++) {
            release(item.first + count);
            await expect
              .poll(
                () =>
                  video.evaluate((element: HTMLVideoElement) =>
                    element.buffered.length
                      ? element.buffered.end(element.buffered.length - 1)
                      : 0,
                  ),
                { timeout: 15000 },
              )
              .toBeCloseTo(count * 2, 1);
            await expect
              .poll(() =>
                scrubber
                  .locator("[data-position-scrubber-buffer]")
                  .evaluateAll((elements) =>
                    elements.map((element) => {
                      if (!(element instanceof HTMLElement))
                        throw new Error("Missing buffered span");
                      return {
                        left: Math.round(parseFloat(element.style.left)),
                        width: Math.round(parseFloat(element.style.width)),
                      };
                    }),
                  ),
              )
              .toEqual([
                {
                  left: 0,
                  width: Math.round(((count * 2) / item.duration) * 100),
                },
              ]);
            if (count === item.duration / 4)
              await page.screenshot({
                path: testInfo.outputPath("partial-buffer.png"),
              });
            if (count === 1) {
              await expect(video).toHaveJSProperty("paused", true);
              const play = page
                .getByRole("button", { name: "Play", exact: true })
                .first();
              if (mobile) await play.tap();
              else await play.click();
            }
          }
        } finally {
          release(Infinity);
        }
      });
    }
  });
}
