import { test, expect } from "./test";

for (const width of [430, 1280]) {
  test(`cards remain usable while the total is pending at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 932 });
    await page.goto("/deferred-list");
    const card = page.locator('article[data-id="1"]');
    await expect(card).toBeVisible();
    const original = await card.elementHandle();
    await expect(
      page.getByRole("button", { name: "Next", exact: true }),
    ).toHaveCount(0);
    expect(
      await page.evaluate(() =>
        window.deferredListFixture.requests.map((r) => r.operation),
      ),
    ).toEqual(["FindSceneList", "FindSceneListCount"]);
    await page.evaluate(() => window.deferredListFixture.releaseCounts());
    await expect
      .poll(() =>
        page.getByRole("button", { name: "Next", exact: true }).count(),
      )
      .toBeGreaterThan(0);
    expect(await original?.evaluate((element) => element.isConnected)).toBe(
      true,
    );
    await page
      .getByRole("button", { name: "Next", exact: true })
      .last()
      .click();
    await expect(page.locator('article[data-id="41"]')).toBeVisible();
    await expect(page).toHaveURL(/[?&]p=2/);
    // A pending total must not clamp page 2 back to page 1 or trigger refills.
    await page.waitForTimeout(300);
    await expect(page.locator('article[data-id="41"]')).toBeVisible();
    expect(
      await page.evaluate(() => window.deferredListFixture.requests),
    ).toEqual([
      { operation: "FindSceneList", page: 1 },
      { operation: "FindSceneListCount", page: 1 },
      { operation: "FindSceneList", page: 2 },
      { operation: "FindSceneListCount", page: 2 },
    ]);
    await page.evaluate(() => window.deferredListFixture.releaseCounts());
    await expect(page.locator('article[data-id="41"]')).toBeVisible();
  });
}
