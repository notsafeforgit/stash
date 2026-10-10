import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";
import { playbackImage, playbackScene } from "../fixtures/album-playback";
import type { GalleryMediaItem } from "../../src/components/detail/gallery-media";

async function fixture(
  page: Page,
  options: {
    changed?: boolean;
    videoOnly?: boolean;
    empty?: boolean;
    failNext?: boolean;
  } = {},
) {
  const pages: number[] = [];
  const image = (id: string): GalleryMediaItem => ({
    __typename: "GalleryMediaItem",
    image: playbackImage(id),
    scene: null,
    source_post_uuid: null,
    source_position: null,
  });
  const video: GalleryMediaItem = {
    __typename: "GalleryMediaItem",
    image: null,
    scene: {
      __typename: "Scene",
      id: "8",
      title: "Second video",
      paths: {
        __typename: "ScenePathsType",
        screenshot: playbackImage().paths.thumbnail,
      },
    },
    source_post_uuid: null,
    source_position: null,
  };
  await serveSceneMedia(page);
  await page.route("**/graphql", async (route) => {
    const body = route.request().postDataJSON();
    expect(body.query.trim().startsWith("query")).toBe(true);
    if (body.operationName === "FindGalleryMedia") {
      const offset = body.variables.offset;
      pages.push(offset);
      if (offset && options.failNext) {
        options.failNext = false;
        return route.fulfill({
          json: { errors: [{ message: "Read temporarily unavailable" }] },
        });
      }
      return route.fulfill({
        json: {
          data: {
            findGallery: {
              id: "12",
              media: {
                signature: offset && options.changed ? "changed" : "original",
                count: options.empty ? 0 : options.videoOnly ? 1 : 3,
                items: options.empty
                  ? []
                  : options.videoOnly
                    ? [video]
                    : offset
                      ? [image("9")]
                      : [image("7"), video],
                next_offset:
                  options.empty || options.videoOnly || offset ? null : 2,
              },
            },
          },
        },
      });
    }
    if (body.operationName === "FindImage")
      return route.fulfill({
        json: { data: { findImage: playbackImage(body.variables.id) } },
      });
    if (body.operationName === "FindScene")
      return route.fulfill({
        json: {
          data: {
            findScene: playbackScene(route.request().url(), body.variables.id),
          },
        },
      });
    throw new Error(`Unexpected GraphQL operation ${body.operationName}`);
  });
  return pages;
}

for (const desktop of [false, true]) {
  test(`gallery shows and plays images and scenes together on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const pages = await fixture(page);
    await page.goto("/gallery-media");
    await expect(page.locator("[data-gallery-media]")).toHaveCount(2);
    await page
      .getByRole("button", { name: "View gallery", exact: true })
      .click();
    const viewer = page.getByRole("dialog", { name: "Gallery viewer" });
    await expect(viewer.locator("img.yarl__slide_image")).toBeVisible();
    await viewer.getByRole("button", { name: "Next", exact: true }).click();
    const video = viewer.locator("video").first();
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
        .outputPath(`gallery-${desktop ? "desktop" : "mobile"}.png`),
      animations: "disabled",
    });
    await viewer.getByRole("button", { name: "Next", exact: true }).click();
    await expect(viewer.locator("img.yarl__slide_image")).toBeVisible();
    await expect(viewer.locator("video")).toHaveCount(0);
    await expect(
      viewer.getByText("3 of 3 · First image", { exact: true }),
    ).toBeVisible();
    await expect(
      viewer.getByRole("button", { name: "Next", exact: true }),
    ).toBeDisabled();
    // StrictMode may begin and cancel an initial page before mounting again.
    expect([...new Set(pages)]).toEqual([0, 2]);
    expect(pages.filter((offset) => offset === 2)).toHaveLength(1);
    await viewer.getByRole("button", { name: "Previous", exact: true }).click();
    await expect(viewer.locator("video").first()).toBeVisible();
  });
}

test("gallery stops pagination playback when membership changes", async ({
  page,
}) => {
  await fixture(page, { changed: true });
  await page.goto("/gallery-media");
  await page.getByRole("button", { name: "Second video", exact: true }).click();
  const viewer = page.getByRole("dialog", { name: "Gallery viewer" });
  await viewer.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    viewer.getByText("This album changed", { exact: true }),
  ).toBeVisible();
  await expect(viewer.locator("video")).toHaveCount(0);
});

test("video-only gallery has playable contents", async ({ page }) => {
  await fixture(page, { videoOnly: true });
  await page.goto("/gallery-media");
  await expect(page.locator('[data-gallery-media="scene:8"]')).toBeVisible();
  await page.getByRole("button", { name: "View gallery", exact: true }).click();
  const viewer = page.getByRole("dialog", { name: "Gallery viewer" });
  await expect(viewer.locator("video").first()).toBeVisible();
  await expect(
    viewer.getByRole("button", { name: "Next", exact: true }),
  ).toBeDisabled();
});

test("empty gallery has an accurate empty state", async ({ page }) => {
  await fixture(page, { empty: true });
  await page.goto("/gallery-media");
  await expect(
    page.getByText("No images or scenes in this gallery", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "View gallery", exact: true }),
  ).toHaveCount(0);
});

test("failed next page can be retried without skipping an item", async ({
  page,
}) => {
  await fixture(page, { failNext: true });
  await page.goto("/gallery-media");
  await page.getByRole("button", { name: "Second video", exact: true }).click();
  const viewer = page.getByRole("dialog", { name: "Gallery viewer" });
  await viewer.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    viewer.getByText("Could not load gallery media", { exact: true }),
  ).toBeVisible();
  await expect(
    viewer.getByRole("button", { name: "Next", exact: true }),
  ).toBeDisabled();
  await viewer.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(viewer.locator("img.yarl__slide_image")).toBeVisible();
  await expect(
    viewer.getByText("3 of 3 · First image", { exact: true }),
  ).toBeVisible();
});
