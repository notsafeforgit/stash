import {
  test,
  expect,
  detailFooter,
  expectCompactRow,
  chooseSection,
  expectTouchTargets,
} from "./test";

test("mobile drawers open directly and dismiss by tapping outside", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await page.goto("/");
  const footer = detailFooter(page);
  for (const [control, title] of [
    ["Navigation", "Fixture navigation"],
    ["Filters", "Filters"],
    ["View options", "View options"],
    ["Entity actions", "Entity actions"],
  ]) {
    await footer.getByRole("button", { name: control, exact: true }).tap();
    const drawer = page.getByRole("dialog", { name: title, exact: true });
    await expect(drawer).toBeVisible();
    await expect(
      drawer.getByRole("button", { name: "Close", exact: true }),
    ).toHaveCount(0);
    await page.touchscreen.tap(8, 8);
    await expect(drawer).toBeHidden();
    await expectCompactRow(footer);
    await expectTouchTargets(footer);
  }
});

test("mobile action groups are flat and their forms outlive drawer dismissal", async ({
  page,
}) => {
  await page.goto("/");
  const trigger = detailFooter(page).getByRole("button", {
    name: "Entity actions",
    exact: true,
  });
  for (const action of [
    "Auto tag…",
    "Rotate clockwise",
    "Rotate counter-clockwise",
  ]) {
    await trigger.tap();
    const drawer = page.getByRole("dialog", {
      name: "Entity actions",
      exact: true,
    });
    await expect(
      drawer.getByRole("button", { name: "Operations", exact: true }),
    ).toHaveCount(0);
    await expect(drawer.getByRole("menuitem")).toHaveCount(0);
    await drawer.getByRole("button", { name: action, exact: true }).tap();
    await expect(drawer).toBeHidden();
    const form = page.getByRole("dialog", { name: "Action form", exact: true });
    await expect(form).toBeVisible();
    await form.getByRole("button", { name: "Cancel action" }).tap();
    await expect(form).toBeHidden();
  }
  await trigger.tap();
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
});

test("desktop keeps operation submenus and opens the same action forms", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/");
  await page.getByRole("button", { name: "Operations", exact: true }).click();
  await page.getByRole("menuitem", { name: "Rotation", exact: true }).hover();
  await page
    .getByRole("menuitem", { name: "Rotate clockwise", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Action form", exact: true }),
  ).toBeVisible();
});

test("drawers track a downward drag and dismiss", async ({ page }) => {
  await page.goto("/");
  const trigger = detailFooter(page).getByRole("button", {
    name: "Entity actions",
    exact: true,
  });
  await trigger.tap();
  const drawer = page.getByRole("dialog", {
    name: "Entity actions",
    exact: true,
  });
  await expect(drawer).toBeVisible();
  // Exercise the primitive's pointer gesture; physical iOS touch remains a device check.
  const handle = drawer.locator('[aria-hidden="true"]').first();
  await handle.hover();
  const bounds = await handle.boundingBox();
  if (!bounds) throw new Error("Missing drawer handle");
  const x = bounds.x + bounds.width / 2;
  const y = bounds.y + bounds.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x, y + 180, { steps: 12 });
  await expect(drawer).toHaveAttribute("data-swiping", "");
  await expect
    .poll(() =>
      drawer.evaluate((element) =>
        parseFloat(getComputedStyle(element).translate.split(" ")[1] ?? "0"),
      ),
    )
    .toBeGreaterThan(100);
  await page.mouse.up();
  await expect(drawer).toBeHidden();
});

test("search commits on close and keeps its value across section changes", async ({
  page,
}) => {
  await page.goto("/");
  const footer = detailFooter(page);
  await footer.getByRole("button", { name: "Search…", exact: true }).tap();
  const search = footer.getByRole("searchbox");
  await expect(search).toBeFocused();
  await expect(
    footer.getByRole("button", { name: "Back", exact: true }),
  ).toHaveCount(0);
  await search.fill("typed just before close");
  await footer.getByRole("button", { name: "Close search" }).tap();
  await expect(
    page.getByTestId("scenes-list").getByTestId("list-state"),
  ).toHaveAttribute("data-term", "typed just before close");
  await expect(
    footer.getByRole("button", { name: "Search…", exact: true }),
  ).toBeFocused();
  await chooseSection(page, "Images");
  await chooseSection(page, "Scenes");
  await footer.getByRole("button", { name: "Search…", exact: true }).tap();
  await expect(search).toHaveValue("typed just before close");
  await footer.getByRole("button", { name: "Clear", exact: true }).tap();
  await footer.getByRole("button", { name: "Close search" }).tap();
  await expect(
    page.getByTestId("scenes-list").getByTestId("list-state"),
  ).toHaveAttribute("data-term", "");
  await expectCompactRow(footer);
});

