import { test, expect, expectTouchTargets } from "./test";

test("mobile Close fits beside playback settings at narrow and landscape sizes", async ({
  page,
}) => {
  await page.goto("/player");
  await page.getByRole("button", { name: "Open player" }).click();
  const player = page.getByTestId("player");
  const row = player.locator("[data-player-control-row]");
  const bar = player.locator("[data-player-control-bar]");
  const close = row.getByRole("button", { name: "Close", exact: true });
  // Pause through the actual playback button so auto-hide cannot race layout checks.
  await row.getByRole("button", { name: "Pause", exact: true }).click();
  for (const viewport of [
    { width: 320, height: 740 },
    { width: 390, height: 844 },
    { width: 844, height: 390 },
  ]) {
    await page.setViewportSize(viewport);
    await expect(close).toBeVisible();
    await expectTouchTargets(row);
    await expect
      .poll(() =>
        row.evaluate((element) => {
          const buttons = Array.from(element.querySelectorAll("button")).filter(
            (button) => button.getClientRects().length > 0,
          );
          const boxes = buttons.map((button) => button.getBoundingClientRect());
          return boxes.every(
            (box, index) =>
              box.left >= 0 &&
              box.right <= innerWidth &&
              box.left >= (boxes[index - 1]?.right ?? 0) - 0.1,
          );
        }),
      )
      .toBe(true);
    const bounds = await close.boundingBox();
    expect(bounds?.x).toBeGreaterThan(viewport.width - 110);
    expect(bounds?.y).toBeGreaterThan(viewport.height - 80);
    if (viewport.width === 390) {
      await page.screenshot({
        path: test.info().outputPath("mobile-player.png"),
      });
    }
  }
  await row.getByRole("button", { name: "Playback speed" }).tap();
  await page.getByRole("menuitemradio", { name: "1.5x", exact: true }).tap();
  await expect(row.getByRole("button", { name: "Playback speed" })).toHaveText(
    "1.5x",
  );
  await row.getByRole("button", { name: "Quality", exact: true }).tap();
  await page
    .getByRole("menuitemradio", { name: "MP4 HD (720p)", exact: true })
    .tap();
  await expect(player.locator("video")).toHaveAttribute(
    "src",
    "/player.mp4?quality=720",
  );
  await expect(bar).not.toHaveAttribute("inert");
  await close.tap();
  await expect(player).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Open player" })).toBeVisible();
});

test("hidden Close does not intercept the tap that reveals playback controls", async ({
  page,
}) => {
  await page.goto("/player");
  await page.getByRole("button", { name: "Open player" }).click();
  const player = page.getByTestId("player");
  const bar = player.locator("[data-player-control-bar]");
  const close = player.locator("[data-player-close]");
  await expect(close).toBeVisible();
  const bounds = await close.boundingBox();
  if (!bounds) throw new Error("Missing close control bounds");
  await expect(bar).toHaveAttribute("inert", "", { timeout: 10000 });
  await page.touchscreen.tap(
    bounds.x + bounds.width / 2,
    bounds.y + bounds.height / 2,
  );
  await expect(player).toBeVisible();
  await expect(bar).not.toHaveAttribute("inert");
  await player.getByRole("button", { name: "Close", exact: true }).tap();
  await expect(player).toHaveCount(0);
});

test("the lightbox remains dismissible at the bottom while a mobile scene loads", async ({
  page,
}) => {
  await page.goto("/player?loading");
  await page.getByRole("button", { name: "Open player" }).click();
  const close = page.getByRole("button", { name: "Close", exact: true });
  await expect(close).toHaveCount(1);
  await expect(close).toBeVisible();
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 });
    // The real lightbox must give its slide the same width exercised by
    // the control-bar test; default YARL padding would consume 32px.
    await expect
      .poll(() =>
        page
          .locator(".yarl__slide_current > div")
          .evaluate((element) => element.getBoundingClientRect().width),
      )
      .toBeCloseTo(width, 0);
    expect((await close.boundingBox())?.y).toBeGreaterThan(740);
  }
  await close.tap();
  await expect(page.locator(".yarl__root")).toHaveCount(0);
});

test.describe("desktop player", () => {
  test.use({
    viewport: { width: 1280, height: 800 },
    isMobile: false,
    hasTouch: false,
  });
  test("retains the lightbox toolbar Close and Escape dismissal", async ({
    page,
  }) => {
    await page.goto("/player?loading");
    await page.getByRole("button", { name: "Open player" }).click();
    const close = page
      .locator(".yarl__toolbar")
      .getByRole("button", { name: "Close", exact: true });
    await expect(close).toBeVisible();
    expect((await close.boundingBox())?.y).toBeLessThan(80);
    await expect(page.locator("[data-player-close]")).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(page.locator(".yarl__root")).toHaveCount(0);
    await page.getByRole("button", { name: "Open player" }).click();
    await close.click();
    await expect(page.locator(".yarl__root")).toHaveCount(0);
  });
});
