import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  account,
  ids,
  preview,
  receipt,
  linkedAccount,
  performer,
} from "../fixtures/account-review";
import type {
  Account,
  OwnershipApply,
  OwnershipReceipt,
} from "../../src/core/native-archive/account-review-api";

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
    linked?: boolean;
    loseResponse?: boolean;
    stale?: boolean;
    stalePreview?: boolean;
    firstPageFails?: boolean;
    accountFailsAfterCommit?: boolean;
    many?: boolean;
  } = {},
) {
  let current = options.linked
    ? linkedAccount({
        ...preview().input,
        digest: preview().digest,
        request_uuid: ids.request,
      })
    : account();
  let committed: OwnershipReceipt | null = null;
  let stale = options.stale;
  let stalePreview = options.stalePreview;
  let pageFails = options.firstPageFails;
  let accountFails = options.accountFailsAfterCommit;
  const writes: OwnershipApply[] = [];
  const previews: unknown[] = [];
  const lists: URL[] = [];
  const requests: URL[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    const path = url.pathname;
    let result: unknown;
    if (path.endsWith("/source-accounts")) {
      lists.push(url);
      if (pageFails) {
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      }
      const status = url.searchParams.get("ownership");
      const q = url.searchParams.get("q");
      if (options.many && !url.searchParams.get("after"))
        result = Array.from(
          { length: 25 },
          (_, index): Account => ({
            ...account(),
            uuid: `40000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
            canonical_uuid: `40000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
            label: `Account ${index + 1}`,
          }),
        );
      else
        result =
          (!status || status === (current.ownership?.state ?? "undecided")) &&
          (!q || current.label.includes(q))
            ? [current]
            : [];
    } else if (path.endsWith("/ownership-history"))
      result = current.ownership ? [current.ownership] : [];
    else if (path.endsWith("/identifiers")) result = current.identifiers;
    else if (path.endsWith("/evidence"))
      result = [
        {
          key: "capture:first",
          basis: "captured-author",
          origin: "gallery-dl",
          details: {
            source_url: "https://www.reddit.com/user/river/",
            extractor: "reddit",
          },
          first_observed: "2026-10-01T00:00:00Z",
          last_observed: "2026-10-03T00:00:00Z",
        },
      ];
    else if (path.includes("/source-accounts/")) {
      if (accountFails && committed)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      const selected = path.split("/").at(-1);
      result =
        options.many && selected !== current.uuid
          ? { ...current, uuid: selected, canonical_uuid: selected }
          : current;
    } else if (path.includes("/entity-identities/performer/")) {
      const owner = performer(path.endsWith("/11"));
      result = {
        uuid: owner.uuid,
        kind: "performer",
        revision: owner.revision,
        local_id: owner.local_id,
      };
    } else if (path.endsWith("/preview")) {
      const input = route.request().postDataJSON();
      previews.push(input);
      if (stalePreview) {
        stalePreview = false;
        current = { ...current, revision: current.revision + 1 };
        return route.fulfill({
          status: 409,
          json: { error: "preview_changed" },
        });
      }
      result = {
        input,
        account: current,
        digest: preview().digest,
        ...(input.state === "linked"
          ? {
              performer: performer(input.performer_uuid === ids.otherPerformer),
            }
          : {}),
      };
    } else if (path.endsWith("/apply")) {
      const input: OwnershipApply = route.request().postDataJSON();
      writes.push(input);
      if (stale) {
        stale = false;
        current = { ...current, revision: current.revision + 1 };
        return route.fulfill({
          status: 409,
          json: { error: "preview_changed" },
        });
      }
      committed = receipt(input);
      current = linkedAccount(input);
      if (options.loseResponse && writes.length === 1)
        return route.abort("connectionreset");
      result = { review: committed, replayed: false };
    } else if (path.includes("/requests/")) {
      if (!committed)
        return route.fulfill({ status: 404, json: { error: "not_found" } });
      result = committed;
    } else throw new Error(`Unexpected account request: ${path}`);
    await route.fulfill({ json: result });
  });
  return {
    writes,
    lists,
    requests,
    previews,
    restoreAccountRead: () => {
      accountFails = false;
    },
    restoreAccountPage: () => {
      pageFails = false;
    },
  };
}

