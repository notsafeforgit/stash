import type { Locator, Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test.beforeEach(async ({ page }) => {
  await serveSceneMedia(page, "portrait");
});

async function open(page: Page, query: string) {
  await page.goto(`/tv-fixture/tv?paused&portrait&${query}`);
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
}

async function box(locator: Locator) {
  const bounds = await locator.boundingBox();
  if (!bounds) throw new Error("Missing bounds");
  return bounds;
}

function expectSamePlace(
  actual: { x: number; y: number },
  expected: { x: number; y: number },
) {
  expect(Math.abs(actual.x - expected.x)).toBeLessThan(1);
  expect(Math.abs(actual.y - expected.y)).toBeLessThan(1);
}

for (const viewport of [
  { name: "phone", width: 390, height: 844, touch: true },
  { name: "desktop", width: 1280, height: 800, touch: false },
]) {
  test.describe(`TV chrome on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.touch,
      hasTouch: viewport.touch,
    });

    for (const [placement, query] of [
      ["first in the dock", ""],
      ["after a pinned action", "visibility=last&counter=pinned"],
      ["in the rail", "visibility=rail"],
    ] as const) {
      test(`the visibility toggle stays in place ${placement}`, async ({
        page,
      }) => {
        await open(page, query);
        const hide = page.getByRole("button", {
          name: "Show or hide controls",
          exact: true,
        });
        const show = page.getByRole("button", {
          name: "Show TV controls",
          exact: true,
        });
        const shown = await box(hide);
        await hide.click();
        await expect(show).toBeVisible();
        await expect(
          page.locator("[data-tv-dock]").getByRole("slider"),
        ).toHaveCount(0);
        expectSamePlace(await box(show), shown);
        await show.click();
        await expect(hide).toBeVisible();
        expectSamePlace(await box(hide), shown);
      });
    }

    test("the dock names the title and performers without opening information", async ({
      page,
    }) => {
      await open(page, "");
      const summary = page.locator("[data-tv-summary]");
      await expect(summary).toContainText("Scene 1");
      const bare = await box(summary);
      await open(page, "performers");
      await expect(summary).toContainText("Scene 1");
      await expect(summary).toContainText("Ada Example and Blake Example");
      // Items without performers keep the same height, so the rail stays put.
      expect((await box(summary)).height).toBeCloseTo(bare.height, 0);
      await page
        .getByRole("button", { name: "Information", exact: true })
        .click();
      await expect(
        page.getByRole("region", { name: "Information", exact: true }),
      ).toContainText("Ada Example");
      await expect(summary).toHaveAttribute("aria-hidden", "true");
    });
  });
}
