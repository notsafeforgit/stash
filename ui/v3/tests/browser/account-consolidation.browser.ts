import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  account,
  ids,
  performer,
  linkedAccount,
  preview as ownershipPreview,
} from "../fixtures/account-review";
import {
  destination,
  consolidationPreview,
  consolidationReceipt,
} from "../fixtures/account-consolidation";
import type {
  ConsolidationApply,
  ConsolidationInput,
  ConsolidationReceipt,
} from "../../src/core/native-archive/account-consolidation-api";

test.use({
  expectedConsoleErrors: [
    "the server responded with a status of 404",
    "the server responded with a status of 409",
    "the server responded with a status of 503",
  ],
});

async function archive(
  page: Page,
  options: {
    conflicts?: boolean;
    loseResponse?: boolean;
    loseBeforeCommit?: boolean;
    stale?: boolean;
    refreshFails?: boolean;
  } = {},
) {
  let source = options.conflicts
    ? linkedAccount({
        ...ownershipPreview().input,
        digest: "a".repeat(64),
        request_uuid: ids.request,
      })
    : account();
  let target = destination();
  if (options.conflicts) {
    target = {
      ...target,
      identifiers: [
        {
          ...target.identifiers[0]!,
          reference: {
            namespace: "native:reddit",
            kind: "id",
            value: "other-id",
          },
        },
      ],
      ownership: {
        decision_uuid: ids.request,
        revision: 1,
        state: "unlinked",
        origin: "review",
        reason: "",
        created_at: "2026-10-03T00:00:00Z",
      },
    };
  }
  let committed: ConsolidationReceipt | null = null;
  let stale = options.stale;
  let refreshFails = options.refreshFails;
  const writes: string[] = [];
  const checks: string[] = [];
  const previews: ConsolidationInput[] = [];
  const lists: URL[] = [];
  const reads: URL[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    reads.push(url);
    let result: unknown;
    if (path.endsWith("/source-accounts")) {
      lists.push(url);
      result = [source, target].filter(
        (row) => row.uuid === row.canonical_uuid,
      );
    } else if (path.endsWith("/consolidation-history"))
      result = committed ? [committed.consolidation] : [];
    else if (path.endsWith("/ownership-history") || path.endsWith("/evidence"))
      result = [];
    else if (path.endsWith("/identifiers"))
      result = path.includes(ids.account)
        ? source.identifiers
        : target.identifiers;
    else if (path.includes("/source-accounts/")) {
      if (committed && refreshFails)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      result = path.endsWith(ids.account) ? source : target;
    } else if (path.includes("/entity-identities/performer/")) {
      const owner = performer(path.endsWith("/11"));
      result = {
        uuid: owner.uuid,
        revision: owner.revision,
        kind: "performer",
        local_id: owner.local_id,
      };
    } else if (path.endsWith("/account-consolidation/preview")) {
      const input: ConsolidationInput = route.request().postDataJSON();
      previews.push(input);
      const ownership =
        input.ownership_mode === "choose"
          ? input.ownership
          : options.conflicts
            ? undefined
            : { state: "undecided" as const };
      const blockers = [
        ...(options.conflicts && !input.accept_identifier_conflicts
          ? ["identifiers"]
          : []),
        ...(!ownership ? ["ownership"] : []),
      ];
      const { performer: _unused, ...base } = consolidationPreview();
      result = {
        ...base,
        input,
        source,
        destination: target,
        ownership,
        blockers,
        ready: blockers.length === 0,
        identifier_conflicts: options.conflicts
          ? [
              {
                namespace: "native:reddit",
                kind: "id",
                values: ["t2_river", "other-id"],
              },
            ]
          : [],
        ...(ownership?.state === "linked"
          ? {
              performer: performer(
                ownership.performer_uuid === ids.otherPerformer,
              ),
            }
          : {}),
      };
    } else if (path.endsWith("/check")) {
      checks.push(route.request().postData() ?? "");
      if (!committed)
        return route.fulfill({ status: 404, json: { error: "not_found" } });
      result = committed;
    } else if (path.endsWith("/account-consolidation/apply")) {
      const body = route.request().postData() ?? "";
      writes.push(body);
      if (options.loseBeforeCommit && writes.length === 1)
        return route.abort("connectionreset");
      if (stale) {
        stale = false;
        source = { ...source, revision: source.revision + 1 };
        return route.fulfill({
          status: 409,
          json: { error: "preview_changed" },
        });
      }
      const input: ConsolidationApply = JSON.parse(body);
      committed = consolidationReceipt(input);
      const state = input.ownership?.state ?? "undecided";
      target = {
        ...target,
        revision: target.revision + 1,
        ownership: {
          decision_uuid: ids.decision,
          revision: target.revision + 1,
          state,
          ...(state === "linked"
            ? {
                performer_uuid: input.ownership?.performer_uuid,
                performer: performer(
                  input.ownership?.performer_uuid === ids.otherPerformer,
                ),
              }
            : {}),
          origin: "review",
          reason: input.reason ?? "",
          created_at: "2026-10-03T00:00:00Z",
        },
      };
      source = {
        ...source,
        revision: source.revision + 1,
        redirect_to: target.uuid,
        canonical_uuid: target.uuid,
        ownership: target.ownership,
      };
      if (options.loseResponse && writes.length === 1)
        return route.abort("connectionreset");
      result = { review: committed, replayed: false };
    } else throw new Error(`Unexpected consolidation request: ${path}`);
    await route.fulfill({ json: result });
  });
  return {
    writes,
    checks,
    previews,
    lists,
    reads,
    restore: () => {
      refreshFails = false;
    },
  };
}

