import { test, expect } from "./test";

for (const desktop of [false, true]) {
  test(`source tracking is opt-in in ${desktop ? "desktop" : "mobile"} navigation`, async ({
    page,
  }) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    for (const state of ["", "&source-tracking-pending", "&source-tracking"]) {
      await page.goto(`/home-fixture/images?plugin-navigation${state}`);
      await page
        .getByRole("button", {
          name: desktop ? "More options" : "Open navigation menu",
          exact: true,
        })
        .click();
      const menu = desktop
        ? page.getByRole("menu")
        : page.locator("[data-mobile-navigation]");
      const role = desktop ? "menuitem" : "link";
      for (const name of [
        "Source posts",
        "Source collections",
        "Account review",
        "Review queue",
        "Archive activity",
        "Import history",
        "Saved actions",
      ]) {
        await expect(menu.getByRole(role, { name, exact: true })).toHaveCount(
          state === "&source-tracking" ? 1 : 0,
        );
      }
      // An unrelated installed plugin remains available in either mode.
      await expect(
        menu.getByRole(role, { name: "Catalog review", exact: true }),
      ).toBeVisible();
    }
  });
}
