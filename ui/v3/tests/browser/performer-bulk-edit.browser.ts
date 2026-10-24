import { test, expect, holdForContextMenu } from "./test";

for (const failure of [false, true]) {
  test(`bulk performer editing preserves the list while its module ${failure ? "fails" : "loads slowly"}`, async ({
    page,
    expectedConsoleErrors,
  }) => {
    if (failure)
      expectedConsoleErrors.push(
        "503",
        "Failed to fetch dynamically imported module",
        "Importing a module script failed",
      );
    let release!: () => void;
    let requested = false;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    const documents: string[] = [];
    page.on("request", (request) => {
      if (request.isNavigationRequest()) documents.push(request.url());
    });
    await page.route("**/performer-bulk-edit-sheet.tsx*", async (route) => {
      requested = true;
      await pending;
      if (!failure) return route.continue();
      // Vite's production import helper emits this event before rejecting.
      await page.evaluate(() => {
        const event = Object.assign(
          new Event("vite:preloadError", { cancelable: true }),
          { payload: new Error("Importing a module script failed") },
        );
        window.dispatchEvent(event);
      });
      await route.fulfill({
        status: 503,
        body: "Bulk editor unavailable",
      });
    });
    try {
      await page.goto("/performer-list");
      const card = page.locator("article[data-id]").first();
      const preview = card.locator("[data-entity-card-preview]");
      const menu = page.getByRole("menu");
      await holdForContextMenu(preview, menu);
      await page.getByRole("menuitem", { name: /^Select all$/i }).tap();
      await expect(menu).toHaveCount(0);
      await holdForContextMenu(preview, menu);
      await page.getByRole("menuitem", { name: "Edit 40 performers…" }).tap();
      await expect.poll(() => requested).toBe(true);
      const original = await card.elementHandle();
      await expect(page.getByRole("dialog")).toContainText("Loading");
      release();
      if (failure) {
        const error = page.getByRole("alert");
        await expect(error).toContainText("Could not load data.");
        await page.getByRole("button", { name: "Close", exact: true }).tap();
      } else {
        await expect(
          page.getByRole("dialog", { name: "Edit 40 performers" }),
        ).toBeVisible();
        await page.getByRole("button", { name: "Cancel", exact: true }).tap();
      }
      await expect(page.getByRole("dialog")).toHaveCount(0);
      expect(await original?.evaluate((element) => element.isConnected)).toBe(
        true,
      );
      await holdForContextMenu(preview, menu);
      await expect(
        page.getByRole("menuitem", { name: "Edit 40 performers…" }),
      ).toBeVisible();
      expect(documents).toHaveLength(1);
      expect(
        await page.evaluate(() =>
          window.performerListRequests.filter(
            (name) =>
              name === "FindPerformerList" || name === "FindPerformerListCount",
          ),
        ),
      ).toEqual(["FindPerformerList", "FindPerformerListCount"]);
    } finally {
      release();
    }
  });
}
