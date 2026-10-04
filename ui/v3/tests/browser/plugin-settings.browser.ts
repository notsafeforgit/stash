import { test, expect } from "./test";

for (const width of [390, 1440]) {
  test(`mapping target choices and collapsible previews work at ${width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/plugin-settings.html");
    await expect(page.getByRole("alert")).toContainText(
      "Some plugins could not be loaded",
    );
    await expect(page.getByRole("alert")).toContainText(
      "community/legacy-plugin.yml",
    );
    await expect(page.getByRole("alert")).toContainText(
      "requires apiVersion: 3",
    );
    const target = page.getByRole("combobox", { name: "Target field" });
    await expect(target.locator('[data-slot="select-value"]')).toHaveText(
      "Title",
    );
    await expect(
      page.getByText("title: String", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Add mapping", exact: true })
      .click();
    await target.nth(1).click();
    await expect(
      page.getByRole("option", { name: "Title", exact: true }),
    ).toBeDisabled();
    await page.getByRole("option", { name: "Performers", exact: true }).click();
    await expect(
      page.getByText("Existing Stash performer IDs, not names.", {
        exact: true,
      }),
    ).toBeVisible();
    await page
      .getByRole("textbox", { name: "jq expression" })
      .nth(1)
      .fill('["12"]');
    await expect(
      page.getByRole("button", { name: "Add mapping", exact: true }),
    ).toBeDisabled();
    const preview = page.getByRole("button", {
      name: "Preview mappings",
      exact: true,
    });
    await preview.focus();
    await page.keyboard.press("Enter");
    await expect(preview).toHaveAttribute("aria-expanded", "true");
    const entity = page.getByRole("combobox", { name: "Scene to preview" });
    await entity.fill("Example");
    await page.getByRole("option", { name: /Example scene/ }).click();
    await page
      .getByRole("button", { name: "Load entity data", exact: true })
      .click();
    const sample = page.getByRole("textbox", { name: "Sample input (JSON)" });
    await expect(sample).toContainText("Catalog title");
    await page
      .getByRole("button", { name: "Test expression", exact: true })
      .click();
    await expect(page.locator("pre")).toContainText("Catalog title");
    const sampleValue = await sample.inputValue();
    await preview.click();
    await expect(preview).toHaveAttribute("aria-expanded", "false");
    await expect(sample).toBeHidden();
    await preview.click();
    await expect(sample).toHaveValue(sampleValue);
    expect(
      await page.evaluate(() =>
        window.mappingRequests.some((name) => name.startsWith("Update")),
      ),
    ).toBe(false);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await testInfo.attach(`mapping-preview-${width}`, {
      body: await page.screenshot({ fullPage: true }),
      contentType: "image/png",
    });
  });
}
