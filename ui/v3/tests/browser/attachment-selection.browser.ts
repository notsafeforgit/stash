import type { Page, Route } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { albumPage, albumUUID } from "../fixtures/source-albums";
import { postIds, postSummary, postAlbum } from "../fixtures/source-posts";
import {
  preview as samplePreview,
  receipt as sampleReceipt,
} from "../fixtures/attachment-selection";
import {
  selectionInputSchema,
  selectionApplySchema,
  type SelectionReceipt,
  type SelectionDecision,
  type SelectionManifest,
  type SelectionList,
  type SelectionPreview,
} from "../../src/core/native-archive/attachment-selection-api";

test.use({
  expectedConsoleErrors: [
    "net::ERR_FAILED",
    "Load failed",
    "the server responded with a status of 409",
    "the server responded with a status of 404",
    "the server responded with a status of 503",
  ],
});

async function archive(page: Page) {
  const requests: URL[] = [],
    writes: string[] = [],
    previews: SelectionPreview[] = [];
  const receipts = new Map<string, SelectionReceipt>();
  const history: SelectionDecision[] = [];
  const manifests: SelectionManifest[] = [
    {
      uuid: albumUUID(600),
      capture_uuid: albumUUID(700),
      complete: false,
      declared_album: true,
      entry_count: 2,
    },
  ];
  let revision = 4,
    loseReply = false,
    wrongReceipt = false,
    conflict = "",
    failRefresh = false;
  let current: SelectionList | null = null;
  let release: (() => void) | undefined;
  let held: Promise<void> | undefined;
  async function handler(route: Route) {
    const request = route.request(),
      url = new URL(request.url());
    requests.push(url);
    const path = url.pathname.split("/archive/")[1]!;
    if (path === "attachment-selection/preview") {
      const input = selectionInputSchema.parse(request.postDataJSON());
      const manifest = manifests.find(
        (item) => item.capture_uuid === input.capture_uuid,
      );
      const result = samplePreview(input.mode === "disabled");
      result.input = input;
      result.current = current;
      result.proposed.mode = input.mode;
      result.proposed.reason = input.reason ?? "";
      if (manifest) {
        result.proposed.capture_uuid = manifest.capture_uuid;
        result.proposed.manifest_uuids = [manifest.uuid];
      }
      previews.push(result);
      await route.fulfill({ json: result });
      return;
    }
    if (path === "attachment-selection/apply") {
      const body = request.postData()!,
        input = selectionApplySchema.parse(request.postDataJSON());
      writes.push(body);
      if (conflict || input.post_revision !== revision) {
        await route.fulfill({
          status: 409,
          json: { error: conflict || "preview_changed" },
        });
        return;
      }
      const receipt = sampleReceipt(input);
      receipts.set(input.request_uuid, receipt);
      revision++;
      current = {
        ...previews.at(-1)!.proposed,
        decision_uuid: receipt.decision_uuid,
        revision,
      };
      history.push({
        uuid: receipt.decision_uuid,
        post_uuid: postIds.post,
        revision,
        mode: input.mode,
        origin: "review",
        reason: input.reason ?? "",
        capture_uuid: input.capture_uuid ?? null,
        manifest_uuids: current.manifest_uuids,
        created_at: receipt.created_at,
      });
      if (held) await held;
      if (loseReply) {
        loseReply = false;
        await route.abort("failed");
        return;
      }
      await route.fulfill({ json: { review: receipt, replayed: false } });
      return;
    }
    if (request.method() !== "GET") throw new Error(`Unexpected write ${path}`);
    let result: unknown;
    if (path.startsWith("attachment-selection/requests/")) {
      const receipt = receipts.get(path.split("/")[2]!);
      if (!receipt) {
        await route.fulfill({ status: 404, json: { error: "not_found" } });
        return;
      }
      result = wrongReceipt
        ? {
            ...receipt,
            request: { ...receipt.request, post_uuid: postIds.otherPost },
          }
        : receipt;
    } else if (path.endsWith("/attachment-manifests")) {
      const after = url.searchParams.get("after") ?? "";
      result = manifests.filter((item) => item.uuid > after).slice(0, 25);
    } else if (path.endsWith("/attachment-selection-history")) {
      result = history
        .filter((item) => item.revision > Number(url.searchParams.get("after")))
        .slice(0, 25);
    } else if (path === `posts/${postIds.post}`) {
      if (failRefresh && receipts.size > 0) {
        await route.fulfill({ status: 503, json: { error: "unavailable" } });
        return;
      }
      result = { ...postSummary(), revision };
    } else if (path === "entity-identities/gallery/12") {
      result = {
        uuid: postIds.gallery,
        kind: "gallery",
        local_id: 12,
        revision: 1,
      };
    } else if (path.endsWith("/album-posts")) {
      result = {
        requested_uuid: postIds.gallery,
        gallery: postAlbum().gallery,
        posts: [postSummary()],
      };
    } else if (path.endsWith("/album-media")) {
      const album = albumPage();
      album.post_revision = revision;
      if (current?.mode === "disabled") {
        album.selection = null;
        album.slots = [];
      }
      result = album;
    } else if (path.endsWith("/album")) result = postAlbum();
    else throw new Error(`Unexpected selection read ${path}`);
    await route.fulfill({ json: result });
  }
  await page.route("**/api/v3/archive/**", handler);
  return {
    requests,
    writes,
    previews,
    manifests,
    history,
    attach: (other: Page) => other.route("**/api/v3/archive/**", handler),
    loseReply() {
      loseReply = true;
    },
    wrongReceipt() {
      wrongReceipt = true;
    },
    conflict(value: string) {
      conflict = value;
    },
    failRefresh(value: boolean) {
      failRefresh = value;
    },
    hold() {
      held = new Promise<void>((resolve) => {
        release = resolve;
      });
    },
    release() {
      release?.();
      held = undefined;
    },
  };
}

