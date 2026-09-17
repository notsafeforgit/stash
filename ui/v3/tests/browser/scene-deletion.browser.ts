import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

test("installed-app lightbox details retain Home's cache and browsing origin", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const matchMedia = window.matchMedia.bind(window);
    window.matchMedia = (query) => {
      const result = matchMedia(query);
      if (query === "(display-mode: standalone)")
        Object.defineProperty(result, "matches", { value: true });
      return result;
    };
  });
  await serveSceneMedia(page);
  await page.goto("/scene-deletion-fixture/");
  const home = page.locator("[data-front-page]");
  const initialHistoryLength = await page.evaluate(() => history.length);
  await home.locator('.scene-card[data-id="1"] .entity-card-preview').click();
  const lightbox = page.locator(".yarl__root");
  await expect(lightbox).toBeVisible();
  await lightbox.getByRole("link", { name: "Scene 1", exact: true }).click();
  await expect(page).toHaveURL(/\/scene-deletion-fixture\/scenes\/1$/);
  await expect(lightbox).toHaveCount(0);
  expect(await page.evaluate(() => history.length)).toBe(
    initialHistoryLength + 1,
  );
  await page
    .getByRole("button", { name: "Entity actions", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Delete scene…", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Delete", exact: true })
    .click();
  await expect(page).toHaveURL(/\/scene-deletion-fixture\/$/);
  await expect(home.locator('.scene-card[data-id="1"]')).toHaveCount(0);
  await page.goBack();
  await expect(
    page.getByText("Scene not found", { exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(home.locator('.scene-card[data-id="2"]')).toBeVisible();
  await expect(home.locator('.scene-card[data-id="1"]')).toHaveCount(0);
  expect(await page.evaluate(() => window.deletionFixture.deleted)).toEqual([
    "1",
  ]);
});

for (const mobile of [true, false]) {
  test.describe(mobile ? "mobile deletion" : "desktop deletion", () => {
    test.use({ viewport: { width: mobile ? 390 : 1280, height: 844 } });
    test("refreshes cached Home when a document is restored from history", async ({
      page,
    }) => {
      await page.goto("/scene-deletion-fixture/");
      const home = page.locator("[data-front-page]");
      await expect(home.locator('.scene-card[data-id="1"]')).toBeVisible();
      const requestsBefore = await page.evaluate(
        () => window.deletionFixture.requests.length,
      );
      await page.evaluate(async () => {
        window.dispatchEvent(
          new PageTransitionEvent("pageshow", { persisted: false }),
        );
        // Initial page display and ordinary visibility changes must not turn
        // cache-first navigation into a network request on every visit.
        document.dispatchEvent(new Event("visibilitychange"));
        await new Promise<void>((resolve) =>
          requestAnimationFrame(() => resolve()),
        );
      });
      expect(
        await page.evaluate(() => window.deletionFixture.requests.length),
      ).toBe(requestsBefore);
      // Model a deletion committed while this document was suspended. This
      // changes the synthetic server only, leaving the original Apollo cache
      // intact. Dispatch the browser's restoration contract explicitly; this
      // fixture's Vite connection prevents reliable native BFCache admission.
      await page.evaluate(() => {
        window.deletionFixture.deleted.push("1");
        window.dispatchEvent(
          new PageTransitionEvent("pageshow", { persisted: true }),
        );
      });
      await expect(home.locator('.scene-card[data-id="1"]')).toHaveCount(0);
      await expect(home.locator('.scene-card[data-id="2"]')).toBeVisible();
      expect(
        await page.evaluate(
          (offset) => window.deletionFixture.requests.slice(offset),
          requestsBefore,
        ),
      ).toEqual(["FindScenes"]);
    });
    for (const { source, history } of [
      { source: "detail", history: false },
      { source: "list", history: false },
      { source: "detail", history: true },
    ] as const) {
      test(`deleting from ${source} removes the scene from cached Home via ${history ? "two browser Back actions" : "navigation"} without a reload`, async ({
        page,
      }) => {
        await serveSceneMedia(page);
        await page.goto("/scene-deletion-fixture/");
        const home = page.locator("[data-front-page]");
        await expect(home.locator('.scene-card[data-id="1"]')).toBeVisible();
        if (source === "detail") {
          await home.getByText("Scene 1", { exact: true }).click();
          await page
            .getByRole("button", {
              name: mobile ? "Entity actions" : "Operations",
              exact: true,
            })
            .click();
          await page
            .getByRole(mobile ? "button" : "menuitem", {
              name: "Delete scene…",
              exact: true,
            })
            .click();
        } else {
          await page
            .getByRole("navigation")
            .getByRole("link", { name: "Scenes", exact: true })
            .click();
          await page
            .locator('.scene-card[data-id="1"]')
            .click({ button: "right" });
          await page
            .getByRole("menuitem", { name: "Delete…", exact: true })
            .click();
        }
        const dialog = page.getByRole("dialog");
        await dialog
          .getByRole("button", { name: "Delete", exact: true })
          .click();
        await expect
          .poll(() => page.evaluate(() => window.deletionFixture.deleted))
          .toEqual(["1"]);
        if (history) {
          await expect(page).not.toHaveURL(/\/scenes\/1$/);
          await page.goBack();
          await expect(page).toHaveURL(/\/scenes\/1$/);
          await expect(
            page.getByText("Scene not found", { exact: true }),
          ).toBeVisible();
          await page.goBack();
          await expect(page).toHaveURL(/\/scene-deletion-fixture\/$/);
        } else {
          await page
            .getByRole("navigation")
            .getByRole("link", { name: "Home", exact: true })
            .click();
        }
        await expect(home.locator('.scene-card[data-id="2"]')).toBeVisible();
        await expect(home.locator('.scene-card[data-id="1"]')).toHaveCount(0);
        expect(
          await page.evaluate(() => window.deletionFixture.deleted),
        ).toEqual(["1"]);
      });
    }
  });
}
