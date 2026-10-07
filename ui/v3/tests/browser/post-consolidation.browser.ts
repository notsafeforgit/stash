import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import {
  mergeInput,
  mergePreview,
  mergeReceipt,
  mergeNotification,
  mergeConflictPreview,
} from "../fixtures/post-consolidation";
import type {
  PostMergeApply,
  PostMergeInput,
  PostMergeReceipt,
} from "../../src/core/native-archive/post-consolidation-schema";
import type { MergeNotification } from "../../src/core/native-archive/post-consolidation-api";

test.use({
  expectedConsoleErrors: [
    "the server responded with a status of 404",
    "the server responded with a status of 409",
    "the server responded with a status of 503",
  ],
});
const source = mergeInput().source_uuid,
  target = mergeInput().destination_uuid;

async function archive(
  page: Page,
  options: {
    conflicts?: boolean;
    stale?: boolean;
    loseResponse?: boolean;
    loseBeforeCommit?: boolean;
    loseRetryResponse?: boolean;
    refreshFails?: boolean;
    externalHistory?: boolean;
  } = {},
) {
  let committed: PostMergeReceipt | null = null;
  if (options.externalHistory) {
    committed = mergeReceipt();
    committed.request.media = null;
    committed.request.attachments = null;
  }
  let retry: MergeNotification | null = null;
  let retryRequest = "";
  let loseResponse = options.loseResponse,
    loseBeforeCommit = options.loseBeforeCommit,
    loseRetryResponse = options.loseRetryResponse;
  let refreshFails = options.refreshFails;
  const previews: PostMergeInput[] = [],
    applies: string[] = [],
    checks: string[] = [],
    retries: string[] = [];
  const preview = options.conflicts ? mergeConflictPreview() : mergePreview();
  const summary = (requested: string) => {
    const uuid = committed ? target : requested;
    const post = preview.posts.find((p) => p.uuid === uuid)!;
    return {
      ...post,
      requested_uuid: requested,
      more_identifiers: false,
      more_urls: false,
      created_at: "2026-10-06T12:00:00Z",
      urls: [],
    };
  };
  await page.route("**/api/v3/archive/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^.*\/api\/v3\/archive\//, "");
    const body = route.request().postData();
    if (path === "posts") return route.fulfill({ json: [summary(target)] });
    if (path === `posts/${source}` || path === `posts/${target}`) {
      if (committed && refreshFails)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      return route.fulfill({ json: summary(path.split("/")[1]!) });
    }
    if (path.endsWith("/urls"))
      return route.fulfill({
        json: { requested_uuid: path.split("/")[1], urls: [] },
      });
    if (path.endsWith("/consolidation-history"))
      return route.fulfill({
        json:
          committed && !Number(url.searchParams.get("after"))
            ? [committed.result.consolidation]
            : [],
      });
    if (path === "post-consolidation/preview") {
      const input: PostMergeInput = JSON.parse(body!);
      previews.push(input);
      const result = structuredClone(preview);
      if (options.conflicts) {
        result.blockers = result.blockers.filter((blocker) =>
          blocker.kind === "source_list_choice"
            ? !input.selection
            : blocker.kind === "gallery_choice"
              ? !input.gallery
              : blocker.kind === "media_choice"
                ? !input.media.length
                : !input.attachments.length,
        );
        result.ready = result.blockers.length === 0;
        if (result.ready) {
          result.album = {
            action: "disabled",
            entries: [],
            add: [],
            remove: [],
          };
          result.gallery = input.gallery ?? null;
        }
      }
      return route.fulfill({ json: result });
    }
    if (path.endsWith("/check")) {
      checks.push(body!);
      return route.fulfill({
        status: committed ? 200 : 404,
        json: committed ?? { error: "not_found" },
      });
    }
    if (path === "post-consolidation/apply") {
      applies.push(body!);
      if (loseBeforeCommit) {
        loseBeforeCommit = false;
        return route.abort("connectionreset");
      }
      if (options.stale)
        return route.fulfill({
          status: 409,
          json: { error: "preview_changed" },
        });
      committed = mergeReceipt(JSON.parse(body!) as PostMergeApply);
      if (loseResponse) {
        loseResponse = false;
        return route.abort("connectionreset");
      }
      return route.fulfill({ json: { review: committed, replayed: false } });
    }
    if (path.endsWith("/notifications")) {
      const original = {
        ...mergeNotification(),
        review_uuid: committed!.request.request_uuid,
      };
      const jobs = retry ? [retry, original] : [original],
        before = Number(url.searchParams.get("before"));
      return route.fulfill({
        json: jobs.filter((job) => !before || job.sequence < before),
      });
    }
    if (path.startsWith("post-merge-notification-requests/"))
      return route.fulfill({
        status: retry && path.endsWith(retryRequest) ? 200 : 404,
        json:
          retry && path.endsWith(retryRequest) ? retry : { error: "not_found" },
      });
    if (path.endsWith("/retry")) {
      retries.push(body!);
      retryRequest = JSON.parse(body!).request_uuid;
      retry = {
        ...mergeNotification(),
        job_uuid: source,
        review_uuid: committed!.request.request_uuid,
        sequence: 2,
        state: "succeeded",
        revision: 3,
        hooks_finished: true,
        resume_from_job_uuid: committed!.result.notification_job_uuid!,
        resume_from_job_revision: 2,
      };
      if (loseRetryResponse) {
        loseRetryResponse = false;
        return route.abort("connectionreset");
      }
      return route.fulfill({ json: retry });
    }
    if (path.startsWith("post-consolidation/requests/"))
      return route.fulfill({
        status: committed ? 200 : 404,
        json: committed ?? { error: "not_found" },
      });
    throw new Error(
      `Unexpected post merge request: ${route.request().method()} ${path}`,
    );
  });
  return {
    previews,
    applies,
    checks,
    retries,
    recoverRefresh: () => {
      refreshFails = false;
    },
  };
}

