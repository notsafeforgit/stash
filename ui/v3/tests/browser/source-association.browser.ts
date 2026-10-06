import type { Page, Route } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { albumPage, albumUUID } from "../fixtures/source-albums";
import { postIds, postSummary, postAlbum } from "../fixtures/source-posts";
import {
  galleryPreview,
  mediaPreview,
  galleryReceipt,
  mediaReceipt,
  ids,
} from "../fixtures/source-association";
import {
  galleryAssociationInputSchema,
  galleryAssociationApplySchema,
  attachmentMediaInputSchema,
  attachmentMediaApplySchema,
  type GalleryAssociationReceipt,
  type AttachmentMediaReceipt,
  type AttachmentMediaContext,
  type GalleryAssociationDecision,
  type AttachmentMediaDecision,
} from "../../src/core/native-archive/association-review-api";
import type {
  PostAlbum,
  PostLibraryItem,
} from "../../src/core/native-archive/source-post-api";

test.use({
  expectedConsoleErrors: [
    "net::ERR_FAILED",
    "Load failed",
    "the server responded with a status of 409",
    "the server responded with a status of 404",
    "the server responded with a status of 503",
  ],
});
type Family = "gallery" | "attachment";
const attachment = albumUUID(100);
const galleryTarget: PostLibraryItem = { ...galleryPreview().proposed! };
const mediaTarget: PostLibraryItem = { ...mediaPreview().proposed! };
async function archive(page: Page) {
  const requests: URL[] = [],
    writes: string[] = [],
    previews: unknown[] = [],
    searches: string[] = [];
  const receipts = new Map<
    string,
    GalleryAssociationReceipt | AttachmentMediaReceipt
  >();
  const galleryHistory: GalleryAssociationDecision[] = [],
    mediaHistory: AttachmentMediaDecision[] = [];
  let revision = 10,
    loseReply = false,
    wrongReceipt = false,
    conflict = "",
    failRefresh = false;
  let gallery: PostAlbum = postAlbum();
  const media: AttachmentMediaContext = {
    ...mediaPreview().current,
    post_uuid: postIds.post,
    post_revision: revision,
    attachment: {
      uuid: attachment,
      revision: 2,
      reference: { namespace: "native:reddit", value: "media-0" },
    },
  };
  let release: (() => void) | undefined, held: Promise<void> | undefined;
  async function handler(route: Route) {
    const request = route.request(),
      url = new URL(request.url());
    requests.push(url);
    const path = url.pathname.split("/archive/")[1]!;
    if (path === "gallery-association/preview") {
      const input = galleryAssociationInputSchema.parse(request.postDataJSON());
      const proposed =
        input.state === "linked"
          ? input.gallery_uuid === galleryTarget.uuid
            ? galleryTarget
            : postAlbum().gallery
          : null;
      const result = {
        input,
        current: gallery,
        proposed,
        changed:
          gallery.state !== input.state ||
          gallery.gallery_uuid !== (input.gallery_uuid ?? null) ||
          gallery.reason !== (input.reason ?? ""),
        digest: "a".repeat(64),
      };
      previews.push(result);
      return route.fulfill({ json: result });
    }
    if (path === "attachment-media/preview") {
      const input = attachmentMediaInputSchema.parse(request.postDataJSON());
      const proposed = input.state === "linked" ? mediaTarget : null;
      const result = {
        input,
        current: { ...media, post_revision: revision },
        proposed,
        changed:
          media.current?.state !== input.state ||
          media.current?.media_uuid !== (input.media_uuid ?? null) ||
          media.current?.reason !== (input.reason ?? ""),
        digest: "b".repeat(64),
      };
      previews.push(result);
      return route.fulfill({ json: result });
    }
    if (
      path === "gallery-association/apply" ||
      path === "attachment-media/apply"
    ) {
      const body = request.postData()!;
      writes.push(body);
      const input = path.startsWith("gallery")
        ? galleryAssociationApplySchema.parse(request.postDataJSON())
        : attachmentMediaApplySchema.parse(request.postDataJSON());
      if (conflict || input.post_revision !== revision)
        return route.fulfill({
          status: 409,
          json: { error: conflict || "preview_changed" },
        });
      revision++;
      let receipt: GalleryAssociationReceipt | AttachmentMediaReceipt;
      if ("attachment_uuid" in input) {
        receipt = mediaReceipt(input);
        media.attachment.revision++;
        media.post_revision = revision;
        media.current = {
          uuid: receipt.decision_uuid,
          attachment_uuid: attachment,
          revision: media.attachment.revision,
          state: input.state,
          media_uuid: input.media_uuid ?? null,
          origin: "review",
          reason: input.reason ?? "",
          created_at: receipt.created_at,
        };
        media.media = input.state === "linked" ? mediaTarget : null;
        media.post_link_state = input.state === "linked" ? "undecided" : "";
        mediaHistory.push(media.current);
      } else {
        receipt = galleryReceipt(input);
        gallery = {
          post_uuid: postIds.post,
          decision_uuid: receipt.decision_uuid,
          revision,
          state: input.state,
          gallery_uuid: input.gallery_uuid ?? null,
          gallery: input.state === "linked" ? galleryTarget : null,
          selection_uuid: null,
          origin: "review",
          reason: input.reason ?? "",
          created_at: receipt.created_at,
        };
        galleryHistory.push({ ...gallery, gallery: null, origin: "review" });
      }
      receipts.set(input.request_uuid, receipt);
      if (held) await held;
      if (loseReply) {
        loseReply = false;
        return route.abort("failed");
      }
      return route.fulfill({ json: { review: receipt, replayed: false } });
    }
    if (request.method() !== "GET") throw new Error(`Unexpected write ${path}`);
    if (path.includes("/requests/")) {
      const receipt = receipts.get(path.split("/")[2]!);
      if (!receipt)
        return route.fulfill({ status: 404, json: { error: "not_found" } });
      return route.fulfill({
        json: wrongReceipt
          ? {
              ...receipt,
              request: { ...receipt.request, post_uuid: postIds.otherPost },
            }
          : receipt,
      });
    }
    let result: unknown;
    if (path === `posts/${postIds.post}`) {
      if (failRefresh && receipts.size)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      result = { ...postSummary(), revision };
    } else if (path === `attachments/${attachment}/review`) {
      if (failRefresh && receipts.size)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      result = { ...media, post_revision: revision };
    } else if (path.endsWith("/gallery-association-history"))
      result = galleryHistory
        .filter((item) => item.revision > Number(url.searchParams.get("after")))
        .slice(0, 25);
    else if (path.endsWith("/media-history"))
      result = mediaHistory
        .filter((item) => item.revision > Number(url.searchParams.get("after")))
        .slice(0, 25);
    else if (path === "entity-identities/gallery/12")
      result = {
        uuid: postIds.gallery,
        kind: "gallery",
        local_id: 12,
        revision: 1,
      };
    else if (path === "entity-identities/gallery/42")
      result = {
        uuid: ids.gallery,
        kind: "gallery",
        local_id: 42,
        revision: 2,
      };
    else if (path === "entity-identities/scene/31")
      result = { uuid: ids.media, kind: "scene", local_id: 31, revision: 4 };
    else if (path.endsWith("/album-posts"))
      result = {
        requested_uuid: postIds.gallery,
        gallery: postAlbum().gallery,
        posts: [postSummary()],
      };
    else if (path.endsWith("/album-media")) {
      const value = albumPage();
      value.post_revision = revision;
      value.album = gallery;
      if (media.current)
        value.slots = value.slots.map((slot) =>
          slot.attachment?.uuid !== attachment
            ? slot
            : {
                ...slot,
                attachment: {
                  ...slot.attachment,
                  revision: media.attachment.revision,
                },
                selection_state: media.current!.state,
                decision_uuid: media.current!.uuid,
                media: media.media,
                post_link_state: media.post_link_state,
                gallery_membership: "absent",
                registered_files: 0,
              },
        );
      result = value;
    } else if (path.endsWith("/album")) result = gallery;
    else throw new Error(`Unexpected association read ${path}`);
    return route.fulfill({ json: result });
  }
  async function graphql(route: Route) {
    const body = route.request().postDataJSON();
    expect(body.variables.filter).toMatchObject({ page: 1, per_page: 25 });
    searches.push(body.operationName);
    if (body.operationName === "FindGalleriesForSelect")
      return route.fulfill({
        json: {
          data: {
            findGalleries: {
              count: 1,
              galleries: [
                {
                  id: "42",
                  title: "Existing album",
                  folder: null,
                  files: [],
                  __typename: "Gallery",
                },
              ],
              __typename: "FindGalleriesResultType",
            },
          },
        },
      });
    if (body.operationName === "FindScenesForSelect")
      return route.fulfill({
        json: {
          data: {
            findScenes: {
              count: 1,
              scenes: [
                {
                  id: "31",
                  title: "Converted animation",
                  files: [
                    { path: "/library/converted.mp4", __typename: "VideoFile" },
                  ],
                  __typename: "Scene",
                },
              ],
              __typename: "FindScenesResultType",
            },
          },
        },
      });
    throw new Error(`Unexpected search ${body.operationName}`);
  }
  await page.route("**/api/v3/archive/**", handler);
  await page.route("**/graphql", graphql);
  return {
    requests,
    writes,
    previews,
    searches,
    galleryHistory,
    mediaHistory,
    attach: async (other: Page) => {
      await other.route("**/api/v3/archive/**", handler);
      await other.route("**/graphql", graphql);
    },
    loseReply: () => {
      loseReply = true;
    },
    wrongReceipt: () => {
      wrongReceipt = true;
    },
    conflict: (value: string) => {
      conflict = value;
    },
    failRefresh: (value: boolean) => {
      failRefresh = value;
    },
    hold: () => {
      held = new Promise<void>((resolve) => {
        release = resolve;
      });
    },
    release: () => {
      release?.();
      held = undefined;
    },
  };
}
async function open(
  page: Page,
  family: Family,
  desktop = false,
  recovery = false,
) {
  if (desktop) await page.setViewportSize({ width: 1280, height: 1000 });
  await page.goto("/source-albums");
  if (desktop)
    await page.getByRole("tab", { name: "Source albums", exact: true }).click();
  else await chooseSection(page, "Source albums");
  await page
    .getByRole("button", {
      name: postSummary().latest_capture!.title!,
      exact: true,
    })
    .click();
  if (family === "gallery")
    await page
      .getByRole("button", { name: "Edit gallery link", exact: true })
      .click();
  else
    await page
      .locator('[data-source-position="0"]')
      .getByRole("button", { name: "Edit media link", exact: true })
      .click();
  if (recovery)
    await expect(
      page.getByRole("button", { name: "Recover saved request", exact: true }),
    ).toBeVisible();
  else
    await expect(
      page.getByRole("group", { name: "Association behavior", exact: true }),
    ).toBeVisible();
}
async function choose(page: Page, family: Family) {
  const picker = page.getByRole("combobox", {
    name: family === "gallery" ? "Existing gallery" : "Existing media",
    exact: true,
  });
  await picker.click();
  await picker.fill(family === "gallery" ? "Existing" : "Converted");
  await page
    .getByRole("option", {
      name:
        family === "gallery"
          ? "Existing album (#42)"
          : "Converted animation (#31)",
      exact: true,
    })
    .click();
}
async function preview(page: Page) {
  await page
    .getByRole("button", { name: "Preview association", exact: true })
    .click();
  await expect(
    page.getByText("Proposed choice", { exact: true }),
  ).toBeVisible();
}
async function apply(page: Page, family: Family) {
  await choose(page, family);
  await preview(page);
  await page
    .getByRole("button", { name: "Save association choice", exact: true })
    .click();
}

