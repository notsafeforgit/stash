import type { Page } from "@playwright/test";
import { test, expect, expectTouchTargets } from "./test";

const currentSlide = (page: Page) => page.locator(".yarl__slide_current");
const toolbar = (page: Page) => page.locator(".image-lightbox .yarl__toolbar");

async function openImages(page: Page, query = "") {
  await page.goto(`/image-lightbox${query}`);
  await page.getByRole("button", { name: "Open images", exact: true }).click();
  await expect(currentSlide(page).locator("img")).toBeVisible();
}

async function revealMetadata(page: Page) {
  await currentSlide(page)
    .locator("img")
    .tap({ position: { x: 100, y: 80 } });
  await expect(page.locator(".image-lightbox")).toHaveClass(/chrome-revealed/);
}

async function expectMobileLayout(page: Page, safeBottom = 0) {
  const footer = currentSlide(page).locator(".lightbox-overlay-bottom");
  const bar = toolbar(page);
  await expectTouchTargets(bar);
  const barBox = await bar.boundingBox();
  const footerBox = await footer.boundingBox();
  if (!barBox || !footerBox) throw new Error("Missing image lightbox controls");
  expect(footerBox.y + footerBox.height).toBeLessThanOrEqual(barBox.y - 7);
  expect(footerBox.y).toBeGreaterThanOrEqual(48);
  const viewport = page.viewportSize();
  if (!viewport) throw new Error("Missing viewport");
  const boxes = await bar.getByRole("button").evaluateAll((buttons) =>
    buttons
      .filter((button) => button.getBoundingClientRect().width > 0)
      .map((button) => {
        const rect = button.getBoundingClientRect();
        return {
          left: rect.left,
          right: rect.right,
          top: rect.top,
          bottom: rect.bottom,
        };
      }),
  );
  for (const [index, box] of boxes.entries()) {
    expect(box.left).toBeGreaterThanOrEqual(boxes[index - 1]?.right ?? 0);
    expect(box.right).toBeLessThanOrEqual(viewport.width);
    expect(box.top).toBeCloseTo(barBox.y, 0);
    expect(box.bottom).toBeLessThanOrEqual(viewport.height - safeBottom);
  }
  expect(
    await footer.evaluate(
      (element) => element.scrollWidth - element.clientWidth,
    ),
  ).toBeLessThanOrEqual(1);
  const metadata = footer.locator(".image-lightbox-metadata");
  expect(
    await metadata.evaluate(
      (element) => element.scrollWidth - element.clientWidth,
    ),
  ).toBeLessThanOrEqual(1);
}

for (const width of [320, 390]) {
  test(`image lightbox controls do not overlap metadata at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 740 });
    await openImages(page);
    await page.addStyleTag({ content: ":root { --safe-area-bottom: 34px; }" });
    await revealMetadata(page);
    await expect(
      currentSlide(page).getByRole("group", { name: "Rating", exact: true }),
    ).toHaveCount(0);
    const counter = currentSlide(page).getByRole("button", { name: /9999/ });
    await expect(counter).toBeVisible();
    await expectMobileLayout(page, 34);
    await counter.tap();
    await expect(
      currentSlide(page).getByRole("button", { name: /10000/ }),
    ).toBeVisible();
    await expectMobileLayout(page, 34);
    await page.screenshot({
      path: test.info().outputPath("mobile-image-lightbox.png"),
    });
    await toolbar(page)
      .getByRole("button", { name: "Close", exact: true })
      .tap();
    await expect(page.locator(".image-lightbox")).toHaveCount(0);
  });
}

test("mobile rating preference persists and still clears the toolbar for fractional ratings", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await openImages(page, "?single&fractional");
  await toolbar(page)
    .getByRole("button", { name: "Settings", exact: true })
    .tap();
  const toggle = page.getByRole("switch", { name: "Show rating on mobile" });
  await expect(toggle).not.toBeChecked();
  await toggle.tap();
  await expect(toggle).toBeChecked();
  await page.reload();
  await page.getByRole("button", { name: "Open images", exact: true }).tap();
  await revealMetadata(page);
  const rating = currentSlide(page).locator('fieldset[aria-label="Rating"]');
  await expect(rating).toBeVisible();
  await expect(rating.getByRole("slider")).toBeVisible();
  await expectMobileLayout(page);
  await page.screenshot({
    path: test.info().outputPath("mobile-image-rating.png"),
  });
  await toolbar(page)
    .getByRole("button", { name: "Settings", exact: true })
    .tap();
  await toggle.tap();
  await expect(rating).toHaveCount(0);
});

test("mobile overflow retains zoom and rotation actions", async ({ page }) => {
  await openImages(page);
  await expect(
    toolbar(page).getByRole("button", { name: "Zoom in", exact: true }),
  ).toHaveCount(0);
  await toolbar(page).getByRole("button", { name: "Image actions" }).tap();
  await expect(
    page.getByRole("menuitem", { name: "Rotate clockwise", exact: true }),
  ).toBeVisible();
  const image = currentSlide(page).locator("img");
  const originalWidth = await image.evaluate(
    (element) => element.getBoundingClientRect().width,
  );
  await page.getByRole("menuitem", { name: "Zoom in", exact: true }).tap();
  await expect
    .poll(() =>
      image.evaluate((element) => element.getBoundingClientRect().width),
    )
    .toBeGreaterThan(originalWidth);
  await toolbar(page).getByRole("button", { name: "Image actions" }).tap();
  await page.getByRole("menuitem", { name: "Zoom out", exact: true }).tap();
  await expect
    .poll(() =>
      image.evaluate((element) =>
        Math.round(element.getBoundingClientRect().width),
      ),
    )
    .toBe(Math.round(originalWidth));
});

test.describe("desktop image lightbox", () => {
  test.use({
    viewport: { width: 1280, height: 900 },
    isMobile: false,
    hasTouch: false,
  });

  test("rating stays visible and zoom stays in the toolbar", async ({
    page,
  }) => {
    await openImages(page);
    await expect(
      currentSlide(page).getByRole("group", { name: "Rating", exact: true }),
    ).toBeVisible();
    await expect(
      toolbar(page).getByRole("button", { name: "Zoom in", exact: true }),
    ).toBeVisible();
    const bar = await toolbar(page).boundingBox();
    const footer = await currentSlide(page)
      .locator(".lightbox-overlay-bottom")
      .boundingBox();
    if (!bar || !footer) throw new Error("Missing controls");
    expect(bar.y + bar.height).toBeLessThan(footer.y);
    await page.screenshot({
      path: test.info().outputPath("desktop-image-lightbox.png"),
    });
  });
});