async function open(page: Page, desktop = false, source = false) {
  if (desktop) await page.setViewportSize({ width: 1280, height: 1000 });
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
        name: postSummary().latest_capture!.title!,
        exact: true,
      })
      .click();
  }
  await page
    .getByRole("button", { name: "Choose source list", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Keep this order", exact: true }),
  ).toBeVisible();
}
async function choose(page: Page) {
  await page
    .getByRole("combobox", { name: "Retained source list", exact: true })
    .click();
  await page
    .getByRole("option", {
      name: "List 1: 2 positions · Partial source list",
      exact: true,
    })
    .click();
}
async function preview(page: Page) {
  await page
    .getByRole("button", { name: "Preview source order", exact: true })
    .click();
  await expect(
    page.locator('[data-selection-details="Proposed choice"]'),
  ).toBeVisible();
}
async function apply(page: Page) {
  await choose(page);
  await preview(page);
  await page
    .getByRole("button", { name: "Save source-list choice", exact: true })
    .click();
}

for (const desktop of [false, true]) {
  test(`source-list preview preserves repeated positions without writes on ${desktop ? "desktop" : "phone"}`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await open(page, desktop);
    expect(remote.previews).toHaveLength(0);
    expect(
      remote.requests.some((url) =>
        url.pathname.endsWith("/attachment-selection-history"),
      ),
    ).toBe(false);
    await choose(page);
    await preview(page);
    await page
      .getByRole("button", { name: "Inspect source positions", exact: true })
      .click();
    const proposed = page.locator('[data-selection-details="Proposed choice"]');
    await expect(
      proposed.getByText("Position 1 · Image", { exact: true }),
    ).toBeVisible();
    await expect(
      proposed.getByText("Position 6 · Image", { exact: true }),
    ).toBeVisible();
    await expect(
      proposed.getByText("native:reddit:original", { exact: true }),
    ).toHaveCount(2);
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    expect(remote.writes).toHaveLength(0);
    await page.screenshot({
      path: test
        .info()
        .outputPath(`selection-${desktop ? "desktop" : "phone"}.png`),
    });
  });
  test(`lost save response requires explicit recovery on ${desktop ? "desktop" : "phone"}`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await open(page, desktop);
    remote.loseReply();
    await apply(page);
    await expect(
      page.getByText("A source-list change needs confirmation", {
        exact: true,
      }),
    ).toBeVisible();
    const original = remote.writes[0]!;
    await open(page, desktop);
    await expect(
      page.getByRole("button", { name: "Preview source order", exact: true }),
    ).toBeDisabled();
    expect(remote.writes).toEqual([original]);
    await page
      .getByRole("button", {
        name: "Check and retry saved change",
        exact: true,
      })
      .click();
    await expect(
      page.getByText("Source-list choice saved", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toEqual([original]);
    await expect(page.locator("[data-album-refresh-count]")).toHaveAttribute(
      "data-album-refresh-count",
      "0",
    );
  });
}

test("a stale choice clears its preview; a request conflict remains pending", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  remote.conflict("preview_changed");
  await apply(page);
  await page.getByRole("button", { name: "Review again", exact: true }).click();
  await expect(page.locator("[data-selection-details]")).toHaveCount(0);
  remote.conflict("request_conflict");
  await apply(page);
  await expect(
    page.getByText("A source-list change needs confirmation", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Review again", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Preview source order", exact: true }),
  ).toBeDisabled();
});

