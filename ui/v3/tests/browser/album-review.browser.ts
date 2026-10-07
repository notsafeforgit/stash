import { emptyDownloadStatus } from "../fixtures/downloads";
import type { Page, Route } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import {
  albumPreview,
  albumJob,
  albumReviewID,
} from "../fixtures/album-review";
import { albumPage, albumSlot } from "../fixtures/source-albums";
import {
  postIds,
  postSummary,
  postAlbum,
  postAlbumContext,
  postIdentity,
} from "../fixtures/source-posts";
import {
  albumPolicySchema,
  type AlbumJob,
  type AlbumPreview,
} from "../../src/core/native-archive/album-review-api";

test.use({
  expectedConsoleErrors: [
    "net::ERR_FAILED",
    "Load failed",
    "the server responded with a status of 409",
    "the server responded with a status of 404",
  ],
});

async function archive(page: Page, create = false) {
  const jobs = new Map<string, AlbumJob>();
  const receipts = new Map<string, string>();
  const writes: { path: string; body: string }[] = [];
  const requests: URL[] = [];
  const previews: string[] = [];
  let canonicalPost = postIds.post,
    identityUnavailable = false;
  let loseReply = false,
    corruptReceipt = false,
    conflict = "",
    sequence = 1;
  let action: AlbumPreview["action"] = create ? "create" : "sync";
  let noop = false;
  function preview(): AlbumPreview {
    const result = { ...albumPreview(), post_uuid: canonicalPost, action };
    if (action !== "create") delete result.initial_metadata;
    if (!create)
      result.gallery = {
        uuid: postIds.gallery,
        kind: "gallery",
        revision: 1,
        state: "active",
        local_id: 12,
      };
    if (action !== "create" && action !== "sync") {
      result.entries = [];
      result.add = [];
      result.matches = [];
    }
    if (noop) {
      result.add = [];
      result.matches = [];
    }
    return result;
  }
  async function handler(route: Route) {
    if (
      new URL(route.request().url()).pathname.endsWith(
        "/attachments/download-status",
      )
    )
      return route.fulfill({
        json: emptyDownloadStatus(route.request().postDataJSON().attachments),
      });
    const request = route.request(),
      url = new URL(request.url());
    requests.push(url);
    const path = url.pathname.split("/archive/")[1]!;
    if (
      request.method() === "POST" &&
      path.endsWith("/album-backfill/preview")
    ) {
      const policy = albumPolicySchema.parse(request.postDataJSON().policy);
      previews.push(policy);
      await route.fulfill({ json: { ...preview(), policy } });
      return;
    }
    if (request.method() === "POST") {
      const body = request.postData()!,
        input = request.postDataJSON();
      writes.push({ path, body });
      if (conflict) {
        await route.fulfill({ status: 409, json: { error: conflict } });
        return;
      }
      let result: AlbumJob;
      if (path.endsWith("/cancel")) {
        const prior = jobs.get(path.split("/")[1]!)!;
        if (prior.revision !== input.expected_revision) {
          await route.fulfill({
            status: 409,
            json: { error: "album_job_changed" },
          });
          return;
        }
        result = { ...prior, state: "cancelled", revision: prior.revision + 1 };
      } else if (path.endsWith("/retry")) {
        const prior = jobs.get(path.split("/")[1]!)!;
        result = {
          ...prior,
          state: "queued",
          revision: 1,
          job_uuid: albumReviewID(100 + ++sequence),
          sequence,
          resume_from_job_uuid: prior.job_uuid,
        };
        receipts.set(input.request_uuid, result.job_uuid);
      } else if (/^posts\/[^/]+\/album-backfills$/.test(path)) {
        if (path.split("/")[1] !== canonicalPost) {
          await route.fulfill({
            status: 409,
            json: { error: "album_preview_changed" },
          });
          return;
        }
        const prior = receipts.get(input.request_uuid);
        result = prior
          ? jobs.get(prior)!
          : albumJob({
              post_uuid: canonicalPost,
              job_uuid: albumReviewID(100 + ++sequence),
              sequence,
              signature: input.signature,
              policy: input.policy,
            });
        receipts.set(input.request_uuid, result.job_uuid);
      } else throw new Error(`Unexpected write ${path}`);
      jobs.set(result.job_uuid, result);
      if (loseReply) {
        loseReply = false;
        await route.abort("failed");
        return;
      }
      await route.fulfill({
        status: result.state === "queued" ? 202 : 200,
        json: result,
      });
      return;
    }
    let result: unknown;
    if (path.endsWith("/identity")) {
      if (identityUnavailable) {
        await route.abort("failed");
        return;
      }
      result = postIdentity(path.split("/")[1], canonicalPost);
    } else if (path.startsWith("album-backfill-requests/")) {
      const id = receipts.get(path.split("/")[1]!);
      const job = id ? jobs.get(id) : undefined;
      if (!job) {
        await route.fulfill({ status: 404, json: { error: "not_found" } });
        return;
      }
      result = corruptReceipt ? { ...job, post_uuid: albumReviewID(999) } : job;
    } else if (
      path.startsWith("album-backfills/") &&
      path.endsWith("/attempts")
    ) {
      const job = jobs.get(path.split("/")[1]!)!;
      result = job.attempts
        ? [
            {
              job_uuid: job.job_uuid,
              fence: 1,
              owner_uuid: albumReviewID(90),
              started_at: job.created_at,
              ended_at: job.updated_at,
              outcome: "retry",
              result: {},
              error_code: "album_processing_unavailable",
            },
          ]
        : [];
    } else if (path.startsWith("album-backfills/"))
      result = jobs.get(path.split("/")[1]!);
    else if (path.endsWith("/album-backfills"))
      result = [...jobs.values()]
        .filter((job) => job.post_uuid === path.split("/")[1])
        .filter((job) => job.sequence > Number(url.searchParams.get("after")))
        .sort((a, b) => a.sequence - b.sequence)
        .slice(0, 25);
    else if (path === "entity-identities/gallery/12")
      result = {
        uuid: postIds.gallery,
        kind: "gallery",
        local_id: 12,
        revision: 1,
      };
    else if (path.endsWith("/album-posts"))
      result = {
        requested_uuid: postIds.gallery,
        gallery: postAlbum().gallery,
        posts: [postSummary()],
      };
    else if (path === `posts/${postIds.post}`) result = postSummary();
    else if (path.endsWith("/album"))
      result = postAlbumContext(
        create && ![...jobs.values()].some((j) => j.publication_committed)
          ? null
          : postAlbum(),
        path.split("/")[1],
      );
    else if (path.endsWith("/album-media")) {
      const p = preview(),
        data = albumPage();
      const committed = [...jobs.values()].some(
        (job) => job.publication_committed,
      );
      data.album = create && !committed ? null : postAlbum();
      data.selection = {
        uuid: p.selection_uuid!,
        mode: "automatic",
        revision: 1,
        complete: true,
        declared_album: true,
        expected_count: 3,
        entry_count: 3,
      };
      data.slots = albumPreview().entries.map((entry) => ({
        ...albumSlot(entry.position),
        attachment: {
          uuid: entry.attachment_uuid,
          revision: 1,
          reference: {
            namespace: "native:reddit",
            value: `item-${entry.position}`,
          },
        },
        selection_state: "unselected",
        media: null,
        decision_uuid: null,
        post_link_state: "",
        registered_files: 0,
        gallery_membership: create && !committed ? "no_gallery" : "absent",
      }));
      result = data;
    } else throw new Error(`Unexpected album read ${path}`);
    if (result === undefined) {
      await route.fulfill({ status: 404, json: { error: "not_found" } });
      return;
    }
    await route.fulfill({ json: result });
  }
  await page.route("**/api/v3/archive/**", handler);
  return {
    jobs,
    writes,
    requests,
    previews,
    mergePost() {
      canonicalPost = postIds.otherPost;
    },
    failIdentity(value: boolean) {
      identityUnavailable = value;
    },
    attach: (other: Page) => other.route("**/api/v3/archive/**", handler),
    loseReply() {
      loseReply = true;
    },
    corruptReceipt() {
      corruptReceipt = true;
    },
    conflict(code: string) {
      conflict = code;
    },
    action(value: AlbumPreview["action"]) {
      action = value;
    },
    noop() {
      noop = true;
    },
    current() {
      return [...jobs.values()].sort((a, b) => b.sequence - a.sequence)[0]!;
    },
    publish() {
      const prior = this.current();
      const job: AlbumJob = {
        ...prior,
        attempts: 1,
        revision: prior.revision + 1,
        publication_committed: true,
        publication: {
          post_uuid: prior.post_uuid,
          event_uuid: prior.job_uuid,
          gallery_uuid: postIds.gallery,
          action: create ? "create" : "sync",
          created: create,
          selected: 1,
          review: 0,
          unavailable: 1,
          added: 2,
          removed: 0,
        },
      };
      jobs.set(job.job_uuid, job);
      return job;
    },
  };
}

