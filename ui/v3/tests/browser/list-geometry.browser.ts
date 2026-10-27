import { test, expect } from "./test";

test.use({ viewport: { width: 430, height: 739 }, deviceScaleFactor: 3 });

test.beforeEach(async ({ page }) => {
  await page.route("**/fixture-cover/*", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="720"><rect width="1280" height="720" fill="teal"/></svg>',
    }),
  );
});

for (const variant of [
  "",
  "&two-columns",
  "&portrait",
  "&landscape",
  "&details",
]) {
  test(`unvisited rows reserve their full size when scrolling up from the bottom ${variant || "auto"}`, async ({
    page,
  }) => {
    const covers = new Set<string>();
    page.on("request", (request) => {
      if (request.url().includes("/fixture-cover/")) covers.add(request.url());
    });
    await page.goto(`/embedded-list?mixed&p=2${variant}`);
    const scroller = page.locator(
      '[data-scroll-restoration-id="collection-detail"]',
    );
    await expect(page.locator("[data-card-id]").first()).toBeAttached();
    await page.evaluate(() => document.fonts.ready);
    const initialHeight = await scroller.evaluate((el) => el.scrollHeight);
    expect(covers.size).toBeLessThan(16);
    await scroller.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    await expect(page.locator('[data-card-id="40"]')).toBeInViewport();
    await expect(page.locator("[data-measure-row]")).toHaveCount(0);

    const measurements = await scroller.evaluate(async (el) => {
      const samples = [];
      for (let step = 0; step < 12; step++) {
        const cards = [...el.querySelectorAll<HTMLElement>("[data-card-id]")];
        const anchor = cards.find(
          (card) =>
            card.getBoundingClientRect().bottom >
            el.getBoundingClientRect().top,
        );
        if (!anchor) throw new Error("No visible anchor");
        const id = anchor.dataset.cardId;
        const top = anchor.getBoundingClientRect().top;
        const offset = el.scrollTop;
        el.scrollTop -= 240;
        await new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        );
        const current = el.querySelector<HTMLElement>(`[data-card-id="${id}"]`);
        if (!current) throw new Error("Visible anchor was unmounted");
        samples.push({
          height: el.scrollHeight,
          drift: Math.abs(
            current.getBoundingClientRect().top - top - (offset - el.scrollTop),
          ),
          mounted: el.querySelectorAll("[data-card-id]").length,
        });
      }
      return samples;
    });
    for (const sample of measurements) {
      expect(Math.abs(sample.height - initialHeight)).toBeLessThanOrEqual(1);
      expect(sample.drift).toBeLessThan(1);
      expect(sample.mounted).toBeLessThan(20);
    }
    // Measuring metadata must not request every preview on the page.
    expect(covers.size).toBeLessThan(40);
  });
}

test("bottom pagination and Back restore complete geometry, including after a resize", async ({
  page,
}) => {
  await page.goto("/embedded-list?mixed&p=2");
  const scroller = page.locator(
    '[data-scroll-restoration-id="collection-detail"]',
  );
  const lastCard = page.locator('[data-card-id="40"]');
  await expect(page.locator('[data-card-id="1"]')).toBeAttached();
  await scroller.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await page.getByRole("button", { name: "Prev", exact: true }).tap();
  await expect(page.locator('[data-card-id="1"]')).toHaveAttribute(
    "data-card-page",
    "1",
  );
  await scroller.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await expect(lastCard).toBeInViewport();
  await page.getByRole("button", { name: "Next", exact: true }).tap();
  await expect(lastCard).toHaveAttribute("data-card-page", "2");
  await expect(lastCard).toBeInViewport();
  await page.getByRole("button", { name: "Prev", exact: true }).tap();
  await expect(lastCard).toHaveAttribute("data-card-page", "1");
  await expect(lastCard).toBeInViewport();
  const before = await scroller.evaluate((el) => ({
    top: el.scrollTop,
    height: el.scrollHeight,
  }));
  const point = await lastCard.evaluate((el) => {
    const rect = el.getBoundingClientRect();
    return {
      x: rect.left + 40,
      y: (Math.max(100, rect.top) + Math.min(600, rect.bottom)) / 2,
    };
  });
  await page.touchscreen.tap(point.x, point.y);
  await expect(page.getByText("Fixture scene", { exact: true })).toBeVisible();
  await page.goBack();
  await expect(lastCard).toBeInViewport();
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollTop))
    .toBeCloseTo(before.top, 0);
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollHeight))
    .toBe(before.height);

  await page.setViewportSize({ width: 320, height: 739 });
  await scroller.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await expect(lastCard).toBeInViewport();
  const resizedHeight = await scroller.evaluate((el) => el.scrollHeight);
  await scroller.evaluate((el) => {
    el.scrollTop -= 1600;
  });
  await expect(lastCard).not.toBeInViewport();
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollHeight))
    .toBe(resizedHeight);
});
