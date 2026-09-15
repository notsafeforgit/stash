import type { BrowserContext, Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test.beforeEach(async ({ page }) => {
  await serveSceneMedia(page);
});

async function open(page: Page, query = "") {
  await page.goto(`/tv-fixture/tv?markers&long-marker&${query}`);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    window.tvFixtureVideo = video;
  });
}

const position = (page: Page) =>
  page
    .locator("video")
    .evaluate((video: HTMLVideoElement) => video.currentTime);
const tap = (page: Page) =>
  page.locator("[data-tv-play-surface]").tap({ position: { x: 150, y: 250 } });

async function panTouch(
  page: Page,
  context: BrowserContext,
  browserName: string,
) {
  if (browserName === "chromium") {
    const input = await context.newCDPSession(page);
    try {
      await input.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [{ x: 150, y: 250 }],
      });
      for (let step = 1; step <= 10; step++) {
        await input.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [{ x: 150 + step * 8, y: 250 + step * 8 }],
        });
      }
      await page.waitForTimeout(600);
      await input.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
    } finally {
      await input.detach();
    }
    return;
  }
  // Playwright has no native WebKit swipe API. Exercise its touch/pointer
  // handlers together, followed by a native tap to detect stale pan ownership.
  await page.evaluate(async () => {
    const target = document.elementFromPoint(150, 250);
    if (!target) throw new Error("Missing video gesture surface");
    const dispatch = (
      phase: "start" | "move" | "end",
      x: number,
      y: number,
    ) => {
      const touch: Touch = {
        identifier: 41,
        target,
        clientX: x,
        clientY: y,
        pageX: x,
        pageY: y,
        screenX: x,
        screenY: y,
        force: 1,
        radiusX: 1,
        radiusY: 1,
        rotationAngle: 0,
      };
      target.dispatchEvent(
        new PointerEvent(
          phase === "start"
            ? "pointerdown"
            : phase === "move"
              ? "pointermove"
              : "pointerup",
          {
            bubbles: true,
            cancelable: true,
            pointerId: 41,
            pointerType: "touch",
            isPrimary: true,
            button: 0,
            buttons: phase === "end" ? 0 : 1,
            clientX: x,
            clientY: y,
          },
        ),
      );
      const event = new Event(`touch${phase}`, {
        bubbles: true,
        cancelable: true,
      });
      Object.defineProperties(event, {
        touches: { value: phase === "end" ? [] : [touch] },
        targetTouches: { value: phase === "end" ? [] : [touch] },
        changedTouches: { value: [touch] },
      });
      target.dispatchEvent(event);
    };
    dispatch("start", 150, 250);
    for (let step = 1; step <= 10; step++) {
      dispatch("move", 150 + step * 8, 250 + step * 8);
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => resolve()),
      );
    }
    await new Promise((resolve) => setTimeout(resolve, 600));
    dispatch("end", 230, 330);
  });
}

for (const source of ["direct", "HLS"] as const) {
  test(`a tap pauses and resumes a playing ${source} marker at its current position`, async ({
    page,
  }) => {
    await open(page, source === "HLS" ? "low" : "");
    const video = page.locator("video");
    await expect(video).toHaveJSProperty("paused", false);
    await expect
      .poll(() => position(page))
      .toBeGreaterThan(source === "HLS" ? 1 : 3);
    const before = await position(page);
    await tap(page);
    await expect(video).toHaveJSProperty("paused", true);
    const after = await position(page);
    expect(after).toBeGreaterThanOrEqual(before);
    expect(after).toBeLessThan(before + 1);
    await tap(page);
    await expect(video).toHaveJSProperty("paused", false);
    await expect.poll(() => position(page)).toBeGreaterThan(after + 0.2);
  });
}

test("a pause tap stays paused when the marker finishes during double-tap recognition", async ({
  page,
}) => {
  await open(page);
  const video = page.locator("video");
  await expect(video).toHaveJSProperty("paused", false);
  // Finish after the tap starts, regardless of browser/CI action latency.
  await page.locator("[data-tv-play-surface]").evaluate((surface) => {
    surface.addEventListener(
      "click",
      () => {
        setTimeout(() => {
          const video = document.querySelector("video");
          if (video) video.currentTime = 10;
        }, 50);
      },
      { once: true },
    );
  });
  await tap(page);
  await page.waitForTimeout(400);
  await expect(video).toHaveJSProperty("paused", true);
  expect(await position(page)).toBeGreaterThanOrEqual(9.9);
});

