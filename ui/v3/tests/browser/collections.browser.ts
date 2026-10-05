import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  collection,
  collectionInput,
  collectionID,
  mediaRoot,
  rootID,
} from "../fixtures/collections";
import { account, ids } from "../fixtures/account-review";
import type {
  CollectionInput,
  Collection,
  CollectionRevision,
} from "../../src/core/native-archive/collection-api";

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
    loseResponse?: boolean;
    stale?: boolean;
    many?: boolean;
    refreshFails?: boolean;
  } = {},
) {
  const rows = new Map<string, Collection>([[collectionID, collection()]]);
  const history = new Map<string, CollectionRevision[]>([
    [
      collectionID,
      [
        {
          ...collection(),
          origin: "review",
          reason: "Initial folder",
          recorded_at: "2026-10-05T12:00:00Z",
        },
      ],
    ],
  ]);
  const writes: CollectionInput[] = [];
  const requests: URL[] = [];
  let loseResponse = options.loseResponse;
  let stale = options.stale;
  let refreshFails = false;
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    const path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    if (path === "collections") {
      let found = [...rows.values()];
      if (options.many && !url.searchParams.has("after"))
        found = Array.from({ length: 25 }, (_, index) => ({
          ...collection(),
          uuid: `60000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
          label: `Folder ${index + 1}`,
        }));
      const q = url.searchParams.get("q");
      const state = url.searchParams.get("state");
      const kind = url.searchParams.get("kind");
      return route.fulfill({
        json: found.filter(
          (row) =>
            (!q || row.label.includes(q)) &&
            (!state || row.state === state) &&
            (!kind || row.kind === kind),
        ),
      });
    }
    if (path === "media-roots") return route.fulfill({ json: [mediaRoot()] });
    if (path === `media-roots/${rootID}`)
      return route.fulfill({ json: mediaRoot() });
    if (path === "source-accounts") return route.fulfill({ json: [account()] });
    if (path === `source-accounts/${ids.account}`)
      return route.fulfill({ json: account() });
    const [, id, tail] = path.split("/");
    if (path.startsWith("collections/") && id) {
      if (route.request().method() === "PUT") {
        const input: CollectionInput = route.request().postDataJSON();
        writes.push(input);
        if (stale) {
          stale = false;
          const changed = {
            ...collectionInput(),
            expected_revision: 1,
            label: "Changed elsewhere",
            reason: "Concurrent change",
          };
          const result = collection(changed);
          rows.set(id, result);
          history.get(id)?.push({
            ...result,
            origin: "review",
            reason: changed.reason,
            recorded_at: "2026-10-05T13:00:00Z",
          });
          return route.fulfill({
            status: 409,
            json: { error: "preview_changed" },
          });
        }
        const result = collection(input);
        refreshFails = !!options.refreshFails;
        rows.set(id, result);
        history.set(id, [
          ...(history.get(id) ?? []),
          {
            ...result,
            origin: "review",
            reason: input.reason,
            recorded_at: "2026-10-05T13:00:00Z",
          },
        ]);
        if (loseResponse) {
          loseResponse = false;
          return route.fulfill({ status: 503, json: { error: "unavailable" } });
        }
        return route.fulfill({ json: result });
      }
      const current = rows.get(id);
      if (!current)
        return route.fulfill({ status: 404, json: { error: "not_found" } });
      if (tail === "history")
        return route.fulfill({
          json: (history.get(id) ?? [])
            .filter(
              (row) => row.revision > Number(url.searchParams.get("after")),
            )
            .slice(0, Number(url.searchParams.get("limit"))),
        });
      if (refreshFails) {
        refreshFails = false;
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      }
      return route.fulfill({ json: current });
    }
    throw new Error(`Unexpected collection request ${path}`);
  });
  return { writes, requests, rows };
}

test("searches bounded pages and exposes collections in the mobile navigation drawer", async ({
  page,
}) => {
  const remote = await archive(page, { many: true });
  await page.goto("/collections");
  await expect(page.getByText("Folder 1", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByText("Purchased videos", { exact: true }),
  ).toBeVisible();
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/collections"))
      .at(-1)
      ?.searchParams.get("after"),
  ).toBe("60000000-0000-4000-8000-000000000025");
  await page
    .getByLabel("Search names, source URLs or folders")
    .fill("Folder 2");
  await page
    .getByRole("button", { name: "Find collections", exact: true })
    .click();
  await expect(page.getByText("Folder 2", { exact: true })).toBeVisible();
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/collections"))
      .at(-1)
      ?.searchParams.get("limit"),
  ).toBe("25");
  await page.screenshot({
    path: test.info().outputPath("collections-mobile.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Open navigation menu", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("link", { name: "Source collections", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "Source collections", exact: true }),
  ).toBeVisible();
});

test("creates a collection with a selected publisher and folder without assigning performers", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto("/collections");
  await page
    .getByRole("button", { name: "New collection", exact: true })
    .click();
  await page.getByLabel("Collection name", { exact: true }).fill("River feed");
  await page
    .getByLabel("Source URL", { exact: true })
    .fill("https://www.reddit.com/user/river/");
  await page
    .getByRole("combobox", { name: "Source account", exact: true })
    .fill("river");
  await page
    .getByRole("option", { name: "river · Reddit t2_river", exact: true })
    .click();
  await expect(page.getByLabel("Source service", { exact: true })).toHaveValue(
    "native:reddit",
  );
  const rootInput = page.getByRole("combobox", {
    name: "Media root",
    exact: true,
  });
  // A user scrolls/taps the next field before typing; WebKit's programmatic
  // fill alone can focus an off-screen input without bringing its popup into view.
  await rootInput.click();
  await rootInput.fill("Media");
  await page
    .getByRole("option", {
      name: "Media library · /media/library",
      exact: true,
    })
    .click();
  await page
    .getByLabel("Folder within the media root", { exact: true })
    .fill("../outside");
  await expect(
    page.getByRole("button", { name: "Save collection", exact: true }),
  ).toBeDisabled();
  await page
    .getByLabel("Folder within the media root", { exact: true })
    .fill("River, reddit");
  await page
    .getByRole("button", { name: "Save collection", exact: true })
    .click();
  await expect(
    page.getByText("Collection saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(remote.writes[0]).toMatchObject({
    expected_revision: 0,
    label: "River feed",
    account_uuid: ids.account,
    root_uuid: rootID,
    path_prefix: "River, reddit",
    namespace: "native:reddit",
  });
  expect(remote.writes[0]).not.toHaveProperty("performer_uuid");
  expect(
    remote.requests.some((url) => url.pathname.includes("account-ownership")),
  ).toBe(false);
});

test("recovers a committed save after reload without another PUT and loads history only on request", async ({
  page,
}) => {
  const remote = await archive(page, { loseResponse: true });
  await page.goto(`/collections?collection=${collectionID}`);
  await expect(page.getByLabel("Collection name", { exact: true })).toHaveValue(
    "Purchased videos",
  );
  expect(remote.requests.some((url) => url.pathname.endsWith("/history"))).toBe(
    false,
  );
  await page
    .getByLabel("Collection name", { exact: true })
    .fill("Renamed folder");
  await page
    .getByRole("button", { name: "Save collection", exact: true })
    .click();
  await expect(
    page.getByText("Confirm this saved change", { exact: true }),
  ).toBeVisible();
  await expect.poll(() => remote.writes.length).toBe(1);
  await expect(
    page.getByText("Could not complete this step", { exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("button", {
      name: "Check and retry saved change",
      exact: true,
    }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "Check and retry saved change", exact: true })
    .click();
  await expect(
    page.getByText("Collection saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  await page
    .getByRole("button", { name: "Collection history", exact: true })
    .click();
  await expect(page.getByText("Revision 2", { exact: false })).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.screenshot({
    path: test.info().outputPath("collection-editor-desktop.png"),
    fullPage: true,
  });
});

test("requires review of a concurrent edit before saving another revision", async ({
  page,
}) => {
  const remote = await archive(page, { stale: true });
  await page.goto(`/collections?collection=${collectionID}`);
  await page.getByLabel("Collection name", { exact: true }).fill("My change");
  await page
    .getByRole("button", { name: "Save collection", exact: true })
    .click();
  await expect(
    page.getByText("Review the current collection before saving again", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Save collection", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Review current collection", exact: true })
    .click();
  await expect(page.getByLabel("Collection name", { exact: true })).toHaveValue(
    "Changed elsewhere",
  );
  expect(remote.writes).toHaveLength(1);
});

test("coordinates pending saves across tabs and isolates deployments on the same origin", async ({
  page,
  context,
}) => {
  await archive(page);
  await page.goto("/collections");
  const other = await context.newPage();
  try {
    await archive(other);
    await other.goto("/collections");
    const [one, same] = await Promise.all([
      page.evaluate(() =>
        window.collectionStorage.prepare("/one/", "First library"),
      ),
      other.evaluate(() =>
        window.collectionStorage.prepare("/one/", "First library"),
      ),
    ]);
    expect(one).toEqual(same);
    expect(
      await other.evaluate(() => window.collectionStorage.read("/two/")),
    ).toBeNull();
    const separate = await other.evaluate(() =>
      window.collectionStorage.prepare("/two/", "Second library"),
    );
    await expect(
      other.evaluate(() =>
        window.collectionStorage.prepare("/one/", "Conflicting edit"),
      ),
    ).rejects.toThrow("pending_review");
    await page.reload();
    expect(
      await page.evaluate(() => window.collectionStorage.read("/one/")),
    ).toEqual(one);
    expect(
      await page.evaluate(() => window.collectionStorage.read("/two/")),
    ).toEqual(separate);
  } finally {
    await other.close();
  }
});

test("refreshes only the changed card and removes it when its new name no longer matches", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto("/collections?q=Purchased%20videos");
  await page
    .getByRole("button", { name: "Manage collection", exact: true })
    .click();
  const before = remote.requests.filter((url) =>
    url.pathname.endsWith("/collections"),
  ).length;
  await page
    .getByLabel("Collection name", { exact: true })
    .fill("Other folder");
  await page
    .getByRole("button", { name: "Save collection", exact: true })
    .click();
  await expect(
    page.getByText("Collection saved", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Back to collections", exact: true })
    .click();
  await expect(
    page.getByText("No matching collections", { exact: true }),
  ).toBeVisible();
  expect(
    remote.requests.filter((url) => url.pathname.endsWith("/collections")),
  ).toHaveLength(before);
});

test("retains confirmation when refreshing a saved collection fails", async ({
  page,
}) => {
  const remote = await archive(page, { refreshFails: true });
  await page.goto(`/collections?collection=${collectionID}`);
  await page
    .getByLabel("Collection name", { exact: true })
    .fill("Saved successfully");
  await page
    .getByRole("button", { name: "Save collection", exact: true })
    .click();
  await expect(
    page.getByText(
      "The collection is saved, but this view could not be refreshed",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByText("Collection saved", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Confirm this saved change", { exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByLabel("Collection name", { exact: true })).toHaveValue(
    "Saved successfully",
  );
  await page
    .getByRole("button", { name: "Back to collections", exact: true })
    .click();
  await expect(
    page.getByText("Saved successfully", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
});
