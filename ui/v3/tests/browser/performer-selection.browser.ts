import { libraryID } from "../fixtures/library";
import { test, expect, detailFooter, expectCompactRow } from "./test";

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
    await expect(form.locator('[data-slot="combobox-chip"]')).toHaveCount(80);
    await form.getByRole("button", { name: "Create share", exact: true }).tap();
    await expect
      .poll(() =>
        page.evaluate(() => window.performerSelectionFixture.shares.length),
      )
      .toBe(1);
    expect(
      await page.evaluate(() =>
        window.performerSelectionFixture.shares[0]?.input.targets.map(
          (item) => item.id,
        ),
      ),
    ).toEqual(Array.from({ length: 80 }, (_, index) => libraryID(index + 1)));
  });
}

test("share all matching resolves the complete performer filter and never publishes a partial selection", async ({
  page,
}, testInfo) => {
  await page.goto("/performer-selection?q=Video");
  await page
    .locator(`article[data-id="${libraryID(1)}"]`)
    .locator("[data-entity-card-preview]")
    .click({ modifiers: ["Control"] });
  await detailFooter(page)
    .getByRole("button", { name: "Share…", exact: true })
    .tap();
  const form = page.getByRole("dialog", { name: "Create share", exact: true });
  const all = form.getByRole("button", {
    name: "Use all matching items across pages",
  });
  const chips = form.locator('[data-slot="combobox-chip"]');
  for (const failure of ["error", "partial", "limit"] as const) {
    await page.evaluate((failure) => {
      window.performerSelectionFixture.matching = failure;
    }, failure);
    await all.tap();
    await expect(
      page.locator('[data-sonner-toast][data-front="true"]'),
    ).toContainText(
      failure === "error"
        ? "Matching query failed"
        : failure === "partial"
          ? "Unable to load all matching"
          : "at most 2,000 items",
    );
    await expect(chips).toHaveCount(1);
    await expect
      .poll(() =>
        page.evaluate(() => window.performerSelectionFixture.shares.length),
      )
      .toBe(0);
  }
  await page.evaluate(() => {
    window.performerSelectionFixture.matching = "pending";
  });
  await all.tap();
  await expect(
    form.getByRole("button", { name: "Create share", exact: true }),
  ).toBeDisabled();
  await page.evaluate(() =>
    window.performerSelectionFixture.releaseMatching?.(),
  );
  await expect(chips).toHaveCount(80);
  const request = await page.evaluate(() =>
    window.performerSelectionFixture.requests.find(
      (item) => item.name === "ShareMatchingTargets",
    ),
  );
  expect(request?.variables.filter).toMatchObject({
    page: 1,
    per_page: 2001,
    q: "Video",
  });
  expect(request?.variables).toMatchObject({
    scenes: true,
    images: false,
    galleries: false,
  });
  expect(JSON.stringify(request?.variables.filter_ast)).toContain(
    libraryID(999),
  );
  await page.screenshot({ path: testInfo.outputPath("share-all.png") });
  await form.getByRole("button", { name: "Create share", exact: true }).tap();
  await expect
    .poll(() =>
      page.evaluate(
        () => window.performerSelectionFixture.shares[0]?.input.targets.length,
      ),
    )
    .toBe(80);
});

test("closing the share dialog cancels a pending matching selection", async ({
  page,
}) => {
  await page.goto("/performer-selection");
  await page
    .locator(`article[data-id="${libraryID(1)}"]`)
    .locator("[data-entity-card-preview]")
    .click({ modifiers: ["Control"] });
  const share = detailFooter(page).getByRole("button", {
    name: "Share…",
    exact: true,
  });
  await share.tap();
  const form = page.getByRole("dialog", { name: "Create share", exact: true });
  await page.evaluate(() => {
    window.performerSelectionFixture.matching = "pending";
  });
  await form
    .getByRole("button", { name: "Use all matching items across pages" })
    .tap();
  await form.getByRole("button", { name: "Cancel", exact: true }).tap();
  await page.evaluate(() =>
    window.performerSelectionFixture.releaseMatching?.(),
  );
  await share.tap();
  await expect(form.locator('[data-slot="combobox-chip"]')).toHaveCount(1);
  await expect(
    form.getByRole("button", { name: "Create share", exact: true }),
  ).toBeEnabled();
});
