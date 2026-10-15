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
  const name = save.getByRole("combobox", { name: "Filter name", exact: true });
  await name.fill("Short");
  await expect(
    page.getByRole("option", { name: "Short clips", exact: true }),
  ).toHaveCount(0);
  await name.fill("New preset");
  await save.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(save).toBeHidden();
  await page.getByRole("button", { name: "New preset", exact: true }).tap();
  await expect(
    picker.getByRole("button", { name: "New preset", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Save filter", exact: true }).tap();
  await name.fill("Favourite");
  await page
    .getByRole("option", { name: "Favourite clips", exact: true })
    .tap();
  await save.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(save).toBeHidden();
  expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
    { operation: "DestroySavedFilter", id: shortId },
    { operation: "SaveFilter", name: "New preset" },
    { operation: "SaveFilter", id: favouriteId, name: "Favourite clips" },
  ]);
});

async function openSaveFilter(page: Page, kind = "image") {
  await openFilters(page, kind);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Save filter", exact: true }).tap();
  const dialog = page.getByRole("dialog", { name: "Save filter", exact: true });
  const name = dialog.getByRole("combobox", {
    name: "Filter name",
    exact: true,
  });
  await expect(name).toBeVisible();
  return {
    dialog,
    name,
    save: dialog.getByRole("button", { name: "Save", exact: true }),
  };
}

