import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const pauseDuringSeek of [false, true]) {
  test(`successive seeks with delayed completion ${pauseDuringSeek ? "honor a subsequent pause" : "keep playback running"}`, async ({
    page,
  }) => {
    await serveSceneMedia(page);
    await page.goto("/tv-fixture/tv?low");
    const video = page.locator("video");
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
      { timeout: 15000 },
    );
    await expect(video).toHaveJSProperty("paused", false);
    await expect
      .poll(() =>
        video.evaluate(
          (element: HTMLVideoElement) =>
            element.buffered.length > 0 &&
            element.buffered.end(element.buffered.length - 1) >=
              element.duration - 0.1,
        ),
      )
      .toBe(true);
    // Delay the browser's completion notifications while retaining real HLS
    // playback and native seeking. This deterministically exercises superseded
    // callbacks without depending on decoder behavior with withheld fragments.
    const completion = await video.evaluateHandle(
      (element: HTMLVideoElement) => {
        const held: Event[] = [];
        const hold = (event: Event) => {
          if (event.target !== element) return;
          event.stopImmediatePropagation();
          held.push(event);
        };
        document.addEventListener("seeked", hold, true);
        return {
          held,
          release: () => {
            document.removeEventListener("seeked", hold, true);
            for (const event of held.splice(0))
              element.dispatchEvent(new Event(event.type));
          },
        };
      },
    );
    try {
      const scrubber = page.getByRole("slider", { name: "Playback position" });
      const bounds = await scrubber.boundingBox();
      if (!bounds) throw new Error("Missing scrubber");
      for (const target of [4.5, 5]) {
        await page.touchscreen.tap(
          bounds.x + (bounds.width * target) / 12,
          bounds.y + bounds.height / 2,
        );
        await expect(video).toHaveJSProperty("paused", false);
      }
      await expect
        .poll(() => completion.evaluate(({ held }) => held.length))
        .toBeGreaterThan(0);
      if (pauseDuringSeek) {
        await page.getByRole("button", { name: "Pause", exact: true }).tap();
        await expect(video).toHaveJSProperty("paused", true);
      }
      const before = await video.evaluate(
        (element: HTMLVideoElement) => element.currentTime,
      );
      await completion.evaluate(({ release }) => release());
      if (pauseDuringSeek) {
        const later = await video.evaluate(
          async (element: HTMLVideoElement) => {
            await new Promise((resolve) => setTimeout(resolve, 300));
            return { paused: element.paused, time: element.currentTime };
          },
        );
        expect(later.paused).toBe(true);
        expect(later.time).toBeCloseTo(before, 1);
      } else {
        await expect
          .poll(() =>
            video.evaluate(
              (element: HTMLVideoElement, time) =>
                !element.paused && element.currentTime > time + 0.2,
              before,
            ),
          )
          .toBe(true);
      }
    } finally {
      await completion.evaluate(({ release }) => release());
    }
  });
}

for (const mobile of [true, false]) {
  test.describe(mobile ? "touch seeking" : "mouse seeking", () => {
    test.use({
      isMobile: mobile,
      hasTouch: mobile,
      viewport: mobile
        ? { width: 390, height: 844 }
        : { width: 1280, height: 800 },
    });
    for (const hls of [false, true]) {
      for (const marker of [false, true]) {
        test(`${hls ? "HLS" : "direct"} ${marker ? "marker" : "scene"} buffered seeks keep playing`, async ({
          page,
        }) => {
          await serveSceneMedia(page);
          await page.goto(
            `/tv-fixture/tv?${hls ? "low&" : ""}${marker ? "markers&long-marker" : ""}`,
          );
          const video = page.locator("video");
          await expect(page.locator("[data-scene-player]")).toHaveAttribute(
            "data-playback-ready",
            "true",
          );
          const unmute = page.getByRole("button", {
            name: "Unmute",
            exact: true,
          });
          if (mobile) await unmute.tap();
          else await unmute.click();
          const scrubber = page.getByRole("slider", {
            name: "Playback position",
          });
          const duration = marker ? 8 : 12;
          const mediaStart = marker && !hls ? 2 : 0;
          await expect
            .poll(() =>
              video.evaluate(
                (element: HTMLVideoElement) =>
                  element.buffered.length > 0 &&
                  element.buffered.end(element.buffered.length - 1) >=
                    element.duration - 0.1,
              ),
            )
            .toBe(true);
          await expect(video).toHaveJSProperty("paused", false);
          const observed = await video.evaluateHandle(
            (element: HTMLVideoElement) => {
              const events: string[] = [];
              for (const name of ["pause", "play", "emptied", "loadstart"])
                element.addEventListener(name, () => events.push(name));
              return { element, events };
            },
          );
          const bounds = await scrubber.boundingBox();
          if (!bounds) throw new Error("Missing scrubber");
          for (const fraction of [0.6, 0.25, 0.5]) {
            const target = mediaStart + fraction * duration;
            // Verify the actual media buffer, independently of the painted bar.
            expect(
              await video.evaluate(
                (element: HTMLVideoElement, time) =>
                  Array.from(
                    { length: element.buffered.length },
                    (_, index) =>
                      time >= element.buffered.start(index) &&
                      time < element.buffered.end(index),
                  ).some(Boolean),
                target,
              ),
            ).toBe(true);
            const x = bounds.x + bounds.width * fraction;
            const y = bounds.y + bounds.height / 2;
            if (mobile) await page.touchscreen.tap(x, y);
            else await page.mouse.click(x, y);
            await expect
              .poll(
                () =>
                  video.evaluate(
                    (element: HTMLVideoElement, time) =>
                      !element.paused &&
                      !element.seeking &&
                      element.currentTime > time + 0.1,
                    target,
                  ),
                { timeout: 2000 },
              )
              .toBe(true);
          }
          const result = await observed.evaluate(({ element, events }) => ({
            events,
            sameVideo: document.querySelector("video") === element,
          }));
          expect(result).toEqual({ events: [], sameVideo: true });
          // The scrubber must release touch ownership to the video surface.
          const pause = page.getByRole("button", {
            name: "Pause",
            exact: true,
          });
          if (mobile) await pause.tap();
          else await pause.click();
          await expect(video).toHaveJSProperty("paused", true);
        });
      }
    }
  });
}
