import { readFile } from "node:fs/promises";
import type { Page } from "@playwright/test";
import { test, expect, holdForContextMenu } from "./test";

const fileMenu = (page: Page) => page.getByRole("menu");

async function saveOriginal(page: Page, name = "image-1.svg") {
  const download = page.waitForEvent("download");
  await fileMenu(page)
    .getByRole("menuitem", { name: "Save image", exact: true })
    .click();
  const file = await download;
  expect(file.suggestedFilename()).toBe(name);
  const path = await file.path();
  if (!path) throw new Error("Missing image download");
  const bytes = await readFile(path, "utf8");
  // Save retains the original format, dimensions and content, rather than
  // using the clipboard's PNG conversion or a card-sized thumbnail.
  expect(bytes).toContain(
    '<svg xmlns="http://www.w3.org/2000/svg" width="900" height="1200">',
  );
  await expect(
    page.getByText("Image download started", { exact: true }).last(),
  ).toBeVisible();
  await expect(fileMenu(page)).toHaveCount(0);
}

test("image lightbox copies image pixels through the native clipboard API", async ({
  page,
  context,
  browserName,
}) => {
  // Chromium's headless permission prompt cannot be answered by a person.
  // WebKit uses its normal click-gesture path with no permission override.
  if (browserName === "chromium")
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/image-lightbox");
  await page.getByRole("button", { name: "Open images", exact: true }).click();
  await page
    .getByRole("button", { name: "Image actions", exact: true })
    .click();
  await fileMenu(page)
    .getByRole("menuitem", { name: "Copy image", exact: true })
    .click();
  await expect(page.getByText("Image copied", { exact: true })).toBeVisible();
  if (browserName === "chromium") {
    const copied = await page.evaluate(async () => {
      const [item] = await navigator.clipboard.read();
      if (!item) throw new Error("Missing clipboard image");
      const blob = await item.getType("image/png");
      const bitmap = await createImageBitmap(blob);
      const dimensions = { width: bitmap.width, height: bitmap.height };
      bitmap.close();
      return dimensions;
    });
    expect(copied).toEqual({ width: 900, height: 1200 });
  }
  await expect(page.locator(".yarl__slide_current img")).toBeVisible();
});

test("lightbox downloads follow the current slide", async ({ page }) => {
  await page.goto("/image-lightbox");
  await page.getByRole("button", { name: "Open images", exact: true }).click();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.locator(".yarl__slide_current img")).toHaveAttribute(
    "src",
    /data:image/,
  );
  await page
    .getByRole("button", { name: "Image actions", exact: true })
    .click();
  await saveOriginal(page, "image-2.svg");
});

test("mobile image cards and table rows expose file actions through long press", async ({
  page,
}) => {
  await page.goto("/image-files");
  await holdForContextMenu(
    page
      .getByTestId("image-card")
      .locator('[data-slot="context-menu-trigger"]'),
    fileMenu(page),
  );
  await expect(
    fileMenu(page).getByRole("menuitem", { name: "Copy image", exact: true }),
  ).toBeVisible();
  await saveOriginal(page);
  await holdForContextMenu(page.getByTestId("image-row"), fileMenu(page));
  await expect(
    fileMenu(page).getByRole("menuitem", { name: "Copy image", exact: true }),
  ).toBeVisible();
  await saveOriginal(page);
});

test("detail actions and the mobile detail viewer both save images", async ({
  page,
}) => {
  await page.goto("/image-files");
  await page.getByRole("button", { name: "Operations", exact: true }).click();
  await saveOriginal(page);
  await page.getByRole("button", { name: "Open image", exact: true }).click();
  await page
    .getByRole("button", { name: "Image actions", exact: true })
    .click();
  await saveOriginal(page);
});

test("Save image remains available when the clipboard API is unavailable", async ({
  page,
}) => {
  await page.addInitScript(() =>
    Object.defineProperty(navigator, "clipboard", { value: undefined }),
  );
  await page.goto("/image-lightbox");
  await page.getByRole("button", { name: "Open images", exact: true }).click();
  await page
    .getByRole("button", { name: "Image actions", exact: true })
    .click();
  await expect(
    fileMenu(page).getByRole("menuitem", { name: "Copy image", exact: true }),
  ).toHaveCount(0);
  await saveOriginal(page);
});

test("denied clipboard access offers a working save fallback", async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      value: {
        write: () =>
          Promise.reject(new DOMException("Denied", "NotAllowedError")),
      },
    });
  });
  await page.goto("/image-lightbox");
  await page.getByRole("button", { name: "Open images", exact: true }).click();
  await page
    .getByRole("button", { name: "Image actions", exact: true })
    .click();
  await fileMenu(page)
    .getByRole("menuitem", { name: "Copy image", exact: true })
    .click();
  await expect(
    page.getByText("Could not copy the image. Try Save image instead.", {
      exact: true,
    }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Image actions", exact: true })
    .click();
  await saveOriginal(page);
});

test.describe("desktop image file actions", () => {
  test.use({
    viewport: { width: 1280, height: 900 },
    isMobile: false,
    hasTouch: false,
  });

  test("card right click and inline detail viewer retain file actions", async ({
    page,
  }) => {
    await page.goto("/image-files");
    await page.getByTestId("image-card").click({ button: "right" });
    await saveOriginal(page);
    await page
      .getByRole("button", { name: "Image actions", exact: true })
      .click();
    await saveOriginal(page);
  });

  test("desktop lightbox exposes Copy and Save without hiding zoom", async ({
    page,
  }) => {
    await page.goto("/image-lightbox");
    await page
      .getByRole("button", { name: "Open images", exact: true })
      .click();
    // Wait for the lazy lightbox module before checking its controls. A cold
    // fixture server can still be showing the loading dialog on slower CI.
    await expect(
      page.getByRole("dialog", { name: "Lightbox", exact: true }),
    ).toBeVisible({ timeout: 15_000 });
    await expect(
      page.getByRole("button", { name: "Zoom in", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Image actions", exact: true })
      .click();
    await expect(
      fileMenu(page).getByRole("menuitem", { name: "Copy image", exact: true }),
    ).toBeVisible();
    await saveOriginal(page);
  });
});