for (const viewport of [
  { width: 390, height: 844 },
  { width: 320, height: 568 },
  { width: 844, height: 390 },
  { width: 1280, height: 844 },
]) {
  test(`save filter layout stays stable while searching and selecting at ${viewport.width}x${viewport.height}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport);
    await page.addInitScript(() => {
      window.savedFilterNames = [
        "Favourite clips",
        "Short clips",
        ...Array.from({ length: 30 }, (_, index) => `Preset ${index + 1}`),
      ];
    });
    const { dialog, name, save } = await openSaveFilter(page);
    await dialog.evaluate(async (element) => {
      await Promise.all(
        element.getAnimations().map((animation) => animation.finished),
      );
    });
    const bounds = async () =>
      Promise.all(
        [dialog, name, save].map((element) =>
          element.evaluate((node) => {
            const { x, y, width, height } = node.getBoundingClientRect();
            return { x, y, width, height };
          }),
        ),
      );
    const initial = await bounds();
    for (const box of initial) {
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.y).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
      expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);
    }
    await testInfo.attach("save-filter-many-matches", {
      body: await dialog.screenshot(),
      contentType: "image/png",
    });
    const expectStable = async () => {
      const current = await bounds();
      for (const [index, box] of current.entries()) {
        const original = initial[index];
        if (!original) throw new Error("Missing initial dialog bounds");
        for (const dimension of ["x", "y", "width", "height"] as const) {
          expect(
            Math.abs(box[dimension] - original[dimension]),
            `${["dialog", "input", "Save button"][index]} ${dimension}`,
          ).toBeLessThan(1);
        }
      }
      await expect(name).toBeFocused();
    };
    for (const letter of "Short") {
      await name.pressSequentially(letter);
      await expectStable();
    }
    await expect(dialog.getByRole("option")).toHaveCount(1);
    await name.fill("No matching preset");
    await expectStable();
    await expect(
      dialog.getByText("No matching filters. Save to create this name.", {
        exact: true,
      }),
    ).toBeVisible();
    await name.fill("Short clips");
    await expect(save).toBeDisabled();
    await expectStable();
    await name.press("ArrowDown");
    await name.press("Enter");
    await expect(save).toBeEnabled();
    await expectStable();
    if (viewport.height >= 568) {
      await expect(
        dialog.getByText('Saved filter "Short clips" will be overwritten.', {
          exact: true,
        }),
      ).toBeInViewport({ ratio: 1 });
    }
    await testInfo.attach("save-filter-overwrite", {
      body: await dialog.screenshot(),
      contentType: "image/png",
    });
    await name.fill("Short clips copy");
    await expectStable();
    await name.clear();
    await expect(dialog.getByRole("option")).toHaveCount(32);
    await expectStable();
    expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([]);
  });
}

for (const kind of ["image", "scene", "marker"]) {
  test(`saving ${kind} filters searches existing names and selects an overwrite with the keyboard`, async ({
    page,
  }) => {
    const { dialog, name, save } = await openSaveFilter(page, kind);
    await expect(save).toBeDisabled();
    await name.fill("cLiPs");
    await expect(page.getByRole("option")).toHaveCount(2);
    await name.fill("sHoRt");
    await expect(page.getByRole("option")).toHaveCount(1);
    await expect(
      page.getByRole("option", { name: "Short clips", exact: true }),
    ).toBeVisible();
    await name.press("ArrowDown");
    await name.press("Enter");
    await expect(name).toHaveValue("Short clips");
    await expect(
      dialog.getByText('Saved filter "Short clips" will be overwritten.', {
        exact: true,
      }),
    ).toBeVisible();
    expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([]);
    await save.tap();
    await expect(dialog).toBeHidden();
    expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
      { operation: "SaveFilter", id: shortId, name: "Short clips" },
    ]);
  });
}

for (const submit of ["button", "Enter"]) {
  test(`a typed name saves a new filter with ${submit} when suggestions are not selected`, async ({
    page,
  }) => {
    const { dialog, name, save } = await openSaveFilter(page);
    await name.fill("clips");
    await expect(page.getByRole("option")).toHaveCount(2);
    if (submit === "button") await save.tap();
    else await name.press("Enter");
    await expect(dialog).toBeHidden();
    expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
      { operation: "SaveFilter", name: "clips" },
    ]);
  });
}

test("many saved filters scroll inside the save dialog on a small screen", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 568 });
  await page.addInitScript(() => {
    window.savedFilterNames = [
      "Favourite clips",
      "Short clips",
      ...Array.from({ length: 30 }, (_, index) => `Preset ${index + 1}`),
    ];
  });
  const { dialog, name, save } = await openSaveFilter(page);
  const list = dialog.getByRole("listbox");
  await expect(dialog.getByRole("option")).toHaveCount(32);
  const dimensions = await list.evaluate((element) => ({
    height: element.clientHeight,
    content: element.scrollHeight,
    bottom: element.getBoundingClientRect().bottom,
  }));
  expect(dimensions.content).toBeGreaterThan(dimensions.height);
  expect(dimensions.bottom).toBeLessThan(568);
  await list.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await dialog.getByRole("option", { name: "Preset 30", exact: true }).tap();
  await expect(name).toHaveValue("Preset 30");
  await save.tap();
  await expect(dialog).toBeHidden();
  expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
    {
      operation: "SaveFilter",
      id: "00000000-0000-4000-8000-000000009032",
      name: "Preset 30",
    },
  ]);
});

test("typing an existing name requires an explicit overwrite selection", async ({
  page,
}) => {
  const { dialog, name, save } = await openSaveFilter(page);
  await name.fill(" Short clips ");
  await expect(save).toBeDisabled();
  await expect(
    dialog.getByText(
      "A filter with this name already exists. Select it to overwrite it, or choose another name.",
      { exact: true },
    ),
  ).toBeVisible();
  await name.press("Enter");
  expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([]);
  await page.getByRole("option", { name: "Short clips", exact: true }).tap();
  await expect(save).toBeEnabled();
  await save.tap();
  await expect(dialog).toBeHidden();
  expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
    { operation: "SaveFilter", id: shortId, name: "Short clips" },
  ]);
});

test("editing a selected name clears overwrite intent and saves a new filter", async ({
  page,
}) => {
  const { dialog, name, save } = await openSaveFilter(page);
  await name.fill("Short");
  await page.getByRole("option", { name: "Short clips", exact: true }).tap();
  await expect(
    dialog.getByText('Saved filter "Short clips" will be overwritten.', {
      exact: true,
    }),
  ).toBeVisible();
  await name.fill(" Short clips copy ");
  await expect(dialog.getByText(/will be overwritten/)).toHaveCount(0);
  await expect(page.getByRole("option")).toHaveCount(0);
  await save.tap();
  await expect(dialog).toBeHidden();
  expect(await page.evaluate(() => window.savedFilterMutations)).toEqual([
    { operation: "SaveFilter", name: "Short clips copy" },
  ]);
});
