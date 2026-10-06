import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  collection,
  collectionID,
  mediaRoot,
  rootID,
} from "../fixtures/collections";
import { metadataPolicy } from "../fixtures/metadata-policy";
import {
  completedManual,
  manualDirectory,
  manualID,
  manualPreview,
  manualStatus,
} from "../fixtures/manual-intake";
import type { ManualFileStatus } from "../../src/core/native-archive/manual-intake-api";

test.use({
  expectedConsoleErrors: [
    "the server responded with a status of 404",
    "the server responded with a status of 409",
    "the server responded with a status of 503",
  ],
});
async function archive(
  page: Page,
  options: { lose?: boolean; stale?: boolean; unavailable?: boolean } = {},
) {
  const writes: string[] = [],
    statuses = new Map<string, ManualFileStatus>();
  const directories: string[] = [];
  let lose = !!options.lose,
    loseCancel = false,
    serial = 20;
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url()),
      path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    if (path === "source-accounts") return route.fulfill({ json: [] });
    if (path === "media-roots") return route.fulfill({ json: [mediaRoot()] });
    if (path === "collections") return route.fulfill({ json: [collection()] });
    if (path === `collections/${collectionID}`)
      return route.fulfill({ json: collection() });
    if (path === `media-roots/${rootID}`)
      return route.fulfill({ json: mediaRoot() });
    if (path === `collections/${collectionID}/metadata-policy`)
      return route.fulfill({ json: metadataPolicy() });
    if (path === "manual-intake/capabilities")
      return route.fulfill({ json: { file_ingestion: !options.unavailable } });
    if (path === `collections/${collectionID}/intake-files`) {
      const folder = url.searchParams.get("directory")!;
      directories.push(folder);
      const result = manualDirectory();
      result.directory = folder;
      if (folder.endsWith("/Albums"))
        result.entries = [
          {
            name: "Album image.jpg",
            relative_path: `${folder}/Album image.jpg`,
            kind: "image",
            size: 12000,
            modified_at: "2026-10-06T20:00:00Z",
          },
        ];
      const q = url.searchParams.get("q") ?? "";
      result.entries = result.entries.filter((row) =>
        row.name.toLowerCase().includes(q.toLowerCase()),
      );
      return route.fulfill({ json: result });
    }
    if (path === "manual-intake/preview") {
      const input = route.request().postDataJSON();
      return route.fulfill({
        json: {
          ...manualPreview(input.relative_path.split("/").at(-1)),
          ...input,
        },
      });
    }
    if (path === "manual-intake/apply") {
      writes.push(route.request().postData()!);
      const input = route.request().postDataJSON();
      if (options.stale)
        return route.fulfill({
          status: 409,
          json: { error: "intake_preview_changed" },
        });
      const result = { ...manualStatus(input), job_uuid: manualID(++serial) };
      statuses.set(input.request_uuid, result);
      if (lose) {
        lose = false;
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      }
      return route.fulfill({ status: 202, json: result });
    }
    if (path.startsWith("manual-intake/requests/")) {
      const id = path.split("/")[2]!,
        prior = statuses.get(id);
      if (!prior)
        return route.fulfill({ status: 404, json: { error: "not_found" } });
      if (path.endsWith("/retry")) {
        writes.push(route.request().postData()!);
        const input = route.request().postDataJSON();
        if (
          prior.revision !== input.expected_revision ||
          !["failed", "cancelled"].includes(prior.state)
        )
          return route.fulfill({
            status: 409,
            json: { error: "intake_request_changed" },
          });
        const result: ManualFileStatus = {
          ...prior,
          request_uuid: input.request_uuid,
          job_uuid: manualID(++serial),
          state: "queued",
          revision: 1,
          attempts: 0,
          media_ingested: false,
          resume_from_job_uuid: prior.job_uuid,
          resume_from_revision: prior.revision,
        };
        statuses.set(input.request_uuid, result);
        if (lose) {
          lose = false;
          return route.fulfill({ status: 503, json: { error: "unavailable" } });
        }
        return route.fulfill({ status: 202, json: result });
      }
      if (path.endsWith("/cancel")) {
        writes.push(route.request().postData()!);
        const input = route.request().postDataJSON();
        if (input.expected_revision !== prior.revision)
          return route.fulfill({
            status: 409,
            json: { error: "intake_request_changed" },
          });
        const result = {
          ...prior,
          state: "cancelled" as const,
          revision: prior.revision + 1,
        };
        statuses.set(id, result);
        if (loseCancel) {
          loseCancel = false;
          return route.fulfill({ status: 503, json: { error: "unavailable" } });
        }
        return route.fulfill({ json: result });
      }
      return route.fulfill({ json: prior });
    }
    throw new Error(`Unexpected native request: ${path}`);
  });
  return {
    writes,
    statuses,
    directories,
    loseNext: () => {
      lose = true;
    },
    loseCancel: () => {
      loseCancel = true;
    },
  };
}
async function open(page: Page) {
  // Finish the collection form's independent option reads before a test can
  // reload. WebKit otherwise reports intentionally abandoned responses as CORS
  // errors while tearing down the previous document.
  const options = Promise.all(
    ["source-accounts", "media-roots"].map((name) =>
      page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname.endsWith(`/archive/${name}`) &&
          response.status() === 200,
      ),
    ),
  );
  await page.goto(`/collections?collection=${collectionID}`);
  await Promise.all((await options).map((response) => response.finished()));
  await page
    .getByRole("button", { name: "Choose files or check imports", exact: true })
    .click();
}
async function preview(page: Page, names = ["Movie.mp4"]) {
  for (const name of names)
    await page
      .getByRole("checkbox", { name: new RegExp(name.replace(".", "\\.")) })
      .check();
  await page
    .getByRole("button", { name: "Review selected files", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Import selected files", exact: true }),
  ).toBeVisible();
}
for (const width of [390, 1280]) {
  test(`reviews and imports a mixed local batch at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const remote = await archive(page);
    await open(page);
    await preview(page, ["Movie.mp4", "Photo.jpg"]);
    expect(remote.writes).toHaveLength(0);
    await expect(
      page.getByText("This batch uses the collection’s saved metadata rules.", {
        exact: false,
      }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Import selected files", exact: true })
      .click();
    await expect(page.getByText("Queued", { exact: true })).toHaveCount(2);
    expect(remote.writes).toHaveLength(2);
    for (const [id, status] of remote.statuses)
      remote.statuses.set(id, completedManual(status));
    await page
      .getByRole("button", { name: "Refresh import status", exact: true })
      .click();
    await expect(page.getByText("Imported", { exact: true })).toHaveCount(2);
    await expect(
      page.getByRole("button", { name: "Start another batch", exact: true }),
    ).toBeEnabled();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: `test-results/manual-intake-${width}.png`,
      fullPage: true,
    });
  });
  test(`recovers an accepted import after a lost response and reload at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const remote = await archive(page, { lose: true });
    await open(page);
    await preview(page);
    await page
      .getByRole("button", { name: "Import selected files", exact: true })
      .click();
    await expect(
      page.getByText("Import unconfirmed", { exact: true }),
    ).toBeVisible();
    await expect.poll(() => remote.writes.length).toBe(1);
    await expect(
      page.getByRole("button", { name: "Continue saved imports", exact: true }),
    ).toBeEnabled();
    await open(page);
    await expect(
      page.getByText("Import unconfirmed", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Refresh import status", exact: true })
      .click();
    await expect(page.getByText("Queued", { exact: true })).toBeVisible();
    await expect.poll(() => remote.writes.length).toBe(1);
    await expect(
      page.getByRole("button", { name: "Start another batch", exact: true }),
    ).toHaveCount(0);
  });
  test(`retains a committed item after interrupted cancellation at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const remote = await archive(page);
    await open(page);
    await preview(page);
    await page
      .getByRole("button", { name: "Import selected files", exact: true })
      .click();
    await expect(page.getByText("Queued", { exact: true })).toBeVisible();
    for (const [id, status] of remote.statuses)
      remote.statuses.set(id, {
        ...completedManual(status),
        state: "queued",
        media_ingested: false,
      });
    await page
      .getByRole("button", { name: "Refresh import status", exact: true })
      .click();
    await expect(
      page.getByText("Media saved; follow-up work unfinished", { exact: true }),
    ).toBeVisible();
    remote.loseCancel();
    await page
      .getByRole("button", { name: "Cancel remaining work", exact: true })
      .click();
    await expect(
      page.getByText("Cancellation unconfirmed", { exact: true }),
    ).toBeVisible();
    await open(page);
    await page
      .getByRole("button", { name: "Refresh import status", exact: true })
      .click();
    await expect(page.getByText("Cancelled", { exact: true })).toBeVisible();
    await expect(
      page.getByText("Media saved; follow-up work unfinished", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(2);
  });
  test(`keeps selection while browsing folders and handles a stale import at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const remote = await archive(page, { stale: true });
    await open(page);
    await page.getByRole("checkbox", { name: /Movie\.mp4/ }).check();
    await page.getByRole("button", { name: "Albums", exact: true }).click();
    await page.getByRole("checkbox", { name: /Album image\.jpg/ }).check();
    await expect(
      page.getByText("2 of 25 files selected", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Review selected files", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Import selected files", exact: true })
      .click();
    await expect(
      page.getByText("Needs a new review", { exact: true }),
    ).toHaveCount(2);
    await page
      .getByRole("button", { name: "Start another batch", exact: true })
      .click();
    await expect(
      page.getByText("0 of 25 files selected", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(2);
  });
  test(`retries unfinished work without losing its saved media at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const remote = await archive(page);
    await open(page);
    await preview(page);
    await page
      .getByRole("button", { name: "Import selected files", exact: true })
      .click();
    await expect(page.getByText("Queued", { exact: true })).toBeVisible();
    for (const [id, status] of remote.statuses)
      remote.statuses.set(id, {
        ...completedManual(status),
        state: "failed",
        media_ingested: false,
      });
    await page
      .getByRole("button", { name: "Refresh import status", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Retry remaining work", exact: true }),
    ).toBeVisible();
    remote.loseNext();
    await page
      .getByRole("button", { name: "Retry remaining work", exact: true })
      .click();
    await expect(
      page.getByText("Retry unconfirmed", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Media saved; follow-up work unfinished", { exact: true }),
    ).toBeVisible();
    await open(page);
    await page
      .getByRole("button", { name: "Continue saved imports", exact: true })
      .click();
    await expect(page.getByText("Queued", { exact: true })).toBeVisible();
    expect(remote.writes).toHaveLength(2);
    expect(remote.statuses.size).toBe(2);
    expect(
      [...remote.statuses.values()].filter((value) => value.state === "failed"),
    ).toHaveLength(1);
  });
}
