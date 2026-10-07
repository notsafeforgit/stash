import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  importDetails,
  importIds,
  importSnapshot,
} from "../fixtures/import-history";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function archive(page: Page, many = false) {
  const requests: URL[] = [];
  const writes: string[] = [];
  const control = { failList: false, malformed: false };
  await page.route("**/api/v3/archive/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    requests.push(url);
    if (request.method() !== "GET") writes.push(request.method());
    const path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    const kind = path.split("/")[1] === "automation" ? "automation" : "catalog";
    if (path === `import-history/${kind}`) {
      if (control.failList)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      return route.fulfill({
        json:
          many && kind === "catalog"
            ? url.searchParams.has("after")
              ? []
              : Array.from({ length: 25 }, (_, index) => ({
                  ...importSnapshot(),
                  uuid: `10000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
                }))
            : [importSnapshot(kind)],
      });
    }
    if (path === `import-history/${kind}/${importIds.snapshot}`) {
      const result = importDetails(kind);
      if (control.malformed) result.snapshot.uuid = importIds.later;
      return route.fulfill({ json: result });
    }
    throw new Error(`Unexpected import-history request: ${path}`);
  });
  return { requests, writes, control };
}

for (const viewport of [
  { name: "mobile", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 900 },
]) {
  test(`inspects historical import steps without creating work on ${viewport.name}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport);
    const fixture = await archive(page);
    await page.goto("/import-history");
    await expect(
      page.getByRole("button", { name: "Inspect import" }),
    ).toBeVisible();
    expect(
      fixture.requests.every((url) => url.pathname.endsWith("/catalog")),
    ).toBe(true);
    await page.getByRole("button", { name: "Inspect import" }).click();
    await expect(
      page.getByRole("button", { name: "Import steps", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("2 records needed review at import time", { exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: "Import steps", exact: true })
      .click();
    await expect(
      page.getByText("2 records needed review at import time", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Not started", { exact: true })).toHaveCount(
      10,
    );
    await expect(
      page.getByRole("link", { name: "Open current collection" }),
    ).toHaveAttribute("href", /collection=/);
    await expect(
      page.getByText(
        /Later association reviews do not change historical warning counts/,
      ),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({ path: testInfo.outputPath("import-history.png") });
    if (viewport.name === "desktop") {
      await page
        .getByRole("button", { name: "More options", exact: true })
        .click();
      await page
        .getByRole("menuitem", { name: "Import history", exact: true })
        .click();
      await expect(page.getByRole("menu")).toHaveCount(0);
    } else {
      await page
        .getByRole("button", { name: "Open navigation menu", exact: true })
        .tap();
      const drawer = page.locator("[data-mobile-navigation]");
      await drawer
        .getByRole("link", { name: "Import history", exact: true })
        .tap();
      await expect(drawer).toHaveCount(0);
    }
    await expect(
      page.getByRole("button", { name: "Inspect import", exact: true }),
    ).toBeVisible();
    expect(fixture.writes).toEqual([]);
  });
}

test("keeps an existing page through a failed refresh and retains its cursor after detail", async ({
  page,
}) => {
  const fixture = await archive(page, true);
  await page.goto("/import-history");
  await expect(
    page.getByRole("button", { name: "Inspect import" }),
  ).toHaveCount(25);
  fixture.control.failList = true;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByText("Could not complete this step")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Inspect import" }),
  ).toHaveCount(25);
  fixture.control.failList = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByText("Could not complete this step")).toHaveCount(0);
  await page.getByRole("button", { name: "Inspect import" }).first().click();
  await expect(
    page.getByRole("button", { name: "Import steps", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Back to imports" }).click();
  await expect(
    page.getByRole("button", { name: "Inspect import" }),
  ).toHaveCount(25);
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page.getByText("No snapshots on this page")).toBeVisible();
  await expect(page.getByRole("button", { name: "Next page" })).toBeDisabled();
  await page.getByRole("button", { name: "Previous page" }).click();
  await expect(
    page.getByRole("button", { name: "Inspect import" }),
  ).toHaveCount(25);
  expect(fixture.writes).toEqual([]);
});

test("refuses a mismatched detail and separates automation from catalog history", async ({
  page,
}) => {
  const fixture = await archive(page);
  fixture.control.malformed = true;
  await page.goto(`/import-history?snapshot=${importIds.snapshot}`);
  await expect(page.getByText("Could not complete this step")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Import steps", exact: true }),
  ).toHaveCount(0);
  fixture.control.malformed = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Import steps", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Automation", exact: true }).click();
  await expect(
    page.getByText("Automation snapshot", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Inspect import" }).click();
  await page.getByRole("button", { name: "Import steps", exact: true }).click();
  await expect(page.getByText("Not started", { exact: true })).toHaveCount(3);
  await expect(
    page.getByRole("link", { name: "Open current collection" }),
  ).toHaveCount(0);
  expect(fixture.writes).toEqual([]);
});
