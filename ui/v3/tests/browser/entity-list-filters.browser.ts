import { test, expect } from "./test";

for (const kind of ["image", "scene"] as const) {
  for (const width of [390, 1280]) {
    test(`${kind} list retains unfinished filter conditions at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      await page.goto(`/lightbox-list?kind=${kind}`);
      await page.getByRole("button", { name: "Filters", exact: true }).tap();
      await page.getByRole("button", { name: "Add first condition" }).tap();
      const conditions = page.locator(".filter-condition-shell");
      await expect(conditions).toHaveCount(1);
      await conditions.first().getByRole("textbox").fill("sample title");
      await expect(page).toHaveURL(/fa=/);
      const appliedURL = page.url();
      const requests = await page.evaluate(() => window.lightboxListRequests);

      await page
        .getByRole("button", { name: "Add condition", exact: true })
        .tap();
      await expect(conditions).toHaveCount(2);
      const field = conditions.nth(1).getByRole("combobox").first();
      await field.tap();
      await expect(field).toHaveAttribute("aria-expanded", "true");
      await field.fill("Details");
      await expect(page.getByRole("option")).toHaveCount(1);
      const option = page.getByRole("option", { name: /^Details\b/ });
      if (width < 768) await option.tap();
      else await option.click();
      await expect(field).toHaveAttribute("aria-expanded", "false");
      await expect(field).toHaveValue("Details");
      await expect(field).toBeFocused();
      await page
        .getByRole("button", { name: "Add subgroup", exact: true })
        .tap();
      await expect(conditions).toHaveCount(3);
      await expect(page).toHaveURL(appliedURL);
      expect(await page.evaluate(() => window.lightboxListRequests)).toEqual(
        requests,
      );

      await conditions.nth(1).getByRole("textbox").fill("sample description");
      await expect(page).not.toHaveURL(appliedURL);
      await expect(conditions).toHaveCount(3);
      await expect(conditions.first().getByRole("textbox")).toHaveValue(
        "sample title",
      );
      await page.getByRole("button", { name: "Clear All", exact: true }).tap();
      await expect(conditions).toHaveCount(0);
      await expect(page).not.toHaveURL(/fa=/);
      await page.getByRole("button", { name: "Add first condition" }).tap();
      await expect(conditions).toHaveCount(1);
      await page.getByRole("button", { name: "Clear All", exact: true }).tap();
      await expect(conditions).toHaveCount(0);
    });
  }
}
