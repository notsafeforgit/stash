import type { Page } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import {
  sourceIds,
  sourcePost,
  sourceReceipt,
} from "../fixtures/source-review";
import { postSummary } from "../fixtures/source-posts";
import type {
  SourceDecision,
  SourceLinkInput,
} from "../../src/core/native-archive/source-review-api";

test.use({
  expectedConsoleErrors: [
    "Failed to load resource",
    "Load failed",
    "the server responded with a status of 404",
  ],
});

async function archive(
  page: Page,
  options: {
    image?: boolean;
    loseReply?: boolean;
    stale?: boolean;
    conflict?: boolean;
    empty?: boolean;
    merged?: boolean;
  } = {},
) {
  let committed: SourceDecision | null = null;
  const bodies: SourceLinkInput[] = [];
  const paths: string[] = [];
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    paths.push(path);
    const post = sourcePost();
    if (options.conflict) {
      post.association.state = "conflict";
      post.association.decisions = [
        {
          uuid: sourceIds.decision,
          post_uuid: sourceIds.post,
          media_uuid: sourceIds.media,
          post_revision: 2,
          media_revision: 1,
          state: "linked",
          origin: "review",
          reason: "Earlier link",
          created_at: "2026-09-29T00:00:00Z",
        },
        {
          uuid: sourceIds.secondCapture,
          post_uuid: sourceIds.post,
          media_uuid: sourceIds.capture,
          post_revision: 3,
          media_revision: 1,
          state: "unlinked",
          origin: "review",
          reason: "Other merged item",
          created_at: "2026-09-29T01:00:00Z",
        },
      ];
    }
    if (committed)
      post.association = {
        ...post.association,
        state: committed.state,
        post_revision: committed.post_revision,
        decisions: [committed],
      };
    if (options.merged) {
      const canonical = "00000000-0000-4000-8000-000000000011";
      post.requested_post_uuid = canonical;
      post.association.post_uuid = canonical;
      if (committed && committed.post_uuid !== canonical) {
        post.association.post_revision += 5;
        post.association.decisions = [
          {
            ...committed,
            uuid: sourceIds.revision,
            post_uuid: canonical,
            post_revision: post.association.post_revision,
          },
        ];
      }
    }
    let result: unknown;
    if (path.includes("/entity-identities/"))
      result = {
        uuid: sourceIds.media,
        kind: options.image ? "image" : "scene",
        revision: 2,
        local_id: 7,
      };
    else if (path.endsWith("/source-posts"))
      result = options.empty ? [] : [post];
    else if (/\/posts\/[^/]+$/.test(path))
      result = {
        ...postSummary(),
        requested_uuid: path.split("/").at(-1),
        uuid: post.association.post_uuid,
        revision: post.association.post_revision,
      };
    else if (path.endsWith("/review"))
      result = { ...post, requested_post_uuid: path.split("/").at(-4) };
    else if (path.includes("/post-media-decisions/")) {
      await route.fulfill({
        status: committed ? 200 : 404,
        json: committed ?? { error: "not_found" },
      });
      return;
    } else if (route.request().method() === "PUT") {
      const input: SourceLinkInput = route.request().postDataJSON();
      bodies.push(input);
      if (options.stale) {
        await route.fulfill({
          status: 409,
          json: { error: "post_media_conflict" },
        });
        return;
      }
      committed = sourceReceipt(input);
      if (options.loseReply) {
        options.loseReply = false;
        await route.abort("failed");
        return;
      }
      result = committed;
    } else if (path.endsWith("/capture-history"))
      result = {
        requested_uuid: path.split("/").at(-2),
        revisions: [
          {
            uuid: sourceIds.revision,
            count: 2,
            unknown_count: 1,
            first_seen: "2026-09-30T08:00:00Z",
            last_seen: "2026-09-30T08:00:00Z",
            metadata: {
              title: "Full retained caption",
              original_text: "One shared description",
              published_at: "2026-09-29",
            },
          },
        ],
      };
    else if (path.endsWith("/history"))
      result = committed ? [committed] : post.association.decisions;
    else throw new Error(`Unexpected archive request: ${path}`);
    await route.fulfill({ json: result });
  });
  return { bodies, paths };
}
async function openReview(page: Page, desktop = false, image = false) {
  await page.setViewportSize(
    desktop ? { width: 1280, height: 900 } : { width: 390, height: 844 },
  );
  await page.goto(`/source-review${image ? "?image" : ""}`);
  if (desktop)
    await page.getByRole("tab", { name: "Sources", exact: true }).click();
  else await chooseSection(page, "Sources");
  await expect(
    page.getByText("Shared album caption", { exact: true }),
  ).toBeVisible();
}
for (const image of [false, true]) {
  for (const desktop of [false, true]) {
    test(`${image ? "image" : "scene"} source review on ${desktop ? "desktop" : "mobile"} loads only targeted summaries until expanded`, async ({
      page,
    }) => {
      const remote = await archive(page, { image });
      await openReview(page, desktop, image);
      expect(remote.bodies).toHaveLength(0);
      expect(remote.paths.some((p) => p.endsWith("/capture-summaries"))).toBe(
        false,
      );
      expect(remote.paths.some((p) => p.endsWith("/capture-history"))).toBe(
        false,
      );
      expect(remote.paths.some((p) => p.endsWith("/history"))).toBe(false);
      await expect(
        page.getByRole("link", {
          name: "https://www.reddit.com/gallery/example",
          exact: true,
        }),
      ).toHaveAttribute("href", "https://www.reddit.com/gallery/example");
      await page
        .getByRole("button", { name: "Post text and captures", exact: true })
        .click();
      await expect(
        page.getByText("One shared description", { exact: true }),
      ).toHaveCount(1);
      await expect(
        page.getByText("2 observations", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("Observation time unknown for 1 imported record.", {
          exact: true,
        }),
      ).toBeVisible();
      // Strict Mode can start and abort an initial read. Every read still
      // targets this post, and its shared description is rendered only once.
      expect([
        ...new Set(remote.paths.filter((p) => p.endsWith("/capture-history"))),
      ]).toEqual([`/api/v3/archive/posts/${sourceIds.post}/capture-history`]);
      expect(remote.paths.some((p) => p.endsWith("/capture-summaries"))).toBe(
        false,
      );
      await page
        .getByRole("button", { name: "Review source link", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Save source link", exact: true }),
      ).toBeDisabled();
      await page.getByRole("button", { name: "Link", exact: true }).click();
      await page
        .getByLabel("Reason (optional)")
        .fill("Verified the original post");
      await page.screenshot({
        path: test
          .info()
          .outputPath(
            `${image ? "image" : "scene"}-${desktop ? "desktop" : "mobile"}-source-review.png`,
          ),
      });
      await page
        .getByRole("button", { name: "Save source link", exact: true })
        .click();
      await expect(
        page.getByText("Source link saved", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("Linked to this item", { exact: true }),
      ).toBeVisible();
      expect(remote.bodies).toHaveLength(1);
      expect(remote.bodies[0]).toMatchObject({
        post_uuid: sourceIds.post,
        media_uuid: sourceIds.media,
        state: "linked",
        expected_post_revision: 4,
        expected_media_revision: 2,
        expected_decisions: [],
        reason: "Verified the original post",
      });
      expect(
        remote.paths.filter((path) => path.endsWith("/source-posts")),
      ).toHaveLength(1);
      expect(
        remote.paths.filter((path) => path.endsWith("/review")),
      ).toHaveLength(1);
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        )
        .toBe(true);
      const openPost = page.getByRole("link", {
        name: "Open post",
        exact: true,
      });
      await expect(openPost).toHaveAttribute(
        "href",
        `/source-posts?post=${sourceIds.post}`,
      );
      await openPost.click();
      await expect(
        page.getByRole("heading", { name: "Source posts", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("Shared album caption", { exact: true }),
      ).toBeVisible();
      expect(remote.paths.some((path) => path.endsWith("/posts"))).toBe(false);
      expect(remote.bodies).toHaveLength(1);
    });
  }
}
for (const desktop of [false, true]) {
  test(`new source links use the consolidated post on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    const remote = await archive(page, { merged: true });
    await openReview(page, desktop);
    await page
      .getByRole("button", { name: "Review source link", exact: true })
      .click();
    await page.getByRole("button", { name: "Link", exact: true }).click();
    await page
      .getByRole("button", { name: "Save source link", exact: true })
      .click();
    await expect(
      page.getByText("Source link saved", { exact: true }),
    ).toBeVisible();
    expect(remote.bodies).toHaveLength(1);
    expect(remote.bodies[0]?.post_uuid).toBe(
      "00000000-0000-4000-8000-000000000011",
    );
  });
}

test("a saved original source decision recovers after its post is consolidated", async ({
  page,
}) => {
  const options = { loseReply: true, merged: false };
  const remote = await archive(page, options);
  await openReview(page);
  await page
    .getByRole("button", { name: "Review source link", exact: true })
    .click();
  await page.getByRole("button", { name: "Unlink", exact: true }).click();
  await page
    .getByRole("button", { name: "Save source link", exact: true })
    .click();
  await expect(
    page.getByText("Could not complete this step", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("A saved source link needs confirmation", { exact: true }),
  ).toBeVisible();
  expect(remote.bodies).toHaveLength(1);
  const original = JSON.stringify(remote.bodies);
  options.merged = true;
  await page.reload();
  await chooseSection(page, "Sources");
  await page
    .getByRole("button", { name: "Check and retry saved change", exact: true })
    .click();
  await expect(
    page.getByText("Source link saved", { exact: true }),
  ).toBeVisible();
  expect(JSON.stringify(remote.bodies)).toBe(original);
  expect(remote.bodies[0]?.post_uuid).toBe(sourceIds.post);
  await expect(
    page.getByRole("link", { name: "Open post", exact: true }),
  ).toHaveAttribute(
    "href",
    "/source-posts?post=00000000-0000-4000-8000-000000000011",
  );
});

test("a lost committed reply is recovered after reload without another write", async ({
  page,
}) => {
  const remote = await archive(page, { loseReply: true });
  await openReview(page);
  await page
    .getByRole("button", { name: "Review source link", exact: true })
    .click();
  await page.getByRole("button", { name: "Unlink", exact: true }).click();
  await page
    .getByRole("button", { name: "Save source link", exact: true })
    .click();
  await expect(
    page.getByText("Could not complete this step", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("A saved source link needs confirmation", { exact: true }),
  ).toBeVisible();
  expect(remote.bodies).toHaveLength(1);
  await page.reload();
  await chooseSection(page, "Sources");
  await page
    .getByRole("button", { name: "Check and retry saved change", exact: true })
    .click();
  await expect(
    page.getByText("Source link saved", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("A saved source link needs confirmation", { exact: true }),
  ).toHaveCount(0);
  expect(remote.bodies).toHaveLength(1);
});
test("a stale link requires review again and does not silently choose new guards", async ({
  page,
}) => {
  const remote = await archive(page, { stale: true });
  await openReview(page);
  await page
    .getByRole("button", { name: "Review source link", exact: true })
    .click();
  await page.getByRole("button", { name: "Link", exact: true }).click();
  await page
    .getByRole("button", { name: "Save source link", exact: true })
    .click();
  await expect(
    page.getByText("The saved link needs a new review", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Save source link", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Review again", exact: true }).click();
  await expect(
    page.getByText("The saved link needs a new review", { exact: true }),
  ).toHaveCount(0);
  expect(remote.bodies).toHaveLength(1);
});
test("merged link conflicts retain every decision guard when returning to attachments", async ({
  page,
}) => {
  const remote = await archive(page, { conflict: true });
  await openReview(page, true);
  await expect(
    page.getByText("Conflicting link choices", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Review source link", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Use attachments", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Save source link", exact: true })
    .click();
  await expect(
    page.getByText("Source link saved", { exact: true }),
  ).toBeVisible();
  expect(remote.bodies[0]?.state).toBe("undecided");
  expect(remote.bodies[0]?.expected_decisions).toEqual(
    [sourceIds.decision, sourceIds.secondCapture].sort(),
  );
});
test("directly scanned media has a useful empty state without implying an import failure", async ({
  page,
}) => {
  const remote = await archive(page, { empty: true });
  await page.goto("/source-review");
  await chooseSection(page, "Sources");
  await expect(
    page.getByText("No source posts for this item", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(/Directly scanned files can exist without a scraped post/),
  ).toBeVisible();
  expect(remote.bodies).toHaveLength(0);
});

test("source-link storage coordinates real tabs and keeps deployment prefixes separate", async ({
  page,
  context,
}) => {
  await page.goto("/source-review");
  const other = await context.newPage();
  try {
    await other.goto("/source-review");
    const [one, same] = await Promise.all([
      page.evaluate(() => window.sourceReviewStorage.prepare("/one/")),
      other.evaluate(() => window.sourceReviewStorage.prepare("/one/")),
    ]);
    expect(one.body).toBe(same.body);
    expect(
      await other.evaluate(() => window.sourceReviewStorage.read("/two/")),
    ).toBeNull();
    const separate = await other.evaluate(() =>
      window.sourceReviewStorage.prepare("/two/"),
    );
    expect(separate.body).not.toBe(one.body);
    await page.reload();
    expect(
      await page.evaluate(() => window.sourceReviewStorage.read("/one/")),
    ).toEqual(one);
    expect(
      await page.evaluate(() => window.sourceReviewStorage.read("/two/")),
    ).toEqual(separate);
  } finally {
    await other.close();
  }
});