test("a wrong receipt stays pending without another write", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  remote.loseReply();
  await apply(page);
  const recover = page.getByRole("button", {
    name: "Check and retry saved change",
    exact: true,
  });
  await expect(recover).toBeEnabled();
  remote.wrongReceipt();
  await recover.click();
  await expect(
    page.getByText("A source-list change needs confirmation", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Could not complete this step", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toHaveLength(1);
});

test("competing tabs retain the first request instead of replacing it", async ({
  page,
  context,
}) => {
  const remote = await archive(page),
    other = await context.newPage();
  await remote.attach(other);
  await open(page);
  await open(other);
  await choose(other);
  await other
    .getByRole("textbox", { name: "Reason (optional)", exact: true })
    .fill("A different choice");
  await preview(other);
  remote.loseReply();
  await apply(page);
  await expect(
    page.getByText("A source-list change needs confirmation", { exact: true }),
  ).toBeVisible();
  const original = remote.writes[0]!;
  await other
    .getByRole("button", { name: "Save source-list choice", exact: true })
    .click();
  await expect(
    other.getByText("A source-list change needs confirmation", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([original]);
  await other
    .getByRole("button", { name: "Check and retry saved change", exact: true })
    .click();
  await expect(
    other.getByText("Source-list choice saved", { exact: true }),
  ).toBeVisible();
  expect(remote.writes).toEqual([original]);
});

test("saving reports refresh failure separately and never resubmits on refresh", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  remote.failRefresh(true);
  await apply(page);
  await expect(
    page.getByText("Source-list choice saved", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "The choice is saved, but this view could not be refreshed",
      { exact: true },
    ),
  ).toBeVisible();
  remote.failRefresh(false);
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Keep this order", exact: true }),
  ).toBeEnabled();
  expect(remote.writes).toHaveLength(1);
});

test("closing the panel during save still refreshes source order without a premature recovery prompt", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page);
  remote.hold();
  await apply(page);
  await expect.poll(() => remote.writes.length).toBe(1);
  await expect(
    page.getByText("A source-list change needs confirmation", { exact: true }),
  ).toHaveCount(0);
  const before = remote.requests.filter((url) =>
    url.pathname.endsWith("/album-media"),
  ).length;
  await page
    .getByRole("button", { name: "Choose source list", exact: true })
    .click();
  remote.release();
  await expect
    .poll(
      () =>
        remote.requests.filter((url) => url.pathname.endsWith("/album-media"))
          .length,
    )
    .toBeGreaterThan(before);
  await page
    .getByRole("button", { name: "Choose source list", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Keep this order", exact: true }),
  ).toBeEnabled();
  expect(remote.writes).toHaveLength(1);
});

test("disabled selection works with no lists and refreshes the post source order", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.manifests.length = 0;
  await open(page, false, true);
  await expect(
    page.getByText(
      "No source attachment lists have been retained for this post.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Preview source order", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Disable source selection", exact: true })
    .click();
  await preview(page);
  await page
    .getByRole("button", { name: "Save source-list choice", exact: true })
    .click();
  await expect(
    page.getByText("Source-list choice saved", { exact: true }),
  ).toBeVisible();
  expect(JSON.parse(remote.writes[0]!)).not.toHaveProperty("capture_uuid");
  await expect(page.locator("[data-source-position]")).toHaveCount(0);
});

test("unique lists and immutable history use independent bounded pages", async ({
  page,
}) => {
  const remote = await archive(page);
  for (let index = 1; index < 26; index++)
    remote.manifests.push({
      ...remote.manifests[0]!,
      uuid: albumUUID(600 + index),
      capture_uuid: albumUUID(700 + index),
    });
  for (let index = 1; index <= 26; index++)
    remote.history.push({
      uuid: albumUUID(800 + index),
      post_uuid: postIds.post,
      revision: index,
      mode: "automatic",
      origin: "ingest",
      reason: `Historical choice ${index}`,
      capture_uuid: albumUUID(700),
      manifest_uuids: Array.from({ length: 27 }, (_, index) =>
        albumUUID(600 + index),
      ),
      created_at: "2026-10-06T14:00:00Z",
    });
  await open(page);
  await page
    .getByRole("button", { name: "Load more source lists", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Load more source lists", exact: true }),
  ).toHaveCount(0);
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/attachment-manifests"))
      .at(-1)
      ?.searchParams.get("after"),
  ).toBe(albumUUID(624));
  await page
    .getByRole("button", { name: "Source-list history", exact: true })
    .click();
  await expect(
    page.getByText("Historical choice 26", { exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(
    page.getByText("Historical choice 26", { exact: true }),
  ).toBeVisible();
  expect(
    remote.requests
      .filter((url) => url.pathname.endsWith("/attachment-selection-history"))
      .at(-1)
      ?.searchParams.get("after"),
  ).toBe("25");
  await page
    .getByRole("button", { name: "Source references", exact: true })
    .first()
    .click();
  await expect(page.getByText(albumUUID(625), { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(page.getByText(albumUUID(625), { exact: true })).toBeVisible();
  expect(remote.writes).toHaveLength(0);
});
