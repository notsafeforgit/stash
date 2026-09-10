import {
  test,
  expect,
  detailFooter,
  expectCompactRow,
  chooseSection,
  expectTouchTargets,
  holdForContextMenu,
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
  // Base UI establishes the drag origin on the first movement.
  const dragY = y + 1;
  await page.mouse.move(x, dragY);
  await expect(drawer).toHaveAttribute("data-swiping", "");
  const initialTop = await drawer.evaluate(
    (element) => element.getBoundingClientRect().top,
  );
  for (const distance of [60, 120, 180]) {
    await page.mouse.move(x, dragY + distance, { steps: 4 });
    await expect(drawer).toHaveAttribute("data-swiping", "");
    // Measure the rendered position: `translate` and `transform` can each
    // look correct in isolation while composing into twice the movement.
    await expect
      .poll(() =>
        drawer.evaluate(
          (element, top) => element.getBoundingClientRect().top - top,
          initialTop,
        ),
      )
      .toBeCloseTo(distance, 0);
  }
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

test("active search can be previewed, edited and cleared without changing sort", async ({
  page,
}) => {
  await page.clock.install();
  await page.setViewportSize({ width: 320, height: 568 });
  await page.goto("/");
  const footer = detailFooter(page);
  // Keep the gesture's original target addressable while the modal menu hides
  // background controls from the accessibility tree.
  const trigger = footer.getByRole("button", {
    name: "Search…",
    exact: true,
    includeHidden: true,
  });
  const input = footer.getByRole("searchbox");
  const state = page.getByTestId("scenes-list").getByTestId("list-state");
  const query = "a-long-query/".repeat(12);
  await trigger.tap();
  await input.fill(query);
  await footer.getByRole("button", { name: "Close search" }).tap();
  await expect(trigger).toHaveAccessibleDescription(`Search: “${query}”`);
  const initialDirection = await state.getAttribute("data-direction");

  for (const title of ["View options", "Filters"]) {
    await footer.getByRole("button", { name: title, exact: true }).tap();
    const drawer = page.getByRole("dialog", { name: title, exact: true });
    await expect(drawer).toHaveAccessibleDescription(`Search: “${query}”`);
    await expect(
      drawer.getByText(`Search: “${query}”`, { exact: true }),
    ).toBeVisible();
    if (title === "View options") {
      await drawer.getByTitle(/^(Ascending|Descending)$/).tap();
      await expect(state).not.toHaveAttribute(
        "data-direction",
        initialDirection ?? "",
      );
      await expect(state).toHaveAttribute("data-term", query);
    }
    await page.touchscreen.tap(8, 8);
    await expect(drawer).toBeHidden();
  }
  const sort = await state.getAttribute("data-sort");
  const direction = await state.getAttribute("data-direction");

  const menu = page.getByRole("menu");
  await holdForContextMenu(trigger, menu, { x: 0.1, y: 0.1 });
  await expect
    .poll(async () => {
      const anchor = await trigger.boundingBox();
      const popup = await menu.boundingBox();
      if (!anchor || !popup) throw new Error("Missing search preview bounds");
      return anchor.y - popup.y - popup.height;
    })
    .toBeCloseTo(8, 0);
  // Some touch browsers synthesize a click on release. It must remain a preview.
  await trigger.dispatchEvent("click");
  await expect(input).toHaveCount(0);
  await expect(menu).toContainText(`Search: “${query}”`);
  expect(
    await menu.evaluate(
      (element) => element.scrollWidth <= element.clientWidth,
    ),
  ).toBe(true);
  // Base UI intentionally ignores outside presses for 500ms after a long press
  // opens the menu, so that releasing the opening gesture cannot dismiss it.
  await page.clock.runFor(500);
  await page.touchscreen.tap(8, 8);
  await expect(menu).toBeHidden();
  await expectCompactRow(footer);

  await holdForContextMenu(trigger, menu, { x: 0.9, y: 0.9 });
  await expect
    .poll(async () => {
      const anchor = await trigger.boundingBox();
      const popup = await menu.boundingBox();
      if (!anchor || !popup) throw new Error("Missing search preview bounds");
      return anchor.y - popup.y - popup.height;
    })
    .toBeCloseTo(8, 0);
  await menu.getByRole("menuitem", { name: "Edit search" }).tap();
  await expect(input).toBeFocused();
  await expect(input).toHaveValue(query);
  await footer.getByRole("button", { name: "Close search" }).tap();
  await holdForContextMenu(trigger, menu);
  await menu.getByRole("menuitem", { name: "Clear search" }).tap();
  await expect(menu).toBeHidden();
  await expect(input).toHaveCount(0);
  await expect(state).toHaveAttribute("data-term", "");
  expect(await state.getAttribute("data-sort")).toBe(sort);
  expect(await state.getAttribute("data-direction")).toBe(direction);
  await expect(trigger).not.toHaveAttribute("aria-description");
  await trigger.tap();
  await expect(input).toBeFocused();
  await expect(input).toHaveValue("");
});

for (const reducedMotion of ["no-preference", "reduce"] as const) {
  test(`search keeps focus and layout when its reveal is interrupted (${reducedMotion})`, async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion });
    await page.goto("/");
    const footer = detailFooter(page);
    const trigger = footer.getByRole("button", {
      name: "Search…",
      exact: true,
    });
    // Inspect the same event turn as opening, then pause partway through the
    // browser animation so this also exercises closing before reveal completes.
    const opening = await trigger.evaluate((element) => {
      if (!(element instanceof HTMLButtonElement))
        throw new Error("Missing search trigger");
      element.click();
      const row = document.querySelector("[data-mobile-search-row]");
      const input = row?.querySelector("input");
      if (!row || !input) throw new Error("Search did not mount synchronously");
      const animation = row.getAnimations()[0];
      const before = input.getBoundingClientRect();
      if (animation) {
        animation.pause();
        animation.currentTime = 60;
      }
      const during = input.getBoundingClientRect();
      return {
        focused: document.activeElement === input,
        animated: !!animation,
        stableBounds:
          before.x === during.x &&
          before.y === during.y &&
          before.width === during.width &&
          before.height === during.height,
      };
    });
    expect(opening).toEqual({
      focused: true,
      animated: reducedMotion === "no-preference",
      stableBounds: true,
    });
    const input = footer.getByRole("searchbox");
    await input.fill("preserve during collapse");
    await footer
      .getByRole("button", { name: "Close search" })
      .dispatchEvent("click");
    await expect(trigger).toBeFocused();
    await expect(input).toHaveCount(0);
    await expectCompactRow(footer);
    await expect(
      page.getByTestId("scenes-list").getByTestId("list-state"),
    ).toHaveAttribute("data-term", "preserve during collapse");
    await trigger.tap();
    await expect(input).toBeFocused();
    await expect(input).toHaveValue("preserve during collapse");
    await footer.getByRole("button", { name: "Close search" }).tap();
    await footer
      .getByRole("button", { name: "View options", exact: true })
      .tap();
    await expect(
      page.getByRole("dialog", { name: "View options", exact: true }),
    ).toBeVisible();
  });
}

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

