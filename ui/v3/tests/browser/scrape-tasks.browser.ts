import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  activityRun,
  activityRunDetail,
  activityIds,
} from "../fixtures/archive-activity";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function setup(page: Page) {
  const remote = {
    running: true,
    fail: false,
    requests: 0,
    writes: [] as string[],
  };
  await page.route("**/api/v3/archive/**", async (route) => {
    const req = route.request();
    if (req.method() !== "GET") remote.writes.push(req.method());
    remote.requests++;
    const url = new URL(req.url());
    if (remote.fail)
      return route.fulfill({ status: 503, json: { error: "unavailable" } });
    const row = {
      ...activityRun(),
      collection_label: "Instagram · example",
      target_url: "https://instagram.com/example",
      operation: "download",
      state: remote.running ? "running" : "succeeded",
      attempt_count: 1,
      pending_windows: 0,
      completed_windows: remote.running ? 0 : 1,
      lease_until: remote.running ? "2026-10-12T01:00:00Z" : null,
    };
    if (url.pathname.endsWith("/page-info")) {
      const state = url.searchParams.get("state");
      return route.fulfill({
        json: {
          total:
            state === "queued"
              ? 500
              : state === "deferred"
                ? 2
                : remote.running
                  ? 1
                  : 0,
          offset: 0,
        },
      });
    }
    if (url.pathname.endsWith("/activity/runs")) {
      return route.fulfill({
        json: remote.running || !url.searchParams.get("state") ? [row] : [],
      });
    }
    if (url.pathname.endsWith(`/activity/runs/${activityIds.run}`)) {
      return route.fulfill({
        json: {
          ...activityRunDetail(),
          summary: row,
          pending: [],
          completed: [],
          window: { since: null, until: "2026-10-12T00:00:00Z" },
          progress: { items_seen: 52, files_completed: 18, cursor: "" },
        },
      });
    }
    if (url.pathname.endsWith("/activity/scrape-metrics"))
      return route.fulfill({
        json: {
          since: "2026-10-11T00:00:00Z",
          metrics: [],
          accounts: [],
          total: 0,
        },
      });
    throw new Error(`Unexpected scrape task request: ${url.pathname}`);
  });
  return remote;
}

for (const desktop of [false, true]) {
  test(`scrape status opens the actual run from Tasks on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }, testInfo) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const remote = await setup(page);
    await page.goto("/scrape-tasks");
    await expect(
      page.getByRole("link", { name: "Running: 1", exact: true }),
    ).toHaveAttribute("href", /view=runs.*state=running/);
    await expect(
      page.getByRole("link", { name: "Queued: 500", exact: true }),
    ).toHaveAttribute("href", /state=queued/);
    await expect(
      page.getByRole("link", { name: "Deferred: 2", exact: true }),
    ).toHaveAttribute("href", /state=deferred/);
    await expect(
      page.getByText("Showing 1 of 1 running scrapes."),
    ).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath(
        `scrape-tasks-${desktop ? "desktop" : "mobile"}.png`,
      ),
    });
    await page
      .getByRole("link", { name: "Instagram · example", exact: true })
      .click();
    await expect(
      page.getByText("52 items seen · 18 files completed"),
    ).toBeVisible();
    await expect(page).toHaveURL(
      new RegExp(`view=runs.*item=${activityIds.run}`),
    );
    expect(remote.writes).toEqual([]);
  });
}

test("visible scrape counts update and hidden pages stop polling", async ({
  page,
}) => {
  const remote = await setup(page);
  await page.clock.install();
  await page.goto("/scrape-tasks");
  await expect(
    page.getByRole("link", { name: "Running: 1", exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  const before = remote.requests;
  remote.running = false;
  await page.clock.fastForward(30_000);
  expect(remote.requests).toBe(before);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: false,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(
    page.getByRole("link", { name: "Running: 0", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Instagram · example", exact: true }),
  ).toHaveCount(0);
  remote.running = true;
  await page.clock.fastForward(10_100);
  await expect(
    page.getByRole("link", { name: "Running: 1", exact: true }),
  ).toBeVisible();
});

test("a failed refresh does not turn unknown scrape counts into zero", async ({
  page,
}) => {
  const remote = await setup(page);
  await page.goto("/scrape-tasks");
  await expect(
    page.getByRole("link", { name: "Queued: 500", exact: true }),
  ).toBeVisible();
  remote.fail = true;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Could not load activity",
  );
  await expect(
    page.getByRole("link", { name: "Queued: 500", exact: true }),
  ).toBeVisible();
  remote.fail = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("disabled source tracking does not mount or request scrape activity", async ({
  page,
}) => {
  const remote = await setup(page);
  await page.goto("/scrape-tasks?tracking-off");
  await expect(page.getByText("Scrape runs", { exact: true })).toHaveCount(0);
  expect(remote.requests).toBe(0);
});

test("the activity entry point opens scrape runs", async ({ page }) => {
  const remote = await setup(page);
  await page.goto("/archive-activity");
  await expect(
    page.getByText("Instagram · example", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Scrape runs", exact: true }),
  ).toHaveAttribute("data-pressed", "");
  await expect(
    page.getByRole("combobox", { name: "Job type", exact: true }),
  ).toHaveCount(0);
  expect(remote.writes).toEqual([]);
});
