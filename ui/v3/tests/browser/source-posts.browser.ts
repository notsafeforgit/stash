import { emptyDownloadStatus } from "../fixtures/downloads";
import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  postSummary,
  postMedia,
  postAlbum,
  postIds,
  postAlbumContext,
} from "../fixtures/source-posts";
import { account } from "../fixtures/account-review";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function archive(
  page: Page,
  options: {
    many?: boolean;
    failMedia?: boolean;
    disabledAlbum?: boolean;
    unsafeURL?: boolean;
    merged?: boolean;
  } = {},
) {
  const requests: URL[] = [];
  const writes: string[] = [];
  let failMedia = options.failMedia;
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
    const path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    const post = postSummary();
    if (options.merged) {
      post.uuid = postIds.otherPost;
      post.requested_uuid =
        path === "posts" ? post.uuid : (path.split("/")[1] ?? postIds.post);
    }
    if (options.unsafeURL)
      post.urls[0]!.url = "javascript:alert('retained text')";
    let result: unknown;
    if (path === "posts") {
      const after = url.searchParams.get("after");
      result = options.many
        ? after
          ? []
          : Array.from({ length: 25 }, (_, index) => {
              const uuid = `20000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`;
              return {
                ...post,
                requested_uuid: uuid,
                uuid,
                urls: [],
                latest_capture: {
                  ...post.latest_capture,
                  title: `Post ${index + 1}`,
                },
              };
            })
        : url.searchParams.get("url") === "https://example.test/missing"
          ? []
          : [post];
    } else if (/^posts\/[^/]+$/.test(path)) result = post;
    else if (path.endsWith("/publishers")) result = [account()];
    else if (path.endsWith("/media")) {
      if (failMedia) {
        await route.fulfill({ status: 503, json: { error: "unavailable" } });
        return;
      }
      const row = postMedia();
      row.requested_post_uuid = post.requested_uuid;
      row.association.post_uuid = post.uuid;
      row.association.state = "unlinked";
      row.linked_attachments = 2;
      const deleted = {
        ...row,
        media: {
          ...row.media,
          uuid: postIds.otherPost,
          state: "deleted",
          local_id: null,
          title: "Retired library item",
        },
        association: {
          ...row.association,
          media_uuid: postIds.otherPost,
          media_state: "deleted",
          state: "conflict",
        },
      };
      result = [row, deleted];
    } else if (path.endsWith("/album")) {
      const album = postAlbum();
      album.post_uuid = post.uuid;
      result = postAlbumContext(
        options.disabledAlbum
          ? {
              ...album,
              state: "disabled",
              gallery: null,
              gallery_uuid: null,
              reason: "Kept separate",
            }
          : album,
        path.split("/")[1],
      );
    } else if (path.endsWith("/capture-summaries"))
      result = {
        requested_uuid: path.split("/").at(-2),
        revisions: [
          {
            uuid: postIds.revision,
            metadata: {
              title: "Full retained caption",
              original_text: "One shared description",
            },
          },
        ],
        captures: [
          {
            uuid: postIds.capture,
            post_uuid: postIds.post,
            revision_uuid: postIds.revision,
            origin: "gallery-dl",
            platform: "reddit",
            captured_at: "2026-10-01T01:00:00Z",
            recorded_at: null,
            extractor_version: "1.32.15",
          },
          {
            uuid: postIds.secondCapture,
            post_uuid: postIds.post,
            revision_uuid: postIds.revision,
            origin: "legacy-nfo",
            platform: "reddit",
            captured_at: null,
            recorded_at: "2026-10-02T01:00:00Z",
            extractor_version: null,
          },
        ],
      };
    else if (path.endsWith("/identifiers")) result = [];
    else if (path.endsWith("/urls"))
      result = { requested_uuid: path.split("/")[1], urls: [] };
    else throw new Error(`Unexpected post browser request: ${path}`);
    await route.fulfill({ json: result });
  });
  return {
    requests,
    writes,
    recoverMedia: () => {
      failMedia = false;
    },
  };
}

