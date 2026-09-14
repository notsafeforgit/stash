import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test.beforeEach(async ({ page }) => {
  await serveSceneMedia(page);
});
async function open(page: Page, query = "?paused") {
  await page.goto(`/tv-fixture/tv${query}`);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect(page.locator("video")).toHaveCount(1);
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    window.tvFixtureVideo = video;
  });
}
async function openSettings(page: Page) {
  await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await page
    .getByRole("main")
    .getByRole("link", { name: "TV", exact: true })
    .click();
  await expect(page).toHaveURL(/\/tv-fixture\/settings\/tv/);
}
async function expectSavedSettings(page: Page, value: Record<string, unknown>) {
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.tvFixtureRequests
          .filter((request) => request.name === "ConfigureUISetting")
          .at(-1),
      ),
    )
    .toMatchObject({ variables: { key: "tv", value } });
}
async function next(page: Page, id: number) {
  // Return keyboard ownership from a previously used playback control.
  await page.evaluate(() => {
    if (document.activeElement instanceof HTMLElement)
      document.activeElement.blur();
  });
  await page.keyboard.press("ArrowDown");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    new RegExp(`scene:${id}$`),
  );
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
}

test("TV keeps one video and three slots across paginated navigation", async ({
  page,
}) => {
  await open(page);
  for (let id = 2; id <= 12; id++) await next(page, id);
  await expect(page.locator("[data-tv-slot]")).toHaveCount(3);
  await expect(page.locator("video")).toHaveCount(1);
  expect(
    await page
      .locator("video")
      .evaluate((video: HTMLVideoElement) => video === window.tvFixtureVideo),
  ).toBe(true);
  const requests = await page.evaluate(() => window.tvFixtureRequests);
  expect(
    requests.filter((request) => request.name === "TvScenes").length,
  ).toBeLessThanOrEqual(4);
  expect(
    requests.filter((request) => request.name === "FindScene").length,
  ).toBeLessThanOrEqual(13);
});

test("TV quality is selected before loading and source changes retain the player", async ({
  page,
}) => {
  const media: string[] = [];
  page.on("request", (request) => {
    if (/\/scene\/\d+\/stream/.test(request.url())) media.push(request.url());
  });
  await open(page, "?paused&low");
  expect(media.some((url) => /\/stream(?:[?#]|$)/.test(url))).toBe(false);
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await page.getByRole("menuitem", { name: "Quality", exact: true }).click();
  await page.getByRole("combobox", { name: "Current item quality" }).click();
  await page
    .getByRole("option", { name: "Direct stream", exact: true })
    .click();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  expect(
    await page
      .locator("video")
      .evaluate((video: HTMLVideoElement) => video === window.tvFixtureVideo),
  ).toBe(true);
  await next(page, 2);
  expect(
    media
      .filter((url) => url.includes("/scene/2/stream"))
      .every((url) => url.includes("resolution=LOW")),
  ).toBe(true);
  expect(
    await page.evaluate(() => localStorage.getItem("stash-player-quality")),
  ).toBeNull();
});

test("TV markers use parent streams and never write activity", async ({
  page,
}) => {
  await open(page, "?markers&low&activity&paused");
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(0.5);
  await openSettings(page);
  await expect(
    page.getByRole("heading", { name: "TV settings", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(() =>
      window.tvFixtureRequests.filter(
        (request) =>
          request.name === "SceneSaveActivity" ||
          request.name === "SceneAddPlay",
      ),
    ),
  ).toEqual([]);
});

test("TV hides unsupported fullscreen and retains inline video, rotation and navigation", async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(document, "fullscreenEnabled", {
      configurable: true,
      value: false,
    });
    Object.defineProperty(HTMLVideoElement.prototype, "webkitEnterFullscreen", {
      configurable: true,
      value: () => {
        window.tvFixtureNativeFullscreen++;
      },
    });
  });
  await open(page);
  await page.keyboard.press("f");
  await expect(page.locator("[data-tv]")).toHaveAttribute(
    "data-tv-presentation",
    "normal",
  );
  expect(
    await page
      .locator("video")
      .evaluate(
        (video: HTMLVideoElement) => video.playsInline && !video.controls,
      ),
  ).toBe(true);
  await page.keyboard.press("o");
  await expect(page.locator("[data-tv]")).toHaveAttribute(
    "data-tv-rotation",
    "clockwise",
  );
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await expect(
    page.getByRole("menuitem", { name: "Fullscreen", exact: true }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: "Show or hide controls", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Show TV controls", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Navigation" })).toBeVisible();
  expect(await page.evaluate(() => window.tvFixtureNativeFullscreen)).toBe(0);
  await page.getByRole("link", { name: "Scenes", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Scenes", exact: true }),
  ).toBeVisible();
  await expect(page.locator("video")).toHaveCount(0);
});

test("a rejected fullscreen request leaves the ordinary TV layout and hides the action", async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(document, "fullscreenEnabled", {
      configurable: true,
      value: true,
    });
    Element.prototype.requestFullscreen = () =>
      Promise.reject(new Error("Fullscreen unavailable"));
  });
  await open(page);
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await page.getByRole("menuitem", { name: "Fullscreen", exact: true }).click();
  await expect(page.locator("[data-tv]")).toHaveAttribute(
    "data-tv-presentation",
    "normal",
  );
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await expect(
    page.getByRole("menuitem", { name: "Fullscreen", exact: true }),
  ).toHaveCount(0);
});

