import { readFile } from "node:fs/promises";
import { PreviewImageDynamicRange } from "../../src/core/generated-graphql";
import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import type {
  SharedContent,
  SharedMedia,
} from "../../src/components/sharing/share-contract";

const id = "abcdefghijklmnopqrstuv";
const base = `/share/${id}/`;
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==",
  "base64",
);

function sharedMedia(key: string, title = ""): SharedMedia {
  const video = key.startsWith("scene-");
  return {
    key,
    kind: video ? "SCENE" : "IMAGE",
    title,
    width: 640,
    height: 360,
    duration: video ? 12 : 0,
    video,
    thumbnail: `${base}media/${key}/thumbnail`,
    image: `${base}media/${key}/image`,
    download: "",
  };
}

async function serveShare(
  page: Page,
  video: boolean,
  expiresIn = 3600,
  options?: {
    originalVideo?: boolean;
    media?: SharedMedia[];
    entries?: SharedContent["entries"];
  },
) {
  const media = options?.media ?? [sharedMedia(video ? "scene-1" : "image-1")];
  const requests: string[] = [];
  let exchanges = 0;
  let expiry = new Date(Date.now() + expiresIn * 1000).toISOString();
  await page.route(`**${base}**`, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const endpoint = url.pathname.slice(base.length);
    const item = media.find((item) =>
      endpoint.startsWith(`media/${item.key}/`),
    );
    if (!endpoint) {
      await route.continue();
      return;
    }
    requests.push(endpoint);
    expect(request.headers().apikey).toBeUndefined();
    if (endpoint === "exchange") {
      exchanges++;
      expect(request.method()).toBe("POST");
      expect(request.postDataJSON()).toEqual({ secret: "test-capability" });
      expiry = new Date(Date.now() + expiresIn * 1000).toISOString();
      await route.fulfill({ status: 204 });
    } else if (endpoint === "content") {
      await route.fulfill({
        json: {
          label: "Shared example",
          expires_at: expiry,
          server_time: new Date().toISOString(),
          entries:
            options?.entries ??
            media.map((item) => ({
              kind: item.kind,
              title: item.title,
              media_keys: [item.key],
            })),
          media,
        },
      });
    } else if (endpoint === "status") {
      await route.fulfill({
        json: { expires_at: expiry, server_time: new Date().toISOString() },
      });
    } else if (item && endpoint === `media/${item.key}/`) {
      await route.fulfill({
        json: {
          media: item,
          video_codec: item.video ? "h264" : "",
          audio_codec: "",
          frame_rate: item.video ? 30 : 0,
          streams: item.video
            ? [
                {
                  url: options?.originalVideo
                    ? `${base}media/${item.key}/stream`
                    : `${base}media/${item.key}/stream.master.m3u8?resolution=LOW`,
                  mime_type: options?.originalVideo
                    ? "video/mp4"
                    : "application/vnd.apple.mpegurl",
                  label: options?.originalVideo
                    ? "Direct stream"
                    : "HLS Low (240p)",
                },
              ]
            : [],
        },
      });
    } else if (
      endpoint.endsWith("/thumbnail") ||
      endpoint.endsWith("/image") ||
      endpoint.includes("/preview-image/")
    ) {
      await route.fulfill({ contentType: "image/png", body: png });
    } else if (/\/streams\.(stop|keepalive)$/.test(endpoint)) {
      await route.fulfill({ status: 204 });
    } else if (endpoint.endsWith("/stream") && options?.originalVideo) {
      await route.fulfill({
        contentType: "video/mp4",
        body: await readFile(
          new URL("fixture/media/short.mp4", import.meta.url),
        ),
      });
    } else if (endpoint.endsWith("/stream.master.m3u8")) {
      await route.fulfill({
        contentType: "application/vnd.apple.mpegurl",
        body: [
          "#EXTM3U",
          "#EXT-X-VERSION:7",
          "#EXT-X-TARGETDURATION:2",
          "#EXT-X-MEDIA-SEQUENCE:0",
          "#EXT-X-PLAYLIST-TYPE:VOD",
          `#EXT-X-MAP:URI="${base}media/${item?.key}/stream.m3u8/video/init.mp4"`,
          ...Array.from(
            { length: 6 },
            (_, index) =>
              `#EXTINF:2.000000,\n${base}media/${item?.key}/stream.m3u8/video/${index}.m4s`,
          ),
          "#EXT-X-ENDLIST",
          "",
        ].join("\n"),
      });
    } else if (/\/stream\.m3u8\/video\/(init\.mp4|\d\.m4s)$/.test(endpoint)) {
      const part = endpoint.split("/").at(-1);
      const name = part === "init.mp4" ? part : `segment-${part}`;
      await route.fulfill({
        contentType: "video/mp4",
        body: await readFile(
          new URL(`fixture/media/hls/${name}`, import.meta.url),
        ),
      });
    } else throw new Error(`Unexpected share request: ${endpoint}`);
  });
  return { requests, exchanges: () => exchanges };
}

