import { test, expect } from "./test";

interface NavigationMotion {
  drawersAtStart: number;
  frames: number;
  finished: boolean;
}

declare global {
  interface Window {
    navigationMotion: NavigationMotion[];
  }
}

for (const theme of ["light", "dark"] as const) {
  test(`navigation reveals the page after the drawer closes in ${theme} mode`, async ({
    page,
  }) => {
    await page.emulateMedia({
      colorScheme: theme,
      reducedMotion: "no-preference",
    });
    await page.addInitScript(() => {
      window.navigationMotion = [];
      const animate = Element.prototype.animate;
      Element.prototype.animate = function (...args) {
        const animation = animate.apply(this, args);
        if (!this.hasAttribute("data-route-transition")) return animation;
        const observation: NavigationMotion = {
          drawersAtStart: document.querySelectorAll("[data-mobile-navigation]")
            .length,
          frames: 0,
          finished: false,
        };
        window.navigationMotion.push(observation);
        const sample = () => {
          observation.frames++;
          if (!observation.finished) requestAnimationFrame(sample);
        };
        requestAnimationFrame(sample);
        void animation.finished.then(
          () => {
            observation.finished = true;
          },
          () => {
            observation.finished = true;
          },
        );
        return animation;
      };
    });
    await page.goto("/home-fixture/images");
    await page.evaluate(
      (theme) =>
        document.documentElement.classList.toggle("dark", theme === "dark"),
      theme,
    );
    const menu = page.getByRole("button", {
      name: "Open navigation menu",
      includeHidden: true,
    });
    await menu.tap();
    const navigation = page.locator("[data-mobile-navigation]");
    const backdrop = page.locator('[data-slot="drawer-overlay"]');
    await expect(backdrop).toHaveCSS("backdrop-filter", "none");
    await expect(backdrop).toHaveCSS("will-change", "auto");
    await navigation.getByRole("link", { name: "Scenes", exact: true }).tap();
    await expect(page).toHaveURL(/\/home-fixture\/scenes$/);
    await expect
      .poll(() => page.evaluate(() => window.navigationMotion[0]?.finished))
      .toBe(true);
    expect(await page.evaluate(() => window.navigationMotion[0])).toMatchObject(
      { drawersAtStart: 0 },
    );
    expect(
      await page.evaluate(() => window.navigationMotion[0]?.frames),
    ).toBeGreaterThan(1);
    const viewport = page.locator("[data-route-viewport]");
    await expect(viewport).toHaveCSS("transform", "none");
    await expect(viewport).toHaveCSS("opacity", "1");
    expect(
      await page
        .locator("[data-route-transition]")
        .evaluate((element) => element.hasAttribute("hidden")),
    ).toBe(true);

    // Reopen during the previous exit, bypassing actionability's animation wait.
    // The latest navigation owns the reveal and there is only one drawer.
    for (let index = 0; index < 20; index++) {
      const target = index % 2 === 0 ? "Images" : "Groups";
      await menu.dispatchEvent("click");
      await expect(navigation).toHaveCount(1);
      await navigation
        .getByRole("link", { name: target, exact: true })
        .dispatchEvent("click");
      await expect(page).toHaveURL(
        new RegExp(`/home-fixture/${target.toLowerCase()}$`),
      );
      expect(
        await page.locator('[data-slot="drawer-overlay"]').count(),
      ).toBeLessThanOrEqual(1);
    }
    await expect(navigation).toHaveCount(0);
    await expect
      .poll(() =>
        page
          .locator("[data-route-transition]")
          .evaluate((element) => element.hasAttribute("hidden")),
      )
      .toBe(true);
    expect(
      await page.evaluate(() =>
        window.navigationMotion.every(
          (motion) => motion.drawersAtStart === 0 && motion.finished,
        ),
      ),
    ).toBe(true);
  });
}

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
