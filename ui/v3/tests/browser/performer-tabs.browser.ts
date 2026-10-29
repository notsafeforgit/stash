import { test, expect, chooseSection } from "./test";

for (const desktop of [false, true]) {
  test(`performer scenes render after returning from images on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      desktop ? { width: 1280, height: 900 } : { width: 430, height: 739 },
    );
    await page.goto("/lightbox-list?tabs&kind=scene");
    const panel = page.getByRole("tabpanel");
    await expect(
      panel.getByRole("link", { name: "Scene 1", exact: true }),
    ).toBeVisible();
    const selectTab = async (name: string) => {
      if (desktop) await page.getByRole("tab", { name, exact: true }).click();
      else await chooseSection(page, name);
      await expect(page).toHaveURL(new RegExp(`tab=${name.toLowerCase()}`));
      if (!desktop) await panel.evaluate((el) => el.scrollIntoView());
    };
    for (let round = 0; round < 3; round++) {
      await selectTab("Images");
      await expect(
        panel.getByRole("link", { name: "Image 1", exact: true }),
      ).toBeInViewport();
      await selectTab("Scenes");
      await expect(
        panel.getByRole("link", { name: "Scene 1", exact: true }),
      ).toBeInViewport();
    }
  });
}
