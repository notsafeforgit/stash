import type { Locator } from "@playwright/test";
import { test, expect } from "./test";

async function expectContained(dialog: Locator) {
  await expect
    .poll(() =>
      dialog.evaluate((element) => {
        const rect = element.getBoundingClientRect();
        const form = element.querySelector("form");
        if (!form) return false;
        return (
          rect.top >= 0 &&
          rect.bottom <= window.innerHeight &&
          rect.left >= 0 &&
          rect.right <= window.innerWidth &&
          element.scrollWidth <= element.clientWidth &&
          form.scrollWidth <= form.clientWidth
        );
      }),
    )
    .toBe(true);
  for (const name of ["Merge", "Cancel"]) {
    await expect(
      dialog.getByRole("button", { name, exact: true }),
    ).toBeInViewport({ ratio: 1 });
  }
}

for (const width of [320, 390, 1280]) {
  test.describe(`${width}px merge dialogs`, () => {
    test.use({ viewport: { width, height: 700 }, isMobile: width < 1024 });
    for (const entity of ["performer", "scene"]) {
      for (const bulk of [false, true]) {
        test(`${entity} ${bulk ? "bulk" : "single"} fits and scrolls only vertically`, async ({
          page,
        }) => {
          await page.goto(`/merge-dialogs?${entity}${bulk ? "&bulk" : ""}`);
          const dialog = page.getByRole("dialog");
          await expectContained(dialog);
          await dialog.getByRole("combobox").click();
          await dialog.getByRole("combobox").fill("Destination");
          await page
            .getByRole("option", { name: "Destination", exact: true })
            .click();
          await expect(
            dialog.getByText("Resolve fields", { exact: true }),
          ).toBeVisible();
          await expectContained(dialog);

          const choices = dialog.locator(
            `#merge-row-${entity === "performer" ? "name" : "title"}`,
          );
          const source = choices
            .getByRole("button", { name: /^Source/ })
            .last();
          await source.click();
          await expect(source).toHaveAttribute("aria-pressed", "true");
          await expectContained(dialog);
          const form = dialog.locator("form");
          await form.evaluate((element) => {
            element.scrollTop = element.scrollHeight;
          });
          await expect
            .poll(() => form.evaluate((element) => element.scrollTop))
            .toBeGreaterThan(0);
          await expectContained(dialog);

          if (width < 1024) {
            await page.setViewportSize({ width, height: 480 });
            await expectContained(dialog);
          }
          await page.screenshot({
            path: test.info().outputPath("merge-dialog.png"),
          });
          await dialog
            .getByRole("button", { name: "Cancel", exact: true })
            .click();
          await expect(dialog).toBeHidden();
        });
      }
    }
  });
}