test("guest image viewer needs no owner providers and removes the link secret", async ({
  page,
}) => {
  const fixture = await serveShare(page, false);
  await page.goto(`${base}#test-capability`);
  await expect(
    page.getByRole("heading", { name: "Shared example" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Preview Image" }),
  ).toBeVisible();
  expect(new URL(page.url()).hash).toBe("");
  expect(fixture.exchanges()).toBe(1);
  await page.getByRole("button", { name: "Preview Image" }).press("Enter");
  await expect(page.locator(".yarl__portal")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Download original" }),
  ).toHaveCount(0);
  expect(fixture.requests).not.toContain("graphql");
});

test("guest HLS playback reuses the player and stops at expiry", async ({
  page,
}) => {
  const fixture = await serveShare(page, true, 12);
  await page.goto(`${base}#test-capability`);
  await page
    .locator('.entity-card[data-id="scene-1"] [data-entity-card-preview]')
    .click();
  await expect(page.locator("video")).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video) =>
          video instanceof HTMLVideoElement ? video.currentTime : 0,
        ),
    )
    .toBeGreaterThan(0.1);
  await expect(
    page.getByRole("button", { name: /Google Cast|AirPlay/ }),
  ).toHaveCount(0);
  await expect(
    page.getByText("This link has expired, was revoked, or is invalid."),
  ).toBeVisible({ timeout: 16_000 });
  await expect(page.locator("video")).toHaveCount(0);
  expect(fixture.requests.some((request) => request.endsWith(".m4s"))).toBe(
    true,
  );
});

const mixed = [
  sharedMedia("scene-1", "First scene"),
  sharedMedia("scene-2", "Second scene"),
  sharedMedia("image-1", "First image"),
  sharedMedia("image-2", "Second image"),
];

test("mixed shares browse tabs, minimal details and gallery lightboxes with Back support", async ({
  page,
}) => {
  const fixture = await serveShare(page, true, 3600, {
    media: mixed,
    entries: [
      { kind: "SCENE", title: "First scene", media_keys: ["scene-1"] },
      { kind: "SCENE", title: "Second scene", media_keys: ["scene-2"] },
      {
        kind: "GALLERY",
        title: "Example gallery",
        media_keys: ["image-1", "image-2"],
      },
    ],
  });
  await page.goto(`${base}#test-capability`);
  await expect(page.getByRole("tab", { name: "Scenes (2)" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(page.locator(".entity-card")).toHaveCount(2);
  await expect(page.locator("video")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /Previous item|Next item|Filter|Sort/ }),
  ).toHaveCount(0);
  await page.getByRole("tab", { name: "Images (2)" }).click();
  await page
    .getByRole("link", { name: "First image", exact: true })
    .press("Enter");
  await expect(
    page.getByRole("heading", { name: "First image", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/media=image-1/);
  await page.getByRole("button", { name: "Open viewer", exact: true }).click();
  await expect(page.locator(".yarl__portal")).toBeVisible();
  await page.goBack();
  await expect(page.locator(".yarl__portal")).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "First image", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Images (2)" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.getByRole("tab", { name: "Galleries (1)" }).click();
  await page
    .getByRole("link", { name: "Example gallery", exact: true })
    .press("Enter");
  await expect(
    page.getByRole("heading", { name: "Example gallery" }),
  ).toBeVisible();
  await expect(page.locator(".entity-card")).toHaveCount(2);
  await page
    .locator('.entity-card[data-id="image-1"] [data-entity-card-preview]')
    .click();
  await expect(page.locator(".yarl__portal")).toBeVisible();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.locator(".yarl__slide_current img.yarl__slide_image"),
  ).toHaveAttribute("src", `${base}media/image-2/image`);
  await expect(
    page.getByRole("button", { name: /Rotate clockwise|Delete|Image actions/ }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await expect(page.locator(".yarl__portal")).toHaveCount(0);
  await page
    .getByRole("link", { name: "Second image", exact: true })
    .press("Enter");
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Second image", exact: true }),
  ).toBeVisible();
  expect(fixture.exchanges()).toBe(1);
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Example gallery" }),
  ).toBeVisible();
});

test("video previews use the scene lightbox and retain its video element when navigating", async ({
  page,
}) => {
  await serveShare(page, true, 3600, { media: mixed.slice(0, 2) });
  await page.goto(`${base}#test-capability`);
  await page
    .locator('.entity-card[data-id="scene-1"] [data-entity-card-preview]')
    .click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video) =>
          video instanceof HTMLVideoElement ? video.currentTime : 0,
        ),
    )
    .toBeGreaterThan(0.1);
  await page.locator("video").evaluate((video) => {
    video.dataset.retained = "true";
  });
  await page.keyboard.press("ArrowRight");
  await expect(
    page.locator(".yarl__portal").getByText("Second scene", { exact: true }),
  ).toBeVisible();
  await expect(page.locator("video")).toHaveAttribute("data-retained", "true");
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video) =>
          video instanceof HTMLVideoElement ? video.currentTime : 0,
        ),
    )
    .toBeGreaterThan(0.1);
  await page.goBack();
  await expect(page.locator(".yarl__portal")).toHaveCount(0);
  await expect(page.locator("video")).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "Scenes (2)" })).toBeVisible();
});