async function choosePerformer(page: Page, other = false) {
  await page
    .getByRole("combobox", { name: "Performer", exact: true })
    .fill("River");
  await expect(
    page.getByRole("option", { name: "River (Model) (#10)", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("option", {
      name: "River (Photographer) (#11)",
      exact: true,
    }),
  ).toBeVisible();
  await page
    .getByRole("option", {
      name: other ? "River (Photographer) (#11)" : "River (Model) (#10)",
      exact: true,
    })
    .click();
  await page
    .getByRole("button", { name: "Preview ownership", exact: true })
    .click();
}

test("previews an explicit performer choice and refreshes only the affected account", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto("/account-review");
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(1);
  const initialPages = remote.lists.length;
  await page
    .getByRole("button", { name: "Review account", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Preview ownership", exact: true }),
  ).toBeDisabled();
  await choosePerformer(page, true);
  await expect(
    page.getByText("Review ownership change", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(0);
  expect(remote.previews).toMatchObject([
    { performer_uuid: ids.otherPerformer, performer_revision: 3 },
  ]);
  await page
    .getByRole("button", { name: "Apply ownership change", exact: true })
    .click();
  await expect(
    page.getByText("Ownership choice saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(remote.lists).toHaveLength(initialPages);
  await expect(
    page.getByRole("link", { name: "River (Photographer) (#11)", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Back to accounts", exact: true })
    .click();
  await expect(
    page.getByText("No accounts on this page", { exact: true }),
  ).toBeVisible();
  expect(remote.lists).toHaveLength(initialPages);
  await page.getByRole("button", { name: "Linked", exact: true }).click();
  await page
    .getByRole("button", { name: "Find accounts", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Change link", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(0);
});

test("retains explicit unlinking without assigning depicted performers", async ({
  page,
}) => {
  const remote = await archive(page, { linked: true });
  await page.goto(`/account-review?ownership=all&account=${ids.account}`);
  await page.getByRole("button", { name: "Unlink", exact: true }).click();
  await page
    .getByRole("button", { name: "Preview ownership", exact: true })
    .click();
  await expect(
    page.getByText(
      "This records account ownership. Depicted performers on scenes and images are unchanged.",
      { exact: true },
    ),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(0);
  await page
    .getByRole("button", { name: "Apply ownership change", exact: true })
    .click();
  await expect(
    page.getByText("Ownership choice saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes[0]).toMatchObject({ state: "unlinked" });
  expect(remote.writes[0]).not.toHaveProperty("performer_uuid");
  await page
    .getByRole("button", { name: "Back to accounts", exact: true })
    .click();
  await expect(
    page.getByText("Explicitly unlinked", { exact: true }),
  ).toBeVisible();
});

test.describe("lost response recovery", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource", "Load failed"],
  });
  test("recovers a lost applied response after a reload without another link write", async ({
    page,
  }) => {
    test.info().annotations.push({
      type: "expected-network-failure",
      description:
        "The simulated server commits then drops its apply response.",
    });
    const remote = await archive(page, { loseResponse: true });
    await page.goto(`/account-review?account=${ids.account}`);
    await choosePerformer(page);
    await page
      .getByRole("button", { name: "Apply ownership change", exact: true })
      .click();
    await expect(
      page.getByText("An ownership change needs confirmation", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("button", { name: "Preview ownership", exact: true }),
    ).toBeDisabled();
    await page
      .getByRole("button", {
        name: "Check and retry saved change",
        exact: true,
      })
      .click();
    await expect(
      page.getByText("Ownership choice saved", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(1);
  });
});

test("refreshes rejected previews before allowing a new ownership decision", async ({
  page,
}) => {
  const remote = await archive(page, { stale: true });
  await page.goto(`/account-review?account=${ids.account}`);
  await choosePerformer(page);
  await page
    .getByRole("button", { name: "Apply ownership change", exact: true })
    .click();
  await page.getByRole("button", { name: "Review again", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Link performer", exact: true }),
  ).toBeEnabled();
  await choosePerformer(page, true);
  await page
    .getByRole("button", { name: "Apply ownership change", exact: true })
    .click();
  await expect(
    page.getByText("Ownership choice saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(2);
  expect(remote.writes[1]?.request_uuid).not.toBe(
    remote.writes[0]?.request_uuid,
  );
  expect(remote.writes[1]).toMatchObject({
    account_revision: 3,
    performer_uuid: ids.otherPerformer,
  });
});

test("can reload an account that changed before preview without writing", async ({
  page,
}) => {
  const remote = await archive(page, { stalePreview: true });
  await page.goto(`/account-review?account=${ids.account}`);
  await choosePerformer(page);
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await choosePerformer(page);
  await expect(
    page.getByText("Review ownership change", { exact: true }),
  ).toBeVisible();
  expect(remote.previews[1]).toMatchObject({ account_revision: 3 });
  expect(remote.writes).toHaveLength(0);
});

test("loads identifiers and evidence lazily and offers account review in the mobile drawer", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto(`/account-review?account=${ids.account}`);
  await expect(page.getByText("Current owner", { exact: true })).toBeVisible();
  expect(
    remote.requests.some((url) =>
      /\/(identifiers|evidence|ownership-history)$/.test(url.pathname),
    ),
  ).toBe(false);
  await page
    .getByRole("button", {
      name: "Account identifiers and evidence",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("native:reddit · handle · river", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("native:reddit · id · t2_river", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Show evidence", exact: true })
    .first()
    .click();
  await expect(
    page.getByText('"source_url": "https://www.reddit.com/user/river/"', {
      exact: false,
    }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Ownership history", exact: true })
    .click();
  await expect(
    page.getByText("No ownership choices recorded.", { exact: true }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: test.info().outputPath("account-review-mobile.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Open navigation menu", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("link", { name: "Account review", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toBeVisible();
});

test("uses desktop navigation and lays out the selected review without horizontal overflow", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await archive(page, { linked: true });
  await page.goto("/account-review?ownership=all");
  await page.getByRole("button", { name: "More options", exact: true }).click();
  await page
    .getByRole("menuitem", { name: "Account review", exact: true })
    .click();
  await page.getByRole("button", { name: "All", exact: true }).click();
  await page
    .getByRole("button", { name: "Find accounts", exact: true })
    .click();
  await page.getByRole("button", { name: "Change link", exact: true }).click();
  await choosePerformer(page, true);
  await expect(
    page.getByText("Review ownership change", { exact: true }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: test.info().outputPath("account-review-desktop.png"),
    fullPage: true,
  });
});

test("preserves confirmed success when refreshing the account fails", async ({
  page,
}) => {
  const remote = await archive(page, { accountFailsAfterCommit: true });
  await page.goto(`/account-review?account=${ids.account}`);
  await choosePerformer(page);
  await page
    .getByRole("button", { name: "Apply ownership change", exact: true })
    .click();
  await expect(
    page.getByText(
      "The choice is saved, but this view could not be refreshed",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Preview ownership", exact: true }),
  ).toBeDisabled();
  remote.restoreAccountRead();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "River (Model) (#10)", exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
});

test("retries a failed account page and advances with its original keyset cursor", async ({
  page,
}) => {
  const remote = await archive(page, { firstPageFails: true, many: true });
  await page.goto("/account-review");
  await expect(
    page.getByRole("button", { name: "Retry", exact: true }),
  ).toBeVisible();
  remote.restoreAccountPage();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(25);
  await page
    .getByRole("button", { name: "Next accounts", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(1);
  expect(remote.lists.at(-1)?.searchParams.get("after")).toBe(
    "40000000-0000-4000-8000-000000000025",
  );
  await page
    .getByRole("button", { name: "Previous accounts", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(25);
  expect(remote.lists.at(-1)?.searchParams.has("after")).toBe(false);
});

test("preserves the account queue position when returning from a selected review", async ({
  page,
}) => {
  const remote = await archive(page, { many: true });
  await page.goto("/account-review");
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(25);
  const initialPages = remote.lists.length;
  const scroller = page.locator(
    '[data-scroll-restoration-id="account-review"]',
  );
  const choose = page
    .getByRole("button", { name: "Review account", exact: true })
    .nth(15);
  await choose.scrollIntoViewIfNeeded();
  const offset = await scroller.evaluate((element) => element.scrollTop);
  expect(offset).toBeGreaterThan(100);
  await choose.click();
  await expect(page.getByText("Current owner", { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: "Back to accounts", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Review account", exact: true }),
  ).toHaveCount(25);
  await expect
    .poll(async () =>
      Math.abs(
        (await scroller.evaluate((element) => element.scrollTop)) - offset,
      ),
    )
    .toBeLessThan(2);
  expect(remote.lists).toHaveLength(initialPages);
});
