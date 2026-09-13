import type { Page } from "@playwright/test";
import { test, expect, expectTouchTargets, expectCompactRow } from "./test";

interface Insets {
  top: number;
  right: number;
  bottom: number;
  left: number;
}

async function setSafeArea(page: Page, insets: Insets) {
  // Desktop engines do not expose iPhone hardware/browser-chrome insets.
  // Override the CSS tokens, retaining all production layout rules.
  await page.addStyleTag({
    content: `:root { ${Object.entries(insets)
      .map(([side, value]) => `--safe-area-${side}: ${value}px;`)
      .join(" ")} }`,
  });
}

for (const profile of [
  {
    name: "Safari portrait",
    width: 390,
    height: 740,
    path: "/home-fixture/",
    footer: ".bottom-tab-bar",
    insets: { top: 0, left: 0, right: 0, bottom: 0 },
  },
  {
    name: "Home Screen portrait",
    width: 390,
    height: 844,
    path: "/home-fixture/",
    footer: ".bottom-tab-bar",
    insets: { top: 59, left: 0, right: 0, bottom: 34 },
  },
  {
    name: "Home Screen landscape",
    width: 844,
    height: 390,
    path: "/?media",
    footer: "[data-mobile-detail-footer]",
    insets: { top: 0, left: 47, right: 47, bottom: 21 },
  },
]) {
  test(`${profile.name} keeps navigation clear of screen edges and reserves its height`, async ({
    page,
  }) => {
    await page.setViewportSize({
      width: profile.width,
      height: profile.height,
    });
    await page.goto(profile.path);
    await setSafeArea(page, profile.insets);
    const viewport = page.locator("[data-app-viewport]");
    const footer = page.locator(profile.footer);
    await expect(footer).toBeVisible();
    await expectTouchTargets(footer);
    await expect(footer).toHaveCSS(
      "padding-bottom",
      `${profile.insets.bottom}px`,
    );
    await expect(viewport).toHaveCSS("padding-top", `${profile.insets.top}px`);
    const row = footer.locator("[data-mobile-toolbar-row]");
    const buttons = await row.getByRole("button").evaluateAll((elements) =>
      elements.map((element) => {
        const rect = element.getBoundingClientRect();
        return { left: rect.left, right: rect.right, bottom: rect.bottom };
      }),
    );
    for (const button of buttons) {
      expect(button.left).toBeGreaterThanOrEqual(profile.insets.left + 16);
      expect(button.right).toBeLessThanOrEqual(
        profile.width - profile.insets.right - 16,
      );
      expect(button.bottom).toBeLessThanOrEqual(
        profile.height - profile.insets.bottom - 6,
      );
    }
    expect(
      await footer.evaluate(
        (element) => element.getBoundingClientRect().height,
      ),
    ).toBe(57 + profile.insets.bottom);
    expect(
      await footer.evaluate(
        (element) =>
          element.previousElementSibling?.getBoundingClientRect().bottom,
      ),
    ).toBeCloseTo((await footer.boundingBox())?.y ?? 0, 0);
    await page.screenshot({
      path: test.info().outputPath("safe-area-navigation.png"),
    });
    await row.getByRole("button").first().click();
    const drawer = page.locator('[data-slot="drawer-content"]:visible');
    await expect(drawer).toBeVisible();
    await expect(drawer).toHaveCSS(
      "padding-bottom",
      `${Math.max(8, profile.insets.bottom)}px`,
    );
    await expect(drawer).toHaveCSS("padding-left", `${profile.insets.left}px`);
    await expect(drawer).toHaveCSS(
      "padding-right",
      `${profile.insets.right}px`,
    );
  });
}

test("dense toolbars retain every 44px target as phone margins grow", async ({
  page,
}) => {
  await page.goto("/");
  const footer = page.locator("[data-mobile-detail-footer]");
  for (const width of [320, 332, 340, 360, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await expectCompactRow(footer);
    await expectTouchTargets(footer);
    const buttons = footer.getByRole("button");
    expect((await buttons.first().boundingBox())?.x).toBeGreaterThanOrEqual(
      width >= 340 ? 16 : 6,
    );
  }
});

test("the keyboard replaces Home Screen bottom padding without moving content behind the footer", async ({
  page,
}) => {
  await page.goto("/?standalone");
  await setSafeArea(page, { top: 59, left: 0, right: 0, bottom: 34 });
  const footer = page.locator("[data-mobile-list-mode]");
  await expect(footer).toHaveCSS("padding-bottom", "34px");
  await footer.getByRole("button", { name: "Search…", exact: true }).tap();
  await page.evaluate(() => {
    if (!visualViewport) throw new Error("Missing visual viewport");
    Object.defineProperty(visualViewport, "height", {
      configurable: true,
      value: 500,
    });
    Object.defineProperty(visualViewport, "offsetTop", {
      configurable: true,
      value: 0,
    });
    visualViewport.dispatchEvent(new Event("resize"));
  });
  await expect(footer).toHaveCSS("padding-bottom", "0px");
  expect(
    await footer.evaluate((element) => element.getBoundingClientRect().bottom),
  ).toBeCloseTo(500, 0);
  const listBottom = await page
    .locator("[data-scroll-restoration-id]")
    .evaluate((element) => element.getBoundingClientRect().bottom);
  expect(listBottom).toBeLessThanOrEqual((await footer.boundingBox())?.y ?? 0);
  await footer.getByRole("searchbox").blur();
  await expect(footer).toHaveCSS("padding-bottom", "34px");
});

test("the lightbox Close control clears the home indicator and landscape cutouts while loading", async ({
  page,
}) => {
  await page.goto("/player?loading");
  await page.getByRole("button", { name: "Open player" }).click();
  const close = page.getByRole("button", { name: "Close", exact: true });
  for (const profile of [
    {
      width: 390,
      height: 844,
      insets: { top: 59, left: 0, right: 0, bottom: 34 },
    },
    {
      width: 844,
      height: 390,
      insets: { top: 0, left: 47, right: 47, bottom: 21 },
    },
  ]) {
    await page.setViewportSize({
      width: profile.width,
      height: profile.height,
    });
    await setSafeArea(page, profile.insets);
    await expect(close).toBeVisible();
    const bounds = await close.boundingBox();
    if (!bounds) throw new Error("Missing Close bounds");
    expect(bounds.width).toBeGreaterThanOrEqual(44);
    expect(bounds.height).toBeGreaterThanOrEqual(44);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(
      profile.width - Math.max(16, profile.insets.right) + 0.1,
    );
    expect(bounds.y + bounds.height).toBeLessThanOrEqual(
      profile.height - profile.insets.bottom + 0.1,
    );
  }
  await close.tap();
  await expect(close).toBeHidden();
});