async function open(page: Page) {
  await page.goto(`/source-posts?post=${source}`);
  await page
    .getByRole("button", { name: "Merge source posts", exact: true })
    .click();
}
async function prepare(page: Page) {
  await page
    .getByRole("textbox", { name: "Post URL", exact: true })
    .fill("https://www.reddit.com/gallery/fixture");
  await page.getByRole("button", { name: "Find posts", exact: true }).click();
  await page
    .getByRole("button", { name: "Keep this post", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Preview merge", exact: true })
    .click();
  await expect(
    page.getByText("Proposed result", { exact: true }),
  ).toBeVisible();
}
async function apply(page: Page) {
  await page
    .getByRole("button", { name: "Merge into selected post", exact: true })
    .click();
}

for (const desktop of [false, true])
  test(`reviews and saves a post merge on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const remote = await archive(page);
    await open(page);
    await prepare(page);
    expect(remote.applies).toHaveLength(0);
    await page
      .getByRole("textbox", { name: "Reason (optional)", exact: true })
      .fill("Recovered duplicate source record");
    await expect(
      page.getByRole("button", { name: "Merge into selected post" }),
    ).toBeDisabled();
    await page
      .getByRole("button", { name: "Preview merge", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Merge into selected post" }),
    ).toBeEnabled();
    await page
      .getByRole("button", {
        name: "Original posts and saved choices",
        exact: true,
      })
      .click();
    await page.screenshot({
      path: test
        .info()
        .outputPath(`post-merge-${desktop ? "desktop" : "mobile"}.png`),
      fullPage: true,
    });
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await apply(page);
    await expect(
      page.getByText("Post merge saved", { exact: true }),
    ).toBeVisible();
    expect(remote.applies).toHaveLength(1);
    expect(JSON.parse(remote.applies[0]!)).toMatchObject({
      source_uuid: source,
      destination_uuid: target,
      reason: "Recovered duplicate source record",
    });
    await page
      .getByRole("button", { name: "Retry notification delivery", exact: true })
      .click();
    await expect(
      page.getByText("Notification delivery finished.", { exact: true }),
    ).toBeVisible();
    expect(remote.applies).toHaveLength(1);
    expect(remote.retries).toHaveLength(1);
  });

test("requires explicit source, gallery and link choices and sends their exact selections", async ({
  page,
}) => {
  const remote = await archive(page, { conflicts: true });
  await open(page);
  await prepare(page);
  await expect(
    page.getByRole("button", { name: "Merge into selected post" }),
  ).toBeDisabled();
  await expect(
    page.getByText("Resolve these choices before merging", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Choose one list", exact: true })
    .click();
  await page
    .getByRole("combobox", {
      name: "Resulting gallery association",
      exact: true,
    })
    .click();
  await page.getByRole("option", { name: "Disable", exact: true }).click();
  await page
    .getByRole("button", { name: "Choose post links for media", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Explicitly unlinked", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Choose attachment links", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "native:reddit · first", exact: true })
    .click();
  await page
    .getByRole("option", { name: "Explicitly unlinked", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Merge into selected post" }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Preview merge", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Merge into selected post" }),
  ).toBeEnabled();
  expect(remote.previews.at(-1)).toMatchObject({
    selection: { mode: "choose" },
    gallery: { state: "disabled" },
    media: [{ state: "unlinked" }],
    attachments: [
      { namespace: "native:reddit", value: "first", state: "unlinked" },
    ],
  });
  expect(remote.applies).toHaveLength(0);
});

test("opens API-created merge history with omitted empty choices without another write", async ({
  page,
}) => {
  const remote = await archive(page, { externalHistory: true });
  await open(page);
  await expect(
    page.getByText("This post has already been merged", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Post merge history", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Inspect saved merge", exact: true })
    .click();
  await expect(
    page.getByText("Post merge saved", { exact: true }),
  ).toBeVisible();
  expect(remote.applies).toHaveLength(0);
  expect(remote.previews).toHaveLength(0);
});

test("requires a fresh review after a stale preview is definitively rejected", async ({
  page,
}) => {
  const remote = await archive(page, { stale: true });
  await open(page);
  await prepare(page);
  await apply(page);
  await expect(
    page.getByText("Review this merge again", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Review again", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "Post URL", exact: true }),
  ).toBeVisible();
  expect(remote.applies).toHaveLength(1);
});

test("keeps the saved result when refreshing the post fails", async ({
  page,
}) => {
  const remote = await archive(page, { refreshFails: true });
  await open(page);
  await prepare(page);
  await apply(page);
  await expect(
    page.getByText("Post merge saved", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Could not load post data", { exact: true }),
  ).toBeVisible();
  remote.recoverRefresh();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByText("Could not load post data", { exact: true }),
  ).toHaveCount(0);
  expect(remote.applies).toHaveLength(1);
});

test.describe("response recovery", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource", "Load failed"],
  });
  for (const beforeCommit of [false, true])
    test(`recovers the original request after reload with ${beforeCommit ? "no" : "a"} server commit`, async ({
      page,
    }) => {
      const remote = await archive(page, {
        loseBeforeCommit: beforeCommit,
        loseResponse: !beforeCommit,
      });
      await open(page);
      await prepare(page);
      await apply(page);
      await expect(
        page.getByText("A saved post merge needs confirmation", {
          exact: true,
        }),
      ).toBeVisible();
      // The saved-request notice also appears while the first send is pending.
      // Complete the simulated failure before testing recovery after reload.
      await expect.poll(() => remote.applies.length).toBe(1);
      await expect(
        page.getByRole("button", {
          name: "Check and retry saved merge",
          exact: true,
        }),
      ).toBeEnabled();
      await page.reload();
      await page
        .getByRole("button", { name: "Merge source posts", exact: true })
        .click();
      await expect(
        page.getByRole("textbox", { name: "Post URL", exact: true }),
      ).toHaveCount(0);
      await page
        .getByRole("button", {
          name: "Check and retry saved merge",
          exact: true,
        })
        .click();
      await expect(
        page.getByText("Post merge saved", { exact: true }),
      ).toBeVisible();
      expect(remote.applies).toHaveLength(beforeCommit ? 2 : 1);
      expect(remote.checks.at(-1)).toBe(remote.applies[0]);
      if (beforeCommit) expect(remote.applies[1]).toBe(remote.applies[0]);
    });
  test("recovers a notification retry from merge history after reload without another merge", async ({
    page,
  }) => {
    const remote = await archive(page, { loseRetryResponse: true });
    await open(page);
    await prepare(page);
    await apply(page);
    await page
      .getByRole("button", { name: "Retry notification delivery", exact: true })
      .click();
    await expect(
      page.getByText("A notification retry needs confirmation", {
        exact: true,
      }),
    ).toBeVisible();
    await page.reload();
    await page
      .getByRole("button", { name: "Merge source posts", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Post merge history", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Inspect saved merge", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Recover notification retry", exact: true })
      .click();
    await expect(
      page.getByText("A notification retry needs confirmation", {
        exact: true,
      }),
    ).toHaveCount(0);
    await expect(
      page.getByText("Notification delivery finished.", { exact: true }),
    ).toBeVisible();
    expect(remote.applies).toHaveLength(1);
    expect(remote.retries).toHaveLength(1);
  });
});
