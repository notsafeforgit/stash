import { test, expect } from "./test";

test("the drawer preloads Home and offscreen rows wait until approached", async ({
  page,
}) => {
  await page.goto("/home-fixture/scenes");
  await page.getByRole("button", { name: "Open navigation menu" }).click();
  await expect
    .poll(() => page.evaluate(() => window.homeFixturePreloaded))
    .toBe(true);
  expect(await page.evaluate(() => window.homeFixtureQueries)).toEqual([]);
  await page.getByRole("link", { name: "Home", exact: true }).click();
  const firstRow = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Row 1", exact: true }) });
  await expect(firstRow.locator(".studio-card")).toHaveCount(25);
  expect(
    await page.evaluate(() => window.homeFixtureQueries.length),
  ).toBeLessThan(5);
  expect(await page.evaluate(() => window.homeFixtureQueries)).not.toContain(
    "saved-filters",
  );
  const firstCard = await firstRow
    .locator(".studio-card")
    .first()
    .elementHandle();
  const lastRow = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Row 5", exact: true }) });
  await expect(lastRow.locator(".studio-card")).toHaveCount(0);
  await lastRow.scrollIntoViewIfNeeded();
  await expect(lastRow.locator(".studio-card")).toHaveCount(25);
  await firstRow.scrollIntoViewIfNeeded();
  expect(await firstCard?.evaluate((element) => element.isConnected)).toBe(
    true,
  );
  expect(
    await page.evaluate(
      () =>
        window.homeFixtureQueries.filter((query) => query === "name").length,
    ),
  ).toBe(1);
});

test("Home loads its customisation code and saved filters only when opened", async ({
  page,
}) => {
  const configRequests: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.endsWith("/front-page-config.tsx"))
      configRequests.push(request.url());
  });
  await page.goto("/home-fixture/");
  await expect(page.locator(".studio-card").first()).toBeVisible();
  expect(configRequests).toEqual([]);
  expect(await page.evaluate(() => window.homeFixtureQueries)).not.toContain(
    "saved-filters",
  );
  await page.addStyleTag({
    content: ":root { --safe-area-top: 59px; --safe-area-bottom: 34px; }",
  });
  await page.getByRole("button", { name: "Customise", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "Customise homepage" }),
  ).toBeVisible();
  const editor = page.getByRole("dialog", { name: "Customise homepage" });
  expect(
    (await editor.getByRole("heading").boundingBox())?.y,
  ).toBeGreaterThanOrEqual(75);
  const save = await editor
    .getByRole("button", { name: "Save", exact: true })
    .boundingBox();
  if (!save) throw new Error("Missing Save control bounds");
  expect(save.y + save.height).toBeLessThanOrEqual(844 - 34 - 16);
  await expect
    .poll(() =>
      page.evaluate(() => window.homeFixtureQueries.includes("saved-filters")),
    )
    .toBe(true);
  expect(configRequests).toHaveLength(1);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toBeHidden();
  await page.getByRole("button", { name: "Customise", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "Customise homepage" }),
  ).toBeVisible();
  expect(configRequests).toHaveLength(1);
});
