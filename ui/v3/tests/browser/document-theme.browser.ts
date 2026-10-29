import type { Page } from "@playwright/test";
import { test, expect } from "./test";

test.use({
  // The real app/offline viewport also opts into Chromium's keyboard sizing.
  expectedConsoleErrors: [
    'Viewport argument key "interactive-widget" not recognized and ignored.',
  ],
});

async function expectTheme(page: Page, dark: boolean) {
  const scheme = dark ? "dark" : "light";
  const background = dark ? [10, 10, 10, 255] : [255, 255, 255, 255];
  await expect(page.locator("html")).toHaveCSS("color-scheme", scheme);
  await expect(page.locator('meta[name="color-scheme"]')).toHaveAttribute(
    "content",
    scheme,
  );
  await expect
    .poll(() =>
      page.evaluate(() => {
        // Compare rendered colors, independent of RGB/OKLCH serialization.
        const canvas = document.createElement("canvas");
        canvas.width = canvas.height = 1;
        const context = canvas.getContext("2d")!;
        return [document.documentElement, document.body].map((element) => {
          context.clearRect(0, 0, 1, 1);
          context.fillStyle = getComputedStyle(element).backgroundColor;
          context.fillRect(0, 0, 1, 1);
          return [...context.getImageData(0, 0, 1, 1).data];
        });
      }),
    )
    .toEqual([background, background]);
  const hint = page.locator('meta[name="theme-color"][data-app-theme]');
  if (await hint.count()) {
    await expect(hint).toHaveAttribute("content", dark ? "#0a0a0a" : "#ffffff");
  }
}

test("document background follows custom theme tokens on both surfaces", async ({
  page,
}) => {
  await page.goto("/theme-shell/app");
  await page.addStyleTag({
    content: ":root { --color-background: #123456 !important; }",
  });
  for (const theme of ["light", "dark"]) {
    await page.getByRole("button", { name: theme, exact: true }).click();
    await expect(page.locator("html")).toHaveCSS(
      "background-color",
      "rgb(18, 52, 86)",
    );
    await expect(page.locator("body")).toHaveCSS(
      "background-color",
      "rgb(18, 52, 86)",
    );
  }
});

for (const entry of ["app", "login", "offline"]) {
  for (const theme of ["system", "dark", "light"] as const) {
    test(`${entry} resolves ${theme} canvas and fixed body before React`, async ({
      page,
    }) => {
      await page.emulateMedia({
        colorScheme: theme === "dark" ? "light" : "dark",
      });
      await page.addInitScript(
        (saved) => localStorage.setItem("theme", saved),
        theme,
      );
      let startApp = () => {};
      const ready = new Promise<void>((resolve) => {
        startApp = resolve;
      });
      await page.route("**/document-theme.tsx", async (route) => {
        await ready;
        await route.continue();
      });
      await page.goto(`/theme-shell/${entry}`, { waitUntil: "commit" });
      try {
        await expect(page.locator("body")).toHaveCSS("position", "fixed");
        await expectTheme(page, theme !== "light");
        await expect(page.locator("#root")).toBeEmpty();
      } finally {
        startApp();
      }
      await expect(
        page.getByRole("button", { name: "system", exact: true }),
      ).toBeVisible();
      await expectTheme(page, theme !== "light");
      await page.emulateMedia({
        colorScheme: theme === "dark" ? "dark" : "light",
      });
      await expectTheme(page, theme === "dark");
      await page.getByRole("button", { name: "dark", exact: true }).click();
      await expectTheme(page, true);
      await page.getByRole("button", { name: "light", exact: true }).click();
      await expectTheme(page, false);
      if (entry === "app") {
        await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute(
          "content",
          "#202b33",
        );
      }
    });
  }
}
