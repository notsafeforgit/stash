import type { Page } from "@playwright/test";
import { test, expect, expectTouchTargets } from "./test";

const favouriteId = "00000000-0000-4000-8000-000000009001";
const shortId = "00000000-0000-4000-8000-000000009002";

async function openFilters(page: Page, kind = "image") {
  await page.addInitScript(() => {
    window.savedFiltersFixture = true;
  });
  await page.goto(
    kind === "marker" ? "/scenes/markers" : `/lightbox-list?kind=${kind}`,
  );
  await page.getByRole("button", { name: "Filters", exact: true }).tap();
  const trigger = page.getByRole("button", {
    name: "Saved filters",
    exact: true,
  });
  await trigger.tap();
  const picker = page.getByRole("dialog", {
    name: "Saved filters",
    exact: true,
  });
  await expect(
    picker.getByRole("button", { name: "Favourite clips", exact: true }),
  ).toBeVisible();
  return picker;
}

for (const kind of ["image", "scene", "marker"]) {
  for (const width of [390, 1280]) {
    test(`saved ${kind} filters can be deleted without altering the current query at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      const picker = await openFilters(page, kind);
      await expectTouchTargets(picker);
      await picker
        .getByRole("button", { name: "Pin Favourite clips", exact: true })
        .tap();
      await expect(
        picker.getByRole("button", {
          name: "Unpin Favourite clips",
          exact: true,
        }),
      ).toBeVisible();
      await expect(page).not.toHaveURL(/fa=/);
      await picker
        .getByRole("button", { name: "Favourite clips", exact: true })
        .tap();
      await expect(picker).toBeHidden();
      await expect(page).toHaveURL(/fa=/);
      const conditions = page.locator(".filter-condition-shell");
      await expect(conditions).toHaveCount(1);
      const appliedInputs = await conditions
        .locator("input")
        .evaluateAll((inputs) =>
          inputs.map((input) =>
            input instanceof HTMLInputElement ? input.value : null,
          ),
        );
      const filteredURL = page.url();
      const trigger = page.getByRole("button", {
        name: "Favourite clips",
        exact: true,
      });
      await trigger.tap();
      await picker
        .getByRole("button", { name: "Delete Short clips", exact: true })
        .tap();
      const confirmation = page.getByRole("dialog", {
        name: "Delete “Short clips”?",
        exact: true,
      });
      await expect(confirmation).toBeVisible();
      await expect(picker).toBeHidden();
      expect(await page.evaluate(() => window.savedFilterMutations)).toEqual(
        [],
      );
      await confirmation
        .getByRole("button", { name: "Cancel", exact: true })
        .tap();
      await expect(confirmation).toBeHidden();
      await expect(trigger).toBeFocused();
      await trigger.tap();
      await picker
        .getByRole("button", { name: "Delete Short clips", exact: true })
        .tap();
      await confirmation
        .getByRole("button", { name: "Delete", exact: true })
        .tap();
      await expect(confirmation).toBeHidden();
      await expect(trigger).toBeFocused();
      await expect(page).toHaveURL(filteredURL);
      await trigger.tap();
      await expect(
        picker.getByRole("button", { name: "Short clips", exact: true }),
      ).toHaveCount(0);

      await picker
        .getByRole("button", { name: "Delete Favourite clips", exact: true })
        .tap();
      const activeConfirmation = page.getByRole("dialog", {
        name: "Delete “Favourite clips”?",
        exact: true,
      });
      await activeConfirmation
        .getByRole("button", { name: "Delete", exact: true })
        .tap();
      await expect(activeConfirmation).toBeHidden();
      await expect(page).toHaveURL(filteredURL);
      await expect(conditions).toHaveCount(1);
      expect(
        await conditions
          .locator("input")
          .evaluateAll((inputs) =>
            inputs.map((input) =>
              input instanceof HTMLInputElement ? input.value : null,
            ),
          ),
      ).toEqual(appliedInputs);
      const clearedTrigger = page.getByRole("button", {
        name: "Saved filters",
        exact: true,
      });
      await expect(clearedTrigger).toBeFocused();
      await clearedTrigger.tap();
      await expect(
        picker.getByText("No saved filters yet", { exact: true }),
      ).toBeVisible();
      expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
        { operation: "DestroySavedFilter", id: shortId },
        { operation: "DestroySavedFilter", id: favouriteId },
      ]);
      expect(
        await page.evaluate(() =>
          Object.entries(localStorage)
            .filter(([key]) =>
              key.startsWith("filter-builder-pinned-saved-filters:"),
            )
            .map(([, value]) => JSON.parse(value)),
        ),
      ).toEqual([[]]);
    });
  }
}

for (const failure of ["false", "network"] as const) {
  test(`saved filter deletion preserves the preset on ${failure} failure and waits for confirmation`, async ({
    page,
  }) => {
    const picker = await openFilters(page);
    await picker
      .getByRole("button", { name: "Pin Favourite clips", exact: true })
      .tap();
    await page.evaluate((failure) => {
      window.savedFilterFailure = failure;
      window.holdSavedFilterDelete = true;
    }, failure);
    await picker
      .getByRole("button", { name: "Delete Favourite clips", exact: true })
      .tap();
    const confirmation = page.getByRole("dialog", {
      name: "Delete “Favourite clips”?",
      exact: true,
    });
    await confirmation
      .getByRole("button", { name: "Delete", exact: true })
      .tap();
    await expect(
      confirmation.getByRole("button", { name: "Loading Delete", exact: true }),
    ).toBeDisabled();
    await expect(
      confirmation.getByRole("button", { name: "Cancel", exact: true }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(confirmation).toBeVisible();
    await page.evaluate(() => window.releaseSavedFilterDelete?.());
    await expect(
      page.getByText(
        failure === "false"
          ? "The saved filter could not be deleted. Try again."
          : "Saved filter service unavailable",
        { exact: true },
      ),
    ).toBeVisible();
    await confirmation
      .getByRole("button", { name: "Cancel", exact: true })
      .tap();
    await page
      .getByRole("button", { name: "Saved filters", exact: true })
      .tap();
    await expect(
      picker.getByRole("button", {
        name: "Unpin Favourite clips",
        exact: true,
      }),
    ).toBeVisible();
    await page.evaluate(() => {
      window.savedFilterFailure = undefined;
      window.holdSavedFilterDelete = false;
    });
    await picker
      .getByRole("button", { name: "Delete Favourite clips", exact: true })
      .tap();
    await confirmation
      .getByRole("button", { name: "Delete", exact: true })
      .tap();
    await expect(confirmation).toBeHidden();
    await page
      .getByRole("button", { name: "Saved filters", exact: true })
      .tap();
    await expect(
      picker.getByRole("button", { name: "Favourite clips", exact: true }),
    ).toHaveCount(0);
    expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
      { operation: "DestroySavedFilter", id: favouriteId },
      { operation: "DestroySavedFilter", id: favouriteId },
    ]);
  });
}

test("saved filter search, pin and delete actions are keyboard accessible", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  const picker = await openFilters(page);
  const search = picker.getByRole("textbox", {
    name: "Search saved filters…",
    exact: true,
  });
  await search.fill("missing");
  await expect(
    picker.getByText("No matching saved filters", { exact: true }),
  ).toBeVisible();
  await search.fill("short");
  await search.press("Tab");
  await expect(
    picker.getByRole("button", { name: "Short clips", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(
    picker.getByRole("button", { name: "Pin Short clips", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(
    picker.getByRole("button", { name: "Unpin Short clips", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(
    picker.getByRole("button", { name: "Delete Short clips", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  const confirmation = page.getByRole("dialog", {
    name: "Delete “Short clips”?",
    exact: true,
  });
  await expect(confirmation).toBeVisible();
  await expect(
    confirmation.getByRole("button", { name: "Cancel", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(
    confirmation.getByRole("button", { name: "Delete", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(confirmation).toBeHidden();
  await expect(
    page.getByRole("button", { name: "Saved filters", exact: true }),
  ).toBeFocused();
  expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
    { operation: "DestroySavedFilter", id: shortId },
  ]);
});

test("a saved filter with a bare root condition remains editable", async ({
  page,
}) => {
  const picker = await openFilters(page);
  await picker
    .getByRole("button", { name: "Favourite clips", exact: true })
    .tap();
  const field = page.locator(".filter-condition-shell").getByRole("textbox");
  await expect(field).toHaveValue("Favourite clips");
  const appliedURL = page.url();
  await field.fill("Updated title");
  await expect(field).toHaveValue("Updated title");
  await expect(page).not.toHaveURL(appliedURL);
  await expect(page.locator(".filter-condition-shell")).toHaveCount(1);
});

test("saving and overwriting still work after removing a preset", async ({
  page,
}) => {
  const picker = await openFilters(page);
  await picker
    .getByRole("button", { name: "Delete Short clips", exact: true })
    .tap();
  const confirmation = page.getByRole("dialog", {
    name: "Delete “Short clips”?",
    exact: true,
  });
  await confirmation.getByRole("button", { name: "Delete", exact: true }).tap();
  await expect(confirmation).toBeHidden();
  await page.getByRole("button", { name: "Save filter", exact: true }).tap();
  const save = page.getByRole("dialog", {
    name: "Save filter",
    exact: true,
  });
  await expect(
    save.getByRole("button", { name: "Short clips", exact: true }),
  ).toHaveCount(0);
  await save
    .getByRole("textbox", { name: "Filter name", exact: true })
    .fill("New preset");
  await save.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(save).toBeHidden();
  await page.getByRole("button", { name: "New preset", exact: true }).tap();
  await expect(
    picker.getByRole("button", { name: "New preset", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Save filter", exact: true }).tap();
  await save
    .getByRole("button", { name: "Favourite clips", exact: true })
    .tap();
  await save.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(save).toBeHidden();
  expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
    { operation: "DestroySavedFilter", id: shortId },
    { operation: "SaveFilter", name: "New preset" },
    { operation: "SaveFilter", id: favouriteId, name: "Favourite clips" },
  ]);
});
