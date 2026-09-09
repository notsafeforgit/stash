import { test, expect, expectCompactRow, expectTouchTargets } from "./test";

for (const width of [320, 390]) {
  test(`settings navigation stays in one reachable row at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/settings/interface");
    const footer = page.locator("[data-mobile-settings-footer]");
    await expectCompactRow(footer);
    await expectTouchTargets(footer);
    await expect(footer.getByRole("button")).toHaveCount(3);
    await expect(page.getByRole("banner").getByRole("heading")).toHaveText(
      "Settings",
    );
    await expect(
      page.getByRole("banner").locator("button, input, a"),
    ).toHaveCount(0);
    await page.locator("[data-settings-scroll]").evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(
      page.getByText("Last setting", { exact: true }),
    ).toBeInViewport();
    await expectCompactRow(footer);

    const sections = footer.getByRole("button", { name: "Settings sections" });
    await sections.tap();
    const menu = page.getByRole("menu", { name: "Settings sections" });
    await expect(
      menu.getByRole("menuitem", { name: "Interface", exact: true }),
    ).toHaveAttribute("aria-current", "page");
    await menu.getByRole("menuitem", { name: "Library", exact: true }).tap();
    await expect(page).toHaveURL(/\/settings\/library$/);
    await expect(sections).toHaveText("Library");
    await expect(menu).toBeHidden();
    await page.goBack();
    await expect(sections).toHaveText("Interface");

    await footer.getByRole("button", { name: "Navigation", exact: true }).tap();
    const navigation = page.getByRole("dialog", {
      name: "Fixture navigation",
      exact: true,
    });
    await expect(navigation).toBeVisible();
    await expect(
      navigation.getByRole("button", { name: /close/i }),
    ).toHaveCount(0);
    await page.touchscreen.tap(8, 8);
    await expect(navigation).toBeHidden();
    await expectCompactRow(footer);
  });
}

test("mobile settings search opens above the row and navigates to the highlighted setting", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 844 });
  await page.goto("/settings/library");
  const footer = page.locator("[data-mobile-settings-footer]");
  const trigger = footer.getByRole("button", { name: "Search settings" });
  await trigger.tap();
  const input = footer.getByRole("searchbox", { name: "Search settings" });
  await expect(input).toBeFocused();
  await expectCompactRow(footer);
  await input.fill("language");
  const result = page.getByRole("link", { name: /Language Interface/ });
  await expect(result).toBeVisible();
  const inputBox = await input.boundingBox();
  const resultBox = await result.boundingBox();
  if (!inputBox || !resultBox) throw new Error("Missing search geometry");
  expect(resultBox.y + resultBox.height).toBeLessThanOrEqual(inputBox.y);
  await result.tap();
  await expect(page).toHaveURL(/\/settings\/interface\?hl=Language$/);
  await expect(input).toBeHidden();
  await expect(trigger).toBeFocused();
  await expect(
    footer.getByRole("button", { name: "Settings sections" }),
  ).toHaveText("Interface");
  await trigger.tap();
  await expect(input).toHaveValue("");
  await input.fill("no-matching-setting");
  await expect(
    page.getByText("No results found.", { exact: true }),
  ).toBeVisible();
  await footer.getByRole("button", { name: "Close search" }).tap();
  await expectCompactRow(footer);
});

test("settings search follows keyboard geometry and supports keyboard result selection", async ({
  page,
}) => {
  await page.goto("/settings/library");
  const footer = page.locator("[data-mobile-settings-footer]");
  await footer.getByRole("button", { name: "Search settings" }).tap();
  const input = footer.getByRole("searchbox");
  await input.fill("language");
  // Model the keyboard's viewport reduction; physical iOS behavior needs a device check.
  await page.evaluate(() => {
    if (!window.visualViewport) throw new Error("Missing visual viewport");
    Object.defineProperty(window.visualViewport, "height", {
      configurable: true,
      value: 500,
    });
    window.visualViewport.dispatchEvent(new Event("resize"));
  });
  await expect
    .poll(() =>
      footer.evaluate((element) =>
        Math.abs(element.getBoundingClientRect().bottom - 500),
      ),
    )
    .toBeLessThan(1);
  // A broad query fills the result list so its scrolling limit is exercised.
  await input.fill("a");
  const results = page.getByRole("navigation", {
    name: "Settings search results",
  });
  await expect
    .poll(() =>
      results.evaluate((element) => {
        const scroller = element.parentElement;
        return Boolean(
          scroller &&
            scroller.clientHeight <= 250 &&
            scroller.scrollHeight > scroller.clientHeight,
        );
      }),
    )
    .toBe(true);
  await input.fill("language");
  await expect(
    page.getByRole("link", { name: /Language Interface/ }),
  ).toBeInViewport();
  await expect
    .poll(async () => {
      const inputBox = await input.boundingBox();
      const resultBox = await page
        .getByRole("link", { name: /Language Interface/ })
        .boundingBox();
      return Boolean(
        inputBox && resultBox && resultBox.y + resultBox.height <= inputBox.y,
      );
    })
    .toBe(true);
  await input.press("Tab");
  await expect(
    footer.getByRole("button", { name: "Close search" }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("link", { name: /Language Interface/ }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/settings\/interface\?hl=Language$/);
  await expectCompactRow(footer);
});

test("settings retains the desktop sidebar and unsaved fields across a mobile resize", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/settings/interface");
  const draft = page.getByRole("textbox", { name: "Unsaved setting" });
  await draft.fill("Keep this draft");
  await expect(
    page
      .getByRole("navigation", { name: "Settings sections" })
      .getByRole("link"),
  ).toHaveCount(11);
  await expect(page.locator("[data-mobile-settings-footer]")).toHaveCount(0);
  await page
    .getByRole("searchbox", { name: "Search settings" })
    .fill("language");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(draft).toHaveValue("Keep this draft");
  const footer = page.locator("[data-mobile-settings-footer]");
  await expectCompactRow(footer);
  await footer.getByRole("button", { name: "Search settings" }).tap();
  await expect(footer.getByRole("searchbox")).toHaveValue("language");
  await page.keyboard.press("Escape");
  await expect(
    footer.getByRole("button", { name: "Search settings" }),
  ).toBeFocused();
  await page.setViewportSize({ width: 1280, height: 844 });
  await expect(draft).toHaveValue("Keep this draft");
  await expect(
    page.getByRole("searchbox", { name: "Search settings" }),
  ).toHaveValue("language");
  await page.getByRole("link", { name: /Language Interface/ }).click();
  await expect(page).toHaveURL(/\/settings\/interface\?hl=Language$/);
});
