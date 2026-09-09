import {
  test,
  expect,
  detailFooter,
  expectCompactRow,
  expectTouchTargets,
  chooseSection,
} from "./test";

for (const width of [320, 390, 768, 1280]) {
  test.describe(`${width}px layout`, () => {
    test.use({
      viewport: { width, height: 844 },
      isMobile: width < 1024,
      hasTouch: width < 1024,
    });

    for (const layout of ["collection", "media"]) {
      test(`${layout} preserves its controls and section panels`, async ({
        page,
      }) => {
        await page.goto(layout === "media" ? "/?media" : "/");
        const mobile = width < (layout === "media" ? 1024 : 768);
        const footer = detailFooter(page);
        if (mobile) {
          await expectCompactRow(footer);
          await expectTouchTargets(footer);
          const back = footer.getByRole("button", {
            name: "Back",
            exact: true,
          });
          await expect
            .poll(() =>
              back.evaluate((button) =>
                Math.abs(
                  button.getBoundingClientRect().right -
                    (window.innerWidth - 12),
                ),
              ),
            )
            .toBeLessThan(1);
          for (const section of ["Images", "Galleries", "Groups", "Scenes"]) {
            await chooseSection(page, section);
            await expect(
              page.getByTestId(`${section.toLowerCase()}-list`),
            ).toBeVisible();
            await expectCompactRow(footer);
            await expect(
              footer.getByRole("button", { name: "Search…", exact: true }),
            ).toHaveCount(1);
          }
          await footer
            .getByRole("button", { name: "More", exact: true })
            .click();
          const popup = page.locator('[data-slot="popover-content"]:visible');
          await expect(
            popup.getByRole("button", { name: "Edit", exact: true }),
          ).toBeVisible();
          await expectTouchTargets(popup);
          await expect
            .poll(() =>
              popup.evaluate((element) => {
                const rect = element.getBoundingClientRect();
                return rect.left >= 0 && rect.right <= window.innerWidth;
              }),
            )
            .toBe(true);
          await page.keyboard.press("Escape");
          await back.click();
          await expect(page.getByTestId("viewport")).toHaveAttribute(
            "data-back-count",
            "1",
          );
        } else {
          await expect(footer).toHaveCount(0);
          await expect(page.getByRole("tablist")).toHaveAttribute(
            "data-orientation",
            "horizontal",
          );
          await page.getByRole("tab", { name: "Images", exact: true }).click();
          await expect(page.getByTestId("images-list")).toBeVisible();
          await expect(
            page.getByRole("button", { name: "Edit", exact: true }),
          ).toBeVisible();
        }
      });
    }
  });
}
