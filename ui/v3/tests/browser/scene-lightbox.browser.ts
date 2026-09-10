import type { Page } from "@playwright/test";
import { test, expect } from "./test";

// Serve synthetic media at the real Stash endpoint shapes. Production source
// selection, clip URLs, HLS attachment, resume and lease cleanup all run intact.
test.beforeEach(async ({ page }) => {
  await page.route("**/scene/*/**", async (route) => {
    const url = new URL(route.request().url());
    if (/\/streams\.(stop|keepalive)$/.test(url.pathname)) {
      await route.fulfill({ status: 204 });
    } else if (url.pathname.endsWith("/caption")) {
      await route.fulfill({
        contentType: "text/vtt",
        body: "WEBVTT\n\n00:00.000 --> 00:12.000\nSample caption\n",
      });
    } else if (url.pathname.endsWith("/stream")) {
      const response = await route.fetch({
        url: new URL("/media/audio.mp4", url).href,
      });
      await route.fulfill({ response });
    } else if (url.pathname.endsWith("/stream.master.m3u8")) {
      const clipped = url.searchParams.has("end");
      const first = clipped
        ? Math.floor(Number(url.searchParams.get("start")) / 2)
        : 0;
      const end = clipped
        ? Math.ceil(Number(url.searchParams.get("end")) / 2)
        : 6;
      const body = [
        "#EXTM3U",
        "#EXT-X-VERSION:7",
        "#EXT-X-TARGETDURATION:2",
        `#EXT-X-MEDIA-SEQUENCE:${first}`,
        "#EXT-X-PLAYLIST-TYPE:VOD",
        '#EXT-X-MAP:URI="/media/hls/init.mp4"',
        ...Array.from(
          { length: end - first },
          (_, i) => `#EXTINF:2.000000,\n/media/hls/segment-${first + i}.m4s`,
        ),
        "#EXT-X-ENDLIST",
        "",
      ].join("\n");
      await route.fulfill({
        contentType: "application/vnd.apple.mpegurl",
        body,
      });
    } else {
      throw new Error(`Unexpected scene request: ${url}`);
    }
  });
});

async function open(page: Page, query = "") {
  await page.goto(`/scene-lightbox${query}`);
  await page.getByRole("button", { name: "Open scenes" }).click();
  const video = page.locator("video");
  await expect(video).toHaveCount(1);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  return video;
}

async function next(page: Page, index: number) {
  await page.keyboard.press("ArrowRight");
  await expect(page.getByTestId("view")).toHaveText(String(index));
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
}

async function revealControls(page: Page) {
  const playbackControls = page.locator("[data-player-playback-controls]");
  if ((await playbackControls.getAttribute("inert")) !== null) {
    await page.touchscreen.tap(180, 250);
  }
  await expect(playbackControls).not.toHaveAttribute("inert");
}

test("mobile Close owns the touch sequence before its native click", async ({
  page,
}) => {
  await open(page);
  const controls = page.locator("[data-player-playback-controls]");
  const close = page.locator("[data-player-close]");
  await expect(controls).toHaveAttribute("inert", "", { timeout: 10000 });
  const pointer = {
    pointerType: "touch",
    pointerId: 1,
    isPrimary: true,
    button: 0,
  };
  await close.dispatchEvent("pointerdown", { ...pointer, buttons: 1 });
  await close.dispatchEvent("pointerup", { ...pointer, buttons: 0 });
  // Safari can consume the first tap if activity handling reveals UI before
  // the compatibility click arrives. Check that gap, not only the final click.
  await expect(controls).toHaveAttribute("inert", "", { timeout: 1000 });
  await close.dispatchEvent("pointermove", {
    pointerType: "mouse",
    buttons: 0,
  });
  await close.focus();
  await expect(controls).toHaveAttribute("inert", "", { timeout: 1000 });
  await close.dispatchEvent("click");
  await expect(page.locator(".yarl__root")).toHaveCount(0);
});