for (const family of ["gallery", "attachment"] as const) {
  for (const desktop of [false, true]) {
    test(`${family} association preview and save on ${desktop ? "desktop" : "phone"}`, async ({
      page,
    }) => {
      const remote = await archive(page);
      await open(page, family, desktop);
      expect(remote.previews).toHaveLength(0);
      expect(remote.writes).toHaveLength(0);
      expect(
        remote.requests.some(
          (url) =>
            url.pathname.endsWith("/media-history") ||
            url.pathname.endsWith("/gallery-association-history"),
        ),
      ).toBe(false);
      await choose(page, family);
      await preview(page);
      expect(remote.writes).toHaveLength(0);
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        )
        .toBe(true);
      await page.screenshot({
        path: test
          .info()
          .outputPath(`${family}-${desktop ? "desktop" : "phone"}.png`),
      });
      await page
        .getByRole("button", { name: "Save association choice", exact: true })
        .click();
      await expect(
        page.getByText("Association choice saved", { exact: true }),
      ).toBeVisible();
      expect(remote.writes).toHaveLength(1);
      await expect(page.locator("[data-album-refresh-count]")).toHaveAttribute(
        "data-album-refresh-count",
        "1",
      );
      await page
        .getByRole("button", { name: "Association history", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Decision identifiers", exact: true }),
      ).toBeVisible();
    });
  }
  test(`${family} lost reply requires explicit recovery`, async ({ page }) => {
    const remote = await archive(page);
    remote.loseReply();
    await open(page, family);
    await apply(page, family);
    await expect(
      page.getByRole("button", { name: "Recover saved request", exact: true }),
    ).toBeVisible();
    await open(page, family, false, true);
    expect(remote.writes).toHaveLength(1);
    await page
      .getByRole("button", { name: "Recover saved request", exact: true })
      .click();
    await expect(
      page.getByText("Association choice saved", { exact: true }),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(1);
  });
  test(`${family} stale requests can be reviewed again while uncertain conflicts remain`, async ({
    page,
  }) => {
    const remote = await archive(page);
    remote.conflict("request_conflict");
    await open(page, family);
    await apply(page, family);
    await expect(
      page.getByRole("button", { name: "Recover saved request", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Review again", exact: true }),
    ).toHaveCount(0);
    remote.conflict("preview_changed");
    await page
      .getByRole("button", { name: "Recover saved request", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Review again", exact: true })
      .click();
    await expect(
      page.getByRole("button", {
        name: "Save association choice",
        exact: true,
      }),
    ).toHaveCount(0);
    remote.conflict("");
    await apply(page, family);
    await expect(
      page.getByText("Association choice saved", { exact: true }),
    ).toBeVisible();
  });
  test(`${family} wrong receipt cannot clear saved intent`, async ({
    page,
  }) => {
    const remote = await archive(page);
    remote.loseReply();
    await open(page, family);
    await apply(page, family);
    await expect(
      page.getByRole("button", { name: "Recover saved request", exact: true }),
    ).toBeVisible();
    remote.wrongReceipt();
    await page
      .getByRole("button", { name: "Recover saved request", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Recover saved request", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Review again", exact: true }),
    ).toHaveCount(0);
    expect(remote.writes).toHaveLength(1);
  });
  test(`${family} reports refresh failure separately from its successful save`, async ({
    page,
  }) => {
    const remote = await archive(page);
    remote.failRefresh(true);
    await open(page, family);
    await apply(page, family);
    await expect(
      page.getByText(
        "The choice is saved, but this view could not be refreshed",
        { exact: true },
      ),
    ).toBeVisible();
    expect(remote.writes).toHaveLength(1);
    remote.failRefresh(false);
    await page.getByRole("button", { name: "Retry", exact: true }).click();
    await expect(
      page.getByRole("group", { name: "Association behavior", exact: true }),
    ).toBeVisible();
  });
}

test("attachment rejection and automatic choice omit media targets", async ({
  page,
}) => {
  const remote = await archive(page);
  await open(page, "attachment");
  await page.getByRole("button", { name: "Reject", exact: true }).click();
  await preview(page);
  await page
    .getByRole("button", { name: "Save association choice", exact: true })
    .click();
  await expect(
    page.getByText("Association choice saved", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Automatic", exact: true }).click();
  await preview(page);
  await page
    .getByRole("button", { name: "Save association choice", exact: true })
    .click();
  await expect.poll(() => remote.writes.length).toBe(2);
  expect(remote.writes.map((value) => JSON.parse(value).state)).toEqual([
    "unlinked",
    "undecided",
  ]);
  expect(
    remote.writes.every((value) => !("media_uuid" in JSON.parse(value))),
  ).toBe(true);
});
test("closing during attachment delivery preserves the request and refreshes its slots", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.hold();
  await open(page, "attachment");
  await apply(page, "attachment");
  await expect.poll(() => remote.writes.length).toBe(1);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Close", exact: true })
    .click();
  remote.release();
  await expect(page.locator("[data-album-refresh-count]")).toHaveAttribute(
    "data-album-refresh-count",
    "1",
  );
  await expect(page.locator('[data-source-position="0"]')).toContainText(
    "Converted animation",
  );
  await expect(page.locator('[data-source-position="2"]')).toContainText(
    "Converted animation",
  );
});