async function open(page: Page) {
  await page.goto(`/account-review?ownership=all&account=${ids.account}`);
  await page
    .getByRole("button", {
      name: "Consolidate duplicate account records",
      exact: true,
    })
    .click();
  await chooseAccount(page);
}
async function chooseAccount(page: Page) {
  const input = page.getByRole("combobox", {
    name: "Account to keep",
    exact: true,
  });
  // A user reaches this lower form by scrolling/tapping. WebKit's synthetic
  // fill can focus an offscreen input without scrolling the parent panel.
  await input.click();
  await input.fill("river");
  await page
    .getByRole("option", { name: new RegExp(`river-id.*${ids.otherAccount}`) })
    .click();
}
async function preview(page: Page) {
  await page
    .getByRole("button", { name: "Preview consolidation", exact: true })
    .click();
  await expect(
    page.getByText("Review account consolidation", { exact: true }),
  ).toBeVisible();
}
async function apply(page: Page) {
  await page
    .getByRole("button", {
      name: "Consolidate these account records",
      exact: true,
    })
    .click();
}

test("reviews same-service identities and refreshes just the affected cards", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  const initialPages = remote.lists.length;
  expect(remote.lists.at(-1)?.searchParams.get("namespace")).toBe(
    "native:reddit",
  );
  await preview(page);
  expect(remote.writes).toHaveLength(0);
  await expect(
    page.getByText(
      "2 account records and 3 identifier claims will belong to the resulting account.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: test.info().outputPath("account-consolidation-mobile.png"),
    fullPage: true,
  });
  await apply(page);
  await expect(
    page.getByText("Account consolidation saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(remote.checks).toEqual(remote.writes);
  expect(remote.lists).toHaveLength(initialPages);
  await page
    .getByRole("button", { name: "Consolidation history", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Open resulting account", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Back to accounts", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(1);
  expect(remote.lists).toHaveLength(initialPages);
});

test("requires explicit ownership and stable-ID acknowledgement when they conflict", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const remote = await archive(page, { conflicts: true });
  await open(page);
  await preview(page);
  await expect(
    page.getByText("Conflicting stable identifiers", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", {
      name: "Consolidate these account records",
      exact: true,
    }),
  ).toBeDisabled();
  await page
    .getByRole("checkbox", {
      name: "I reviewed the conflicting IDs and confirm this is the same account",
      exact: true,
    })
    .check();
  await page
    .getByRole("group", { name: "Resulting ownership", exact: true })
    .getByRole("button", { name: "Link performer", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "Resulting performer", exact: true })
    .fill("River");
  await page
    .getByRole("option", { name: "River (Photographer) (#11)", exact: true })
    .click();
  await preview(page);
  await expect(
    page.getByRole("button", {
      name: "Consolidate these account records",
      exact: true,
    }),
  ).toBeEnabled();
  expect(remote.writes).toHaveLength(0);
  await page.screenshot({
    path: test.info().outputPath("account-consolidation-desktop.png"),
    fullPage: true,
  });
  await apply(page);
  await expect(
    page.getByText("Account consolidation saved", { exact: true }),
  ).toBeVisible();
  expect(JSON.parse(remote.writes[0]!)).toMatchObject({
    ownership_mode: "choose",
    ownership: { state: "linked", performer_uuid: ids.otherPerformer },
    accept_identifier_conflicts: true,
  });
});

test.describe("response recovery", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource", "Load failed"],
  });
  test("recovers a committed consolidation after reload even though the source redirects", async ({
    page,
  }) => {
    const remote = await archive(page, { loseResponse: true });
    await open(page);
    await preview(page);
    await apply(page);
    await expect(
      page.getByText("An account consolidation needs confirmation", {
        exact: true,
      }),
    ).toBeVisible();
    await page.reload();
    await page
      .getByRole("button", {
        name: "Check and retry saved consolidation",
        exact: true,
      })
      .click();
    await expect(
      page.getByText("Account consolidation saved", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(1);
    expect(remote.checks.at(-1)).toBe(remote.writes[0]);
  });
  test("keeps the exact unsent request and blocks a replacement ownership choice", async ({
    page,
  }) => {
    const remote = await archive(page, { loseBeforeCommit: true });
    await open(page);
    await preview(page);
    await apply(page);
    await expect(
      page.getByText("An account consolidation needs confirmation", {
        exact: true,
      }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("button", { name: "Unlink", exact: true }),
    ).toBeDisabled();
    await expect(
      page.getByRole("combobox", { name: "Performer", exact: true }),
    ).toBeDisabled();
    await page
      .getByRole("button", {
        name: "Check and retry saved consolidation",
        exact: true,
      })
      .click();
    await expect(
      page.getByText("Account consolidation saved", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(2);
    expect(remote.writes[0]).toBe(remote.writes[1]);
  });
});

test("refreshes a definitively stale request before creating a new review", async ({
  page,
}) => {
  const remote = await archive(page, { stale: true });
  await open(page);
  await preview(page);
  await apply(page);
  await page.getByRole("button", { name: "Review again", exact: true }).click();
  await chooseAccount(page);
  await preview(page);
  await apply(page);
  await expect(
    page.getByText("Account consolidation saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(2);
  expect(JSON.parse(remote.writes[0]!).request_uuid).not.toBe(
    JSON.parse(remote.writes[1]!).request_uuid,
  );
});

test("keeps confirmed success when refreshing the two accounts fails", async ({
  page,
}) => {
  const remote = await archive(page, { refreshFails: true });
  await open(page);
  await preview(page);
  await apply(page);
  await expect(
    page.getByText(
      "The consolidation is saved, but this view could not be refreshed",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Preview consolidation", exact: true }),
  ).toBeDisabled();
  remote.restore();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Open current account", exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
});