async function open(
  page: Page,
  desktop = false,
  source = false,
  navigate = true,
  recovery = false,
) {
  if (desktop) await page.setViewportSize({ width: 1280, height: 1000 });
  if (navigate)
    await page.goto(
      source ? `/source-posts?post=${postIds.post}` : "/source-albums",
    );
  if (source)
    await page
      .getByRole("button", { name: "Source order", exact: true })
      .click();
  else {
    if (desktop)
      await page
        .getByRole("tab", { name: "Source albums", exact: true })
        .click();
    else await chooseSection(page, "Source albums");
    await page
      .getByRole("button", {
        name: postSummary().latest_capture?.title ?? "Untitled source post",
        exact: true,
      })
      .click();
  }
  await expect(page.locator("[data-source-position]")).toHaveCount(3);
  await page
    .getByRole("button", { name: "Match existing media", exact: true })
    .click();
  await expect(
    page.getByRole("button", {
      name: recovery
        ? "Check and retry saved request"
        : "Preview album changes",
      exact: true,
    }),
  ).toBeEnabled();
}
async function preview(page: Page) {
  await page
    .getByRole("button", { name: "Preview album changes", exact: true })
    .click();
  await expect(page.locator("[data-album-preview]")).toBeVisible();
}
async function apply(page: Page) {
  await preview(page);
  await page
    .getByRole("button", { name: "Apply reviewed album changes", exact: true })
    .click();
}

