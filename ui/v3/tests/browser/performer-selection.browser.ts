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

for (const width of [320, 1280]) {
  for (const scope of ["Selected items", "Filtered list", "Performer"]) {
    test(`filtered performer selection offers distinct share scopes: ${scope} at ${width}px`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 844 });
      await page.goto("/performer-selection?filtered");
      for (const id of [1, 2])
        await page
          .locator(
            `article[data-id="${libraryID(id)}"] [data-entity-card-preview]`,
          )
          .click({ modifiers: ["Control"] });
      if (width === 320) {
        await detailFooter(page)
          .getByRole("button", { name: "Share…", exact: true })
          .tap();
      } else {
        // The collection's action must see the active list, just like the
        // selection toolbar. It is independent of the list's own menu.
        await page
          .getByRole("button", { name: "Operations", exact: true })
          .first()
          .click();
        await page
          .getByRole("menuitem", { name: "Share…", exact: true })
          .click();
      }
      const form = page.getByRole("dialog", {
        name: "Create share",
        exact: true,
      });
      await expect(
        form.getByRole("button", { name: "Selected items", exact: true }),
      ).toHaveAttribute("aria-pressed", "true");
      await expect(
        form.getByText("2 selected items", { exact: true }),
      ).toBeVisible();
      await expect(
        form.getByRole("button", { name: "Scenes", exact: true }),
      ).toHaveCount(0);
      await expect(
        form.getByRole("button", { name: "Ongoing", exact: true }),
      ).toHaveCount(0);
      for (const option of ["Selected items", "Filtered list", "Performer"])
        await expect(
          form.getByRole("button", { name: option, exact: true }),
        ).toBeVisible();
      await form.getByRole("button", { name: scope, exact: true }).click();
      if (scope === "Performer") {
        await expect(
          form.getByText(/All media linked to Example performer/),
        ).toBeVisible();
        for (const type of ["Scenes", "Images", "Galleries"])
          await expect(
            form.getByRole("button", { name: type, exact: true }),
          ).toHaveAttribute("aria-pressed", "true");
        await form.getByRole("button", { name: "Images", exact: true }).click();
        await form
          .getByRole("button", { name: "Galleries", exact: true })
          .click();
      }
      if (scope !== "Selected items")
        await form
          .getByRole("button", { name: "Ongoing", exact: true })
          .click();
      if (scope === "Filtered list") {
        await expect(
          form.getByText(
            /All items matching this list’s current search and filters/,
          ),
        ).toBeVisible();
        await expect(
          form.getByRole("button", { name: "Scenes", exact: true }),
        ).toHaveCount(0);
      }
      await expect(form).toBeInViewport({ ratio: 1 });
      expect(
        await form.evaluate(
          (element) => element.scrollWidth - element.clientWidth,
        ),
      ).toBeLessThanOrEqual(1);
      await expect(
        form.getByRole("button", { name: "Create share", exact: true }),
      ).toBeInViewport({ ratio: 1 });
      await page.screenshot({
        path: testInfo.outputPath(`share-scope-${scope}-${width}.png`),
      });
      await form
        .getByRole("button", { name: "Create share", exact: true })
        .click();
      await expect
        .poll(() =>
          page.evaluate(() => window.performerSelectionFixture.shares.length),
        )
        .toBe(1);
      const input = await page.evaluate(
        () => window.performerSelectionFixture.shares[0]!.input,
      );
      if (scope === "Selected items") {
        expect(input.targets).toEqual(
          [1, 2].map((id) => ({ kind: "SCENE", id: libraryID(id) })),
        );
        expect(input.source).toBeUndefined();
      } else if (scope === "Filtered list") {
        expect(input.targets).toBeUndefined();
        expect(input.source).toMatchObject({
          mode: "ONGOING",
          filter: { kind: "SCENE", q: "Video" },
        });
        const ast = JSON.stringify(input.source?.filter?.filter_ast);
        expect(ast).toContain(libraryID(999));
        expect(ast).toContain("rating100");
        expect(input.source?.entity).toBeUndefined();
        expect(input.source?.media_types).toBeUndefined();
      } else {
        expect(input.targets).toBeUndefined();
        expect(input.source).toEqual({
          mode: "ONGOING",
          entity: { kind: "PERFORMER", id: libraryID(999) },
          media_types: ["SCENE"],
        });
      }
    });
  }
}

test("empty performer categories do not invalidate a fixed selection", async ({
  page,
}) => {
  await page.goto("/performer-selection");
  await page
    .locator(`article[data-id="${libraryID(1)}"] [data-entity-card-preview]`)
    .click({ modifiers: ["Control"] });
  await detailFooter(page)
    .getByRole("button", { name: "Share…", exact: true })
    .tap();
  const form = page.getByRole("dialog", { name: "Create share", exact: true });
  await form.getByRole("button", { name: "Performer", exact: true }).click();
  for (const type of ["Scenes", "Images", "Galleries"])
    await form.getByRole("button", { name: type, exact: true }).click();
  await expect(
    form.getByRole("button", { name: "Create share", exact: true }),
  ).toBeDisabled();
  await expect(
    form.getByText("Choose at least one media type.", { exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => window.performerSelectionFixture.shares.length),
  ).toBe(0);
  await form
    .getByRole("button", { name: "Selected items", exact: true })
    .click();
  await form.getByRole("button", { name: "Create share", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(() => window.performerSelectionFixture.shares.length),
    )
    .toBe(1);
  const input = await page.evaluate(
    () => window.performerSelectionFixture.shares[0]!.input,
  );
  expect(input.targets).toEqual([{ kind: "SCENE", id: libraryID(1) }]);
  expect(input.source).toBeUndefined();
});

test("hidden performer tabs cannot contribute stale selections to sharing", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/performer-selection?q=Video");
  await page
    .locator(`article[data-id="${libraryID(1)}"] [data-entity-card-preview]`)
    .click({ modifiers: ["Control"] });
  await page.getByRole("tab", { name: "Details", exact: true }).click();
  await page
    .getByRole("button", { name: "Operations", exact: true })
    .first()
    .click();
  await page.getByRole("menuitem", { name: "Share…", exact: true }).click();
  const form = page.getByRole("dialog", { name: "Create share", exact: true });
  await expect(
    form.getByRole("button", { name: "Selected items", exact: true }),
  ).toHaveCount(0);
  await expect(
    form.getByRole("button", { name: "Filtered list", exact: true }),
  ).toHaveCount(0);
  await expect(
    form.getByRole("button", { name: "Scenes", exact: true }),
  ).toBeVisible();
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("tab", { name: "Videos", exact: true }).click();
  await page
    .getByRole("button", { name: "Operations", exact: true })
    .first()
    .click();
  await page.getByRole("menuitem", { name: "Share…", exact: true }).click();
  await expect(
    form.getByText("1 selected item", { exact: true }),
  ).toBeVisible();
  await expect(
    form.getByRole("button", { name: "Selected items", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
});
