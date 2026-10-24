import { test, expect, expectTouchTargets, holdForContextMenu } from "./test";

for (const width of [320, 1280]) {
  for (const empty of [false, true]) {
    test(`creates a performer from the ${empty ? "empty" : "populated"} list at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      await page.goto(`/performer-list${empty ? "?empty" : ""}`);
      const overflow = page.getByRole("button", {
        name: width === 320 ? "List actions" : "Operations",
        exact: true,
      });
      const create = page.getByRole(width === 320 ? "button" : "menuitem", {
        name: "Create performer",
        exact: true,
      });
      await expect(create).toBeHidden();
      if (width === 320) {
        const toolbar = page.locator("[data-mobile-toolbar-row]");
        await expectTouchTargets(toolbar);
        await expect(toolbar.getByRole("button")).toHaveCount(4);
        expect(
          await toolbar.evaluate(
            (element) => element.scrollWidth <= element.clientWidth,
          ),
        ).toBe(true);
      }
      await overflow.tap();
      await create.tap();
      await expect(
        page.getByRole(width === 320 ? "dialog" : "menu", {
          name: width === 320 ? "List actions" : undefined,
          exact: true,
        }),
      ).toBeHidden();
      const form = page.getByRole("dialog", {
        name: "Create performer",
        exact: true,
      });
      await expect(form).toBeVisible();
      const name = form.getByRole("textbox", {
        name: "Canonical name",
        exact: true,
      });
      await name.fill("Unsaved performer");
      await form.getByRole("button", { name: "Close", exact: true }).tap();
      await expect(form).toBeHidden();
      expect(await page.evaluate(() => window.createdPerformerInputs)).toEqual(
        [],
      );
      await overflow.tap();
      await create.tap();
      await expect(name).toHaveValue("");
      await name.fill("New performer");
      await form.getByRole("button", { name: "Create", exact: true }).tap();
      await expect(page).toHaveURL(
        /\/performers\/00000000-0000-4000-8000-000000000100$/,
      );
      await expect(
        page.getByText("Created performer detail", { exact: true }),
      ).toBeVisible();
      expect(
        await page.evaluate(() => window.createdPerformerInputs),
      ).toMatchObject([{ name: "New performer" }]);
    });
  }
}

test("mobile list overflow stays out of search and selection controls", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 844 });
  await page.goto("/performer-list");
  const overflow = page.getByRole("button", {
    name: "List actions",
    exact: true,
  });
  await expect(overflow).toBeVisible();
  const toolbar = page.locator("[data-mobile-toolbar-row]");
  await toolbar.getByRole("button", { name: "Search…", exact: true }).tap();
  await expect(overflow).toBeHidden();
  await toolbar
    .getByRole("button", { name: "Close search", exact: true })
    .tap();
  await expect(overflow).toBeVisible();
  const preview = page
    .locator("article[data-id] [data-entity-card-preview]")
    .first();
  await holdForContextMenu(preview, page.getByRole("menu"));
  await page.getByRole("menuitem", { name: /^Select all$/i }).tap();
  await expect(overflow).toBeHidden();
  await toolbar.getByRole("button", { name: "Select None", exact: true }).tap();
  await expect(overflow).toBeVisible();
});