for (const desktop of [false, true]) {
  test(`an original album link submits new work under its current post on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    const remote = await archive(page);
    remote.mergePost();
    await open(page, desktop);
    expect(remote.writes).toEqual([]);
    await apply(page);
    await expect(
      page.locator("[data-album-job]").getByText("Queued", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(1);
    expect(remote.writes[0]!.path).toBe(
      `posts/${postIds.otherPost}/album-backfills`,
    );
  });
  test(`album preview on ${desktop ? "desktop" : "mobile"} is read-only with explicit file evidence`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await open(page, desktop);
    expect(
      remote.requests.some((url) => url.pathname.endsWith("/album-backfills")),
    ).toBe(false);
    expect(remote.previews).toEqual([]);
    await preview(page);
    await expect(
      page.getByText("Verified match: 1", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Keep saved choice: 1", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("No current match: 1", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Add to gallery (2)", exact: true })
      .click();
    await expect(
      page.getByRole("link", { name: "Image #21", exact: true }),
    ).toHaveAttribute("href", "/images/21");
    await expect(
      page.getByRole("link", { name: "Video #31", exact: true }),
    ).toHaveAttribute("href", "/scenes/31");
    await page
      .getByRole("button", { name: "Attachment choices", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Inspect matching evidence", exact: true })
      .click();
    await expect(
      page.getByText("Coast/photo-a.jpg", { exact: true }),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: test
        .info()
        .outputPath(`album-review-${desktop ? "desktop" : "mobile"}.png`),
    });
    expect(remote.writes).toEqual([]);
  });
  test(`lost Apply response on ${desktop ? "desktop" : "mobile"} requires explicit recovery after reload`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await open(page, desktop);
    remote.loseReply();
    await apply(page);
    await expect(
      page.getByText("An album request needs confirmation", { exact: true }),
    ).toBeVisible();
    await expect.poll(() => remote.writes.length).toBe(1);
    const original = remote.writes[0]!.body;
    await page.reload();
    // Open the controls without requiring the preview button to be enabled.
    if (desktop)
      await page
        .getByRole("tab", { name: "Source albums", exact: true })
        .click();
    else await chooseSection(page, "Source albums");
    await page
      .getByRole("button", {
        name: postSummary().latest_capture?.title ?? "Untitled source post",
        exact: true,
      })
      .click();
    await page
      .getByRole("button", { name: "Match existing media", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Preview album changes", exact: true }),
    ).toBeDisabled();
    expect(remote.writes.map((write) => write.body)).toEqual([original]);
    await page
      .getByRole("button", {
        name: "Check and retry saved request",
        exact: true,
      })
      .click();
    await expect(
      page.locator("[data-album-job]").getByText("Queued", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("This job has not committed album changes.", {
        exact: true,
      }),
    ).toBeVisible();
    expect(remote.writes.map((write) => write.body)).toEqual([original]);
  });
}

test("an original admitted album request recovers after consolidation without current context or another write", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  remote.loseReply();
  await apply(page);
  await expect(
    page.getByText("An album request needs confirmation", { exact: true }),
  ).toBeVisible();
  await expect.poll(() => remote.writes.length).toBe(1);
  const original = { ...remote.writes[0]! };
  remote.mergePost();
  remote.failIdentity(true);
  await open(page, false, false, true, true);
  expect(remote.writes).toEqual([original]);
  await page
    .getByRole("button", { name: "Check and retry saved request", exact: true })
    .click();
  await expect(
    page.locator("[data-album-job]").getByText("Queued", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([original]);
  remote.failIdentity(false);
  await open(page);
  await apply(page);
  await expect.poll(() => remote.writes.length).toBe(2);
  expect(remote.writes[1]!.path).toBe(
    `posts/${postIds.otherPost}/album-backfills`,
  );
  expect(remote.writes[0]).toEqual(original);
});

test("inspecting original job history preserves a pending request at the current post", async ({
  page,
}) => {
  const remote = await archive(page);
  const prior = albumJob({
    post_uuid: postIds.post,
    job_uuid: albumReviewID(200),
    state: "failed",
    revision: 3,
  });
  remote.jobs.set(prior.job_uuid, prior);
  remote.mergePost();
  await open(page);
  remote.loseReply();
  await apply(page);
  const pending = page.getByText("An album request needs confirmation", {
    exact: true,
  });
  await expect(pending).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  await page
    .getByRole("button", { name: "Album job history", exact: true })
    .click();
  await page.getByRole("button", { name: "Inspect job", exact: true }).click();
  await expect(
    page.locator("[data-album-job]").getByText("Failed", { exact: true }),
  ).toBeVisible();
  await expect(pending).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Preview album changes", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Check and retry saved request", exact: true })
    .click();
  await expect(pending).toHaveCount(0);
  await expect(
    page.locator("[data-album-job]").getByText("Queued", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(remote.writes[0]!.path).toBe(
    `posts/${postIds.otherPost}/album-backfills`,
  );
});

test("committed changes refresh the gallery once and survive cancellation and notification retry", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page, true);
  await apply(page);
  await expect(page.locator("[data-album-job]")).toBeVisible();
  const published = remote.publish();
  await page
    .getByRole("button", { name: "Refresh album status", exact: true })
    .click();
  await expect(
    page.getByText("Album changes saved", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Plugin notifications are still pending. Saved changes remain in the library.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(page.locator("[data-album-refresh-count]")).toHaveAttribute(
    "data-album-refresh-count",
    "1",
  );
  remote.loseReply();
  await page
    .getByRole("button", { name: "Cancel pending work", exact: true })
    .click();
  await expect(
    page.getByRole("button", {
      name: "Check and retry saved request",
      exact: true,
    }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Check and retry saved request", exact: true })
    .click();
  await expect(
    page.locator("[data-album-job]").getByText("Cancelled", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Album changes saved", { exact: true }),
  ).toBeVisible();
  remote.loseReply();
  await page
    .getByRole("button", { name: "Retry notification delivery", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Check and retry saved request", exact: true })
    .click();
  const current = remote.current();
  expect(current.publication).toEqual(published.publication);
  remote.jobs.set(current.job_uuid, {
    ...current,
    state: "succeeded",
    hooks_finished: true,
    revision: 2,
  });
  await page
    .getByRole("button", { name: "Refresh album status", exact: true })
    .click();
  await expect(
    page.getByText("Notification delivery finished.", { exact: true }),
  ).toBeVisible();
  await expect(page.locator("[data-album-refresh-count]")).toHaveAttribute(
    "data-album-refresh-count",
    "1",
  );
  expect(remote.writes).toHaveLength(3);
});

test("stale previews need review again while ambiguous request conflicts stay saved", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  remote.conflict("album_preview_changed");
  await apply(page);
  await page
    .getByRole("button", { name: "Review album again", exact: true })
    .click();
  await expect(page.locator("[data-album-preview]")).toHaveCount(0);
  remote.conflict("album_job_changed");
  await apply(page);
  await expect(
    page.getByText("An album request needs confirmation", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Review album again", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Preview album changes", exact: true }),
  ).toBeDisabled();
});

test("wrong receipts stay pending without another write", async ({ page }) => {
  const remote = await archive(page);
  await open(page);
  remote.loseReply();
  await apply(page);
  await expect(
    page.getByRole("button", {
      name: "Check and retry saved request",
      exact: true,
    }),
  ).toBeEnabled();
  remote.corruptReceipt();
  await page
    .getByRole("button", { name: "Check and retry saved request", exact: true })
    .click();
  await expect(
    page.getByText("Could not complete this album step", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("An album request needs confirmation", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
});

test("stale cancellation reloads the current job before offering a new cancel", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  await apply(page);
  await expect(page.locator("[data-album-job]")).toBeVisible();
  const prior = remote.current();
  remote.jobs.set(prior.job_uuid, {
    ...prior,
    state: "running",
    revision: prior.revision + 1,
  });
  await page
    .getByRole("button", { name: "Cancel pending work", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Review album again", exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  await page
    .getByRole("button", { name: "Review album again", exact: true })
    .click();
  await expect(
    page.locator("[data-album-job]").getByText("Running", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Cancel pending work", exact: true })
    .click();
  await expect(
    page.locator("[data-album-job]").getByText("Cancelled", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(2);
  expect(JSON.parse(remote.writes[1]!.body)).toEqual({
    expected_revision: prior.revision + 1,
  });
});

test("two open tabs recover one saved Apply request", async ({ page }) => {
  const remote = await archive(page);
  const second = await page.context().newPage();
  const failures: string[] = [];
  second.on("pageerror", (error) => failures.push(error.message));
  await remote.attach(second);
  await open(page, true);
  await open(second, true);
  await preview(page);
  await preview(second);
  remote.loseReply();
  await page
    .getByRole("button", { name: "Apply reviewed album changes", exact: true })
    .click();
  await expect(
    page.getByText("An album request needs confirmation", { exact: true }),
  ).toBeVisible();
  await second
    .getByRole("button", { name: "Apply reviewed album changes", exact: true })
    .click();
  await expect(second.locator("[data-album-job]")).toBeVisible();
  await page
    .getByRole("button", { name: "Check and retry saved request", exact: true })
    .click();
  await expect(page.locator("[data-album-job]")).toBeVisible();
  expect(remote.writes).toHaveLength(1);
  expect(failures).toEqual([]);
  await second.close();
});

test("changing matching policy clears the old preview and no-op or blocked previews cannot apply", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  await preview(page);
  await page
    .getByRole("button", { name: "Reddit file names", exact: true })
    .click();
  await expect(page.locator("[data-album-preview]")).toHaveCount(0);
  await preview(page);
  expect(remote.previews).toEqual([
    "source-identifiers-v1",
    "legacy-reddit-filename-v1",
  ]);
  for (const action of ["disabled", "ineligible", "review"] as const) {
    remote.action(action);
    await preview(page);
    await expect(
      page.getByRole("button", {
        name: "Apply reviewed album changes",
        exact: true,
      }),
    ).toHaveCount(0);
  }
  remote.action("sync");
  remote.noop();
  await preview(page);
  await expect(
    page.getByText("No automatic changes are needed for this preview.", {
      exact: true,
    }),
  ).toBeVisible();
  expect(remote.writes).toEqual([]);
});

test("post-page publication refreshes its gallery association and preserves previewed creation metadata", async ({
  page,
}) => {
  const remote = await archive(page, true);
  await open(page, true, true);
  await page
    .getByRole("button", { name: "Album gallery", exact: true })
    .click();
  await expect(
    page.getByText("No associated album gallery", { exact: true }),
  ).toBeVisible();
  const before = remote.requests.filter((url) =>
    url.pathname.endsWith("/album"),
  ).length;
  await preview(page);
  await page
    .getByRole("button", { name: "New gallery metadata", exact: true })
    .click();
  await expect(
    page.getByText("A day at the coast", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Apply reviewed album changes", exact: true })
    .click();
  await expect(page.locator("[data-album-job]")).toBeVisible();
  remote.publish();
  await page
    .getByRole("button", { name: "Refresh album status", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: "Open gallery", exact: true }),
  ).toHaveAttribute("href", "/galleries/12");
  const refreshed = remote.requests.filter((url) =>
    url.pathname.endsWith("/album"),
  ).length;
  expect(refreshed).toBeGreaterThan(before);
  await page
    .getByRole("button", { name: "Refresh album status", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Refresh album status", exact: true }),
  ).toBeEnabled();
  expect(
    remote.requests.filter((url) => url.pathname.endsWith("/album")),
  ).toHaveLength(refreshed);
});

test("job history is lazy and paginated and worker attempts load independently", async ({
  page,
}) => {
  const remote = await archive(page);
  for (let i = 1; i <= 26; i++) {
    const job = albumJob({
      post_uuid: postIds.post,
      job_uuid: albumReviewID(200 + i),
      sequence: i,
      state: "failed",
      attempts: 1,
      revision: 3,
    });
    remote.jobs.set(job.job_uuid, job);
  }
  await open(page);
  expect(
    remote.requests.some((url) => url.pathname.endsWith("/album-backfills")),
  ).toBe(false);
  await page
    .getByRole("button", { name: "Album job history", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Inspect job", exact: true }),
  ).toHaveCount(25);
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Inspect job", exact: true }),
  ).toHaveCount(26);
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/album-backfills"))
      .at(-1)
      ?.searchParams.get("after"),
  ).toBe("25");
  await page
    .getByRole("button", { name: "Inspect job", exact: true })
    .first()
    .click();
  await expect(page.locator("[data-album-job]")).toBeVisible();
  expect(
    remote.requests.some((url) => url.pathname.endsWith("/attempts")),
  ).toBe(false);
  await page
    .getByRole("button", { name: "Worker attempts", exact: true })
    .click();
  await expect(page.getByText("Attempt 1", { exact: true })).toBeVisible();
  expect(remote.writes).toEqual([]);
});