for (const width of [390, 1280]) {
  test(`shared lists scroll and restore their position after a detail at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const media = Array.from({ length: 80 }, (_, index) =>
      sharedMedia(`image-${index + 1}`, `Shared image ${index + 1}`),
    );
    await serveShare(page, false, 3600, { media });
    await page.goto(`${base}#test-capability`);
    const scroll = page.locator("[data-share-scroll]");
    await expect(page.locator(".entity-card").first()).toBeVisible();
    await scroll.evaluate((element) => {
      element.scrollTop = 1000;
    });
    await expect
      .poll(() => scroll.evaluate((element) => element.scrollTop))
      .toBeGreaterThan(950);
    const card = page
      .locator(".entity-card")
      .filter({ has: page.locator("a[data-card-link]") })
      .last();
    const title = await card
      .locator("a[data-card-link]")
      .getAttribute("aria-label");
    if (!title) throw new Error("Missing card title");
    const link = page.getByRole("link", { name: title, exact: true });
    await link.focus();
    const before = await scroll.evaluate((element) => element.scrollTop);
    await link.press("Enter");
    await expect(
      page.getByRole("heading", { name: title, exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Back", exact: true }).click();
    await expect
      .poll(() => scroll.evaluate((element) => element.scrollTop))
      .toBeCloseTo(before, 0);
    expect(
      await page
        .locator("main")
        .evaluate((element) => element.scrollWidth <= element.clientWidth),
    ).toBe(true);
    await page.screenshot({ path: test.info().outputPath("share-list.png") });
  });
}

test("only granted items and download actions are exposed through public details", async ({
  page,
}) => {
  const media = sharedMedia("image-1", "Allowed image");
  media.download = `${base}media/image-1/download`;
  const fixture = await serveShare(page, false, 3600, { media: [media] });
  await page.goto(`${base}?media=scene-999#test-capability`);
  await expect(
    page.getByText("This item is no longer available."),
  ).toBeVisible();
  expect(fixture.requests).not.toContain("media/scene-999/");
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await page
    .getByRole("link", { name: "Allowed image", exact: true })
    .press("Enter");
  await expect(
    page.getByRole("link", { name: "Download original" }),
  ).toHaveAttribute("href", media.download);
  await expect(
    page.getByRole("button", { name: /Edit|Delete|Rotate/ }),
  ).toHaveCount(0);
});

test("shared cards and lightbox reuse scoped HDR preview catalogs", async ({
  page,
}) => {
  const media = ["scene-1", "scene-2"].map((key) => {
    const previewBase = `${base}media/${key}/preview-image/`;
    const rendition = (name: string) => ({
      fallback: `${previewBase}${name}.jpg?revision=example`,
      sources: [
        {
          url: `${previewBase}${name}.avif?revision=example`,
          mime_type: "image/avif" as const,
          dynamic_range: PreviewImageDynamicRange.Adaptive,
          width: 640,
          height: 360,
        },
      ],
    });
    return {
      ...sharedMedia(key),
      preview_image: {
        ...rendition("cover"),
        thumbnail: rendition("thumbnail"),
      },
    };
  });
  const fixture = await serveShare(page, true, 3600, { media });
  await page.goto(`${base}#test-capability`);
  const card = page.locator('.entity-card[data-id="scene-1"]');
  await expect(
    card.locator('source[type="image/avif"]').first(),
  ).toHaveAttribute("srcset", /preview-image\/thumbnail\.avif/);
  await expect(card.locator("img").first()).toHaveAttribute(
    "src",
    /preview-image\/thumbnail\.jpg/,
  );
  await card.locator("[data-entity-card-preview]").click();
  const lightbox = page.locator(".yarl__portal");
  await expect(lightbox).toBeVisible();
  await expect(
    lightbox.locator('source[type="image/avif"]').first(),
  ).toHaveAttribute("srcset", /preview-image\/cover\.avif/);
  expect(
    fixture.requests.some((request) => request.includes("/preview-image/")),
  ).toBe(true);
  expect(fixture.requests).not.toContain("graphql");
});

test("As-is shares play original video with the existing player and no encoder requests", async ({
  page,
}) => {
  const fixture = await serveShare(page, true, 3600, { originalVideo: true });
  await page.goto(`${base}#test-capability`);
  await page
    .locator('.entity-card[data-id="scene-1"] [data-entity-card-preview]')
    .click();
  const video = page.locator("video");
  await expect(video).toBeVisible();
  await expect
    .poll(() =>
      video.evaluate((element) =>
        element instanceof HTMLVideoElement ? element.currentTime : 0,
      ),
    )
    .toBeGreaterThan(0.1);
  await expect(video).toHaveJSProperty(
    "currentSrc",
    new URL(`${base}media/scene-1/stream`, page.url()).href,
  );
  await expect(
    page.getByRole("link", { name: "Download original" }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(page.locator(".yarl__portal")).toHaveCount(0);
  expect(
    fixture.requests.some((url) => /\.m3u8|\.m4s|streams\./.test(url)),
  ).toBe(false);
});
