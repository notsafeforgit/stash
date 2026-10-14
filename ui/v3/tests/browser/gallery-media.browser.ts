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
    failScene?: boolean;
  } = {},
) {
  const pages: number[] = [];
  const coverWrites: SetGalleryCoverMutationVariables[] = [];
  const coverReads: string[] = [];
  const detailReads: string[] = [];
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
    if (body.operationName === "FindImage") {
      detailReads.push(`image:${body.variables.id}`);
      return route.fulfill({
        json: { data: { findImage: playbackImage(body.variables.id) } },
      });
    }
    if (body.operationName === "FindScene") {
      detailReads.push(`scene:${body.variables.id}`);
      if (options.failScene) {
        options.failScene = false;
        return route.fulfill({
          json: { errors: [{ message: "Scene temporarily unavailable" }] },
        });
      }
      return route.fulfill({
        json: {
          data: {
            findScene: playbackScene(route.request().url(), body.variables.id),
          },
        },
      });
    }
    throw new Error(`Unexpected GraphQL operation ${body.operationName}`);
  });
  return { pages, coverWrites, coverReads, detailReads };
}

for (const desktop of [false, true]) {
  test(`gallery shows and plays images and scenes together on ${desktop ? "desktop" : "mobile"}`, async ({
    page,
  }) => {
    if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
    const { pages } = await fixture(page);
    await page.goto("/gallery-media");
    await expect(page.locator("[data-gallery-media]")).toHaveCount(2);
    await expect(
      page.locator("[data-gallery-media] [data-slot=card]"),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: "View gallery", exact: true })
      .click();
    const viewer = page.locator(".yarl__root");
    await expect(
      viewer.locator(".yarl__slide_current img.yarl__slide_image"),
    ).toBeVisible();
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
    await expect(
      viewer.locator(".yarl__slide_current img.yarl__slide_image"),
    ).toBeVisible();
    await expect(viewer.locator("video")).toBeHidden();
    await expect(viewer.locator("video")).toHaveJSProperty("paused", true);
    await expect(viewer.locator("video")).not.toHaveAttribute("src", /.+/);
    await expect(viewer.getByText("3 / 3", { exact: true })).toBeVisible();
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
  const viewer = page.locator(".yarl__root");
  await viewer.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    viewer.getByText("This album changed", { exact: true }),
  ).toBeVisible();
  await expect(viewer.locator("video")).toBeHidden();
  await expect(viewer.locator("video")).toHaveJSProperty("paused", true);
  await expect(viewer.locator("video")).not.toHaveAttribute("src", /.+/);
});

