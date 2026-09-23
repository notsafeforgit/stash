import type { Page } from "@playwright/test";
import { test, expect, expectTouchTargets } from "./test";
import { serveSceneMedia } from "./scene-media";

const player = (page: Page) => page.locator("[data-scene-player]");
async function ready(page: Page, key: RegExp) {
  await expect(player(page)).toHaveAttribute("data-playback-key", key);
  await expect(player(page)).toHaveAttribute("data-playback-ready", "true");
}
async function next(page: Page) {
  await page.evaluate(() => {
    if (document.activeElement instanceof HTMLElement)
      document.activeElement.blur();
  });
  await page.keyboard.press("ArrowDown");
}
async function choose(page: Page, label: "Scenes" | "Markers", touch = false) {
  const toggle = page.getByRole("button", {
    name: label === "Scenes" ? "Switch to scenes" : "Switch to markers",
    exact: true,
  });
  if (touch) await toggle.tap();
  else await toggle.click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
}

test.beforeEach(async ({ page }) => {
  await serveSceneMedia(page);
});

for (const mobile of [true, false]) {
  test.describe(mobile ? "Mobile TV feed" : "Desktop TV feed", () => {
    test.use({
      isMobile: mobile,
      hasTouch: mobile,
      viewport: mobile
        ? { width: 390, height: 844 }
        : { width: 1280, height: 800 },
    });
    test("switches between scenes and markers and restores the previous scene selection", async ({
      page,
    }) => {
      await page.goto("/tv-fixture/tv?paused&feed-action");
      await ready(page, /scene:1$/);
      await next(page);
      await ready(page, /scene:2$/);
      await expectTouchTargets(
        page.getByRole("complementary", { name: "TV actions" }),
      );
      await expect(
        page.getByRole("button", { name: "Switch to markers", exact: true }),
      ).toHaveAccessibleDescription("Current feed: Scenes");
      await page.locator("video").evaluate((video: HTMLVideoElement) => {
        window.tvFixtureVideo = video;
        video.currentTime = 2;
      });
      await expect(page.locator("video")).toHaveJSProperty("seeking", false);
      await choose(page, "Markers", mobile);
      await expect(page).toHaveURL(/mode=markers/);
      await ready(page, /marker:\d+$/);
      await expect(
        page.getByRole("button", { name: "Switch to scenes", exact: true }),
      ).toHaveAccessibleDescription("Current feed: Markers");
      await choose(page, "Scenes", mobile);
      await ready(page, /scene:2$/);
      await expect
        .poll(() =>
          page
            .locator("video")
            .evaluate((video: HTMLVideoElement) => video.currentTime),
        )
        .toBeCloseTo(2, 1);
      await expect(page.locator("video")).toHaveCount(1);
      expect(
        await page
          .locator("video")
          .evaluate((video) => video === window.tvFixtureVideo),
      ).toBe(true);
      expect(
        await page.evaluate(() => window.tvFixtureSaveAttempts),
      ).toHaveLength(0);
    });
  });
}

test("pins Feed, saves the default, and switches directly with reduced motion", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/tv-fixture/settings/tv?paused");
  await page.getByRole("combobox", { name: "New action", exact: true }).click();
  await page.getByRole("option", { name: "Feed", exact: true }).click();
  await page.getByRole("button", { name: "Add action", exact: true }).click();
  await page.getByRole("button", { name: "Pin Feed", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Default feed", exact: true })
    .click();
  await page.getByRole("option", { name: "Markers", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => window.tvFixtureSaveAttempts.at(-1)))
    .toMatchObject({
      key: "tv",
      value: {
        mode: "markers",
        rail: expect.arrayContaining([
          {
            type: "action",
            pinned: true,
            action: {
              kind: "feed",
              id: expect.any(String),
              label: "",
              icon: "default",
            },
          },
        ]),
      },
    });
  await page.getByRole("link", { name: "Return to TV", exact: true }).click();
  await ready(page, /marker:\d+$/);
  const toggle = page
    .locator("[data-tv-dock]")
    .getByRole("button", { name: "Switch to scenes", exact: true });
  await expect(toggle).toHaveAccessibleDescription("Current feed: Markers");
  await expect(toggle.locator("svg.lucide-bookmark")).toBeVisible();
  await toggle.click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await ready(page, /scene:1$/);
  await expect(page.locator("[data-tv] [data-content-reveal]")).toBeHidden();
  await expect(
    page
      .locator("[data-tv-dock]")
      .getByRole("button", { name: "Switch to markers", exact: true })
      .locator("svg.lucide-list-video"),
  ).toBeVisible();
});

test("can switch away from an empty scene feed", async ({ page }) => {
  await page.goto("/tv-fixture/tv?paused&empty");
  await expect(
    page.getByText("No matching items", { exact: true }),
  ).toBeVisible();
  await choose(page, "Markers");
  await ready(page, /marker:\d+$/);
});
