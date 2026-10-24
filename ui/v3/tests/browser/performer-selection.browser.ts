import { libraryID } from "../fixtures/library";
import {
  test,
  expect,
  detailFooter,
  expectCompactRow,
  expectTouchTargets,
} from "./test";

for (const width of [320, 390]) {
  test(`performer browse bar keeps navigation first among seven controls at ${width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/performer-selection");
    const footer = detailFooter(page);
    await expect(footer.getByRole("button")).toHaveCount(7);
    await expect(footer.getByRole("button").first()).toHaveAccessibleName(
      "Open navigation menu",
    );
    for (const name of [
      "Open navigation menu",
      "Detail sections",
      "Search…",
      "Filters",
      "View options",
      "Entity actions",
      "Back",
    ]) {
      await expect(
        footer.getByRole("button", { name, exact: true }),
      ).toBeVisible();
    }
    await expectCompactRow(footer);
    await expectTouchTargets(footer);
    await page.screenshot({ path: testInfo.outputPath(`browse-${width}.png`) });
    await footer
      .getByRole("button", { name: "Entity actions", exact: true })
      .tap();
    const actions = page.getByRole("dialog", {
      name: "Entity actions",
      exact: true,
    });
    await expect(
      actions.getByRole("button", { name: "Navigation", exact: true }),
    ).toHaveCount(0);
    await actions
      .getByRole("button", { name: "Share this list…", exact: true })
      .tap();
    await expect(actions).toBeHidden();
    await expect(
      page.getByRole("dialog", { name: "Create share", exact: true }),
    ).toBeVisible();
  });
}

for (const width of [320, 390]) {
  test(`mobile performer selection accumulates pages and shares all selected UUIDs at ${width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/performer-selection");
    const first = page.locator(`article[data-id="${libraryID(1)}"]`);
    await first
      .locator("[data-entity-card-preview]")
      .click({ modifiers: ["Control"] });
    const footer = detailFooter(page);
    await footer.getByRole("button", { name: "Select all on page" }).tap();
    await expect(footer).toContainText("40 selected");
    await expectCompactRow(footer);
    await page.screenshot({
      path: testInfo.outputPath(`selection-${width}.png`),
    });
    await footer.getByRole("button", { name: "Pages", exact: true }).tap();
    const picker = page.getByRole("dialog", { name: "Pages", exact: true });
    await picker.getByRole("button", { name: "Next", exact: true }).tap();
    await expect(
      page.locator(`article[data-id="${libraryID(41)}"]`),
    ).toBeVisible();
    await footer.getByRole("button", { name: "Pages", exact: true }).tap();
    await expect(footer).toContainText("40 selected");
    await footer.getByRole("button", { name: "Select all on page" }).tap();
    await expect(footer).toContainText("80 selected");
    await footer.getByRole("button", { name: "Pages", exact: true }).tap();
    await picker.getByRole("button", { name: "Prev", exact: true }).tap();
    await footer.getByRole("button", { name: "Pages", exact: true }).tap();
    await expect(first).toHaveAttribute("data-selected", "true");
    await footer.getByRole("button", { name: "Share…", exact: true }).tap();
    const form = page.getByRole("dialog", {
      name: "Create share",
      exact: true,
    });
    await expect(
      form.getByText("80 selected items", { exact: true }),
    ).toBeVisible();
    await expect(form.getByRole("combobox")).toHaveCount(0);
    await form.getByRole("button", { name: "Create share", exact: true }).tap();
    await expect
      .poll(() =>
        page.evaluate(() => window.performerSelectionFixture.shares.length),
      )
      .toBe(1);
    expect(
      await page.evaluate(() =>
        window.performerSelectionFixture.shares[0]?.input.targets?.map(
          (item) => item.id,
        ),
      ),
    ).toEqual(Array.from({ length: 80 }, (_, index) => libraryID(index + 1)));
  });
}

