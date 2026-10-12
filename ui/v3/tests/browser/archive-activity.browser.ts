import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  activityIds,
  activityJob,
  activityJobDetail,
  activityRun,
  activityRunDetail,
  activityAttempt,
} from "../fixtures/archive-activity";
import { collection } from "../fixtures/collections";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function archive(page: Page, many = false) {
  const requests: URL[] = [];
  const writes: string[] = [];
  const control = {
    failList: false,
    failDetail: false,
    failCollection: false,
    malformed: false,
    traversal: false,
  };
  await page.route("**/api/v3/archive/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname.endsWith("/page-info")) {
      const resource = url.searchParams.get("resource");
      const total =
        resource === "job-attempts" ? 26 : resource === "jobs" && many ? 26 : 1;
      return route.fulfill({
        json: {
          total,
          offset:
            Number(url.searchParams.get("cursor")) > 0
              ? Math.min(25, total)
              : 0,
        },
      });
    }
    requests.push(url);
    if (request.method() !== "GET") writes.push(request.method());
    const path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    const fail = () =>
      route.fulfill({ status: 503, json: { error: "unavailable" } });
    let result: unknown;
    if (path === "activity/jobs") {
      if (control.failList) return fail();
      const filterKind = url.searchParams.get("kind");
      const filterState = url.searchParams.get("state");
      const job = {
        ...activityJob(),
        kind: filterKind || "media.verify",
        state: filterState || "queued",
      };
      result = many
        ? Number(url.searchParams.get("before")) > 0
          ? [
              {
                ...job,
                sequence: 75,
                uuid: "10000000-0000-4000-8000-000000000026",
              },
            ]
          : Array.from({ length: 25 }, (_, index) => ({
              ...job,
              sequence: 100 - index,
              uuid: `10000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
            }))
        : [job];
    } else if (path === `activity/jobs/${activityIds.job}`) {
      if (control.failDetail) return fail();
      result = control.malformed
        ? {
            ...activityJobDetail(),
            summary: { ...activityJob(), uuid: activityIds.other },
          }
        : {
            ...activityJobDetail(),
            summary: { ...activityJob(), attempt_count: 30, max_attempts: 40 },
          };
    } else if (path === "activity/scrape-metrics") {
      const offset = Number(url.searchParams.get("offset"));
      result = {
        since: "2026-10-10T00:00:00Z",
        metrics: [
          {
            source_scope: "service:twitter",
            class: "routine",
            operation: "listing",
            requests: 12,
            responses: 12,
            rate_limited: 1,
            errors: 1,
            new_posts: 0,
            repeated_posts: 0,
          },
        ],
        accounts: Array.from({ length: offset === 0 ? 50 : 1 }, (_, index) => ({
          collection_uuid: `40000000-0000-4000-8000-${String(offset + index + 1).padStart(12, "0")}`,
          label: `Account ${offset + index + 1}`,
          url: `https://x.com/account${offset + index + 1}`,
          last_success_ms: 0,
          overdue_seconds: 0,
        })),
        total: 51,
      };
    } else if (path === "activity/runs") result = [activityRun()];
    else if (path === `activity/runs/${activityIds.run}`) {
      const detail = activityRunDetail();
      detail.summary.attempt_count = 1;
      detail.summary.completed_windows = 1;
      detail.completed = [
        { since: "2026-10-01T00:00:00Z", until: "2026-10-02T00:00:00Z" },
      ];
      detail.pending = [
        { since: "2026-10-02T00:00:00Z", until: "2026-10-06T12:00:00Z" },
      ];
      if (control.traversal) {
        detail.summary.operation = "download";
        detail.completed = detail.completed.map((row) => ({
          ...row,
          since: null,
          basis: "traversal",
        }));
        detail.pending = detail.pending.map((row) => ({
          ...row,
          since: null,
          basis: "traversal",
        }));
      }
      result = detail;
    } else if (path.endsWith("/attempts")) {
      result = path.startsWith("activity/runs/")
        ? [{ ...activityAttempt(), outcome: "succeeded", error_code: "" }]
        : Number(url.searchParams.get("before")) > 0
          ? [activityAttempt()]
          : Array.from({ length: 25 }, (_, index) => ({
              ...activityAttempt(),
              number: 30 - index,
            }));
    } else if (
      path === "collections" ||
      path === `collections/${activityIds.collection}`
    ) {
      if (control.failCollection) return fail();
      const row = {
        ...collection(),
        uuid: activityIds.collection,
        label: "Example source",
      };
      result = path === "collections" ? [row] : row;
    } else throw new Error(`Unexpected activity request: ${path}`);
    await route.fulfill({ json: result });
  });
  return { requests, writes, control };
}

