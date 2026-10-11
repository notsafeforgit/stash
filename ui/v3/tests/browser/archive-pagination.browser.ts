import { test, expect } from "./test";
import { mediaRoot } from "../fixtures/collections";
import { reviewQueuePage } from "../fixtures/review-queue";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.name = "library-folder-fixture";
  });
});

const id = (value: number) =>
  `00000000-0000-4000-8000-${String(value).padStart(12, "0")}`;

for (const viewport of [
  { name: "mobile", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 900 },
]) {
  test(`filtered totals and exact final pages on ${viewport.name}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport);
    const roots = Array.from({ length: 50 }, (_, i) => ({
      ...mediaRoot(),
      uuid: id(i + 1),
      label: `Root ${i + 1}`,
    }));
    const listRequests: URL[] = [];
    await page.route("**/api/v3/archive/**", async (route) => {
      const url = new URL(route.request().url());
      const q = url.searchParams.get("q") ?? "";
      const matching = roots.filter((root) => root.label.includes(q));
      if (url.pathname.endsWith("/page-info")) {
        const cursor = url.searchParams.get("cursor") ?? "";
        return route.fulfill({
          json: {
            total: matching.length,
            offset: matching.filter((root) => root.uuid <= cursor).length,
          },
        });
      }
      expect(url.pathname.endsWith("/media-roots")).toBe(true);
      listRequests.push(url);
      const after = url.searchParams.get("after") ?? "";
      return route.fulfill({
        json: matching.filter((root) => root.uuid > after).slice(0, 25),
      });
    });
    await page.goto("/media-roots");
    await expect(
      page.getByText("Page 1 of 2 · 50 items", { exact: true }),
    ).toBeVisible();
    expect(new Set(listRequests.map(String)).size).toBe(1);
    await page.getByRole("button", { name: "Next", exact: true }).click();
    const summary = page.getByText("Page 2 of 2 · 50 items", { exact: true });
    await expect(summary).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Next", exact: true }),
    ).toBeDisabled();
    await expect(
      page.getByRole("button", { name: "Previous", exact: true }),
    ).toBeEnabled();
    await summary.scrollIntoViewIfNeeded();
    const box = await summary.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width);
    await page.screenshot({
      path: testInfo.outputPath(`pagination-${viewport.name}.png`),
    });
    const search = page.getByRole("textbox", {
      name: "Search folder names or server paths",
    });
    await search.fill("Root 50");
    await page.getByRole("button", { name: "Find folders" }).click();
    await expect(
      page.getByText("Page 1 of 1 · 1 item", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Previous", exact: true }),
    ).toBeDisabled();
    await search.fill("Missing");
    await page.getByRole("button", { name: "Find folders" }).click();
    await expect(
      page.getByText("Page 0 of 0 · 0 items", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Next", exact: true }),
    ).toBeDisabled();
    expect(new Set(listRequests.map(String)).size).toBe(4);
  });
}

test("review deep links show their position without loading earlier pages", async ({
  page,
}) => {
  const example = reviewQueuePage("accounts").items[0]!;
  const items = Array.from({ length: 50 }, (_, i) => ({
    ...example,
    uuid: id(i + 1),
    account: {
      ...example.account!,
      uuid: id(i + 1),
      canonical_uuid: id(i + 1),
      identifiers: [],
    },
  }));
  const requests: URL[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/page-info")) {
      const cursor = url.searchParams.get("cursor") ?? "";
      return route.fulfill({
        json: {
          total: 50,
          offset: items.filter((item) => item.uuid <= cursor).length,
        },
      });
    }
    requests.push(url);
    const after = url.searchParams.get("after") ?? "";
    const rows = items.filter((item) => item.uuid > after).slice(0, 25);
    return route.fulfill({
      json: {
        kind: "accounts",
        items: rows,
        checked: rows.length,
        ...(after ? {} : { next: id(25) }),
      },
    });
  });
  await page.goto(`/review-queue?kind=accounts&after=${id(25)}`);
  await expect(
    page.getByText("Page 2 of 2 · 50 items", { exact: true }),
  ).toBeVisible();
  expect(new Set(requests.map(String)).size).toBe(1);
  await page.getByRole("button", { name: "First page", exact: true }).click();
  await expect(
    page.getByText("Page 1 of 2 · 50 items", { exact: true }),
  ).toBeVisible();
});

test("a failed count can be retried without replacing the loaded rows", async ({
  page,
}) => {
  let fail = true;
  await page.route("**/api/v3/archive/**", async (route) => {
    if (new URL(route.request().url()).pathname.endsWith("/page-info")) {
      return fail
        ? route.fulfill({ status: 503, json: { error: "unavailable" } })
        : route.fulfill({ json: { total: 1, offset: 0 } });
    }
    return route.fulfill({ json: [mediaRoot()] });
  });
  await page.goto("/media-roots");
  await expect(page.getByRole("button", { name: "Edit folder" })).toBeVisible();
  await expect(
    page.getByRole("status").filter({ hasText: "Could not load item count" }),
  ).toBeVisible();
  fail = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByText("Page 1 of 1 · 1 item", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Edit folder" })).toBeVisible();
});
