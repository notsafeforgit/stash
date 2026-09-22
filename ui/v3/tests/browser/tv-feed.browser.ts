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
async function choose(page: Page, label: "Scenes" | "Markers" | "Both") {
  await page.getByRole("button", { name: "Feed", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Feed", exact: true });
  await expectTouchTargets(dialog);
  await dialog.getByRole("button", { name: label, exact: true }).click();
  await expect(dialog).toBeHidden();
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
    test("switches all three feeds and restores the previous scene selection", async ({
      page,
    }) => {
      await page.goto("/tv-fixture/tv?paused&feed-action");
      await ready(page, /scene:1$/);
      await next(page);
      await ready(page, /scene:2$/);
      await page.locator("video").evaluate((video: HTMLVideoElement) => {
        window.tvFixtureVideo = video;
      });
      await choose(page, "Markers");
      await expect(page).toHaveURL(/mode=markers/);
      await ready(page, /marker:\d+$/);
      await choose(page, "Both");
      await expect(page).toHaveURL(/mode=both/);
      await ready(page, /scene:1$/);
      await next(page);
      await ready(page, /marker:\d+$/);
      await next(page);
      await ready(page, /scene:2$/);
      await choose(page, "Scenes");
      await ready(page, /scene:2$/);
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

test("adds and pins Feed through the existing rail editor and saves Both as a default", async ({
  page,
}) => {
  await page.goto("/tv-fixture/settings/tv?paused");
  await page.getByRole("combobox", { name: "New action", exact: true }).click();
  await page.getByRole("option", { name: "Feed", exact: true }).click();
  await page.getByRole("button", { name: "Add action", exact: true }).click();
  await page.getByRole("button", { name: "Pin Feed", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Default feed", exact: true })
    .click();
  await page.getByRole("option", { name: "Both", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => window.tvFixtureSaveAttempts.at(-1)))
    .toMatchObject({
      key: "tv",
      value: {
        mode: "both",
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
  await ready(page, /scene:1$/);
  await page
    .locator("[data-tv-dock]")
    .getByRole("button", { name: "Feed", exact: true })
    .click();
  await expect(
    page.getByRole("dialog").getByRole("button", { name: "Both", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
});

test("can switch away from an empty scene feed", async ({ page }) => {
  await page.goto("/tv-fixture/tv?paused&empty");
  await expect(
    page.getByText("No matching items", { exact: true }),
  ).toBeVisible();
  await choose(page, "Markers");
  await ready(page, /marker:\d+$/);
});