test("TV is a top-level page with navigation instead of exit and item buttons", async ({
  page,
}) => {
  await open(page);
  await expect(
    page.getByRole("button", {
      name: /^(Exit TV|Exit immersive mode|Previous TV item|Next TV item)$/,
    }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/\/tv-fixture\/tv\?/);
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await expect(page.locator("video")).toHaveJSProperty("paused", true);
  await page.keyboard.press("ArrowDown");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /scene:1$/,
  );
  await page.getByRole("link", { name: "Scenes", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Scenes", exact: true }),
  ).toBeVisible();
  await expect(page.locator("video")).toHaveCount(0);
});

test("TV exposes mute at the bottom and uses video taps instead of transport buttons", async ({
  page,
}) => {
  await open(page);
  const dock = page.locator("[data-tv-dock]");
  const surface = page.locator("[data-tv-play-surface]");
  await expect(
    page.getByRole("button", { name: "TV settings", exact: true }),
  ).toHaveCount(0);
  await expect(
    dock.getByRole("button", { name: /^(Play|Pause|Seek to .* marker)$/ }),
  ).toHaveCount(0);
  const unmute = dock.getByRole("button", { name: "Unmute", exact: true });
  await expect(unmute).toBeVisible();
  for (const name of ["Navigation", "Unmute"]) {
    const bounds = await page
      .getByRole("button", { name, exact: true })
      .boundingBox();
    expect(bounds?.y).toBeGreaterThan(844 * 0.45);
  }
  await unmute.click();
  await expect(page.locator("video")).toHaveJSProperty("muted", false);
  await surface.tap({ position: { x: 150, y: 250 } });
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await surface.tap({ position: { x: 150, y: 250 } });
  await expect(page.locator("video")).toHaveJSProperty("paused", true);
  await page.keyboard.press("h");
  await expect(
    page.getByRole("button", { name: "Show TV controls", exact: true }),
  ).toBeVisible();
  await dock.getByRole("button", { name: "Mute", exact: true }).click();
  await expect(page.locator("video")).toHaveJSProperty("muted", true);
  await expect(
    dock.getByRole("button", { name: "Unmute", exact: true }),
  ).toBeVisible();
});

for (const viewport of [
  { name: "mobile", width: 390, height: 844, isMobile: true, hasTouch: true },
  {
    name: "desktop",
    width: 1280,
    height: 800,
    isMobile: false,
    hasTouch: false,
  },
]) {
  test.describe(`TV settings shortcuts on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.isMobile,
      hasTouch: viewport.hasTouch,
    });
    test("removes the default gear and supports optional direct links in the dock and folders", async ({
      page,
    }, testInfo) => {
      const editorRequests: string[] = [];
      page.on("request", (request) => {
        if (new URL(request.url()).pathname.endsWith("/tv-rail-editor.tsx"))
          editorRequests.push(request.url());
      });
      await open(page, "?paused&legacy-gear");
      await next(page, 2);
      const shortcut = page.getByRole("button", {
        name: "TV settings",
        exact: true,
      });
      await expect(shortcut).toHaveCount(0);
      await page.keyboard.press("h");
      await expect(
        page.getByRole("button", { name: "Show TV controls", exact: true }),
      ).toBeVisible();
      await expect(shortcut).toHaveCount(0);
      expect(editorRequests).toEqual([]);
      await openSettings(page);

      const returnToTv = async () => {
        await page
          .getByRole("link", { name: "Return to TV", exact: true })
          .click();
        await expect(page.locator("[data-scene-player]")).toHaveAttribute(
          "data-playback-key",
          /scene:2$/,
        );
      };

      await page
        .getByRole("combobox", { name: "New action", exact: true })
        .click();
      await page
        .getByRole("option", { name: "TV settings", exact: true })
        .click();
      await page
        .getByRole("button", { name: "Add action", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Remove TV settings", exact: true }),
      ).toBeEnabled();
      await page
        .getByRole("switch", { name: "Pin TV settings", exact: true })
        .check();
      await returnToTv();
      await expect(
        page
          .locator("[data-tv-dock]")
          .getByRole("button", { name: "TV settings", exact: true }),
      ).toBeVisible();
      await shortcut.click();
      await expect(page).toHaveURL(/\/tv-fixture\/settings\/tv/);
      await expect(page.getByRole("dialog")).toHaveCount(0);

      await shortcut.click();
      await page
        .getByRole("combobox", { name: "Move into folder", exact: true })
        .click();
      await page.getByRole("option", { name: "Playback", exact: true }).click();
      await returnToTv();
      await expect(shortcut).toHaveCount(0);
      await page.getByRole("button", { name: "Playback", exact: true }).click();
      const folderShortcut = page.getByRole("menuitem", {
        name: "TV settings",
        exact: true,
      });
      await expect(folderShortcut).toBeVisible();
      await page.screenshot({
        path: testInfo.outputPath("tv-settings-folder.png"),
      });
      await folderShortcut.click();
      await expect(page).toHaveURL(/\/tv-fixture\/settings\/tv/);
      await expect(page.getByRole("dialog")).toHaveCount(0);

      await page.getByRole("button", { name: /^Playback \(\d+\)$/ }).click();
      const settingsRow = page
        .locator("p")
        .filter({ hasText: /^TV settings$/ })
        .locator("..");
      await settingsRow
        .getByRole("button", { name: "Remove", exact: true })
        .click();
      await returnToTv();
      await expect(shortcut).toHaveCount(0);
      await page.getByRole("button", { name: "Playback", exact: true }).click();
      await expect(
        page.getByRole("menuitem", { name: "TV settings", exact: true }),
      ).toHaveCount(0);
    });
  });
}

for (const mode of ["scenes", "markers"] as const) {
  test(`TV ${mode} respect the saved startup mute preference and retain manual audio changes`, async ({
    page,
  }) => {
    await open(page, mode === "markers" ? "?paused&markers" : "?paused");
    await expect(page.locator("video")).toHaveJSProperty("muted", true);
    for (const startMuted of [false, true]) {
      await openSettings(page);
      const preference = page.getByRole("switch", {
        name: "Start muted",
        exact: true,
      });
      await expect(preference).toBeChecked({ checked: !startMuted });
      await preference.setChecked(startMuted);
      await expect
        .poll(() =>
          page.evaluate(() =>
            window.tvFixtureRequests
              .filter((request) => request.name === "ConfigureUISetting")
              .at(-1),
          ),
        )
        .toMatchObject({ variables: { key: "tv", value: { startMuted } } });
      await page
        .getByRole("link", { name: "Return to TV", exact: true })
        .click();
      const video = page.locator("video");
      await expect(video).toHaveJSProperty("muted", startMuted);
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      await video.evaluate((element: HTMLVideoElement) => {
        window.tvFixtureVideo = element;
      });
      const selection = await page
        .locator("[data-scene-player]")
        .getAttribute("data-playback-key");
      await page
        .getByRole("button", {
          name: startMuted ? "Unmute" : "Mute",
          exact: true,
        })
        .click();
      await expect(video).toHaveJSProperty("muted", !startMuted);
      await page.evaluate(() => {
        if (document.activeElement instanceof HTMLElement)
          document.activeElement.blur();
      });
      await page.keyboard.press("ArrowDown");
      await expect(page.locator("[data-scene-player]")).not.toHaveAttribute(
        "data-playback-key",
        selection ?? "",
      );
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      await expect(video).toHaveJSProperty("muted", !startMuted);
      expect(
        await video.evaluate((element) => element === window.tvFixtureVideo),
      ).toBe(true);
    }
    expect(
      await page.evaluate(
        () =>
          window.tvFixtureRequests.filter(
            (request) => request.name === "ConfigureUISetting",
          ).length,
      ),
    ).toBe(2);
  });
}

test("holding the video uses 2x, consumes movement and release, and restores the saved rate", async ({
  page,
  context,
  browserName,
}) => {
  await open(page, "?advance");
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await page
    .locator("[data-tv-play-surface]")
    .tap({ position: { x: 150, y: 250 } });
  await expect(page.locator("video")).toHaveJSProperty("paused", true);
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await page
    .getByRole("menuitem", { name: "Playback speed", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "Playback speed", exact: true })
    .click();
  await page.getByRole("option", { name: "1.5×", exact: true }).click();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  const input =
    browserName === "chromium" ? await context.newCDPSession(page) : undefined;
  const begin = async () => {
    if (input)
      await input.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [{ x: 150, y: 250 }],
      });
    else {
      await page.mouse.move(150, 250);
      await page.mouse.down();
    }
  };
  const release = async () => {
    if (input)
      await input.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
    else await page.mouse.up();
  };
  try {
    await begin();
    await expect(page.locator("video")).toHaveJSProperty("playbackRate", 2);
    await expect(page.locator("video")).toHaveJSProperty("paused", false);
    if (input)
      await input.send("Input.dispatchTouchEvent", {
        type: "touchMove",
        touchPoints: [{ x: 150, y: 400 }],
      });
    else await page.mouse.move(150, 400, { steps: 5 });
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:1$/,
    );
    await release();
    await expect(page.locator("video")).toHaveJSProperty("playbackRate", 1.5);
    // Wait through the deferred single-tap window; releasing a hold is consumed.
    await page.waitForTimeout(250);
    await expect(page.locator("video")).toHaveJSProperty("paused", true);
    await page
      .locator("[data-tv-play-surface]")
      .tap({ position: { x: 150, y: 250 } });
    await expect(page.locator("video")).toHaveJSProperty("paused", false);
    await begin();
    await expect(page.locator("video")).toHaveJSProperty("playbackRate", 2);
    await page.locator("video").evaluate((video: HTMLVideoElement) => {
      video.currentTime = video.duration - 0.1;
    });
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:2$/,
    );
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
    await expect(page.locator("video")).toHaveJSProperty("playbackRate", 2);
    await release();
    await expect(page.locator("video")).toHaveJSProperty("playbackRate", 1.5);
    await expect(page.locator("video")).toHaveJSProperty("paused", false);
    expect(
      await page
        .locator("video")
        .evaluate((video) => video === window.tvFixtureVideo),
    ).toBe(true);
    await begin();
    await expect(page.locator("video")).toHaveJSProperty("playbackRate", 2);
    await page.evaluate(() => window.dispatchEvent(new Event("blur")));
    await expect(page.locator("video")).toHaveJSProperty("playbackRate", 1.5);
    await release();
  } finally {
    await input?.detach();
  }
});

test("double-tap zoom owns the gesture without changing playback", async ({
  page,
}) => {
  await open(page);
  const surface = page.locator("[data-tv-play-surface]");
  await surface.tap({ position: { x: 150, y: 250 } });
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await surface.tap({ position: { x: 150, y: 250 } });
  await surface.tap({ position: { x: 150, y: 250 } });
  await expect(
    page.getByRole("button", { name: "Reset zoom", exact: true }),
  ).toBeVisible();
  await page.waitForTimeout(550);
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await expect(page.locator("video")).toHaveJSProperty("playbackRate", 1);
});

for (const device of ["mobile", "desktop"] as const) {
  test.describe(`TV info on ${device}`, () => {
    test.use(
      device === "mobile"
        ? {
            viewport: { width: 390, height: 844 },
            isMobile: true,
            hasTouch: true,
          }
        : {
            viewport: { width: 1280, height: 800 },
            isMobile: false,
            hasTouch: false,
          },
    );
    test("long titles and many tags wrap without horizontal overflow and Close stays reachable", async ({
      page,
    }, testInfo) => {
      await open(page, "?paused&long-info");
      await page.screenshot({
        path: testInfo.outputPath(`tv-controls-${device}.png`),
      });
      await page
        .getByRole("button", { name: "Information", exact: true })
        .click();
      const dialog = page.getByRole("dialog", {
        name: "Information",
        exact: true,
      });
      const body = dialog.locator("[data-tv-dialog-body]");
      await expect(dialog).toBeVisible();
      expect(
        await dialog.evaluate(
          (element) => element.scrollWidth <= element.clientWidth,
        ),
      ).toBe(true);
      expect(
        await body.evaluate(
          (element) => element.scrollWidth <= element.clientWidth,
        ),
      ).toBe(true);
      const title = dialog.getByRole("link", { name: /^Scene 1 / });
      expect(
        await title.evaluate(
          (element) => element.scrollWidth <= element.clientWidth,
        ),
      ).toBe(true);
      const lastTag = dialog.getByRole("link", { name: /^Tag 59 / });
      await lastTag.scrollIntoViewIfNeeded();
      await expect(lastTag).toBeVisible();
      expect(
        await body.evaluate((element) => {
          const bounds = element.getBoundingClientRect();
          return Array.from(
            element.querySelectorAll('[data-slot="badge"]'),
          ).every((badge) => {
            const rect = badge.getBoundingClientRect();
            return (
              rect.left >= bounds.left - 1 && rect.right <= bounds.right + 1
            );
          });
        }),
      ).toBe(true);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: testInfo.outputPath(`tv-info-${device}.png`),
      });
      await dialog.getByRole("button", { name: "Close", exact: true }).click();
      await expect(dialog).toHaveCount(0);
      await expect(page.locator("video")).toHaveJSProperty("paused", true);
    });
  });
}

for (const [query, title] of [
  ["empty", "No matching items"],
  ["missing", "Media unavailable"],
  ["feed-loading", "Loading TV…"],
] as const) {
  test(`TV navigation is available during ${title}`, async ({ page }) => {
    await page.goto(`/tv-fixture/tv?paused&${query}`);
    await expect(page.getByText(title, { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Navigation", exact: true }).click();
    await page.getByRole("link", { name: "Scenes", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Scenes", exact: true }),
    ).toBeVisible();
  });
}

test("vertical gestures change videos in both directions while retaining one player", async ({
  page,
  context,
  browserName,
}) => {
  await open(page);
  const input =
    browserName === "chromium" ? await context.newCDPSession(page) : undefined;
  const swipe = async (from: number, to: number) => {
    if (input) {
      await input.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [{ x: 150, y: from }],
      });
      for (let step = 1; step <= 10; step++) {
        await input.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [{ x: 150, y: from + ((to - from) * step) / 10 }],
        });
      }
      await input.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
    } else {
      // Playwright exposes no native WebKit swipe. Use a real pointer drag
      // through the same handler, including the browser's pointer capture.
      await page.mouse.move(150, from);
      await page.mouse.down();
      await page.mouse.move(150, to, { steps: 10 });
      await page.mouse.up();
    }
  };
  try {
    await swipe(500, 250);
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:2$/,
    );
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
    await swipe(250, 500);
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:1$/,
    );
    await expect(page.locator("video")).toHaveCount(1);
    expect(
      await page
        .locator("video")
        .evaluate((video) => video === window.tvFixtureVideo),
    ).toBe(true);
  } finally {
    await input?.detach();
  }
});

test.describe("desktop TV", () => {
  test.use({
    isMobile: false,
    hasTouch: false,
    viewport: { width: 1280, height: 800 },
  });
  test("supported fullscreen targets TV and navigation returns to the app", async ({
    page,
  }) => {
    await open(page);
    const supported = await page.evaluate(
      () =>
        document.fullscreenEnabled &&
        typeof document.documentElement.requestFullscreen === "function",
    );
    await page.getByRole("button", { name: "Playback", exact: true }).click();
    const action = page.getByRole("menuitem", {
      name: "Fullscreen",
      exact: true,
    });
    if (!supported) {
      await expect(action).toHaveCount(0);
      return;
    }
    await action.click();
    await expect(page.locator("[data-tv]")).toHaveAttribute(
      "data-tv-presentation",
      "fullscreen",
    );
    expect(
      await page.evaluate(() =>
        document.fullscreenElement?.hasAttribute("data-tv"),
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Navigation", exact: true }).click();
    await expect(
      page.getByRole("dialog", { name: "Navigation" }),
    ).toBeVisible();
    expect(await page.evaluate(() => document.fullscreenElement)).toBeNull();
    await page.getByRole("link", { name: "Scenes", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Scenes", exact: true }),
    ).toBeVisible();
  });
  test("wheel momentum advances once and keyboard returns to the previous video", async ({
    page,
  }) => {
    await open(page);
    await expect(
      page.getByRole("button", { name: /^(Previous TV item|Next TV item)$/ }),
    ).toHaveCount(0);
    await page.mouse.move(500, 350);
    for (let step = 0; step < 5; step++) await page.mouse.wheel(0, 120);
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:2$/,
    );
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-ready",
      "true",
    );
    await page.keyboard.press("ArrowUp");
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:1$/,
    );
  });
});

test("TV restores feed context after settings without refetching page one", async ({
  page,
}) => {
  await open(page);
  await next(page, 2);
  const before = await page.evaluate(
    () =>
      window.tvFixtureRequests.filter((request) => request.name === "TvScenes")
        .length,
  );
  await openSettings(page);
  await expect(page.locator("video")).toHaveCount(0);
  await page.getByRole("link", { name: "Return to TV" }).click();
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /scene:2$/,
  );
  expect(
    await page.evaluate(
      () =>
        window.tvFixtureRequests.filter(
          (request) => request.name === "TvScenes",
        ).length,
    ),
  ).toBe(before);
});

test("scene activity records watched time on pause and counts once per visit", async ({
  page,
}) => {
  await open(page, "?paused&activity");
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(1);
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          window.tvFixtureRequests.filter(
            (request) => request.name === "SceneAddPlay",
          ).length,
      ),
    )
    .toBe(1);
  await openSettings(page);
  const requests = await page.evaluate(() =>
    window.tvFixtureRequests.filter(
      (request) => request.name === "SceneSaveActivity",
    ),
  );
  expect(requests.length).toBeGreaterThan(0);
  for (const request of requests) {
    expect(request.variables).toMatchObject({
      id: "1",
      playDuration: expect.any(Number),
      resume_time: expect.any(Number),
    });
    const value = request.variables;
    if (
      value &&
      typeof value === "object" &&
      "playDuration" in value &&
      typeof value.playDuration === "number"
    )
      expect(value.playDuration).toBeLessThan(5);
  }
  expect(
    await page.evaluate(
      () =>
        window.tvFixtureRequests.filter(
          (request) => request.name === "SceneAddPlay",
        ).length,
    ),
  ).toBe(1);
});

test("rotated timeline seeking follows the visible track and keeps the item selected", async ({
  page,
}) => {
  await open(page);
  await page.keyboard.press("o");
  const track = page.locator('[data-slot="slider-track"]').first();
  const bounds = await track.boundingBox();
  if (!bounds) throw new Error("Missing timeline track");
  await page.mouse.click(
    bounds.x + bounds.width / 2,
    bounds.y + bounds.height * 0.6,
  );
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(3);
  await expect(page.locator("video")).toHaveJSProperty("paused", true);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /scene:1$/,
  );
});

test("held seeking restores the previous rate and pause state", async ({
  page,
}) => {
  await open(page);
  await page.keyboard.down("ArrowRight");
  await expect(page.locator("video")).toHaveJSProperty("playbackRate", 2);
  await page.keyboard.press("ArrowUp");
  await expect(page.locator("video")).toHaveJSProperty("playbackRate", 4);
  await page.keyboard.up("ArrowRight");
  await expect(page.locator("video")).toHaveJSProperty("playbackRate", 1);
  await expect(page.locator("video")).toHaveJSProperty("paused", true);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /scene:1$/,
  );
});

test("quality changes preserve active playback and menus own navigation keys", async ({
  page,
}) => {
  await open(page);
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(1);
  await page.getByRole("button", { name: "Playback", exact: true }).click();
  await page.getByRole("menuitem", { name: "Quality", exact: true }).click();
  await page.keyboard.press("ArrowDown");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /scene:1$/,
  );
  const quality = page.getByRole("combobox", { name: "Current item quality" });
  if ((await quality.getAttribute("aria-expanded")) !== "true")
    await quality.click();
  await page.getByRole("option", { name: "HLS (240p)", exact: true }).click();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(0.5);
});

test("automatic advance retains audio state and one video", async ({
  page,
}) => {
  await open(page, "?advance");
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await page.keyboard.press("m");
  await expect(page.locator("video")).toHaveJSProperty("muted", false);
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    video.currentTime = video.duration - 0.3;
  });
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /scene:2$/,
  );
  await expect(page.locator("video")).toHaveJSProperty("paused", false);
  await expect(page.locator("video")).toHaveJSProperty("muted", false);
  expect(
    await page
      .locator("video")
      .evaluate((video) => video === window.tvFixtureVideo),
  ).toBe(true);
});

test("TV has one localized sort choice and retains Random between feed modes", async ({
  page,
}) => {
  await page.goto("/tv-fixture/settings/tv?paused");
  const sort = page.getByRole("combobox", { name: "Sort order", exact: true });
  await expect(
    page.getByRole("switch", { name: "Shuffle", exact: true }),
  ).toHaveCount(0);
  await sort.click();
  await page.getByRole("option", { name: "Created At", exact: true }).click();
  await expect(sort).toContainText("Created At");
  await sort.click();
  await page.getByRole("option", { name: "Studio Code", exact: true }).click();
  await expect(sort).toContainText("Studio Code");
  await sort.click();
  await page.getByRole("option", { name: "Random", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Default feed", exact: true })
    .click();
  await page.getByRole("option", { name: "Markers", exact: true }).click();
  await expect(sort).toContainText("Random");
  await sort.click();
  await expect(
    page.getByRole("option", { name: "Scene Updated At", exact: true }),
  ).toBeVisible();
  await page.getByRole("option", { name: "Random", exact: true }).click();
  await expectSavedSettings(page, {
    version: 4,
    sort: "random",
    mode: "markers",
  });
  const saved = await page.evaluate(() =>
    window.tvFixtureRequests
      .filter((request) => request.name === "ConfigureUISetting")
      .at(-1),
  );
  expect(saved?.variables).not.toHaveProperty("value.shuffle");
});

test("TV settings load a legacy Shuffle preference as Random", async ({
  page,
}) => {
  await page.goto("/tv-fixture/settings/tv?paused&legacy-shuffle");
  await expect(
    page.getByRole("combobox", { name: "Sort order", exact: true }),
  ).toContainText("Random");
  await expect(
    page.getByRole("switch", { name: "Shuffle", exact: true }),
  ).toHaveCount(0);
});

test("TV settings show the rail editor directly and save reordered actions without extra rules", async ({
  page,
}) => {
  const editorRequests: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.endsWith("/tv-rail-editor.tsx")) {
      editorRequests.push(request.url());
    }
  });
  await open(page, "?paused&legacy-rules");
  expect(editorRequests).toEqual([]);
  await openSettings(page);
  await expect(
    page.getByRole("combobox", { name: "Scene filter", exact: true }),
  ).toContainText("Scene picks");
  await expect(
    page.getByRole("combobox", { name: "Marker filter", exact: true }),
  ).toContainText("Marker picks");
  await expect(
    page.getByRole("button", { name: "Customize action rail", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("Additional feed rules", { exact: true }),
  ).toHaveCount(0);
  const drag = page.getByRole("button", {
    name: "Drag Information",
    exact: true,
  });
  await expect(drag).toBeVisible();
  expect(editorRequests).toHaveLength(1);
  await drag.focus();
  await page.keyboard.press("Space");
  await expect(drag).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("status")).toContainText(
    "Draggable item info was moved over droppable area info.",
  );
  await page.keyboard.press("ArrowUp");
  await expect(page.getByRole("status")).toContainText(
    "Draggable item info was moved over droppable area visibility.",
  );
  await page.keyboard.press("Space");
  await expect(drag).not.toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Information", exact: true }).click();
  const icon = page.getByRole("combobox", { name: "Action icon", exact: true });
  await expect(icon).toContainText("Default");
  await icon.click();
  await page.getByRole("option", { name: "Heart", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.tvFixtureRequests
          .filter((request) => request.name === "ConfigureUISetting")
          .at(-1),
      ),
    )
    .toMatchObject({
      variables: {
        key: "tv",
        value: {
          version: 4,
          sceneFilter: { kind: "saved", id: "1" },
          markerFilter: { kind: "saved", id: "2" },
          rail: [
            { action: { id: "info", icon: "heart" } },
            { action: { id: "visibility" } },
            { action: { id: "counter" } },
            { id: "edit" },
            { id: "playback" },
          ],
        },
      },
    });
  const saved = await page.evaluate(() =>
    window.tvFixtureRequests
      .filter((request) => request.name === "ConfigureUISetting")
      .at(-1),
  );
  expect(saved?.variables).not.toHaveProperty("value.rules");
});

test("TV settings save the shared quality under only the TV key", async ({
  page,
}) => {
  await open(page);
  await openSettings(page);
  await page
    .getByRole("combobox", { name: "Default quality for scenes and markers" })
    .click();
  await page.getByRole("option", { name: "480p", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(() =>
        window.tvFixtureRequests.filter(
          (request) => request.name === "ConfigureUISetting",
        ),
      ),
    )
    .toEqual([
      expect.objectContaining({
        variables: expect.objectContaining({
          key: "tv",
          value: expect.objectContaining({
            defaultQuality: { kind: "fixed", resolution: "STANDARD" },
          }),
        }),
      }),
    ]);
  await page.getByRole("link", { name: "Return to TV" }).click();
  await openSettings(page);
  await expect(
    page.getByRole("combobox", {
      name: "Default quality for scenes and markers",
    }),
  ).toContainText("480p");
});

test("failed TV settings saves retain the draft for retry", async ({
  page,
}) => {
  await open(page, "?paused&save-error");
  await openSettings(page);
  await page
    .getByRole("combobox", { name: "Default quality for scenes and markers" })
    .click();
  await page.getByRole("option", { name: "480p", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          window.tvFixtureRequests.filter(
            (request) => request.name === "ConfigureUISetting",
          ).length,
      ),
    )
    .toBe(1);
  await expect(
    page.getByText("TV settings could not be saved", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Return to TV" }).click();
  await openSettings(page);
  await expect(
    page.getByRole("combobox", {
      name: "Default quality for scenes and markers",
    }),
  ).toContainText("480p");
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          window.tvFixtureRequests.filter(
            (request) => request.name === "ConfigureUISetting",
          ).length,
      ),
    )
    .toBe(2);
  await expect(
    page.getByText("TV settings could not be saved", { exact: true }),
  ).toHaveCount(0);
});

test("TV settings commit numbers and rail text without a Save or Reset toolbar", async ({
  page,
}) => {
  await page.goto("/tv-fixture/settings/tv?paused");
  await expect(
    page.getByRole("button", {
      name: /^(Save TV settings|Reset TV settings)$/,
    }),
  ).toHaveCount(0);
  const pageSize = page.getByRole("spinbutton", {
    name: "Items per page",
    exact: true,
  });
  await pageSize.fill("12");
  expect(await page.evaluate(() => window.tvFixtureSaveAttempts)).toEqual([]);
  await pageSize.press("Enter");
  await expectSavedSettings(page, { pageSize: 12 });
  await expect(pageSize).toBeFocused();
  await expect(page.getByLabel("Saved", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: /^Playback \(\d+\)$/ }).click();
  const name = page.getByRole("textbox", { name: "Folder name", exact: true });
  const before = await page.evaluate(() => window.tvFixtureSaveAttempts.length);
  await name.fill("");
  await name.press("Tab");
  await expect(
    page.getByText("Check TV settings", { exact: true }),
  ).toBeVisible();
  expect(await page.evaluate(() => window.tvFixtureSaveAttempts.length)).toBe(
    before,
  );
  await name.fill("Viewing");
  expect(await page.evaluate(() => window.tvFixtureSaveAttempts.length)).toBe(
    before,
  );
  await name.press("Tab");
  await expectSavedSettings(page, {
    pageSize: 12,
    rail: expect.arrayContaining([
      expect.objectContaining({ id: "playback", label: "Viewing" }),
    ]),
  });
  await expect(
    page.getByText("Check TV settings", { exact: true }),
  ).toHaveCount(0);
});

for (const failure of [false, true]) {
  test(`TV preserves newer automatic edits across navigation when an older save ${failure ? "fails" : "finishes"}`, async ({
    page,
  }) => {
    await page.goto(
      `/tv-fixture/settings/tv?paused&slow-save${failure ? "&save-error" : ""}`,
    );
    await page
      .getByRole("switch", { name: "Start muted", exact: true })
      .uncheck();
    await expect
      .poll(() => page.evaluate(() => window.tvFixtureSaveAttempts.length))
      .toBe(1);
    await page
      .getByRole("switch", {
        name: "Place the action rail on the left",
        exact: true,
      })
      .check();
    await page.getByRole("link", { name: "Return to TV", exact: true }).click();
    await expectSavedSettings(page, { startMuted: false, leftHanded: true });
    await openSettings(page);
    await expect(
      page.getByRole("switch", { name: "Start muted", exact: true }),
    ).not.toBeChecked();
    await expect(
      page.getByRole("switch", {
        name: "Place the action rail on the left",
        exact: true,
      }),
    ).toBeChecked();
    await expect(
      page.getByText("TV settings could not be saved", { exact: true }),
    ).toHaveCount(0);
  });
}
