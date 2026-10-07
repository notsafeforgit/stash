import { test, expect, type Page } from "@playwright/test";
import type {
  OwnershipApply,
  OwnershipReceipt,
} from "../../src/core/native-archive/account-review-api";
import { ids, preview, receipt, account } from "../fixtures/account-review";

function saved(key = ids.account, state: "pending" | "rejected" = "pending") {
  const value = preview();
  return {
    account_uuid: key,
    body: JSON.stringify({
      ...value.input,
      account_uuid: key,
      digest: value.digest,
      request_uuid: ids.request,
    }),
    state,
  };
}

async function seed(page: Page, records: { key: string; value: unknown }[]) {
  await page.goto("/saved-actions");
  await expect(page.getByText("No saved actions on this page.")).toBeVisible();
  await page.evaluate(async (records) => {
    const endpoint = new URL("/api/v3/archive/", location.href).toString();
    await new Promise<void>((resolve, reject) => {
      const open = indexedDB.open(`stash-account-review:v1:${endpoint}`, 1);
      open.onupgradeneeded = () => open.result.createObjectStore("requests");
      open.onerror = () => reject(open.error);
      open.onsuccess = () => {
        const db = open.result;
        const tx = db.transaction("requests", "readwrite");
        for (const record of records)
          tx.objectStore("requests").put(record.value, record.key);
        tx.oncomplete = () => {
          db.close();
          resolve();
        };
        tx.onerror = () => {
          db.close();
          reject(tx.error);
        };
      };
    });
  }, records);
  await page.reload();
}

async function server(page: Page) {
  let committed: OwnershipReceipt | null = null;
  const control = { loseReply: false };
  const requests: string[] = [];
  const writes: string[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const request = route.request();
    requests.push(request.url());
    const path = new URL(request.url()).pathname;
    if (path.includes("/requests/"))
      return route.fulfill({
        status: committed ? 200 : 404,
        json: committed ?? { error: "not_found" },
      });
    if (request.method() !== "GET") {
      writes.push(request.postData() ?? "");
      if (!path.endsWith("/apply"))
        throw new Error(`Unexpected mutation: ${path}`);
      committed = receipt(request.postDataJSON() as OwnershipApply);
      if (control.loseReply) {
        control.loseReply = false;
        return route.abort("failed");
      }
      return route.fulfill({ json: { review: committed, replayed: false } });
    }
    if (path.endsWith(`/source-accounts/${ids.account}`))
      return route.fulfill({ json: account() });
    return route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  return { control, requests, writes };
}

for (const viewport of [
  { name: "desktop", width: 1280, height: 900 },
  { name: "mobile", width: 390, height: 844 },
]) {
  test(`discovers local actions and uses shared navigation on ${viewport.name}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport);
    const remote = await server(page);
    await seed(page, [{ key: ids.account, value: saved() }]);
    await expect(
      page.getByText("Awaiting confirmation", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText(ids.account, { exact: true })).toHaveCount(0);
    await page
      .getByRole("button", { name: "Action details", exact: true })
      .click();
    await expect(page.getByText(ids.account, { exact: true })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({ path: testInfo.outputPath("saved-actions.png") });
    if (viewport.name === "desktop") {
      await page
        .getByRole("button", { name: "More options", exact: true })
        .click();
      await page
        .getByRole("menuitem", { name: "Saved actions", exact: true })
        .click();
      await expect(page.getByRole("menu")).toHaveCount(0);
    } else {
      await page
        .getByRole("button", { name: "Open navigation menu", exact: true })
        .tap();
      const drawer = page.locator("[data-mobile-navigation]");
      await drawer
        .getByRole("link", { name: "Saved actions", exact: true })
        .tap();
      await expect(drawer).toHaveCount(0);
    }
    await expect(
      page.getByRole("button", { name: "Resume saved action", exact: true }),
    ).toBeVisible();
    expect(remote.requests).toEqual([]);
    expect(remote.writes).toEqual([]);
  });
}

test("recovers a lost apply reply after reload without applying twice", async ({
  page,
}) => {
  const remote = await server(page);
  const original = saved();
  await seed(page, [{ key: ids.account, value: original }]);
  remote.control.loseReply = true;
  await page
    .getByRole("button", { name: "Resume saved action", exact: true })
    .click();
  await expect(page.getByText("Could not complete this step")).toBeVisible();
  expect(remote.writes).toEqual([original.body]);
  await page.reload();
  await expect(
    page.getByText("Awaiting confirmation", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Resume saved action", exact: true })
    .click();
  await expect(
    page.getByText("Saved action checked", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("No saved actions on this page.")).toBeVisible();
  expect(remote.writes).toEqual([original.body]);
});

test("keeps rejected actions and opens their original review without resending", async ({
  page,
}) => {
  const remote = await server(page);
  await seed(page, [
    { key: ids.account, value: saved(ids.account, "rejected") },
  ]);
  await expect(
    page.getByText("Needs a new review", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Resume saved action", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Open original review", exact: true })
    .click();
  await expect(page).toHaveURL(/account-review.*account=/);
  expect(new URL(page.url()).searchParams.get("account")).toBe(ids.account);
  expect(remote.writes).toEqual([]);
});

test("retains successful results when browser storage fails during refresh", async ({
  page,
}) => {
  const remote = await server(page);
  await seed(page, [{ key: ids.account, value: saved() }]);
  await expect(
    page.getByText("Awaiting confirmation", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    const original = IDBFactory.prototype.open;
    IDBFactory.prototype.open = () => {
      IDBFactory.prototype.open = original;
      throw new DOMException("Fixture storage unavailable", "UnknownError");
    };
  });
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByText("Could not complete this step")).toBeVisible();
  await expect(
    page.getByText("Awaiting confirmation", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByText("Could not complete this step")).toHaveCount(0);
  await expect(
    page.getByText("Awaiting confirmation", { exact: true }),
  ).toBeVisible();
  expect(remote.requests).toEqual([]);
});

test("pages local records and isolates action families", async ({ page }) => {
  const remote = await server(page);
  const records = Array.from({ length: 26 }, (_, i) => {
    const key = `10000000-0000-4000-8000-${String(i + 1).padStart(12, "0")}`;
    return { key, value: saved(key) };
  });
  await seed(page, records);
  await expect(
    page.getByRole("button", { name: "Resume saved action", exact: true }),
  ).toHaveCount(25);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Resume saved action", exact: true }),
  ).toHaveCount(1);
  await expect(
    page.getByRole("button", { name: "Next", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Resume saved action", exact: true }),
  ).toHaveCount(25);
  await page
    .getByRole("combobox", { name: "Action type", exact: true })
    .click();
  await page
    .getByRole("option", { name: "Account merges", exact: true })
    .click();
  await expect(page.getByText("No saved actions on this page.")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Resume saved action", exact: true }),
  ).toHaveCount(0);
  expect(remote.requests).toEqual([]);
});

test("shows damaged records without a recovery or erase action", async ({
  page,
}) => {
  const remote = await server(page);
  await seed(page, [
    { key: ids.account, value: { original: "retained damaged record" } },
  ]);
  await expect(
    page.getByText("Cannot read saved action", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Resume saved action", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Open original review", exact: true }),
  ).toBeDisabled();
  await page.reload();
  await expect(
    page.getByText("Cannot read saved action", { exact: true }),
  ).toBeVisible();
  expect(remote.requests).toEqual([]);
});