for (const input of ["mouse", "touch"] as const) {
  for (const paused of [false, true]) {
    test(`zoomed ${input} panning preserves a ${paused ? "paused" : "playing"} marker`, async ({
      page,
      context,
      browserName,
    }) => {
      await open(page, paused ? "paused" : "");
      const video = page.locator("video");
      await expect(video).toHaveJSProperty("paused", paused);
      await video.evaluate((video: HTMLVideoElement) => {
        video.currentTime = 4;
      });
      await tap(page);
      await tap(page);
      await expect(
        page.getByRole("button", { name: "Reset zoom", exact: true }),
      ).toBeVisible();
      await page.waitForTimeout(300);
      const frame = page.locator("[data-video-frame-zoom] > div");
      const transform = await frame.evaluate(
        (element) => element.style.transform,
      );
      const before = await position(page);
      if (input === "touch") await panTouch(page, context, browserName);
      else {
        await page.mouse.move(150, 250);
        await page.mouse.down();
        await page.mouse.move(230, 330, { steps: 10 });
        // Keep dragging beyond the hold deadline: pan must own this gesture.
        await page.waitForTimeout(600);
        await expect(video).toHaveJSProperty("playbackRate", 1);
        await page.mouse.up();
      }
      await page.waitForTimeout(250);
      await expect(video).toHaveJSProperty("paused", paused);
      await expect(video).toHaveJSProperty("playbackRate", 1);
      expect(await position(page)).toBeGreaterThanOrEqual(before);
      expect(
        await frame.evaluate((element) => element.style.transform),
      ).not.toBe(transform);
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-key",
        /marker:10$/,
      );
      await tap(page);
      await page.waitForTimeout(600);
      await expect(video).toHaveJSProperty("paused", !paused);
      await expect(video).toHaveJSProperty("playbackRate", 1);
      await tap(page);
      await expect(video).toHaveJSProperty("paused", paused);
      await tap(page);
      await tap(page);
      await expect(
        page.getByRole("button", { name: "Reset zoom", exact: true }),
      ).toHaveCount(0);
      await page.waitForTimeout(250);
      await expect(video).toHaveJSProperty("paused", paused);
      expect(
        await video.evaluate((video) => video === window.tvFixtureVideo),
      ).toBe(true);
    });
  }
}

test("an unzoomed vertical drag changes markers and a horizontal drag does not", async ({
  page,
}) => {
  await open(page, "paused");
  await page.mouse.move(150, 350);
  await page.mouse.down();
  await page.mouse.move(260, 350, { steps: 10 });
  await page.mouse.up();
  await page.waitForTimeout(600);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /marker:10$/,
  );
  await expect(page.locator("video")).toHaveJSProperty("paused", true);
  await expect(page.locator("video")).toHaveJSProperty("playbackRate", 1);
  for (const [from, to, marker] of [
    [500, 250, 11],
    [250, 500, 10],
  ] as const) {
    await page.mouse.move(150, from);
    await page.mouse.down();
    await page.mouse.move(150, to, { steps: 10 });
    await page.mouse.up();
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      new RegExp(`marker:${marker}$`),
    );
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
  }
  expect(
    await page
      .locator("video")
      .evaluate((video) => video === window.tvFixtureVideo),
  ).toBe(true);
});

test("holding a zoomed marker owns movement until release and restores pause", async ({
  page,
}) => {
  await open(page, "paused");
  const video = page.locator("video");
  await tap(page);
  await tap(page);
  await expect(
    page.getByRole("button", { name: "Reset zoom", exact: true }),
  ).toBeVisible();
  await page.waitForTimeout(300);
  const frame = page.locator("[data-video-frame-zoom] > div");
  const transform = await frame.evaluate((element) => element.style.transform);
  await page.mouse.move(150, 250);
  await page.mouse.down();
  await expect(video).toHaveJSProperty("playbackRate", 2);
  await expect(video).toHaveJSProperty("paused", false);
  await page.mouse.move(230, 330, { steps: 10 });
  expect(await frame.evaluate((element) => element.style.transform)).toBe(
    transform,
  );
  await page.mouse.up();
  await page.waitForTimeout(250);
  await expect(video).toHaveJSProperty("playbackRate", 1);
  await expect(video).toHaveJSProperty("paused", true);
  await tap(page);
  await expect(video).toHaveJSProperty("paused", false);
});