test("mobile Close dismisses the real lightbox on the first tap while controls are hidden", async ({
  page,
}) => {
  const video = await open(page);
  await expect(page.locator("[data-player-playback-controls]")).toHaveAttribute(
    "inert",
    "",
    { timeout: 10000 },
  );
  await page.locator("[data-player-close]").tap();
  await expect(page.locator(".yarl__root")).toHaveCount(0);
  await expect(video).toHaveCount(0);
});

test("the real lightbox retains one video and audio through swipes, HLS and wraparound", async ({
  page,
}) => {
  const video = await open(page);
  // Explicit unmute/play in a user gesture before asking the browser to keep it.
  await page.getByRole("button", { name: "Mute", exact: true }).click();
  await page.getByRole("button", { name: "Unmute", exact: true }).click();
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video");
  for (const index of [1, 2, 0, 1]) {
    await next(page, index);
    await expect(video).toHaveJSProperty("paused", false);
    await expect(video).toHaveJSProperty("muted", false);
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeGreaterThan(0.1);
    expect(
      await video.evaluate((v, previous) => v === previous, original),
    ).toBe(true);
    expect(await video.locator("track").getAttribute("src")).toContain(
      `/scene/${index + 1}/caption`,
    );
  }
  await revealControls(page);
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(video).toHaveCount(0);
  expect(
    await original.evaluate(
      (v) => v instanceof HTMLVideoElement && v.paused && !v.isConnected,
    ),
  ).toBe(true);
});

test("loading sentinels suspend the old scene and preserve the media element", async ({
  page,
}) => {
  const video = await open(page, "?mode=pending");
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "inert",
    "",
  );
  await expect(video).toHaveJSProperty("paused", true);
  await expect(
    page.getByRole("button", { name: "Close", exact: true }),
  ).toBeVisible();
  await expect(page.locator("[data-scene-player]")).not.toHaveAttribute(
    "inert",
    "",
  );
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect(video).toHaveJSProperty("paused", false);
  expect(await video.evaluate((v, previous) => v === previous, original)).toBe(
    true,
  );
});

test("rapid navigation ignores late query results and missing scenes remain dismissible", async ({
  page,
}) => {
  const video = await open(page, "?mode=slow");
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video");
  await page.keyboard.press("ArrowRight");
  await expect(page.getByTestId("view")).toHaveText("1");
  await next(page, 2);
  await expect(
    page.getByRole("link", { name: "Scene 3", exact: true }),
  ).toBeVisible();
  await page.waitForTimeout(1300); // Let the abandoned query's delayed result arrive.
  await expect(
    page.getByRole("link", { name: "Scene 3", exact: true }),
  ).toBeVisible();
  expect(await video.evaluate((v, previous) => v === previous, original)).toBe(
    true,
  );
  await page.keyboard.press("ArrowRight");
  await expect(page.getByTestId("view")).toHaveText("3");
  await expect(video).toHaveJSProperty("paused", true);
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(video).toHaveCount(0);
});

test("a multi-segment marker loads its beginning before later clip segments", async ({
  page,
}) => {
  const firstSegment = page.waitForRequest((request) =>
    /\/media\/hls\/segment-\d+\.m4s$/.test(new URL(request.url()).pathname),
  );
  const video = await open(page, "?mode=long-marker&paused");
  expect(new URL((await firstSegment).url()).pathname).toBe(
    "/media/hls/segment-1.m4s",
  );
  await expect(video).toHaveJSProperty("paused", true);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeLessThan(0.1);
  await page.getByRole("button", { name: "Play", exact: true }).first().click();
  await expect(video).toHaveJSProperty("paused", false);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(2.1);
});

test("same-scene marker navigation resets clip timestamps and end state", async ({
  page,
}) => {
  const video = await open(page, "?mode=markers&paused");
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video");
  await expect(video).toHaveJSProperty("paused", true);
  await page.getByRole("button", { name: "Play", exact: true }).first().click();
  await expect(video).toHaveJSProperty("paused", false);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(1.8);
  await expect(video).toHaveJSProperty("paused", true);
  await next(page, 1);
  await expect(video).toHaveJSProperty("paused", true);
  await expect(
    page.getByRole("link", { name: "Marker 2", exact: true }),
  ).toBeVisible();
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeLessThan(0.1);
  await next(page, 0);
  await expect(
    page.getByRole("link", { name: "Marker 1", exact: true }),
  ).toBeVisible();
  await expect(video).toHaveJSProperty("paused", true);
  expect(await video.evaluate((v, previous) => v === previous, original)).toBe(
    true,
  );
});

