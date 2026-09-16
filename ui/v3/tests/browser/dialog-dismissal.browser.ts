import type { Locator, Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

async function openTvAction(page: Page, action: string) {
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page.getByRole("menuitem", { name: action, exact: true }).click();
}

async function expectSingleCancel(dialog: Locator) {
  await expect(
    dialog.getByRole("button", { name: "Cancel", exact: true }),
  ).toHaveCount(1);
  await expect(
    dialog.getByRole("button", { name: "Close", exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "Cancel", exact: true }),
  ).toBeInViewport();
  expect(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(
    true,
  );
}

for (const viewport of [
  { width: 320, height: 568 },
  { width: 390, height: 844 },
  { width: 1280, height: 800 },
]) {
  test.describe(`Dialog dismissal at ${viewport.width}px`, () => {
    test.use({
      viewport,
      isMobile: viewport.width < 768,
      hasTouch: viewport.width < 768,
    });

    test("TV marker form keeps one Cancel beside Save while its fields scroll", async ({
      page,
    }, testInfo) => {
      await serveSceneMedia(page, "portrait");
      await page.goto("/tv-fixture/tv?paused&portrait&long-info");
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      await openTvAction(page, "Create marker");
      const dialog = page.getByRole("dialog", {
        name: "Create marker",
        exact: true,
      });
      await expectSingleCancel(dialog);
      const cancel = dialog.getByRole("button", {
        name: "Cancel",
        exact: true,
      });
      const save = dialog.getByRole("button", { name: "Save", exact: true });
      await expect(save).toBeInViewport();
      await expect(save).toBeDisabled();
      await expect
        .poll(() =>
          dialog.evaluate((element) =>
            element
              .getAnimations()
              .some((animation) => animation.playState === "running"),
          ),
        )
        .toBe(false);
      const before = await cancel.boundingBox();
      await dialog.getByRole("combobox").first().click();
      await page.getByRole("option", { name: "Example", exact: true }).click();
      await expect(save).toBeEnabled();
      await dialog.getByRole("combobox").last().scrollIntoViewIfNeeded();
      await expect(cancel).toBeInViewport();
      const after = await cancel.boundingBox();
      expect(before && after && Math.abs(before.y - after.y)).toBeLessThan(1);
      await page.screenshot({
        path: testInfo.outputPath("tv-marker-actions.png"),
      });
      await cancel.click();
      await expect(dialog).toHaveCount(0);
      expect(
        await page.evaluate(() =>
          window.tvFixtureRequests.filter(
            (r) => r.name === "SceneMarkerCreate",
          ),
        ),
      ).toEqual([]);

      await openTvAction(page, "Create marker");
      await dialog.getByRole("combobox").first().click();
      await page.getByRole("option", { name: "Example", exact: true }).click();
      await save.click();
      await expect(cancel).toBeDisabled();
      await expect(save).toBeDisabled();
      await expect(dialog).toHaveCount(0);
      expect(
        await page.evaluate(() =>
          window.tvFixtureRequests.filter(
            (r) => r.name === "SceneMarkerCreate",
          ),
        ),
      ).toEqual([
        {
          name: "SceneMarkerCreate",
          variables: {
            scene_id: "1",
            title: "",
            seconds: 0,
            end_seconds: null,
            primary_tag_id: "tag",
            tag_ids: [],
          },
        },
      ]);
    });

    test("TV tags cancel without saving and content dialogs retain one Close", async ({
      page,
    }) => {
      await serveSceneMedia(page, "portrait");
      await page.goto("/tv-fixture/tv?paused&portrait");
      await expect(page.locator("[data-scene-player]")).toHaveAttribute(
        "data-playback-ready",
        "true",
      );
      await openTvAction(page, "Edit tags");
      const tags = page.getByRole("dialog", { name: "Edit tags", exact: true });
      await expectSingleCancel(tags);
      await tags.getByRole("button", { name: "Cancel", exact: true }).click();
      await expect(tags).toHaveCount(0);
      expect(
        await page.evaluate(() =>
          window.tvFixtureRequests.filter((r) => r.name === "SceneUpdate"),
        ),
      ).toEqual([]);
      await openTvAction(page, "Rating");
      const rating = page.getByRole("dialog", { name: "Rating", exact: true });
      await expect(
        rating.getByRole("button", { name: "Cancel", exact: true }),
      ).toHaveCount(0);
      const close = rating.getByRole("button", { name: "Close", exact: true });
      await expect(close).toHaveCount(1);
      await expect(close).toBeInViewport();
      await close.click();
      await expect(rating).toHaveCount(0);
    });

    for (const [trigger, title, action] of [
      ["Open confirmation", "Confirm action", "Confirm"],
      ["Open bulk editor", "Bulk editor", "Save"],
    ] as const) {
      test(`${title} has one dismissal, restores focus and preserves its primary action`, async ({
        page,
      }) => {
        await page.goto("/dialog-dismissal");
        const opener = page.getByRole("button", { name: trigger, exact: true });
        const dialog = page.getByRole("dialog", { name: title, exact: true });
        await opener.click();
        await expectSingleCancel(dialog);
        await dialog
          .getByRole("button", { name: "Cancel", exact: true })
          .click();
        await expect(dialog).not.toBeVisible();
        await expect(opener).toBeFocused();
        await expect(page.getByTestId("completed")).toHaveText("0");
        await opener.click();
        await expect(dialog).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(dialog).not.toBeVisible();
        await expect(opener).toBeFocused();
        await opener.click();
        await dialog.getByRole("button", { name: action, exact: true }).click();
        await expect(dialog).not.toBeVisible();
        await expect(page.getByTestId("completed")).toHaveText("1");
      });
    }

    test("search without footer actions keeps an accessible Close button", async ({
      page,
    }) => {
      await page.goto("/dialog-dismissal");
      const opener = page.getByRole("button", {
        name: "Open search",
        exact: true,
      });
      await opener.click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByRole("textbox")).toBeFocused();
      const close = dialog.getByRole("button", { name: "Close", exact: true });
      await expect(close).toHaveCount(1);
      await expect(close).toBeInViewport();
      await expect(
        dialog.getByRole("button", { name: "Cancel", exact: true }),
      ).toHaveCount(0);
      await close.click();
      await expect(dialog).not.toBeVisible();
      await expect(opener).toBeFocused();
    });
  });
}
