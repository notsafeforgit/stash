import { test, expect } from "./test";

for (const kind of ["image", "scene"]) {
  test(`${kind} bulk deletion retains its dialog and shows committed progress`, async ({
    page,
  }) => {
    await page.goto(`/bulk-deletion?kind=${kind}`);
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog.getByText("Deleted 0 of 120")).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "Cancel", exact: true }),
    ).toBeDisabled();
    const progress = dialog.getByRole("progressbar");
    await expect(progress).toHaveAttribute("aria-valuemax", "120");
    for (const completed of [50, 100]) {
      await page.evaluate(() => window.bulkDeletionFixture.finishBatch(true));
      await expect(
        dialog.getByText(`Deleted ${completed} of 120`),
      ).toBeVisible();
      await expect(progress).toHaveAttribute(
        "aria-valuenow",
        String(completed),
      );
      await expect(page.locator('[data-item-id="1"]')).toHaveCount(1);
      expect(await page.evaluate(() => window.bulkDeletionFixture.reads)).toBe(
        1,
      );
    }
    await page.evaluate(() => window.bulkDeletionFixture.finishBatch(true));
    await expect(dialog).toHaveCount(0);
    await expect(page.locator("[data-item-id]")).toHaveCount(0);
    expect(
      await page.evaluate(() =>
        window.bulkDeletionFixture.batches.map((ids) => ids.length),
      ),
    ).toEqual([50, 50, 20]);
    await expect
      .poll(() => page.evaluate(() => window.bulkDeletionFixture.reads))
      .toBe(2);
  });

  test(`${kind} bulk deletion stops on failure and reconciles only confirmed removals`, async ({
    page,
  }) => {
    await page.goto(`/bulk-deletion?kind=${kind}`);
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog.getByText("Deleted 0 of 120")).toBeVisible();
    await page.evaluate(() => window.bulkDeletionFixture.finishBatch(true));
    await expect(dialog.getByText("Deleted 50 of 120")).toBeVisible();
    await page.evaluate(() => window.bulkDeletionFixture.finishBatch(false));
    await expect(dialog).toHaveCount(0);
    await expect(
      page.getByText(
        "Deletion stopped: 50 of 120 confirmed. Refresh the list before retrying.",
      ),
    ).toBeVisible();
    await expect(page.locator("[data-item-id]")).toHaveCount(70);
    expect(
      await page.evaluate(() => window.bulkDeletionFixture.batches.length),
    ).toBe(2);
    await expect
      .poll(() => page.evaluate(() => window.bulkDeletionFixture.reads))
      .toBe(2);
  });
}
