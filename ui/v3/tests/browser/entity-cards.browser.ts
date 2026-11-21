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
    const trigger = page.locator('[data-slot="menu-context-trigger"]');
    const menu = page.getByRole("menu");
    await holdForContextMenu(trigger, menu);
    const label = menu.locator('[data-slot="menu-group-label"]');
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

// A long title widens the hold menu. Opening sideways it could only flip, and
// from a right-hand card it ran off the left edge; below the press it slides.
for (const width of [320, 390]) {
  test(`the hold menu stays on screen from either column at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    for (const path of ["/entity-cards", "/entity-cards?end"]) {
      await page.goto(path);
      const trigger = page.locator('[data-slot="menu-context-trigger"]');
      const menu = page.getByRole("menu");
      for (const x of [0.1, 0.5, 0.9]) {
        await holdForContextMenu(trigger, menu, { x, y: 0.4 });
        const bounds = await menu.boundingBox();
        if (!bounds) throw new Error("Missing hold menu");
        expect(bounds.x).toBeGreaterThanOrEqual(0);
        expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
        await page.keyboard.press("Escape");
        await expect(menu).toBeHidden();
      }
    }
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
    const tooltip = page.locator('[data-slot="tooltip-content"][data-open]');
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