for (const preference of ["muted", "playbackRate"] as const) {
  test(`mixed lightbox loads details on demand and retains one idle player across images (${preference})`, async ({
    page,
  }) => {
    const state = await fixture(page);
    await page.goto("/gallery-media");
    await expect(page.locator("[data-gallery-media]")).toHaveCount(2);
    expect(state.detailReads).toEqual([]);
    await page
      .getByRole("button", { name: "First image", exact: true })
      .click();
    const viewer = page.getByRole("dialog", { name: "Lightbox", exact: true });
    await expect(
      viewer.locator(".yarl__slide_current img.yarl__slide_image"),
    ).toBeVisible();
    await expect.poll(() => state.detailReads).toEqual(["image:7"]);
    await expect(viewer.locator("video")).toHaveCount(0);
    await viewer.getByRole("button", { name: "Zoom in", exact: true }).click();
    await expect(
      viewer.getByRole("button", { name: "Zoom out", exact: true }),
    ).toBeEnabled();
    await viewer.getByRole("button", { name: "Next", exact: true }).click();
    const video = viewer.locator("video");
    await expect(video).toHaveCount(1);
    await expect
      .poll(() =>
        video.evaluate((element: HTMLVideoElement) => element.readyState),
      )
      .toBeGreaterThanOrEqual(2);
    const originalVideo = await video.elementHandle();
    expect(originalVideo).not.toBeNull();
    if (preference === "muted") {
      await page.keyboard.press("m");
      await expect(video).toHaveJSProperty("muted", true);
    } else {
      await video.evaluate((element: HTMLVideoElement) => {
        element.playbackRate = 1.5;
      });
      await expect(video).toHaveJSProperty("playbackRate", 1.5);
    }
    await viewer.getByRole("button", { name: "Next", exact: true }).click();
    await expect(viewer.getByText("3 / 3", { exact: true })).toBeVisible();
    await expect(video).toBeHidden();
    await expect(video).toHaveJSProperty("paused", true);
    await expect(video).not.toHaveAttribute("src", /.+/);
    // A hidden video's global hotkeys must not change its sound or play it.
    expect(
      await video.evaluate(
        (element, original) => element === original,
        originalVideo,
      ),
    ).toBe(true);
    // WebKit's idle native element may report its default audio flags while
    // there is no source. Check hotkeys leave it idle, then check the retained
    // user preference once media is loaded again below.
    const idleMuted = await video.evaluate(
      (element: HTMLVideoElement) => element.muted,
    );
    await page.keyboard.press("m");
    await expect(video).toHaveJSProperty("muted", idleMuted);
    await expect(video).toHaveJSProperty("paused", true);
    await viewer.getByRole("button", { name: "Previous", exact: true }).click();
    await expect(video).toBeVisible();
    await expect
      .poll(() =>
        video.evaluate((element: HTMLVideoElement) => element.readyState),
      )
      .toBeGreaterThanOrEqual(2);
    expect(
      await video.evaluate(
        (element, original) => element === original,
        originalVideo,
      ),
    ).toBe(true);
    await expect(video).toHaveJSProperty(
      preference,
      preference === "muted" ? true : 1.5,
    );
    expect(state.detailReads.filter((read) => read === "scene:8")).toEqual([
      "scene:8",
    ]);
    const pages = [...state.pages];
    await page.goBack();
    await expect(viewer).toHaveCount(0);
    await expect(page.locator("[data-gallery-media]")).toHaveCount(3);
    expect(state.pages).toEqual(pages);
    expect(
      await originalVideo!.evaluate((element) => element.isConnected),
    ).toBe(false);
  });
}

test("mixed lightbox can retry unavailable video details without losing its position", async ({
  page,
}) => {
  await fixture(page, { failScene: true });
  await page.goto("/gallery-media");
  await page.getByRole("button", { name: "Second video", exact: true }).click();
  const viewer = page.getByRole("dialog", { name: "Lightbox", exact: true });
  await expect(
    viewer.getByText("This media is unavailable", { exact: true }),
  ).toBeVisible();
  await expect(viewer.getByText("2 / 3", { exact: true })).toBeVisible();
  await viewer.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(viewer.locator("video")).toBeVisible();
  await expect(viewer.getByText("2 / 3", { exact: true })).toBeVisible();
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
  const setCover = page.getByRole("menuitem", {
    name: "Set as gallery cover",
    exact: true,
  });
  for (const kind of ["scene", "image"] as const) {
    const id = kind === "scene" ? "8" : "7";
    await page
      .locator(`[data-gallery-media="${kind}:${id}"]`)
      .getByRole("button", { name: "Media actions", exact: true })
      .click();
    await setCover.click();
    await expect(cover).toHaveAttribute("src", new RegExp(`#${kind}-${id}$`));
    // The cover can update before the previous menu's exit animation ends.
    await expect(setCover).toHaveCount(0);
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
  const viewer = page.locator(".yarl__root");
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
  const viewer = page.locator(".yarl__root");
  await viewer.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    viewer.getByText("Could not load gallery media", { exact: true }),
  ).toBeVisible();
  await expect(
    viewer.getByRole("button", { name: "Next", exact: true }),
  ).toBeDisabled();
  await viewer.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    viewer.locator(".yarl__slide_current img.yarl__slide_image"),
  ).toBeVisible();
  await expect(viewer.getByText("3 / 3", { exact: true })).toBeVisible();
});