for (const desktop of [false, true]) {
  test(`post browse and detail on ${desktop ? "desktop" : "mobile"} expands only requested shared data`, async ({
    page,
  }) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const remote = await archive(page);
    await page.goto("/source-posts");
    await expect(
      page.getByRole("heading", { name: "Source posts", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Open post", exact: true }).click();
    const back = page.getByRole("button", {
      name: "Back to posts",
      exact: true,
    });
    await expect(back).toBeVisible();
    expect(
      remote.requests.some((u) => u.pathname.endsWith("/capture-summaries")),
    ).toBe(false);
    expect(
      remote.requests.some((u) => u.pathname.endsWith("/publishers")),
    ).toBe(false);
    await page
      .getByRole("button", { name: "Post text and captures", exact: true })
      .click();
    await expect(
      page.getByText("One shared description", { exact: true }),
    ).toHaveCount(1);
    await page
      .getByRole("button", { name: "2 loaded captures", exact: true })
      .click();
    await expect(
      page.getByText(/Observation time unknown · stored/),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Publisher accounts", exact: true })
      .click();
    await expect(page.getByText("river", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Review account", exact: true }),
    ).toHaveAttribute("href", /\/account-review\?.*account=/);
    await page
      .getByRole("button", { name: "Media associations", exact: true })
      .click();
    await expect(
      page.getByText("Explicitly unlinked", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Conflicting link choices", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Open Sources", exact: true }),
    ).toHaveCount(1);
    await expect(
      page.getByRole("link", { name: "Open Sources", exact: true }),
    ).toHaveAttribute("href", /\/scenes\/7\?tab=source-review/);
    await page
      .getByRole("button", { name: "Album gallery", exact: true })
      .click();
    await expect(
      page.getByRole("link", { name: "Open gallery", exact: true }),
    ).toHaveAttribute("href", "/galleries/12");
    await page
      .getByRole("button", { name: "Identifiers", exact: true })
      .click();
    await expect(page.getByText(postIds.post, { exact: true })).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(desktop ? 1280 : 390);
    const scroller = page.locator(
      '[data-scroll-restoration-id="source-posts"]',
    );
    expect(
      await scroller.evaluate(
        (element) => element.scrollWidth > element.clientWidth,
      ),
    ).toBe(false);
    await page.screenshot({
      path: test
        .info()
        .outputPath(`post-detail-${desktop ? "desktop" : "mobile"}.png`),
      fullPage: true,
    });
    await back.click();
    await expect(
      page.getByRole("button", { name: "Open post", exact: true }),
    ).toBeVisible();
    expect(remote.writes).toEqual([]);
  });
}

for (const desktop of [false, true]) {
  test(`an original post link opens shared current metadata on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    const remote = await archive(page, { merged: true });
    await page.setViewportSize(
      desktop ? { width: 1280, height: 900 } : { width: 390, height: 844 },
    );
    await page.goto(`/source-posts?post=${postIds.post}`);
    await expect(
      page.getByText("Shared album caption", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Identifiers", exact: true })
      .click();
    await expect(
      page.getByText(postIds.otherPost, { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Post text and captures", exact: true })
      .click();
    await expect(
      page.getByText("Full retained caption", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Media associations", exact: true })
      .click();
    await expect(
      page.getByText("Associated library video", { exact: true }),
    ).toBeVisible();
    expect(
      remote.requests.filter((request) => request.pathname.endsWith("/posts")),
    ).toHaveLength(0);
    expect(remote.writes).toEqual([]);
  });
}

test("a direct post link avoids browsing the library and remains accessible from the mobile drawer", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto(`/source-posts?post=${postIds.post}`);
  await expect(
    page.getByRole("button", { name: "Back to posts", exact: true }),
  ).toBeVisible();
  expect(
    remote.requests.filter((u) => u.pathname.endsWith("/posts")),
  ).toHaveLength(0);
  await page
    .getByRole("button", { name: "Open navigation menu", exact: true })
    .tap();
  const drawer = page.locator("[data-mobile-navigation]");
  const link = drawer.getByRole("link", { name: "Source posts", exact: true });
  await expect(link).toHaveCount(1);
  await link.tap();
  await expect(drawer).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Open post", exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([]);
});

test("the desktop utility menu includes source posts and closes after navigation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await archive(page);
  await page.goto(`/source-posts?post=${postIds.post}`);
  await page.getByRole("button", { name: "More options", exact: true }).click();
  const menu = page.getByRole("menu");
  await menu
    .getByRole("menuitem", { name: "Source posts", exact: true })
    .click();
  await expect(menu).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Open post", exact: true }),
  ).toBeVisible();
});

test("post pages use bounded cursors and allow returning to the previous page", async ({
  page,
}) => {
  const remote = await archive(page, { many: true });
  await page.goto("/source-posts");
  await expect(
    page.getByRole("button", { name: "Open post", exact: true }),
  ).toHaveCount(25);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByText("No matching posts", { exact: true }),
  ).toBeVisible();
  expect(remote.requests.at(-1)?.searchParams.get("after")).toBe(
    "20000000-0000-4000-8000-000000000025",
  );
  await page.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Open post", exact: true }),
  ).toHaveCount(25);
});

test("exact search encodes the whole URL, handles empty results and clears obsolete selectors", async ({
  page,
}) => {
  const remote = await archive(page);
  await page.goto("/source-posts");
  await page.getByRole("button", { name: "Post URL", exact: true }).click();
  const input = page.getByRole("textbox", { name: "Post URL", exact: true });
  await input.fill("https://example.test/post?a=1&b=2");
  await page.getByRole("button", { name: "Find posts", exact: true }).click();
  await expect
    .poll(() => remote.requests.at(-1)?.searchParams.get("url"))
    .toBe("https://example.test/post?a=1&b=2");
  await input.fill("https://example.test/missing");
  await page.getByRole("button", { name: "Find posts", exact: true }).click();
  await expect(
    page.getByText("No matching posts", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Source ID", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Source namespace", exact: true })
    .fill("native:reddit");
  await page
    .getByRole("textbox", { name: "Source ID", exact: true })
    .fill("example");
  await page.getByRole("button", { name: "Find posts", exact: true }).click();
  await expect
    .poll(() => remote.requests.at(-1)?.searchParams.get("namespace"))
    .toBe("native:reddit");
  expect(remote.requests.at(-1)?.searchParams.has("url")).toBe(false);
});

test("failed media reads retry independently and disabled albums stay disabled", async ({
  page,
}) => {
  const remote = await archive(page, {
    failMedia: true,
    disabledAlbum: true,
    unsafeURL: true,
  });
  await page.goto(`/source-posts?post=${postIds.post}`);
  await expect(
    page.getByText("javascript:alert('retained text')", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", {
      name: "javascript:alert('retained text')",
      exact: true,
    }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Media associations", exact: true })
    .click();
  await expect(
    page.getByText("Could not load post data", { exact: true }),
  ).toBeVisible();
  remote.recoverMedia();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByText("Associated library video", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Album gallery", exact: true })
    .click();
  await expect(
    page.getByText("Automatic album gallery disabled", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Open gallery", exact: true }),
  ).toHaveCount(0);
  expect(remote.writes).toEqual([]);
});