test("selection replaces the row and keeps select-all and close at the right", async ({
  page,
}) => {
  await page.goto("/");
  const footer = detailFooter(page);
  await page.getByRole("button", { name: "Enter selection" }).tap();
  const selectAll = footer.getByRole("button", { name: "Select all on page" });
  const close = footer.getByRole("button", {
    name: "Select None",
    exact: true,
  });
  await selectAll.tap();
  await expect(footer).toContainText("40 selected");
  await expect(
    footer.getByRole("button", { name: "Detail sections" }),
  ).toBeHidden();
  await expect(
    footer.getByRole("button", { name: "Entity actions", exact: true }),
  ).toBeHidden();
  await expectCompactRow(footer);
  const allBounds = await selectAll.boundingBox();
  const closeBounds = await close.boundingBox();
  if (!allBounds || !closeBounds) throw new Error("Missing selection controls");
  expect(closeBounds.x).toBeGreaterThan(allBounds.x);
  expect(closeBounds.x + closeBounds.width).toBeCloseTo(
    await footer.evaluate(
      (element) => element.getBoundingClientRect().right - 6,
    ),
    0,
  );
  await close.tap();
  await expect(
    footer.getByRole("button", { name: "Back", exact: true }),
  ).toBeVisible();
});

test("page jump rejects invalid values and resets its draft after navigation", async ({
  page,
}) => {
  await page.goto("/");
  const footer = detailFooter(page);
  const sections = footer.getByRole("button", { name: "Detail sections" });
  await sections.tap();
  const input = page.getByRole("spinbutton", { name: "Go to page" });
  const state = page.getByTestId("scenes-list").getByTestId("list-state");
  await expect(input).not.toBeFocused();
  for (const invalid of ["", "0", "6", "1.5"]) {
    await input.fill(invalid);
    await page.getByRole("button", { name: "Go", exact: true }).tap();
    await expect(input).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByRole("alert")).toHaveText(
      "Enter a whole page number between 1 and 5.",
    );
    await expect(state).toContainText("page 1:");
    await expect(sections).toHaveAttribute("aria-expanded", "true");
  }
  await input.fill("3");
  await input.press("Enter");
  await expect(state).toContainText("page 3:");
  await expect(sections).toHaveAttribute("aria-expanded", "false");
  await sections.tap();
  await expect(input).toHaveValue("3");
  await input.fill("5");
  await page
    .locator('[data-slot="popover-content"]:visible')
    .getByRole("button", { name: "Prev", exact: true })
    .tap();
  await expect(state).toContainText("page 2:");
  await sections.tap();
  await expect(input).toHaveValue("2");
  await page.keyboard.press("Escape");
  await page
    .getByRole("tabpanel")
    .getByRole("navigation", { name: "Pages", exact: true })
    .getByRole("button", { name: "Next", exact: true })
    .tap();
  await expect(state).toContainText("page 3:");
  await expectCompactRow(footer);
});

test("page jump resets its draft when the result count changes", async ({
  page,
}) => {
  await page.goto("/");
  const sections = detailFooter(page).getByRole("button", {
    name: "Detail sections",
  });
  await sections.tap();
  const input = page.getByRole("spinbutton", { name: "Go to page" });
  await input.fill("5");
  await page.getByRole("button", { name: "Reduce results", exact: true }).tap();
  await expect(sections).toHaveAttribute("aria-expanded", "false");
  await sections.tap();
  await expect(input).toHaveAttribute("max", "2");
  await expect(input).toHaveValue("1");
  await expect(input).toHaveAttribute("aria-invalid", "false");
});

