import { test, expect, chooseSection } from "./test";

test.use({ viewport: { width: 430, height: 739 }, deviceScaleFactor: 3 });

test.beforeEach(async ({ page }) => {
  await page.route("**/fixture-cover/*", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="720"><rect width="1280" height="720" fill="teal"/></svg>',
    }),
  );
});

test("embedded cards fetch only viewport thumbnails and remain bounded while scrolling", async ({
  page,
}) => {
  const covers = new Set<string>();
  page.on("request", (request) => {
    if (request.url().includes("/fixture-cover/")) covers.add(request.url());
  });
  await page.goto("/embedded-list");
  const scroller = page.locator(
    '[data-scroll-restoration-id="collection-detail"]',
  );
  const cards = page.locator("[data-card-id]");
  await expect(cards.first()).toBeAttached();
  await expect.poll(() => covers.size).toBeGreaterThan(0);
  await expect.poll(() => cards.count()).toBeLessThan(16);
  expect(covers.size).toBeLessThan(16);
  // WebKit may preload the thumbnail's fallback before selecting <source>.
  // Both are bounded card renditions; full-size and legacy covers stay unused.
  expect([...covers].every((url) => url.includes("/thumbnail-"))).toBe(true);

  await scroller.evaluate((el) => {
    el.scrollTop = 2200;
  });
  await expect
    .poll(() => cards.first().getAttribute("data-card-id"))
    .not.toBe("1");
  // These compact fixture cards fit six rows in the viewport, plus overscan.
  await expect.poll(() => cards.count()).toBeLessThan(24);
  await scroller.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await expect(page.locator('[data-card-id="40"]')).toBeInViewport();
  await expect.poll(() => cards.count()).toBeLessThan(24);
  expect([...covers].every((url) => url.includes("/thumbnail-"))).toBe(true);
});

test("embedded pagination starts at the list and Back restores its visible cards", async ({
  page,
}) => {
  await page.goto("/embedded-list");
  const scroller = page.locator(
    '[data-scroll-restoration-id="collection-detail"]',
  );
  await expect(page.locator('[data-card-id="1"]')).toBeAttached();
  await scroller.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await page.getByRole("button", { name: "Next", exact: true }).tap();
  await expect(page).toHaveURL(/(?:\?|&)p=2(?:&|$)/);
  await expect(page.locator('[data-card-id="1"]')).toHaveAttribute(
    "data-card-page",
    "2",
  );
  await expect(page.locator('[data-card-id="1"]')).toBeInViewport();
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollTop))
    .toBeGreaterThanOrEqual(900);

  await scroller.evaluate((el) => {
    el.scrollTop = 2100;
  });
  const card = page.locator('[data-card-id="19"]');
  await expect(card).toBeInViewport();
  const offset = await scroller.evaluate((el) => el.scrollTop);
  const cardTop = await card.evaluate((el) => el.getBoundingClientRect().top);
  await card.tap();
  await expect(page.getByText("Fixture scene", { exact: true })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/(?:\?|&)p=2(?:&|$)/);
  await expect(card).toHaveAttribute("data-card-page", "2");
  await expect(card).toBeInViewport();
  await expect
    .poll(() => scroller.evaluate((el) => el.scrollTop))
    .toBeCloseTo(offset, 0);
  await expect
    .poll(() => card.evaluate((el) => el.getBoundingClientRect().top))
    .toBeCloseTo(cardTop, 0);

  await page.goBack();
  await expect(page).not.toHaveURL(/(?:\?|&)p=2(?:&|$)/);
  const previousPageEnd = page.locator('[data-card-id="40"]');
  await expect(previousPageEnd).toHaveAttribute("data-card-page", "1");
  await expect(previousPageEnd).toBeInViewport();
});

test("header resizing, tab changes and desktop resizing keep the correct scroll owner", async ({
  page,
}) => {
  await page.goto("/embedded-list");
  await page.getByRole("button", { name: "Grow header" }).tap();
  const scroller = page.locator(
    '[data-scroll-restoration-id="collection-detail"]',
  );
  await scroller.evaluate((el) => {
    el.scrollTop = 1300;
  });
  await expect(page.locator('[data-card-id="1"]')).toBeInViewport();
  await expect
    .poll(() => page.locator("[data-card-id]").count())
    .toBeLessThan(16);
  await chooseSection(page, "Other");
  await expect(page.getByText("Other content")).toBeInViewport();
  await chooseSection(page, "Scenes");
  await expect(page.locator('[data-card-id="1"]')).toBeInViewport();
  await expect
    .poll(() => page.locator("[data-card-id]").count())
    .toBeLessThan(16);

  await page.setViewportSize({ width: 1280, height: 900 });
  const desktopScroller = page.locator(
    '[data-scroll-restoration-id^="entity-list-"]',
  );
  await expect(desktopScroller).toBeVisible();
  await desktopScroller.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await expect(page.locator('[data-card-id="40"]')).toBeInViewport();
  await expect
    .poll(() => page.locator("[data-card-id]").count())
    .toBeLessThan(40);
});
