import { expect, test, expectTouchTargets } from "./test";

for (const width of [320, 390]) {
  test(`mobile downloads expose live progress and queue controls at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/downloads");
    const trigger = page.getByRole("button", {
      name: "Downloads",
      exact: true,
    });
    await expect(trigger).toHaveCount(1);
    await expect(trigger).toContainText("25 MB / 100 MB · 25%");
    expect((await trigger.boundingBox())?.height).toBeGreaterThanOrEqual(44);
    await page.getByRole("button", { name: "Advance download" }).click();
    await expect(trigger).toContainText("50 MB / 100 MB · 50%");
    await trigger.tap();
    const tray = page.getByRole("dialog", { name: "Downloads" });
    await expect(tray).toBeVisible();
    await expect(tray.getByRole("progressbar")).toHaveAttribute(
      "aria-valuenow",
      "50",
    );
    await expect(tray.getByText("Queued video", { exact: true })).toBeVisible();
    await expect(
      tray.getByRole("button", { name: "Retry download" }),
    ).toBeVisible();
    await expectTouchTargets(tray);
    const bounds = await tray.boundingBox();
    expect(bounds?.x).toBeGreaterThanOrEqual(0);
    expect((bounds?.x ?? 0) + (bounds?.width ?? 0)).toBeLessThanOrEqual(width);
    await tray.getByRole("button", { name: "Cancel download" }).nth(1).tap();
    await expect(tray.getByText("Queued video", { exact: true })).toHaveCount(
      0,
    );
    await page.screenshot({
      path: test.info().outputPath("mobile-downloads.png"),
    });
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Clear fixture" }).click();
    await expect(trigger).toHaveCount(0);
  });
}

test("unknown totals show received bytes without a misleading percentage", async ({
  page,
}) => {
  await page.goto("/downloads?unknown");
  const trigger = page.getByRole("button", { name: "Downloads", exact: true });
  await expect(trigger).toContainText("25 MB");
  await expect(trigger).not.toContainText("%");
  await trigger.tap();
  const progress = page
    .getByRole("dialog", { name: "Downloads" })
    .getByRole("progressbar");
  await expect(progress).not.toHaveAttribute("aria-valuenow");
  await expect(
    progress.locator(".download-progress-indeterminate"),
  ).toBeVisible();
});

test("downloaded file info displays retained frame rate, average bitrate, and HDR details on mobile", async ({
  page,
}) => {
  await page.goto("/downloads?idle");
  const row = (label: string) =>
    page
      .locator("dl > div")
      .filter({ has: page.locator("dt", { hasText: label }) })
      .locator("dd");
  await expect(row("Frame rate")).toHaveText("29.97 fps");
  await expect(row("Bit Rate")).toHaveText("12000 kbps");
  await expect(row("Dynamic range")).toHaveText("HDR 10-bit");
  await expect(row("Transfer")).toHaveText("PQ (SMPTE ST 2084)");
  await expect(
    page.getByRole("button", { name: "Downloads", exact: true }),
  ).toHaveCount(0);
});

test("desktop retains the compact download tray", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/downloads");
  const trigger = page.getByRole("button", { name: "Downloads", exact: true });
  await expect(trigger).toHaveCount(1);
  expect((await trigger.boundingBox())?.width).toBeLessThan(44);
  await trigger.click();
  await expect(
    page.getByRole("dialog", { name: "Downloads" }).getByRole("progressbar"),
  ).toHaveAttribute("aria-valuenow", "25");
});
