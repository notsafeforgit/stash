import type { Page } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { account, performer, ids } from "../fixtures/account-review";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});
const oldIdentity = "00000000-0000-4000-8000-000000000090";

async function archive(
  page: Page,
  options: {
    empty?: boolean;
    many?: boolean;
    failed?: boolean;
    changedOnNext?: boolean;
  } = {},
) {
  const requests: URL[] = [];
  const writes: string[] = [];
  let failed = options.failed;
  let revision = 3;
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    if (route.request().method() !== "GET")
      writes.push(route.request().method());
    const path = url.pathname;
    let result: unknown;
    if (path.endsWith("/entity-identities/performer/7")) {
      result = {
        uuid: ids.performer,
        kind: "performer",
        local_id: 7,
        revision,
      };
    } else if (path.endsWith("/source-accounts")) {
      if (failed) {
        await route.fulfill({ status: 503, json: { error: "unavailable" } });
        return;
      }
      const after = url.searchParams.get("after");
      if (after && options.changedOnNext) revision = 4;
      const owner = { ...performer(), local_id: 7, revision };
      const row = {
        ...account(),
        ownership: {
          decision_uuid: ids.decision,
          state: "linked",
          revision: 1,
          performer_uuid: oldIdentity,
          performer: owner,
          origin: "review",
          reason: "Confirmed before merge",
          created_at: "2026-10-01T00:00:00Z",
        },
      };
      result = {
        requested_uuid: ids.performer,
        performer: owner,
        accounts: options.empty
          ? []
          : options.many
            ? Array.from({ length: after ? 1 : 25 }, (_, n) => {
                const uuid = `10000000-0000-4000-8000-${String(after ? 26 : n + 1).padStart(12, "0")}`;
                return {
                  ...row,
                  uuid,
                  canonical_uuid: uuid,
                  identifiers: [],
                  label: `Account ${after ? 26 : n + 1}`,
                };
              })
            : [row],
      };
    } else if (path.endsWith("/performer-identities")) {
      result = {
        requested_uuid: ids.performer,
        performer: { ...performer(), local_id: 7, revision },
        identities: [
          {
            uuid: oldIdentity,
            state: "redirected",
            revision: 2,
            original_id: 72,
            redirect_to: ids.performer,
            created_at: "2026-09-01T00:00:00Z",
            retired_at: "2026-10-01T00:00:00Z",
          },
          {
            uuid: ids.performer,
            state: "active",
            revision,
            original_id: 7,
            redirect_to: null,
            created_at: "2026-09-01T00:00:00Z",
            retired_at: null,
          },
        ],
      };
    } else throw new Error(`Unexpected performer source request: ${path}`);
    await route.fulfill({ json: result });
  });
  return {
    requests,
    writes,
    recover: () => {
      failed = false;
    },
  };
}

async function openSources(page: Page, desktop = false) {
  if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/performer-sources");
  if (desktop)
    await page
      .getByRole("tab", { name: "Source accounts", exact: true })
      .click();
  else await chooseSection(page, "Source accounts");
}

for (const desktop of [false, true]) {
  test(`performer accounts on ${desktop ? "desktop" : "mobile"} retain merged ownership and load history on demand`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await openSources(page, desktop);
    await expect(page.getByText("river", { exact: true })).toBeVisible();
    await expect(
      page.getByText("Linked to this performer", { exact: true }),
    ).toBeVisible();
    expect(
      remote.requests.some((u) => u.pathname.endsWith("/performer-identities")),
    ).toBe(false);
    const manage = page.getByRole("link", {
      name: "Manage account link",
      exact: true,
    });
    await expect(manage).toHaveAttribute(
      "href",
      new RegExp(`/account-review\\?.*account=${ids.account}`),
    );
    await expect(
      page.getByRole("button", { name: "Link performer", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: "Identity history", exact: true })
      .click();
    await expect(
      page.getByText("Redirected identity", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Current identity", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Original library ID: 72", { exact: true }),
    ).toBeVisible();
    await expect(page.locator('a[href="/performers/72"]')).toHaveCount(0);
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: test
        .info()
        .outputPath(`performer-sources-${desktop ? "desktop" : "mobile"}.png`),
    });
    expect(remote.writes).toEqual([]);
  });
}

test("directly scanned performers do not need an invented source account", async ({
  page,
}) => {
  const remote = await archive(page, { empty: true });
  await openSources(page);
  await expect(
    page.getByText("No linked source accounts", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(
      /directly scanned or purchased media without a source account/,
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Open account review", exact: true }),
  ).toHaveAttribute("href", /\/account-review\?/);
  expect(remote.writes).toEqual([]);
});

test("performer source reads recover through retry", async ({ page }) => {
  const remote = await archive(page, { failed: true });
  await openSources(page);
  await expect(
    page.getByText("Could not load performer sources", { exact: true }),
  ).toBeVisible();
  remote.recover();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Manage account link", exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([]);
});

test("account pagination is bounded and keeps loaded accounts", async ({
  page,
}) => {
  const remote = await archive(page, { many: true });
  await openSources(page, true);
  await expect(
    page.getByRole("link", { name: "Manage account link", exact: true }),
  ).toHaveCount(25);
  await page
    .getByRole("button", { name: "Load more accounts", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: "Manage account link", exact: true }),
  ).toHaveCount(26);
  const last = remote.requests
    .filter((url) => url.pathname.endsWith("/source-accounts"))
    .at(-1)!;
  expect(last.searchParams.get("after")).toBe(
    "10000000-0000-4000-8000-000000000025",
  );
  await expect(
    page.getByRole("button", { name: "Load more accounts", exact: true }),
  ).toHaveCount(0);
  expect(remote.writes).toEqual([]);
});

test("a performer changed between pages requires a fresh read", async ({
  page,
}) => {
  await archive(page, { many: true, changedOnNext: true });
  await openSources(page, true);
  await expect(
    page.getByRole("link", { name: "Manage account link", exact: true }),
  ).toHaveCount(25);
  await page
    .getByRole("button", { name: "Load more accounts", exact: true })
    .click();
  await expect(
    page.getByText(
      "This performer changed. Reload to view the current source accounts.",
      { exact: true },
    ),
  ).toBeVisible();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Manage account link", exact: true }),
  ).toHaveCount(25);
  await expect(
    page.getByText("Could not load performer sources", { exact: true }),
  ).toHaveCount(0);
});
