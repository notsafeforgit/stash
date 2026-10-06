import type { Page } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { albumPage, albumUUID } from "../fixtures/source-albums";
import { postAlbum, postIds, postSummary } from "../fixtures/source-posts";
import {
  downloadTransfer,
  downloadTime,
  emptyDownloadStatus,
} from "../fixtures/downloads";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function archive(page: Page) {
  const batches: string[][] = [],
    history: URL[] = [],
    writes: string[] = [];
  let failed = false,
    empty = false,
    verified = false;
  await page.route("**/api/v3/archive/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname;
    if (path.endsWith("/attachments/download-status")) {
      expect(request.method()).toBe("POST");
      const attachments = request.postDataJSON().attachments as string[];
      batches.push(attachments);
      if (failed)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      const result = emptyDownloadStatus(attachments);
      if (!empty)
        result.attachments = result.attachments.map((row) => ({
          ...row,
          latest: {
            ...downloadTransfer(100, row.attachment_uuid),
            verification_state: verified ? "succeeded" : "queued",
          },
        }));
      return route.fulfill({ json: result });
    }
    if (request.method() !== "GET") writes.push(path);
    if (path.endsWith("/entity-identities/gallery/12"))
      return route.fulfill({
        json: {
          uuid: postIds.gallery,
          kind: "gallery",
          local_id: 12,
          revision: 1,
        },
      });
    if (path.endsWith("/album-posts"))
      return route.fulfill({
        json: {
          requested_uuid: postIds.gallery,
          gallery: postAlbum().gallery,
          posts: [postSummary()],
        },
      });
    if (path.endsWith("/album-media"))
      return route.fulfill({ json: albumPage() });
    if (path.endsWith("/download-transfers")) {
      history.push(url);
      const attachment = path.split("/").at(-2)!;
      const before = url.searchParams.get("before");
      const transfers = empty
        ? []
        : before
          ? [downloadTransfer(75, attachment)]
          : Array.from({ length: 25 }, (_, index) =>
              downloadTransfer(100 - index, attachment),
            );
      if (transfers[0]) {
        const transfer = transfers[0];
        delete transfer.file_event_uuid;
        delete transfer.verification_job_uuid;
        delete transfer.verification_state;
        transfer.state = "skipped";
        transfer.reported_state = "skipped";
        transfer.reason_code = "archive_entry_without_file";
      }
      return route.fulfill({
        json: {
          attachment_uuid: attachment,
          checked_at: downloadTime,
          transfers,
          next_before: !empty && !before ? 76 : null,
        },
      });
    }
    throw new Error(`Unexpected download UI request: ${path}`);
  });
  return {
    batches,
    history,
    writes,
    fail: () => {
      failed = true;
    },
    recover: () => {
      failed = false;
      verified = true;
    },
    empty: () => {
      empty = true;
    },
  };
}

async function openAlbum(page: Page, desktop = false) {
  if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/source-albums");
  if (desktop)
    await page.getByRole("tab", { name: "Source albums", exact: true }).click();
  else await chooseSection(page, "Source albums");
  await page
    .getByRole("button", {
      name: postSummary().latest_capture?.title ?? "Untitled source post",
      exact: true,
    })
    .click();
}

for (const desktop of [false, true]) {
  test(`grouped transfer history on ${desktop ? "desktop" : "mobile"} preserves library links and stable pagination`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await openAlbum(page, desktop);
    const first = page.locator('[data-source-position="0"]');
    await expect(
      first.getByText("File check queued", { exact: true }),
    ).toBeVisible();
    expect(remote.batches).toEqual([
      [albumUUID(100), albumUUID(101), albumUUID(106)],
    ]);
    expect(remote.history).toHaveLength(0);
    await expect(
      page.getByRole("link", { name: "Open media", exact: true }),
    ).toHaveCount(3);
    await expect(
      page
        .locator('[data-source-position="6"]')
        .getByRole("link", { name: "Open media", exact: true }),
    ).toHaveCount(0);
    await first
      .getByRole("button", { name: "Download history", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Download history",
      exact: true,
    });
    await expect(dialog.locator("[data-download-transfer]")).toHaveCount(25);
    await expect(
      dialog.getByText("Skipped without a file", { exact: true }),
    ).toHaveCount(1);
    await expect(
      dialog.getByText(
        "The downloader's archive marks this item as handled, but it could not confirm a local file.",
        { exact: true },
      ),
    ).toBeVisible();
    await dialog
      .locator('[data-download-transfer="100"]')
      .getByRole("button", { name: "Report references", exact: true })
      .click();
    await expect(
      dialog.getByText(downloadTransfer().capture_event_uuid, { exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: test
        .info()
        .outputPath(`download-history-${desktop ? "desktop" : "mobile"}.png`),
    });
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await dialog
      .getByRole("button", { name: "Load older transfers", exact: true })
      .click();
    await expect(dialog.locator("[data-download-transfer]")).toHaveCount(26);
    await expect(
      dialog.getByRole("button", { name: "Close", exact: true }),
    ).toBeInViewport();
    await expect(
      dialog.getByRole("heading", { name: "Download history", exact: true }),
    ).toBeInViewport();
    // Strict Mode may dispatch and abort the first page read once at mount.
    expect(remote.history[0]?.searchParams.get("before")).toBeNull();
    expect(
      remote.history.flatMap((url) => {
        const before = url.searchParams.get("before");
        return before === null ? [] : [before];
      }),
    ).toEqual(["76"]);
    await dialog
      .getByRole("button", { name: "Refresh download reports", exact: true })
      .click();
    await expect(dialog.locator("[data-download-transfer]")).toHaveCount(25);
    await dialog.getByRole("button", { name: "Close", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(remote.writes).toEqual([]);
  });
}

test("failed refresh keeps prior download evidence and retry updates only the report status", async ({
  page,
}) => {
  const remote = await archive(page);
  await openAlbum(page);
  await expect(
    page
      .locator('[data-source-position="0"]')
      .getByText("File check queued", { exact: true }),
  ).toBeVisible();
  remote.fail();
  await page
    .getByRole("button", { name: "Refresh download reports", exact: true })
    .click();
  await expect(
    page.getByText("Could not refresh download reports", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("File check queued", { exact: true }),
  ).toHaveCount(4);
  remote.recover();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByText("Could not refresh download reports", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("File check passed", { exact: true }),
  ).toHaveCount(4);
  await expect(
    page.getByRole("link", { name: "Open media", exact: true }),
  ).toHaveCount(3);
  expect(remote.writes).toEqual([]);
});

test("empty reporting history does not mean an existing file is unavailable", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.empty();
  await openAlbum(page);
  const first = page.locator('[data-source-position="0"]');
  await expect(
    first.getByText("No download reports recorded", { exact: true }),
  ).toBeVisible();
  await expect(
    first.getByRole("link", { name: "Open media", exact: true }),
  ).toHaveAttribute("href", "/images/7");
  await first
    .getByRole("button", { name: "Download history", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Download history",
    exact: true,
  });
  await expect(
    dialog.getByText("No download reports recorded", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByText(
      "Files imported or downloaded before native reporting may still exist in the library.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Load older transfers", exact: true }),
  ).toHaveCount(0);
  expect(remote.writes).toEqual([]);
});
