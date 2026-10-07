import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { reviewQueuePage } from "../fixtures/review-queue";
import type { ReviewQueueKind } from "../../src/core/native-archive/review-queue-api";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function archive(page: Page) {
  const requests: URL[] = [],
    writes: string[] = [];
  const control = {
    fail: false,
    emptyContinuation: false,
    empty: false,
    delayAccounts: false,
  };
  let release: (() => void) | undefined;
  const accountDelay = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v3/archive/review-queue/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    requests.push(url);
    if (request.method() !== "GET") writes.push(request.method());
    const kind = url.pathname.split("/").at(-1) as ReviewQueueKind;
    if (control.delayAccounts && kind === "accounts") await accountDelay;
    if (control.fail)
      return route.fulfill({ status: 503, json: { error: "unavailable" } });
    const result = reviewQueuePage(kind);
    if (control.empty) {
      result.items = [];
      result.checked = 0;
    }
    if (
      kind === "media" &&
      control.emptyContinuation &&
      !url.searchParams.has("after")
    ) {
      result.items = [];
      result.checked = 100;
      result.next = "00000000-0000-4000-8000-000000000000";
    }
    await route.fulfill({ json: result });
  });
  return { control, requests, writes, release: () => release?.() };
}

for (const viewport of [
  { name: "mobile", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 900 },
]) {
  test(`reviews accounts, media and metadata through targeted links on ${viewport.name}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport);
    const fixture = await archive(page);
    await page.goto("/review-queue");
    await expect(
      page.getByRole("link", { name: "Review account owner" }),
    ).toHaveAttribute("href", /account-review\?.*account=/);
    await expect(page.getByText("river", { exact: true })).toBeVisible();
    await expect(
      page.getByText(reviewQueuePage("accounts").items[0]?.uuid ?? "", {
        exact: true,
      }),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "Technical references" }).click();
    await expect(
      page.getByText(reviewQueuePage("accounts").items[0]?.uuid ?? "", {
        exact: true,
      }),
    ).toBeVisible();
    if (viewport.name === "desktop") {
      await page
        .getByRole("button", { name: "More options", exact: true })
        .click();
      await page
        .getByRole("menuitem", { name: "Review queue", exact: true })
        .click();
      await expect(page.getByRole("menu")).toHaveCount(0);
    } else {
      await page
        .getByRole("button", { name: "Open navigation menu", exact: true })
        .tap();
      const drawer = page.locator("[data-mobile-navigation]");
      await drawer
        .getByRole("link", { name: "Review queue", exact: true })
        .tap();
      await expect(drawer).toHaveCount(0);
    }
    await page.getByRole("button", { name: "Media", exact: true }).click();
    await expect(
      page.getByRole("link", { name: "Review post media" }),
    ).toHaveAttribute("href", /source-posts\?.*post=/);
    await expect(
      page.getByText("Shared album post", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Attachment associations conflict", { exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Metadata", exact: true }).click();
    await expect(
      page.getByRole("link", { name: "Review retained metadata" }),
    ).toHaveAttribute("href", /scenes\/7\?.*metadata-review/);
    await expect(
      page.getByRole("link", { name: "Review post media" }),
    ).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({ path: testInfo.outputPath("review-queue.png") });
    expect(fixture.writes).toEqual([]);
  });
}

test("continues past empty inspected posts and preserves the cursor in the URL", async ({
  page,
}) => {
  const fixture = await archive(page);
  fixture.control.emptyContinuation = true;
  await page.goto("/review-queue?kind=media");
  await expect(
    page.getByText("More posts to check", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Nothing to review on this page", { exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Continue checking", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: "Review post media" }),
  ).toBeVisible();
  expect(new URL(page.url()).searchParams.get("after")).toBe(
    "00000000-0000-4000-8000-000000000000",
  );
  await page.reload();
  await expect(
    page.getByRole("link", { name: "Review post media" }),
  ).toBeVisible();
  expect(fixture.requests.at(-1)?.searchParams.has("after")).toBe(true);
  await page.getByRole("button", { name: "First page", exact: true }).click();
  await expect(
    page.getByText("More posts to check", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Metadata", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Review retained metadata" }),
  ).toBeVisible();
  expect(new URL(page.url()).searchParams.has("after")).toBe(false);
});

test("retains a successful page through a failed refresh and removes reviewed items on refresh", async ({
  page,
}) => {
  const fixture = await archive(page);
  await page.goto("/review-queue");
  await expect(
    page.getByRole("link", { name: "Review account owner" }),
  ).toBeVisible();
  fixture.control.fail = true;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(
    page.getByText("Could not complete this step", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Review account owner" }),
  ).toBeVisible();
  fixture.control.fail = false;
  fixture.control.empty = true;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByText("Nothing to review on this page", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Review account owner" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Next page", exact: true }),
  ).toBeDisabled();
});

test("late account results cannot replace the selected metadata queue", async ({
  page,
}) => {
  const fixture = await archive(page);
  fixture.control.delayAccounts = true;
  await page.goto("/review-queue");
  await expect.poll(() => fixture.requests.length).toBeGreaterThan(0);
  await page.getByRole("button", { name: "Metadata", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Review retained metadata" }),
  ).toBeVisible();
  fixture.release();
  await expect(
    page.getByRole("link", { name: "Review account owner" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Review retained metadata" }),
  ).toBeVisible();
});
