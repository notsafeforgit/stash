import type { BrowserContext, Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

type Point = { x: number; y: number };
async function swipe(
  page: Page,
  context: BrowserContext,
  browserName: string,
  start: Point,
  end: Point,
) {
  if (browserName === "chromium") {
    const input = await context.newCDPSession(page);
    try {
      await input.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [start],
      });
      for (let step = 1; step <= 10; step++) {
        await input.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [
            {
              x: start.x + ((end.x - start.x) * step) / 10,
              y: start.y + ((end.y - start.y) * step) / 10,
            },
          ],
        });
      }
      await input.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
    } finally {
      await input.detach();
    }
  } else {
    // Playwright has no native WebKit swipe API. Retain real pointer capture.
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 10 });
    await page.mouse.up();
  }
}

async function open(page: Page) {
  await serveSceneMedia(page);
  await page.goto("/tv-fixture/tv?paused");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
}

for (const surface of [
  "rail",
  "mute",
  "title",
  "padding",
  "timeline",
  "information",
] as const) {
  for (const gesture of ["swipe", "wheel"] as const) {
    test.describe(`${surface} ${gesture}`, () => {
      test.use({ isMobile: gesture === "swipe" });
      test(`${gesture} over ${surface} advances TV without activating the control`, async ({
        page,
        context,
        browserName,
      }) => {
        await open(page);
        const info = page.getByRole("button", {
          name: "Information",
          exact: true,
        });
        if (surface === "information") await info.click();
        const target =
          surface === "rail"
            ? info
            : surface === "mute"
              ? page.getByRole("button", { name: "Unmute", exact: true })
              : surface === "title"
                ? page.locator("[data-tv-dock] p")
                : surface === "timeline"
                  ? page.locator("[data-position-scrubber]")
                  : surface === "information"
                    ? page.locator("[data-tv-info]").getByRole("link").first()
                    : page.locator("[data-tv-dock]");
        const bounds = await target.boundingBox();
        if (!bounds) throw new Error("Missing gesture target");
        const start = {
          x: bounds.x + (surface === "padding" ? 2 : bounds.width / 2),
          y: bounds.y + bounds.height / 2,
        };
        if (gesture === "swipe")
          await swipe(page, context, browserName, start, {
            x: start.x,
            y: start.y - 120,
          });
        else {
          await page.mouse.move(start.x, start.y);
          await page.mouse.wheel(0, 180);
        }
        await expect(page.locator("[data-scene-player]")).toHaveAttribute(
          "data-playback-key",
          /scene:2$/,
        );
        await expect(
          page.locator("video[data-playback-video]"),
        ).toHaveJSProperty("muted", true);
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(page.locator("[data-tv-info]")).toHaveCount(
          surface === "information" ? 1 : 0,
        );
        await expect(page).toHaveURL(/\/tv-fixture\/tv/);
        // The next deliberate tap still works after drag-click suppression.
        await page.getByRole("button", { name: "Unmute", exact: true }).tap();
        await expect(
          page.locator("video[data-playback-video]"),
        ).toHaveJSProperty("muted", false);
      });
    });
  }
}

for (const rotation of ["normal", "clockwise", "counterclockwise"] as const) {
  test(`the ${rotation} timeline separates seeking from feed navigation`, async ({
    page,
    context,
    browserName,
  }) => {
    await open(page);
    if (rotation !== "normal") {
      await page.keyboard.press("o");
      if (rotation === "counterclockwise")
        await page.evaluate(() => {
          const key = Object.keys(localStorage).find((key) =>
            key.startsWith("stash:tv:rotation:v1:"),
          );
          if (!key) throw new Error("Missing rotation setting");
          const newValue = JSON.stringify("counterclockwise");
          localStorage.setItem(key, newValue);
          window.dispatchEvent(new StorageEvent("storage", { key, newValue }));
        });
      await expect(page.locator("[data-tv]")).toHaveAttribute(
        "data-tv-rotation",
        rotation,
      );
    }
    const slider = page.locator("[data-position-scrubber]");
    const bounds = await slider.boundingBox();
    if (!bounds) throw new Error("Missing timeline");
    const start = {
      x: bounds.x + bounds.width / 2,
      y: bounds.y + bounds.height / 2,
    };
    const point = (along: number, across: number) => ({
      x:
        start.x +
        (rotation === "normal"
          ? along
          : rotation === "clockwise"
            ? -across
            : across),
      y:
        start.y +
        (rotation === "normal"
          ? across
          : rotation === "clockwise"
            ? along
            : -along),
    });
    await swipe(page, context, browserName, start, point(60, 0));
    await expect
      .poll(async () => Number(await slider.getAttribute("aria-valuenow")))
      .toBeGreaterThan(6);
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:1$/,
    );
    await swipe(page, context, browserName, start, point(0, -120));
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:2$/,
    );
  });
}

test.describe("TV menu wheel input", () => {
  test.use({ isMobile: false });
  test("an open TV menu keeps wheel input inside the menu", async ({
    page,
  }) => {
    await open(page);
    await page.getByRole("button", { name: "Playback", exact: true }).click();
    const menu = page.getByRole("menu");
    await menu.hover();
    await page.mouse.wheel(0, 180);
    await expect(menu).toBeVisible();
    await expect(page.locator("[data-scene-player]")).toHaveAttribute(
      "data-playback-key",
      /scene:1$/,
    );
  });
});

test("a short button swipe cancels navigation without activating the button", async ({
  page,
  context,
  browserName,
}) => {
  await open(page);
  const button = page.getByRole("button", { name: "Information", exact: true });
  const bounds = await button.boundingBox();
  if (!bounds) throw new Error("Missing information action");
  const start = {
    x: bounds.x + bounds.width / 2,
    y: bounds.y + bounds.height / 2,
  };
  await swipe(page, context, browserName, start, {
    x: start.x,
    y: start.y - 30,
  });
  await expect(page.locator("[data-tv-info]")).toHaveCount(0);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-key",
    /scene:1$/,
  );
  // Chromium suppresses a tap immediately after this CDP touch drag even on
  // a plain HTML button with no handlers. Let its native gesture window end.
  if (browserName === "chromium") await page.waitForTimeout(350);
  await button.tap();
  await expect(page.locator("[data-tv-info]")).toBeVisible();
});
