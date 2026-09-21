import { expect, test as base, expectTouchTargets } from "./test";

const test = base.extend({
  context: async (
    { browserName, playwright, context, baseURL, viewport, isMobile, hasTouch },
    use,
    info,
  ) => {
    if (browserName !== "webkit") return use(context);
    // Cancellation removes partial OPFS files. WebKit's ephemeral profile
    // rejects OPFS access, so use the same isolated disk profile as the PWA
    // storage suite rather than replacing the real cancellation command.
    const persistent = await playwright.webkit.launchPersistentContext(
      info.outputPath("webkit-profile"),
      { baseURL, viewport, isMobile, hasTouch, headless: true },
    );
    try {
      await use(persistent);
    } finally {
      await persistent.close();
    }
  },
});

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

for (const width of [320, 1280]) {
  test(`streaming downloads show processing and saving progress at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let processedSeconds = 25;
    let state = "processing";
    await page.route("**/scene/1/download/progress?*", (route) =>
      route.fulfill({
        contentType: "application/json",
        headers: {
          "Access-Control-Allow-Origin": "http://127.0.0.1:3025",
          "Access-Control-Allow-Credentials": "true",
        },
        body: JSON.stringify({
          request_id: "fixture-download",
          state,
          processed_seconds: processedSeconds,
          duration_seconds: 100,
        }),
      }),
    );
    await page.goto("/downloads?processing");
    const trigger = page.getByRole("button", {
      name: "Downloads",
      exact: true,
    });
    if (width < 768)
      await expect(trigger).toContainText("25% processed · 25 MB");
    await trigger.click();
    const tray = page.getByRole("dialog", { name: "Downloads" });
    const progress = tray.getByRole("progressbar");
    await expect(progress).toHaveAttribute("aria-valuenow", "25");
    await expect(
      tray.getByText("25% processed · 25 MB", { exact: true }),
    ).toBeVisible();
    processedSeconds = 65;
    await expect(progress).toHaveAttribute("aria-valuenow", "65");
    await expect(
      tray.getByText("65% processed · 25 MB", { exact: true }),
    ).toBeVisible();
    state = "finished";
    processedSeconds = 100;
    await expect(progress).toHaveAttribute("aria-valuenow", "99");
    await expect(
      tray.getByText("Saving to device · 25 MB", { exact: true }),
    ).toBeVisible();
    await expect(progress).not.toHaveAttribute("aria-valuenow", "100");
    if (width < 768) await expectTouchTargets(tray);
    await page.screenshot({
      path: test.info().outputPath("download-processing.png"),
    });
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