test("ended-driven auto-advance preserves the same unmuted player", async ({
  page,
}) => {
  await page.addInitScript(() =>
    localStorage.setItem("stash-player-auto-advance", "true"),
  );
  const video = await open(page);
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video");
  for (const index of [1, 2, 0]) {
    await expect(video).toHaveJSProperty("paused", false);
    await video.evaluate((v: HTMLVideoElement) => {
      v.currentTime = v.duration - 0.3;
    });
    await expect(page.getByTestId("view")).toHaveText(String(index));
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
    await expect(video).toHaveJSProperty("paused", false);
    expect(
      await video.evaluate((v, previous) => v === previous, original),
    ).toBe(true);
    await expect(video).toHaveJSProperty("muted", false);
  }
});

test("swipe drag tracks the pointer and the center player survives the animation", async ({
  page,
}) => {
  const video = await open(page);
  const original = await video.elementHandle();
  if (!original) throw new Error("Missing video");
  const surface = page
    .getByRole("button", { name: "Skip back 10 seconds", exact: true })
    .locator("..");
  const origin = await video.boundingBox();
  if (!origin) throw new Error("Missing video bounds");
  await surface.dispatchEvent("pointerdown", {
    pointerId: 7,
    pointerType: "touch",
    isPrimary: true,
    buttons: 1,
    clientX: 300,
    clientY: 250,
  });
  await surface.dispatchEvent("pointermove", {
    pointerId: 7,
    pointerType: "touch",
    isPrimary: true,
    buttons: 1,
    clientX: 250,
    clientY: 250,
  });
  await surface.dispatchEvent("pointermove", {
    pointerId: 7,
    pointerType: "touch",
    isPrimary: true,
    buttons: 1,
    clientX: 100,
    clientY: 250,
  });
  await expect
    .poll(async () => (await video.boundingBox())?.x)
    .toBeCloseTo(origin.x - 150, 0);
  await surface.dispatchEvent("pointerup", {
    pointerId: 7,
    pointerType: "touch",
    isPrimary: true,
    buttons: 0,
    clientX: 100,
    clientY: 250,
  });
  await expect(page.getByTestId("view")).toHaveText("1");
  await expect
    .poll(async () => (await video.boundingBox())?.x)
    .toBeCloseTo(origin.x, 0);
  await expect(video).toHaveJSProperty("paused", false);
  expect(await video.evaluate((v, previous) => v === previous, original)).toBe(
    true,
  );
});

test("a held 2x gesture continues through auto-advance and restores its original rate on release", async ({
  page,
}) => {
  await page.addInitScript(() =>
    localStorage.setItem("stash-player-auto-advance", "true"),
  );
  const video = await open(page);
  await revealControls(page);
  await page.getByRole("button", { name: "Playback speed" }).click();
  await page.getByRole("menuitemradio", { name: "1.5x", exact: true }).click();
  await expect(video).toHaveJSProperty("playbackRate", 1.5);
  const surface = page
    .getByRole("button", { name: "Skip back 10 seconds", exact: true })
    .locator("..");
  const original = await surface.elementHandle();
  if (!original) throw new Error("Missing gesture surface");
  await surface.evaluate((element) => {
    // WebKit's Touch constructor is not exposed in Playwright. Dispatch the
    // touch event payload directly to the production React gesture handler.
    const touch = {
      identifier: 1,
      target: element,
      clientX: 180,
      clientY: 250,
    };
    const event = new Event("touchstart", { bubbles: true, cancelable: true });
    Object.defineProperties(event, {
      touches: { value: [touch] },
      targetTouches: { value: [touch] },
      changedTouches: { value: [touch] },
    });
    element.dispatchEvent(event);
  });
  await expect(video).toHaveJSProperty("playbackRate", 2);
  await video.evaluate((v: HTMLVideoElement) => {
    v.currentTime = v.duration - 0.2;
  });
  await expect(page.getByTestId("view")).toHaveText("1");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect(video).toHaveJSProperty("paused", false);
  await expect(video).toHaveJSProperty("playbackRate", 2);
  expect(await original.evaluate((element) => element.isConnected)).toBe(true);
  await original.evaluate((element) => {
    const event = new Event("touchend", { bubbles: true, cancelable: true });
    Object.defineProperties(event, {
      touches: { value: [] },
      targetTouches: { value: [] },
      changedTouches: { value: [] },
    });
    element.dispatchEvent(event);
  });
  await expect(video).toHaveJSProperty("playbackRate", 1.5);
  await expect(video).toHaveJSProperty("paused", false);
});

