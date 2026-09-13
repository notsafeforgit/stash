import { test, expect, holdForContextMenu } from "./test";

for (const width of [320, 1024]) {
  test(`touch cards at ${width}px show full titles in the hold menu without tooltips`, async ({
    page,
  }) => {
    await page.clock.install();
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/entity-cards");
    const card = page.locator("article");
    for (const selector of [".entity-card-title", ".entity-card-subtitle"]) {
      const text = card.locator(selector);
      await text.tap();
      await text.focus();
      // Touch browsers can synthesize hover after a tap; none should show titles.
      await text.hover();
      await page.clock.runFor(1000);
      await expect(page.locator('[data-slot="tooltip-content"]')).toHaveCount(
        0,
      );
    }
    const title = await card.locator(".entity-card-title").textContent();
    const trigger = page.locator('[data-slot="context-menu-trigger"]');
    const menu = page.getByRole("menu");
    await holdForContextMenu(trigger, menu);
    const label = menu.locator('[data-slot="context-menu-label"]');
    await expect(label).toHaveText(title ?? "");
    await expect(label).toBeVisible();
    expect(
      await label.evaluate((element) => ({
        clippedHorizontally: element.scrollWidth > element.clientWidth,
        clippedVertically: element.scrollHeight > element.clientHeight,
      })),
    ).toEqual({ clippedHorizontally: false, clippedVertically: false });
    const bounds = await menu.boundingBox();
    expect(bounds?.x).toBeGreaterThanOrEqual(0);
    expect((bounds?.x ?? 0) + (bounds?.width ?? 0)).toBeLessThanOrEqual(width);
    await page.screenshot({
      path: test.info().outputPath("entity-title-menu.png"),
    });
    await menu.getByRole("menuitem", { name: "Edit", exact: true }).tap();
    await expect(page.getByTestId("card-action")).toHaveText("edit");
    await expect(menu).toBeHidden();
    await expect(page.locator('[data-slot="tooltip-content"]')).toHaveCount(0);
  });
}

test.describe("desktop cursor", () => {
  test.use({
    isMobile: false,
    hasTouch: false,
    viewport: { width: 1280, height: 844 },
  });

  test("truncated titles and subtitles show tooltips only on hover", async ({
    page,
  }) => {
    await page.goto("/entity-cards");
    const tooltip = page.locator('[data-slot="tooltip-content"]');
    for (const selector of [".entity-card-title", ".entity-card-subtitle"]) {
      const text = page.locator(selector);
      await text.focus();
      await expect(tooltip).toHaveCount(0);
      await text.hover();
      await expect(tooltip).toHaveText((await text.textContent()) ?? "");
      await page.mouse.move(500, 500);
      await expect(tooltip).toBeHidden();
    }
  });
});
