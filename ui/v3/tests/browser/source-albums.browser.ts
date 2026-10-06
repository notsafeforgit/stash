import { emptyDownloadStatus } from "../fixtures/downloads";
import type { Page } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { albumPage, albumSlot, albumUUID } from "../fixtures/source-albums";
import { postAlbum, postIds, postSummary } from "../fixtures/source-posts";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function archive(
  page: Page,
  options: {
    empty?: boolean;
    manySlots?: boolean;
    changed?: boolean;
    disabled?: boolean;
    failed?: boolean;
    manyPosts?: boolean;
  } = {},
) {
  const requests: URL[] = [];
  const writes: string[] = [];
  let revision = "a";
  let failed = options.failed;
  await page.route("**/api/v3/archive/**", async (route) => {
    if (
      new URL(route.request().url()).pathname.endsWith(
        "/attachments/download-status",
      )
    )
      return route.fulfill({
        json: emptyDownloadStatus(route.request().postDataJSON().attachments),
      });
    const url = new URL(route.request().url());
    requests.push(url);
    if (route.request().method() !== "GET")
      writes.push(route.request().method());
    let result: unknown;
    if (url.pathname.endsWith("/entity-identities/gallery/12"))
      result = {
        uuid: postIds.gallery,
        kind: "gallery",
        local_id: 12,
        revision: 1,
      };
    else if (url.pathname.endsWith("/album-posts")) {
      if (failed) {
        await route.fulfill({ status: 503, json: { error: "unavailable" } });
        return;
      }
      const after = url.searchParams.get("after");
      result = {
        requested_uuid: postIds.gallery,
        gallery: postAlbum().gallery,
        posts: options.empty
          ? []
          : options.manyPosts
            ? Array.from({ length: after ? 1 : 25 }, (_, i) => {
                const n = after ? 26 : i + 1;
                const uuid = albumUUID(1000 + n);
                const summary = postSummary();
                return {
                  ...summary,
                  uuid,
                  latest_capture: {
                    ...summary.latest_capture,
                    title: `Post ${n}`,
                  },
                  urls: summary.urls.map((row) => ({
                    ...row,
                    post_uuid: uuid,
                  })),
                };
              })
            : [postSummary()],
      };
    } else if (url.pathname.endsWith("/album-media")) {
      const response = albumPage();
      const after = url.searchParams.get("after");
      if (options.manySlots) {
        if (after && options.changed) revision = "b";
        response.signature = revision.repeat(64);
        response.selection = {
          ...response.selection!,
          complete: true,
          expected_count: 26,
          entry_count: 26,
        };
        response.slots = Array.from({ length: after ? 1 : 25 }, (_, i) =>
          albumSlot(after ? 25 : i),
        );
        response.next_after = after ? null : 24;
      }
      if (options.disabled) {
        response.album = {
          ...postAlbum(),
          state: "disabled",
          gallery_uuid: null,
          gallery: null,
        };
        response.slots = response.slots.map((slot) => ({
          ...slot,
          gallery_membership: "no_gallery",
          registered_files: 0,
          media: slot.media
            ? { ...slot.media, state: "deleted", local_id: null }
            : null,
        }));
      }
      result = response;
    } else throw new Error(`Unexpected album request: ${url.pathname}`);
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

async function openAlbums(page: Page, desktop = false) {
  if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/source-albums");
  if (desktop)
    await page.getByRole("tab", { name: "Source albums", exact: true }).click();
  else await chooseSection(page, "Source albums");
}
async function expandPost(page: Page) {
  await page
    .getByRole("button", {
      name: postSummary().latest_capture?.title ?? "Untitled source post",
      exact: true,
    })
    .click();
}

for (const desktop of [false, true])
  test(`mixed source order on ${desktop ? "desktop" : "mobile"} retains repeats and gaps without writing`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await openAlbums(page, desktop);
    await expect(
      page.getByRole("button", {
        name: postSummary().latest_capture?.title ?? "Untitled source post",
        exact: true,
      }),
    ).toBeVisible();
    expect(
      remote.requests.some((url) => url.pathname.endsWith("/album-media")),
    ).toBe(false);
    await expandPost(page);
    await expect(page.locator("[data-source-position]")).toHaveCount(5);
    expect(
      await page
        .locator("[data-source-position]")
        .evaluateAll((rows) =>
          rows.map((row) => row.getAttribute("data-source-position")),
        ),
    ).toEqual(["0", "1", "2", "3", "6"]);
    await expect(page.getByText("First image", { exact: true })).toHaveCount(2);
    await expect(page.getByText("Second video", { exact: true })).toHaveCount(
      1,
    );
    await expect(
      page.getByText("Positions 4–6", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("No selected library item", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Open media", exact: true }),
    ).toHaveCount(3);
    await expect(
      page
        .locator('[data-source-position="1"]')
        .getByRole("link", { name: "Open media", exact: true }),
    ).toHaveAttribute("href", "/scenes/8");
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: test
        .info()
        .outputPath(`source-albums-${desktop ? "desktop" : "mobile"}.png`),
    });
    expect(remote.writes).toEqual([]);
  });

test("manual galleries remain valid without an invented source post", async ({
  page,
}) => {
  const remote = await archive(page, { empty: true });
  await openAlbums(page);
  await expect(
    page.getByText("No associated source posts", { exact: true }),
  ).toBeVisible();
  expect(
    remote.requests.some((url) => url.pathname.endsWith("/album-media")),
  ).toBe(false);
  expect(remote.writes).toEqual([]);
});

test("disabled albums retain order and deleted media have no reused local links", async ({
  page,
}) => {
  const remote = await archive(page, { disabled: true });
  await openAlbums(page, true);
  await expandPost(page);
  await expect(page.locator("[data-source-position]")).toHaveCount(5);
  await expect(
    page.getByText("Automatic album gallery disabled", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Deleted from the library", { exact: true }),
  ).toHaveCount(3);
  await expect(
    page.getByRole("link", { name: "Open media", exact: true }),
  ).toHaveCount(0);
  expect(remote.writes).toEqual([]);
});

test("album pages continue by source position", async ({ page }) => {
  const remote = await archive(page, { manySlots: true });
  await openAlbums(page, true);
  await expandPost(page);
  await expect(page.locator("[data-source-position]")).toHaveCount(25);
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(page.locator("[data-source-position]")).toHaveCount(26);
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/album-media"))
      .at(-1)
      ?.searchParams.get("after"),
  ).toBe("24");
  await expect(
    page.getByRole("button", { name: "Load more", exact: true }),
  ).toHaveCount(0);
});

test("changed source lists require reload before appending more positions", async ({
  page,
}) => {
  await archive(page, { manySlots: true, changed: true });
  await openAlbums(page, true);
  await expandPost(page);
  await expect(page.locator("[data-source-position]")).toHaveCount(25);
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(
    page.getByText("This album changed", { exact: true }),
  ).toBeVisible();
  await expect(page.locator("[data-source-position]")).toHaveCount(25);
  await page.getByRole("button", { name: "Reload album", exact: true }).click();
  await expect(
    page.getByText("This album changed", { exact: true }),
  ).toHaveCount(0);
  await expect(page.locator("[data-source-position]")).toHaveCount(25);
});

test("gallery source lookups retry and page after retained merged associations", async ({
  page,
}) => {
  const remote = await archive(page, { manyPosts: true, failed: true });
  await openAlbums(page, true);
  await expect(
    page.getByText("Could not load post data", { exact: true }),
  ).toBeVisible();
  remote.recover();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Post 25", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Post 26", exact: true }),
  ).toBeVisible();
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/album-posts"))
      .at(-1)
      ?.searchParams.get("after"),
  ).toBe(albumUUID(1025));
  expect(remote.writes).toEqual([]);
});