for (const desktop of [false, true]) {
  test(`scrape request metrics retain expanded account paging on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const remote = await archive(page);
    await page.goto("/archive-activity?view=runs");
    const card = page
      .locator('[data-slot="card"]')
      .filter({ hasText: "Scrape request budget" });
    await expect(
      card.getByText("Routine checks · Post listings"),
    ).toBeVisible();
    await card
      .getByRole("button", {
        name: "Account checks, oldest first",
        exact: true,
      })
      .click();
    const summary = card.getByRole("status");
    await expect(summary).toHaveText("Page 1 of 2 · 51 items");
    await card.getByRole("button", { name: "Next", exact: true }).click();
    await expect(summary).toBeVisible();
    await expect(summary).toHaveText("Page 2 of 2 · 51 items");
    await expect(
      card.getByRole("link", { name: "Account 51", exact: true }),
    ).toBeVisible();
    await expect(
      card.getByRole("button", { name: "Next", exact: true }),
    ).toBeDisabled();
    await card.getByRole("button", { name: "Previous", exact: true }).click();
    await expect(summary).toHaveText("Page 1 of 2 · 51 items");
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    expect(remote.writes).toEqual([]);
  });

  test(`activity navigation and lazy history on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }, testInfo) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const remote = await archive(page);
    await page.goto(`/archive-activity?view=jobs&item=${activityIds.job}`);
    await expect(
      page.getByText("Example clip.mp4", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Example source", exact: true }),
    ).toHaveAttribute("href", /collection=44444444/);
    await expect(
      page.getByText("Collection as scheduled · revision 1"),
    ).toBeVisible();
    expect(
      remote.requests.filter((u) => u.pathname.endsWith("/attempts")),
    ).toHaveLength(0);
    await page
      .getByRole("button", { name: "Attempt history", exact: true })
      .click();
    await expect(page.getByText("Attempt 30", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Older", exact: true }).click();
    await expect(page.getByText("Attempt 1", { exact: true })).toBeVisible();
    expect(
      remote.requests.some(
        (u) =>
          u.pathname.endsWith("/attempts") &&
          u.searchParams.get("before") === "6",
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Newer", exact: true }).click();
    await expect(page.getByText("Attempt 30", { exact: true })).toBeVisible();
    await page
      .getByRole("button", { name: "Attempt history", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Job details", exact: true })
      .click();
    await expect(
      page.getByText(activityIds.job, { exact: true }),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.screenshot({ path: testInfo.outputPath("activity.png") });
    if (desktop) {
      await page
        .getByRole("button", { name: "More options", exact: true })
        .click();
      await page
        .getByRole("menuitem", { name: "Archive activity", exact: true })
        .click();
      await expect(page.getByRole("menu")).toHaveCount(0);
    } else {
      await page
        .getByRole("button", { name: "Open navigation menu", exact: true })
        .tap();
      const drawer = page.locator("[data-mobile-navigation]");
      await drawer
        .getByRole("link", { name: "Archive activity", exact: true })
        .tap();
      await expect(drawer).toHaveCount(0);
    }
    await expect(
      page.getByRole("button", { name: "Inspect activity", exact: true }),
    ).toBeVisible();
    expect(remote.writes).toEqual([]);
  });
}

test("filters use readable labels and reset paging without leaking a previous result", async ({
  page,
}) => {
  const remote = await archive(page, true);
  await page.goto("/archive-activity?view=jobs");
  await expect(
    page.getByRole("button", { name: "Inspect activity", exact: true }),
  ).toHaveCount(25);
  await page.getByRole("button", { name: "Older", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Inspect activity", exact: true }),
  ).toHaveCount(1);
  expect(remote.requests.at(-1)?.searchParams.get("before")).toBe("76");
  await page.getByRole("button", { name: "Newer", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Inspect activity", exact: true }),
  ).toHaveCount(25);
  await page.getByRole("combobox", { name: "Job type", exact: true }).click();
  await page
    .getByRole("option", { name: "Translate source text", exact: true })
    .click();
  await page.getByRole("button", { name: "Failed", exact: true }).click();
  await page
    .getByRole("button", { name: "Apply filters", exact: true })
    .click();
  await expect
    .poll(() =>
      remote.requests.some(
        (u) =>
          u.searchParams.get("kind") === "text.translate" &&
          u.searchParams.get("state") === "failed" &&
          u.searchParams.get("before") === "0",
      ),
    )
    .toBe(true);
  await expect(page.getByText("Import media", { exact: true })).toHaveCount(0);
  expect(remote.writes).toEqual([]);
});

test("failed reads recover and a failed refresh preserves the last successful view", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.control.failList = true;
  await page.goto("/archive-activity?view=jobs");
  await expect(page.getByRole("alert")).toContainText(
    "Could not load activity",
  );
  await expect(
    page.getByRole("button", { name: "Inspect activity", exact: true }),
  ).toHaveCount(0);
  remote.control.failList = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Inspect activity", exact: true }),
  ).toBeVisible();
  remote.control.failList = true;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Could not load activity",
  );
  await expect(
    page.getByRole("button", { name: "Inspect activity", exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([]);
});

test("a mismatched detail cannot display another job and retry recovers", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.control.malformed = true;
  await page.goto(`/archive-activity?view=jobs&item=${activityIds.job}`);
  await expect(page.getByRole("alert")).toContainText(
    "Could not load activity",
  );
  await expect(page.getByText("Example clip.mp4", { exact: true })).toHaveCount(
    0,
  );
  remote.control.malformed = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByText("Example clip.mp4", { exact: true }),
  ).toBeVisible();
  remote.control.failDetail = true;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Could not load activity",
  );
  await expect(
    page.getByText("Example clip.mp4", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([]);
});

test("a completed scrape attempt with pending windows still displays a queued run", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto(`/archive-activity?view=runs&item=${activityIds.run}`);
  await expect(
    page.getByText("Queued", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page
      .getByText("Fetch metadata only", { exact: true })
      .filter({ visible: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Attempt history", exact: true })
    .click();
  await expect(
    page.getByText("Finished", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Queued", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Scrape coverage", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Pending time windows", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(/Finishing one window does not finish a run/),
  ).toBeVisible();
  expect(remote.writes).toEqual([]);
});

test("a failed collection lookup keeps its filter visible and permits clearing", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.control.failCollection = true;
  await page.goto(
    `/archive-activity?view=runs&collection=${activityIds.collection}`,
  );
  await expect(page.getByRole("alert")).toContainText(
    "The collection filter is still applied",
  );
  expect(
    remote.requests.some(
      (u) =>
        u.pathname.endsWith("/runs") &&
        u.searchParams.get("collection") === activityIds.collection,
    ),
  ).toBe(true);
  remote.control.failCollection = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("combobox", { name: "Source collection", exact: true }),
  ).toHaveValue("Example source");
  remote.control.failCollection = true;
  await page.reload();
  await expect(page.getByRole("alert")).toBeVisible();
  await page
    .getByRole("button", { name: "Clear collection filter", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Apply filters", exact: true })
    .click();
  await expect
    .poll(() =>
      remote.requests.some(
        (u) =>
          u.pathname.endsWith("/runs") && !u.searchParams.has("collection"),
      ),
    )
    .toBe(true);
  expect(remote.writes).toEqual([]);
});

test("configured scans do not claim all-history or publication coverage", async ({
  page,
}, testInfo) => {
  const remote = await archive(page);
  remote.control.traversal = true;
  await page.goto(`/archive-activity?view=runs&item=${activityIds.run}`);
  await page
    .getByRole("button", { name: "Scrape coverage", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Pending scans", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Completed scans", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Scan requested", { exact: true })).toHaveCount(
    2,
  );
  await expect(
    page.getByText(/Completion does not establish publication-date coverage/),
  ).toBeVisible();
  await expect(
    page.getByText("All earlier history", { exact: true }),
  ).toHaveCount(0);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.screenshot({ path: testInfo.outputPath("configured-scan.png") });
  expect(remote.writes).toEqual([]);
});