for (const layout of ["embedded", "standalone", "desktop"]) {
  for (const mode of ["Snapshot", "Ongoing"]) {
    test(`share a ${layout} filtered performer list as ${mode} without fetching its items`, async ({
      page,
    }, testInfo) => {
      const desktop = layout === "desktop";
      if (desktop) await page.setViewportSize({ width: 1280, height: 844 });
      await page.goto(
        `/performer-selection?q=Video${layout === "embedded" ? "" : "&standalone"}`,
      );
      const label = desktop
        ? "Operations"
        : layout === "embedded"
          ? "Entity actions"
          : "List actions";
      await page.getByRole("button", { name: label, exact: true }).tap();
      const actions = desktop
        ? page.getByRole("menu")
        : page.getByRole("dialog", { name: label, exact: true });
      await actions
        .getByRole(desktop ? "menuitem" : "button", {
          name: "Share this list…",
          exact: true,
        })
        .tap();
      await expect(actions).toBeHidden();
      const form = page.getByRole("dialog", {
        name: "Create share",
        exact: true,
      });
      await form.getByRole("button", { name: mode, exact: true }).tap();
      await expect(form.getByRole("combobox")).toHaveCount(0);
      await page.evaluate(() => {
        window.performerSelectionFixture.createError =
          "Unable to create complete snapshot";
      });
      await form
        .getByRole("button", { name: "Create share", exact: true })
        .tap();
      await expect(
        page.locator('[data-sonner-toast][data-front="true"]'),
      ).toContainText("Unable to create complete snapshot");
      expect(
        await page.evaluate(
          () => window.performerSelectionFixture.shares.length,
        ),
      ).toBe(0);
      await expect(form).toBeVisible();
      await expect(
        form.getByRole("button", { name: mode, exact: true }),
      ).toHaveAttribute("aria-pressed", "true");
      await page.evaluate(() => {
        window.performerSelectionFixture.createError = undefined;
      });
      await page.screenshot({
        path: testInfo.outputPath(`share-filter-${mode}.png`),
      });
      await form
        .getByRole("button", { name: "Create share", exact: true })
        .tap();
      await expect
        .poll(() =>
          page.evaluate(() => window.performerSelectionFixture.shares.length),
        )
        .toBe(1);
      const request = await page.evaluate(
        () => window.performerSelectionFixture.shares[0],
      );
      expect(request?.input.targets).toBeUndefined();
      expect(request?.input.source).toMatchObject({
        mode: mode.toUpperCase(),
        filter: { kind: "SCENE", q: "Video" },
      });
      expect(
        JSON.stringify(request?.input.source?.filter?.filter_ast),
      ).toContain(libraryID(999));
      expect(
        await page.evaluate(() =>
          window.performerSelectionFixture.requests.some(
            (request) => request.name === "ShareMatchingTargets",
          ),
        ),
      ).toBe(false);
    });
  }
}

test("switching from filtered ongoing sharing back to selection keeps explicit snapshot membership", async ({
  page,
}) => {
  await page.goto("/performer-selection");
  await page
    .locator(`article[data-id="${libraryID(1)}"]`)
    .locator("[data-entity-card-preview]")
    .click({ modifiers: ["Control"] });
  await detailFooter(page)
    .getByRole("button", { name: "Share…", exact: true })
    .tap();
  const form = page.getByRole("dialog", { name: "Create share", exact: true });
  await form.getByRole("button", { name: "Filtered list", exact: true }).tap();
  await form.getByRole("button", { name: "Ongoing", exact: true }).tap();
  await form.getByRole("button", { name: "Selected items", exact: true }).tap();
  await expect(
    form.getByRole("button", { name: "Ongoing", exact: true }),
  ).toHaveCount(0);
  await form.getByRole("button", { name: "Create share", exact: true }).tap();
  await expect
    .poll(() =>
      page.evaluate(() => window.performerSelectionFixture.shares.length),
    )
    .toBe(1);
  const request = await page.evaluate(
    () => window.performerSelectionFixture.shares[0],
  );
  expect(request?.input.source).toBeUndefined();
  expect(request?.input.targets).toEqual([{ kind: "SCENE", id: libraryID(1) }]);
});
