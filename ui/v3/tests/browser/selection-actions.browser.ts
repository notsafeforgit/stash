import { test, expect, chooseSelectionAction, expectCompactRow } from "./test";

const performerID = (index: number) =>
  `00000000-0000-4000-8000-${String(index).padStart(12, "0")}`;

for (const width of [390, 1280]) {
  test.describe(`selection actions at ${width}px`, () => {
    test.use({ viewport: { width, height: 800 }, isMobile: width < 1024 });

    test("edit and delete a selection that has scrolled out of view", async ({
      page,
    }) => {
      await page.goto("/performer-list");
      for (const index of [1, 2])
        await page
          .locator(
            `article[data-id="${performerID(index)}"] [data-entity-card-preview]`,
          )
          .click({ modifiers: ["Control"] });
      const row = page.locator("[data-mobile-list-mode]");
      const bar = width < 1024 ? row : page;
      if (width < 1024) {
        await expect(row).toHaveAttribute("data-mobile-list-mode", "selection");
        await expectCompactRow(row);
        // Sharing moves into the sheet so the row keeps its width.
        await expect(
          row.getByRole("button", { name: "Share…", exact: true }),
        ).toHaveCount(0);
      }

      await page.locator("[data-scroll-restoration-id]").evaluate((element) => {
        element.scrollTop = element.scrollHeight;
      });
      await expect(
        page.locator(`article[data-id="${performerID(40)}"]`),
      ).toBeInViewport();
      await expect(
        page.locator(`article[data-id="${performerID(1)}"]`),
      ).not.toBeInViewport();

      await chooseSelectionAction(page, "Edit…", bar);
      const edit = page.getByRole("dialog", { name: "Edit 2 performers" });
      await expect(edit).toBeVisible();
      await edit.getByRole("button", { name: "Cancel", exact: true }).click();
      await expect(page.getByRole("dialog")).toHaveCount(0);

      await chooseSelectionAction(page, "Delete…", bar);
      const confirm = page.getByRole("dialog");
      await expect(
        confirm.getByRole("heading", { name: "Delete 2 performers?" }),
      ).toBeVisible();
      await confirm
        .getByRole("button", { name: "Delete", exact: true })
        .click();
      await expect(confirm).toHaveCount(0);
      expect(await page.evaluate(() => window.deletedPerformerIds)).toEqual([
        [performerID(1), performerID(2)],
      ]);
      await expect(
        page.getByRole("button", { name: "Selection actions", exact: true }),
      ).toHaveCount(0);
      await expect(page.locator("article[data-id]").first()).toBeAttached();
      await expect(
        page.locator(`article[data-id="${performerID(1)}"]`),
      ).toHaveCount(0);
    });
  });
}
