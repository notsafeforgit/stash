import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";
import { playbackImage, playbackScene } from "../fixtures/album-playback";
import type { GalleryMediaItem } from "../../src/components/detail/gallery-media";
import type {
  FindGalleryCoverQuery,
  SetGalleryCoverMutationVariables,
} from "../../src/core/generated-graphql";

async function fixture(
  page: Page,
  options: {
    changed?: boolean;
    videoOnly?: boolean;
    empty?: boolean;
    failNext?: boolean;
    failCover?: boolean;
  } = {},
) {
  const pages: number[] = [];
  const coverWrites: SetGalleryCoverMutationVariables[] = [];
  const coverReads: string[] = [];
  let selected: SetGalleryCoverMutationVariables | undefined;
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
    if (body.operationName === "SetGalleryCover") {
      expect(body.query.trim().startsWith("mutation")).toBe(true);
      coverWrites.push(body.variables);
      if (options.failCover) {
        options.failCover = false;
        return route.fulfill({
          json: { errors: [{ message: "Cover selection unavailable" }] },
        });
      }
      selected = body.variables;
      return route.fulfill({ json: { data: { setGalleryCover: true } } });
    }
    expect(body.query.trim().startsWith("query")).toBe(true);
    if (body.operationName === "FindGalleryCover") {
      coverReads.push(body.variables.id);
      const src = playbackImage().paths.thumbnail;
      const findGallery: FindGalleryCoverQuery["findGallery"] = {
        __typename: "Gallery",
        id: "12",
        updated_at: "2026-10-10T09:00:00Z",
        paths: {
          __typename: "GalleryPathsType",
          cover: `${src}#image-${selected?.image_id ?? "initial"}`,
        },
        cover: selected
          ? {
              __typename: "GalleryCover",
              image: selected.image_id
                ? {
                    __typename: "Image",
                    id: selected.image_id,
                    preview_image: null,
                  }
                : null,
              scene: selected.scene_id
                ? {
                    __typename: "Scene",
                    id: selected.scene_id,
                    paths: {
                      __typename: "ScenePathsType",
                      screenshot: `${src}#scene-${selected.scene_id}`,
                    },
                  }
                : null,
            }
          : null,
      };
      return route.fulfill({ json: { data: { findGallery } } });
    }
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
  return { pages, coverWrites, coverReads };
}

for (const desktop of [false, true]) {
  test(`gallery shows and plays images and scenes together on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const { pages } = await fixture(page);
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

test("image and scene covers refresh only the selected gallery and keep its media page", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.goto("/gallery-media");
  const cover = page.getByRole("img", { name: "Gallery cover", exact: true });
  await expect(cover).toHaveAttribute("src", /#image-initial$/);
  await expect(page.locator("[data-gallery-media]")).toHaveCount(2);
  expect(state.coverWrites).toEqual([]);
  const initialPages = [...state.pages];
  for (const kind of ["scene", "image"] as const) {
    const id = kind === "scene" ? "8" : "7";
    await page
      .locator(`[data-gallery-media="${kind}:${id}"]`)
      .getByRole("button", { name: "Media actions", exact: true })
      .click();
    await page
      .getByRole("menuitem", { name: "Set as gallery cover", exact: true })
      .click();
    await expect(cover).toHaveAttribute("src", new RegExp(`#${kind}-${id}$`));
    expect(state.coverWrites.at(-1)).toEqual({
      gallery_id: "12",
      image_id: kind === "image" ? id : null,
      scene_id: kind === "scene" ? id : null,
    });
    expect(state.pages).toEqual(initialPages);
  }
  expect(state.coverWrites).toHaveLength(2);
  expect(new Set(state.coverReads)).toEqual(new Set(["12"]));
  await page
    .locator('[data-gallery-media="scene:8"]')
    .getByRole("button", { name: "Media actions", exact: true })
    .click();
  await page.screenshot({
    path: test.info().outputPath("gallery-cover-menu.png"),
    animations: "disabled",
  });
});

test("a failed cover change retains the previous cover and can be retried", async ({
  page,
}) => {
  const state = await fixture(page, { failCover: true });
  await page.goto("/gallery-media");
  const cover = page.getByRole("img", { name: "Gallery cover", exact: true });
  await expect(cover).toHaveAttribute("src", /#image-initial$/);
  await page
    .locator('[data-gallery-media="scene:8"]')
    .getByRole("button", { name: "Media actions", exact: true })
    .click();
  await page
    .getByRole("menuitem", { name: "Set as gallery cover", exact: true })
    .click();
  await expect(
    page.getByText(
      "Could not set the gallery cover. Refresh the gallery and try again.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(cover).toHaveAttribute("src", /#image-initial$/);
  await page
    .locator('[data-gallery-media="scene:8"]')
    .getByRole("button", { name: "Media actions", exact: true })
    .click();
  await page
    .getByRole("menuitem", { name: "Set as gallery cover", exact: true })
    .click();
  await expect(cover).toHaveAttribute("src", /#scene-8$/);
  expect(state.coverWrites).toHaveLength(2);
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
