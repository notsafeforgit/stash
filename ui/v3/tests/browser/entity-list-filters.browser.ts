import { test, expect } from "./test";

test("mobile text case toggle persists in URLs and remains independent of the operator", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/lightbox-list?kind=scene");
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await page.getByRole("button", { name: "Add first condition" }).tap();
  const condition = page.locator(".filter-condition-shell");
  const field = condition.getByRole("combobox").first();
  await field.tap();
  await field.fill("Details");
  await page.getByRole("option", { name: /^Details\b/ }).tap();
  await condition.getByRole("textbox").fill("Bright");
  await expect(page).toHaveURL(/fa=/);
  const toggle = condition.getByRole("switch", { name: "Case sensitive" });
  await expect(toggle).not.toBeChecked();
  const ignoreCaseURL = page.url();
  await toggle.tap();
  await expect(toggle).toBeChecked();
  await expect(page).not.toHaveURL(ignoreCaseURL);
  const caseSensitiveURL = page.url();
  await page.reload();
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await expect(toggle).toBeChecked();
  await expect(condition.getByRole("textbox")).toHaveValue("Bright");
  await condition.getByRole("combobox").nth(1).tap();
  await page.getByRole("option", { name: "Includes", exact: true }).tap();
  await expect(toggle).toBeChecked();
  await expect(page).not.toHaveURL(caseSensitiveURL);
  await toggle.tap();
  await expect(toggle).not.toBeChecked();
  await page.screenshot({ path: "/tmp/stash-string-case-mobile.png" });
});

for (const kind of ["image", "scene"] as const) {
  for (const width of [390, 1280]) {
    test(`${kind} list retains unfinished filter conditions at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      await page.goto(`/lightbox-list?kind=${kind}`);
      await page.getByRole("button", { name: "Filters", exact: true }).tap();
      if (width >= 768) {
        // The sidebar clips its fixed-width contents while opening. Tapping
        // early can make Playwright scroll it horizontally, then miss the
        // button as that scroll offset shrinks before the touch is released.
        await page.getByRole("complementary").evaluate(async (sidebar) => {
          await Promise.all(
            sidebar.getAnimations().map((animation) => animation.finished),
          );
        });
      }
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