for (const layout of ["standalone", "collection", "media"]) {
  test(`search reserves keyboard space without a delayed pan correction (${layout})`, async ({
    page,
  }) => {
    await page.goto(`/?${layout}`);
    const footer =
      layout === "standalone"
        ? page.locator("[data-mobile-list-mode]")
        : detailFooter(page);
    await footer.getByRole("button", { name: "Search…", exact: true }).tap();
    await expect(footer.getByRole("searchbox")).toBeFocused();
    // Model keyboard opening, Safari's pan, then its reset. Check geometry during
    // the event, not after polling: the old rAF + React update briefly double-lifted
    // the footer. Actual OS keyboard timing still needs a physical iPhone check.
    for (const geometry of [
      { height: 650, offsetTop: 0 },
      { height: 500, offsetTop: 0 },
      { height: 500, offsetTop: 240 },
      { height: 500, offsetTop: 344 },
      { height: 500, offsetTop: 0 },
    ]) {
      const bounds = await footer.evaluate((element, geometry) => {
        if (!window.visualViewport) throw new Error("Missing visual viewport");
        for (const [key, value] of Object.entries(geometry)) {
          Object.defineProperty(window.visualViewport, key, {
            configurable: true,
            value,
          });
        }
        window.visualViewport.dispatchEvent(new Event("resize"));
        window.visualViewport.dispatchEvent(new Event("scroll"));
        // Detail pages scroll the full collection/media region. Standalone
        // pages scroll the list itself; both must end above the search row.
        const scroller = element.hasAttribute("data-mobile-detail-footer")
          ? element.previousElementSibling
          : document.querySelector("[data-scroll-restoration-id]");
        if (!scroller) throw new Error("Missing list scroller");
        return {
          footerTop: element.getBoundingClientRect().top,
          footerBottom: element.getBoundingClientRect().bottom,
          listBottom: scroller.getBoundingClientRect().bottom,
        };
      }, geometry);
      expect(bounds.footerBottom).toBeCloseTo(
        geometry.height + geometry.offsetTop,
        0,
      );
      expect(bounds.listBottom).toBeLessThanOrEqual(bounds.footerTop + 1);
    }
    // Dismiss the keyboard while keeping search open, then focus the same field.
    await footer.getByRole("searchbox").blur();
    await expectCompactRow(footer);
    await footer.getByRole("searchbox").focus();
    expect(
      await footer.evaluate(
        (element) => element.getBoundingClientRect().bottom,
      ),
    ).toBeCloseTo(500, 0);
    await footer.getByRole("button", { name: "Close search" }).tap();
    await expectCompactRow(footer);
  });
}
