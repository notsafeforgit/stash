import { emptyDownloadStatus } from "../fixtures/downloads";
import type { Page } from "@playwright/test";
import { test, expect, chooseSection } from "./test";
import { serveSceneMedia } from "./scene-media";
import { albumPage, albumSlot } from "../fixtures/source-albums";
import { postAlbum, postIds, postSummary } from "../fixtures/source-posts";
import { playbackImage, playbackScene } from "../fixtures/album-playback";
import type { AlbumPage } from "../../src/core/native-archive/source-album-api";

test.use({
  expectedConsoleErrors: ["the server responded with a status of 503"],
});

async function archive(
  page: Page,
  first: AlbumPage = albumPage(),
  following?: AlbumPage,
) {
  const operations: string[] = [],
    pages: number[] = [];
  let unavailable = false,
    failNext = false,
    holdScene = false;
  let release: (() => void) | undefined;
  await serveSceneMedia(page);
  await page.route("**/api/v3/archive/**", async (route) => {
    if (
      new URL(route.request().url()).pathname.endsWith(
        "/attachments/download-status",
      )
    )
      return route.fulfill({
        json: emptyDownloadStatus(route.request().postDataJSON().attachments),
      });
    expect(route.request().method()).toBe("GET");
    const url = new URL(route.request().url());
    const path = url.pathname.split("/archive/")[1]!;
    if (path.endsWith("entity-identities/gallery/12"))
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
    if (path.endsWith("/album-media")) {
      const after = Number(url.searchParams.get("after") ?? -1);
      pages.push(after);
      if (after >= 0 && failNext)
        return route.fulfill({ status: 503, json: { error: "unavailable" } });
      return route.fulfill({ json: after >= 0 ? following! : first });
    }
    throw new Error(`Unexpected archive read ${path}`);
  });
  await page.route("**/graphql", async (route) => {
    const body = route.request().postDataJSON();
    operations.push(body.operationName);
    expect(body.query.trim().startsWith("query")).toBe(true);
    if (body.operationName === "FindImage")
      return route.fulfill({
        json: {
          data: {
            findImage: unavailable ? null : playbackImage(body.variables.id),
          },
        },
      });
    if (body.operationName === "FindScene") {
      if (holdScene)
        await new Promise<void>((resolve) => {
          release = resolve;
        });
      return route.fulfill({
        json: {
          data: {
            findScene: unavailable
              ? null
              : playbackScene(route.request().url(), body.variables.id),
          },
        },
      });
    }
    throw new Error(`Unexpected GraphQL operation ${body.operationName}`);
  });
  return {
    operations,
    pages,
    unavailable: (value: boolean) => {
      unavailable = value;
    },
    failNext: (value: boolean) => {
      failNext = value;
    },
    holdScene: () => {
      holdScene = true;
    },
    release: () => release?.(),
  };
}
async function open(page: Page, desktop = false) {
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
  await page
    .getByRole("button", { name: "View album in source order", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Source album viewer" }),
  ).toBeVisible();
}
const viewer = (page: Page) =>
  page.getByRole("dialog", { name: "Source album viewer" });
async function next(page: Page, position: number) {
  await viewer(page)
    .getByRole("button", { name: "Next position", exact: true })
    .click();
  await expect(
    viewer(page).locator("[data-album-viewer-position]"),
  ).toHaveAttribute("data-album-viewer-position", String(position));
}
for (const desktop of [false, true]) {
  test(`mixed image/video source order on ${desktop ? "desktop" : "phone"}`, async ({
    page,
  }) => {
    const remote = await archive(page);
    await open(page, desktop);
    await expect(viewer(page).locator("img.yarl__slide_image")).toBeVisible();
    await expect(
      viewer(page).getByRole("button", { name: "Zoom in", exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: test
        .info()
        .outputPath(`image-${desktop ? "desktop" : "phone"}.png`),
      animations: "disabled",
    });
    await next(page, 1);
    const video = viewer(page).locator("video").first();
    await expect
      .poll(() => video.evaluate((node: HTMLVideoElement) => node.readyState))
      .toBeGreaterThanOrEqual(2);
    await video.evaluate((node: HTMLVideoElement) => node.play());
    await expect
      .poll(() => video.evaluate((node: HTMLVideoElement) => node.currentTime))
      .toBeGreaterThan(0.1);
    await page.screenshot({
      path: test
        .info()
        .outputPath(`video-${desktop ? "desktop" : "phone"}.png`),
      animations: "disabled",
    });
    await next(page, 2);
    await expect(viewer(page).locator("img.yarl__slide_image")).toBeVisible();
    await expect(viewer(page).locator("video")).toHaveCount(0);
    await next(page, 3);
    await expect(
      viewer(page).getByText("Positions 4–6", { exact: true }),
    ).toBeVisible();
    await expect(
      viewer(page).getByText(
        "The source has not described these positions yet.",
        { exact: true },
      ),
    ).toBeVisible();
    await next(page, 6);
    await expect(
      viewer(page).getByText(
        "No library media is selected for this source attachment.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(
      viewer(page).getByRole("button", { name: "Next position", exact: true }),
    ).toBeDisabled();
    expect(remote.operations).toEqual(["FindImage", "FindScene", "FindImage"]);
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
  });
}

test("rejected, conflicting, excluded and deleted positions never fetch media", async ({
  page,
}) => {
  const first = albumPage();
  first.slots = [0, 1, 2, 3].map(albumSlot);
  first.slots[0]!.post_link_state = "unlinked";
  first.slots[1]!.post_link_state = "conflict";
  first.slots[2]!.gallery_membership = "excluded";
  first.slots[3]!.media = {
    ...first.slots[3]!.media!,
    state: "deleted",
    local_id: null,
  };
  first.slots[3]!.registered_files = 0;
  first.selection = {
    ...first.selection!,
    entry_count: 4,
    expected_count: 4,
    complete: true,
  };
  const remote = await archive(page, first);
  await open(page);
  for (const [index, text] of [
    "This source link was explicitly rejected.",
    "Resolve the conflicting source links before playing this position.",
    "This media was manually excluded from the gallery.",
    "The linked media was deleted from the library.",
  ].entries()) {
    if (index) await next(page, index);
    await expect(viewer(page).getByText(text, { exact: true })).toBeVisible();
  }
  expect(remote.operations).toEqual([]);
});
test("media disappearing after the album read retains its position and offers retry", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.unavailable(true);
  await open(page);
  await expect(
    viewer(page).getByText("This media is unavailable", { exact: true }),
  ).toBeVisible();
  remote.unavailable(false);
  await viewer(page)
    .getByRole("button", { name: "Retry", exact: true })
    .click();
  await expect(viewer(page).locator("img.yarl__slide_image")).toBeVisible();
});
test("a late scene response cannot play after navigating away", async ({
  page,
}) => {
  const remote = await archive(page);
  remote.holdScene();
  await open(page);
  await next(page, 1);
  await expect.poll(() => remote.operations.includes("FindScene")).toBe(true);
  await next(page, 2);
  remote.release();
  await expect(viewer(page).locator("img.yarl__slide_image")).toBeVisible();
  await expect(viewer(page).locator("video")).toHaveCount(0);
});

function paged() {
  const first = albumPage();
  first.slots = Array.from({ length: 25 }, (_, index) => ({
    ...albumSlot(index),
    registered_files: 0,
  }));
  first.selection = {
    ...first.selection!,
    entry_count: 26,
    expected_count: 26,
    complete: true,
  };
  first.next_after = 24;
  const last: AlbumPage = {
    ...first,
    slots: [{ ...albumSlot(25), registered_files: 0 }],
    next_after: null,
  };
  return { first, last };
}
test("source pages load only when reached and retry without losing position", async ({
  page,
}) => {
  const { first, last } = paged();
  const remote = await archive(page, first, last);
  await open(page);
  for (let index = 1; index < 25; index++) await next(page, index);
  expect(remote.pages.every((after) => after === -1)).toBe(true);
  remote.failNext(true);
  await next(page, 25);
  await expect(
    viewer(page).getByRole("button", { name: "Retry", exact: true }),
  ).toBeVisible();
  remote.failNext(false);
  await viewer(page)
    .getByRole("button", { name: "Retry", exact: true })
    .click();
  await expect(
    viewer(page).getByText("The selected media has no registered files.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    viewer(page).getByRole("button", { name: "Next position", exact: true }),
  ).toBeDisabled();
  expect(remote.pages.filter((after) => after >= 0)).toEqual([24, 24]);
  expect(remote.operations).toEqual([]);
});
test("a changed source list stops playback before appending another order", async ({
  page,
}) => {
  const { first, last } = paged();
  last.signature = "b".repeat(64);
  const remote = await archive(page, first, last);
  await open(page);
  for (let index = 1; index < 25; index++) await next(page, index);
  await viewer(page)
    .getByRole("button", { name: "Next position", exact: true })
    .click();
  await expect(
    viewer(page).getByText("This album changed", { exact: true }),
  ).toBeVisible();
  await expect(viewer(page).locator("video,img")).toHaveCount(0);
  await viewer(page)
    .getByRole("button", { name: "Return to album", exact: true })
    .click();
  await expect(viewer(page)).toHaveCount(0);
  expect(remote.operations).toEqual([]);
});

test("adjacent videos keep one player and closing releases playback", async ({
  page,
}) => {
  const first = albumPage();
  const video = first.slots[1]!;
  first.slots = [
    { ...video, position: 0, through: 0 },
    {
      ...video,
      position: 1,
      through: 1,
      media: { ...video.media!, local_id: 9, uuid: postIds.media },
    },
  ];
  first.selection = {
    ...first.selection!,
    entry_count: 2,
    expected_count: 2,
    complete: true,
  };
  await archive(page, first);
  await open(page);
  const player = viewer(page).locator("video").first();
  await expect
    .poll(() => player.evaluate((node: HTMLVideoElement) => node.readyState))
    .toBeGreaterThanOrEqual(2);
  const original = await player.elementHandle();
  await player.evaluate((node: HTMLVideoElement) => node.play());
  await next(page, 1);
  await expect
    .poll(() => player.evaluate((node: HTMLVideoElement) => node.currentSrc))
    .toContain("/scene/9/stream");
  expect(await player.evaluate((node, prior) => node === prior, original)).toBe(
    true,
  );
  await viewer(page)
    .getByRole("button", { name: "Close", exact: true })
    .click();
  await expect(viewer(page)).toHaveCount(0);
  await expect
    .poll(() =>
      original!.evaluate(
        (node: HTMLVideoElement) => node.paused && !node.isConnected,
      ),
    )
    .toBe(true);
});

test("mobile images zoom inside the album and reset at another position", async ({
  page,
}) => {
  const first = albumPage();
  first.slots = [albumSlot(0), albumSlot(1)];
  first.selection = {
    ...first.selection!,
    entry_count: 2,
    expected_count: 2,
    complete: true,
  };
  await archive(page, first);
  await open(page);
  const image = viewer(page).locator("img.yarl__slide_image");
  await expect(image).toBeVisible();
  const width = (await image.boundingBox())!.width;
  await viewer(page)
    .getByRole("button", { name: "Zoom in", exact: true })
    .click();
  await expect
    .poll(async () => (await image.boundingBox())!.width)
    .toBeGreaterThan(width * 1.2);
  await next(page, 1);
  await expect(image).toBeVisible();
  await expect
    .poll(async () => (await image.boundingBox())!.width)
    .toBeLessThan(width * 1.1);
});
