import { test, expect } from "./test";

for (const width of [390, 1280]) {
  test.describe(`bulk updates at ${width}px`, () => {
    test.use({ viewport: { width, height: 800 }, isMobile: width < 1024 });

    test("selected edits refresh and close only after completion", async ({
      page,
    }) => {
      await page.goto("/bulk-update");
      await page.getByRole("button", { name: "Save", exact: true }).click();
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await expect(page.getByTestId("saved")).toHaveText("1");
      const input = JSON.parse(await page.getByTestId("input").innerText());
      expect(input.ids).toEqual(["1", "2"]);
      expect(input.apply_to_items_matching_filters).toBeUndefined();
    });

    test("filtered edits report admission without a completed callback", async ({
      page,
    }) => {
      await page.goto("/bulk-update?result=queued");
      await page
        .getByRole("switch", { name: "Apply to all 30 matching" })
        .click();
      await page.getByRole("button", { name: "Save", exact: true }).click();
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await expect(page.getByTestId("saved")).toHaveText("0");
      await expect(
        page.getByText("Bulk update queued", { exact: true }),
      ).toBeVisible();
      const input = JSON.parse(await page.getByTestId("input").innerText());
      expect(input.ids).toEqual([]);
      expect(input.apply_to_items_matching_filters).toBe(true);
      expect(input.find_filter).toEqual({ q: "example" });
    });

    for (const result of ["invalid", "error"]) {
      test(`${result} results leave the sheet open and show an error`, async ({
        page,
      }) => {
        await page.goto(`/bulk-update?result=${result}`);
        await page.getByRole("button", { name: "Save", exact: true }).click();
        await expect(page.getByRole("dialog")).toBeVisible();
        await expect(page.getByTestId("saved")).toHaveText("0");
        const message = page.getByText(
          result === "error"
            ? "Update rejected"
            : "The server did not confirm the bulk update. Check its status before retrying.",
          { exact: true },
        );
        await expect(message).toBeInViewport();
        await expect
          .poll(() =>
            message.evaluate((element) => {
              const rect = element.getBoundingClientRect();
              const top = document.elementFromPoint(
                rect.x + rect.width / 2,
                rect.y + rect.height / 2,
              );
              return top === element || element.contains(top);
            }),
          )
          .toBe(true);
        await expect(
          page.getByRole("button", { name: "Save", exact: true }),
        ).toBeEnabled();
        await page.screenshot({
          path: test.info().outputPath(`bulk-${result}.png`),
        });
      });
    }
  });
}
