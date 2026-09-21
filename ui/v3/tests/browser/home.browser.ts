import { test, expect } from "./test";

test.beforeEach(async ({ page }) => {
  await page.route("**/home-covers/*.svg", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="640" height="360" fill="#746"/></svg>',
    }),
  );
});

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
  await expect
    .poll(() => page.evaluate(() => window.homeFixtureDefinitions))
    .toEqual(["2"]);
  expect(await page.evaluate(() => window.homeFixtureQueries)).toEqual([]);
  await page.getByRole("link", { name: "Home", exact: true }).click();
  const firstRow = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Row 1", exact: true }) });
  await expect(firstRow.locator(".studio-card").first()).toBeVisible();
  await expect(firstRow.locator(".studio-card img").first()).toBeVisible();
  expect(await firstRow.locator(".studio-card").count()).toBeLessThan(10);
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
  await expect(lastRow.locator(".studio-card").first()).toBeVisible();
  await expect(firstRow.locator(".studio-card img")).toHaveCount(0);
  expect(await lastRow.locator(".studio-card").count()).toBeLessThan(10);
  await firstRow.scrollIntoViewIfNeeded();
  await expect(firstRow.locator(".studio-card img").first()).toBeVisible();
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

test("returning Home restores cached random rows and both scroll positions without placeholders", async ({
  page,
}) => {
  await page.goto("/home-fixture/");
  const home = page.locator("[data-front-page]");
  // The deferred row's wrapper stays mounted while its placeholder is replaced.
  const row = home.locator(":scope > div").nth(1);
  await row.scrollIntoViewIfNeeded();
  await expect(row.locator(".studio-card").first()).toBeVisible();
  await row.locator(".overflow-x-auto").evaluate((element) => {
    element.scrollLeft = 650;
  });
  await expect
    .poll(() =>
      row.locator(".overflow-x-auto").evaluate((element) => element.scrollLeft),
    )
    .toBeGreaterThan(500);
  // Let nearby rows finish before comparing the request count on return.
  await page.waitForTimeout(250);
  const before = {
    top: await home.evaluate((element) => element.scrollTop),
    left: await row
      .locator(".overflow-x-auto")
      .evaluate((element) => element.scrollLeft),
    queries: await page.evaluate(() => window.homeFixtureQueries),
    visibleCards: await row.locator(".overflow-x-auto").evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      return [...element.querySelectorAll("article")]
        .filter((card) => {
          const rect = card.getBoundingClientRect();
          return rect.right > bounds.left && rect.left < bounds.right;
        })
        .map((card) => card.getAttribute("data-id"));
    }),
  };
  expect(before.queries.some((query) => query.startsWith("random_"))).toBe(
    true,
  );
  await page.getByRole("button", { name: "Open navigation menu" }).click();
  await page.getByRole("link", { name: "Scenes", exact: true }).click();
  await expect(home).toHaveCount(0);
  const paintedCards = page.evaluate(
    () =>
      new Promise<(string | null)[][]>((resolve) => {
        const frames: (string | null)[][] = [];
        const sample = () => {
          const home = document.querySelector("[data-front-page]");
          const strip = home
            ?.querySelectorAll("section")[1]
            ?.querySelector(".overflow-x-auto");
          if (strip) {
            const bounds = strip.getBoundingClientRect();
            frames.push(
              [...strip.querySelectorAll("article")]
                .filter((card) => {
                  const rect = card.getBoundingClientRect();
                  return rect.right > bounds.left && rect.left < bounds.right;
                })
                .map((card) => card.getAttribute("data-id")),
            );
          }
          if (frames.length === 15) resolve(frames);
          else requestAnimationFrame(sample);
        };
        requestAnimationFrame(sample);
      }),
  );
  await page.goBack();
  expect(before.visibleCards.length).toBeGreaterThan(0);
  expect(await paintedCards).toEqual(
    Array.from({ length: 15 }, () => before.visibleCards),
  );
  expect(await row.locator(".studio-card").count()).toBeLessThan(10);
  expect(await page.evaluate(() => window.homeFixtureQueries)).toEqual(
    before.queries,
  );
  expect(await home.evaluate((element) => element.scrollTop)).toBeCloseTo(
    before.top,
    0,
  );
  expect(
    await row
      .locator(".overflow-x-auto")
      .evaluate((element) => element.scrollLeft),
  ).toBeCloseTo(before.left, 0);
});

test("a Home carousel keeps its full snap range while mounting nearby cards", async ({
  page,
}) => {
  await page.goto("/home-fixture/");
  const strip = page
    .locator("[data-front-page] section")
    .first()
    .locator(".overflow-x-auto");
  await expect(strip.locator("article").first()).toBeVisible();
  const width = await strip.evaluate((element) => element.scrollWidth);
  const height = await strip.evaluate((element) => element.clientHeight);
  const firstCard = strip.locator('article[data-id="0"]');
  await expect(firstCard.locator("img")).toHaveCount(1);
  expect(await strip.locator("article").count()).toBeLessThan(10);
  await strip.evaluate((element) => {
    element.scrollLeft = element.scrollWidth;
  });
  await expect
    .poll(() => strip.locator("article").last().getAttribute("data-id"))
    .toBe("24");
  await expect(strip.locator("article").last()).toBeInViewport();
  await expect(firstCard).toHaveCount(1);
  await expect(firstCard.locator("img")).toHaveCount(0);
  await expect(strip.locator("article").last().locator("img")).toHaveCount(1);
  expect(await strip.evaluate((element) => element.scrollWidth)).toBe(width);
  expect(await strip.evaluate((element) => element.clientHeight)).toBe(height);
  expect(await strip.locator("article").count()).toBeLessThan(15);
});
