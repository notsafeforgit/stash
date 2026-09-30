import { test, expect } from "./test";

test("the mobile drawer includes every plugin placement and closes after navigation", async ({
  page,
}) => {
  await page.goto("/home-fixture/images?plugin-navigation");
  const menu = page.getByRole("button", { name: "Open navigation menu" });
  await expect(page.locator(".bottom-tab-bar").getByRole("link")).toHaveText([
    "Scenes",
    "Images",
    "Groups",
  ]);
  await menu.tap();
  const navigation = page.locator("[data-mobile-navigation]");
  await expect(
    navigation.getByRole("link", { name: "Plugin library", exact: true }),
  ).toHaveCount(1);
  await expect(
    navigation.getByRole("link", { name: "Mobile plugin page", exact: true }),
  ).toBeVisible();
  const review = navigation.getByRole("link", {
    name: "Catalog review",
    exact: true,
  });
  await expect(review).toHaveAttribute(
    "href",
    "/home-fixture/examplePlugin/review",
  );
  await expect(review.locator("svg")).toHaveCount(1);
  expect(
    await navigation.evaluate((element) => element.scrollWidth),
  ).toBeLessThanOrEqual(390);
  await review.tap();
  await expect(page).toHaveURL(/\/home-fixture\/examplePlugin\/review$/);
  await expect(
    page.getByRole("heading", { name: "Catalog review", exact: true }),
  ).toBeVisible();
  await expect(navigation).toHaveCount(0);
  await expect(page.locator('[data-slot="drawer-overlay"]')).toHaveCount(0);
  await menu.tap();
  await expect(
    navigation.getByRole("link", { name: "Catalog review", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await navigation.getByRole("link", { name: "Images", exact: true }).tap();
  await expect(page).toHaveURL(/\/home-fixture\/images$/);
  await expect(navigation).toHaveCount(0);
});

test("utility plugin pages use the desktop menu while primary links stay in primary navigation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/home-fixture/images?plugin-navigation");
  const primary = page.getByRole("navigation", { name: "Primary navigation" });
  await expect(
    primary.getByRole("link", { name: "Plugin library", exact: true }),
  ).toHaveCount(1);
  await expect(
    primary.getByRole("link", { name: "Catalog review", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Mobile plugin page", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "More options", exact: true }).click();
  const menu = page.getByRole("menu");
  await expect(
    menu.getByRole("menuitem", { name: "Plugin library", exact: true }),
  ).toHaveCount(0);
  await expect(
    menu.getByRole("menuitem", { name: "Settings", exact: true }),
  ).toBeVisible();
  await menu
    .getByRole("menuitem", { name: "Catalog review", exact: true })
    .click();
  await expect(page).toHaveURL(/\/home-fixture\/examplePlugin\/review$/);
  await expect(
    page.getByRole("heading", { name: "Catalog review", exact: true }),
  ).toBeVisible();
  await expect(menu).toHaveCount(0);
});