test("Actions retains action state and closes before opening the editor", async ({
  page,
}) => {
  await page.goto("/");
  const more = detailFooter(page).getByRole("button", {
    name: "Entity actions",
    exact: true,
  });
  await more.tap();
  await page.getByRole("button", { name: "Favourite", exact: true }).tap();
  await page.keyboard.press("Escape");
  await more.tap();
  await expect(
    page.getByRole("button", { name: "Favourited", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Edit", exact: true }).tap();
  await expect(more).toHaveAttribute("aria-expanded", "false");
  await page.getByRole("button", { name: "Close editor" }).tap();
  await expect(page.getByRole("button", { name: "Close editor" })).toBeHidden();
  await expectCompactRow(detailFooter(page));
});

test("direct filters preserve the active list's filter context", async ({
  page,
}) => {
  await page.goto("/");
  const footer = detailFooter(page);
  await chooseSection(page, "Images");
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  await expect(
    page.getByRole("dialog", { name: "Filters", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Example filters", { exact: true }),
  ).toBeVisible();
  await expect(
    footer.getByRole("button", {
      name: "Entity actions",
      exact: true,
      includeHidden: true,
    }),
  ).toHaveAttribute("aria-expanded", "false");
});

test("standalone lists provide page jumping, view options, and navigation", async ({
  page,
}) => {
  await page.goto("/?standalone");
  const row = page.locator("[data-mobile-list-mode]");
  await expectCompactRow(row);
  await row.getByRole("button", { name: "Pages", exact: true }).tap();
  await page.getByRole("spinbutton", { name: "Go to page" }).fill("2");
  await page.getByRole("button", { name: "Go", exact: true }).tap();
  await expect(page.getByTestId("list-state")).toContainText("page 2:");
  await page.getByRole("button", { name: "View options", exact: true }).tap();
  await expect(
    page.getByRole("dialog", { name: "View options", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await row.getByRole("button", { name: "Navigation", exact: true }).tap();
  await expect(
    page.getByRole("dialog", { name: "Fixture navigation" }),
  ).toBeVisible();
  await page.touchscreen.tap(8, 8);
  await expectCompactRow(row);
});

test("single-page lists omit page navigation", async ({ page }) => {
  await page.goto("/?single");
  await detailFooter(page)
    .getByRole("button", { name: "Detail sections" })
    .tap();
  await expect(
    page.getByRole("navigation", { name: "Pages", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("spinbutton", { name: "Go to page" }),
  ).toHaveCount(0);
  await page.goto("/?standalone&single");
  await expectCompactRow(page.locator("[data-mobile-list-mode]"));
  await expect(
    page.getByRole("button", { name: "Pages", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("navigation", { name: "Pages", exact: true }),
  ).toHaveCount(0);
});

test("section picker supports vertical keyboard navigation and dismissal", async ({
  page,
}) => {
  await page.goto("/");
  const trigger = detailFooter(page).getByRole("button", {
    name: "Detail sections",
  });
  await trigger.tap();
  await expect(page.getByRole("tablist")).toHaveAttribute(
    "aria-orientation",
    "vertical",
  );
  await page.getByRole("tab", { name: "Scenes", exact: true }).focus();
  await page.keyboard.press("ArrowDown");
  await expect(
    page.getByRole("tab", { name: "Images", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(trigger).toHaveText("Images");
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
  await expect(trigger).toBeFocused();
});

test("resizing to desktop and back preserves the mounted list and its state", async ({
  page,
}) => {
  await page.goto("/");
  await chooseSection(page, "Scenes");
  const footer = detailFooter(page);
  await footer.getByRole("button", { name: "Search…", exact: true }).tap();
  await footer.getByRole("searchbox").fill("retained across resize");
  await footer.getByRole("button", { name: "Close search" }).tap();
  const panel = await page.getByTestId("scenes-list").elementHandle();
  if (!panel) throw new Error("Missing list panel");
  await page.setViewportSize({ width: 1280, height: 844 });
  await expect(detailFooter(page)).toHaveCount(0);
  expect(await panel.evaluate((element) => element.isConnected)).toBe(true);
  await page.setViewportSize({ width: 390, height: 844 });
  await expectCompactRow(detailFooter(page));
  expect(await panel.evaluate((element) => element.isConnected)).toBe(true);
  await expect(
    page.getByTestId("scenes-list").getByTestId("list-state"),
  ).toHaveAttribute("data-term", "retained across resize");
});

test("search follows a reduced visual viewport and returns when focus leaves", async ({
  page,
}) => {
  await page.goto("/");
  const footer = detailFooter(page);
  await footer.getByRole("button", { name: "Search…", exact: true }).tap();
  await expect(footer.getByRole("searchbox")).toBeFocused();
  // Desktop browser automation cannot open a physical iOS keyboard. Model only
  // its viewport resize here; native keyboard behavior still needs a device check.
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
  await footer.getByRole("button", { name: "Close search" }).tap();
  await expectCompactRow(footer);
});