test("a paused quality change preserves its playhead, then the next scene starts independently", async ({
  page,
}) => {
  const video = await open(page);
  await revealControls(page);
  const row = page.locator("[data-player-control-row]");
  await row.getByRole("button", { name: "Pause", exact: true }).click();
  await video.evaluate((v: HTMLVideoElement) => {
    v.currentTime = 4;
  });
  // A late freeze-frame encode must not put an old scene's poster onto the
  // shared video after readiness or a subsequent scene change.
  await page.evaluate(() => {
    const toBlob = HTMLCanvasElement.prototype.toBlob;
    HTMLCanvasElement.prototype.toBlob = function (callback, type, quality) {
      toBlob.call(
        this,
        (blob) => {
          setTimeout(() => callback(blob), 1500);
        },
        type,
        quality,
      );
    };
  });
  await row.getByRole("button", { name: "Quality", exact: true }).click();
  const playlistRequest = page.waitForRequest(
    (request) =>
      request.url().includes("stream.master.m3u8") &&
      request.url().includes("start=4"),
  );
  await page
    .getByRole("menuitemradio", { name: "HLS (240p)", exact: true })
    .click();
  await playlistRequest;
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect(video).toHaveJSProperty("paused", true);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeCloseTo(4, 0);
  await next(page, 1);
  await expect(video).toHaveJSProperty("paused", false);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeLessThan(3);
  await next(page, 2);
  await expect(
    page.getByRole("link", { name: "Scene 3", exact: true }),
  ).toBeVisible();
  await expect(video).toHaveJSProperty("paused", false);
  await expect
    .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeLessThan(3);
  await page.waitForTimeout(1600); // Deliver the deliberately delayed JPEG callback.
  await expect(video).not.toHaveAttribute("poster");
});

test("marker boundaries advance once per clip on the persistent player", async ({
  page,
}) => {
  await page.addInitScript(() =>
    localStorage.setItem("stash-player-auto-advance", "true"),
  );
  const video = await open(page, "?mode=markers");
  for (const index of [1, 0, 1]) {
    await expect(video).toHaveJSProperty("paused", false);
    await video.evaluate((v: HTMLVideoElement) => {
      v.currentTime = v.duration - 0.15;
    });
    await expect(page.getByTestId("view")).toHaveText(String(index));
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
    await expect(video).toHaveJSProperty("paused", false);
    await expect(video).toHaveJSProperty("muted", false);
  }
});

test("a marker loop resumes at the clip start even after native EOF pauses the element", async ({
  page,
}) => {
  await page.addInitScript(() =>
    localStorage.setItem("stash-lightbox-loop", "true"),
  );
  const video = await open(page, "?mode=markers");
  for (let iteration = 0; iteration < 2; iteration += 1) {
    await expect(video).toHaveJSProperty("paused", false);
    await video.evaluate((v: HTMLVideoElement) => {
      v.currentTime = v.duration - 0.1;
    });
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeLessThan(0.5);
    await expect(video).toHaveJSProperty("paused", false);
    await expect(page.getByTestId("view")).toHaveText("0");
  }
});
