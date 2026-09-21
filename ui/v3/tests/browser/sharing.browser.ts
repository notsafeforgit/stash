import { readFile } from "node:fs/promises";
import type { Page } from "@playwright/test";
import { test, expect } from "./test";

const id = "abcdefghijklmnopqrstuv";
const base = `/share/${id}/`;
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==",
  "base64",
);

async function serveShare(page: Page, video: boolean, expiresIn = 3600) {
  const mediaKey = video ? "scene-1" : "image-1";
  const media = {
    key: mediaKey,
    kind: video ? "SCENE" : "IMAGE",
    title: "",
    width: 640,
    height: 360,
    duration: video ? 12 : 0,
    video,
    thumbnail: `${base}media/${mediaKey}/thumbnail`,
    image: `${base}media/${mediaKey}/image`,
    download: "",
  };
  const requests: string[] = [];
  let exchanges = 0;
  let expiry: string;
  await page.route(`**${base}**`, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const endpoint = url.pathname.slice(base.length);
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
          entries: [{ kind: media.kind, title: "", media_keys: [mediaKey] }],
          media: [media],
        },
      });
    } else if (endpoint === "status") {
      await route.fulfill({
        json: { expires_at: expiry, server_time: new Date().toISOString() },
      });
    } else if (endpoint === `media/${mediaKey}/`) {
      await route.fulfill({
        json: {
          media,
          video_codec: video ? "h264" : "",
          audio_codec: "",
          frame_rate: video ? 30 : 0,
          streams: video
            ? [
                {
                  url: `${base}media/${mediaKey}/stream.master.m3u8?resolution=LOW`,
                  mime_type: "application/vnd.apple.mpegurl",
                  label: "HLS Low (240p)",
                },
              ]
            : [],
        },
      });
    } else if (endpoint.endsWith("/thumbnail") || endpoint.endsWith("/image")) {
      await route.fulfill({ contentType: "image/png", body: png });
    } else if (/\/streams\.(stop|keepalive)$/.test(endpoint)) {
      await route.fulfill({ status: 204 });
    } else if (endpoint.endsWith("/stream.master.m3u8")) {
      await route.fulfill({
        contentType: "application/vnd.apple.mpegurl",
        body: [
          "#EXTM3U",
          "#EXT-X-VERSION:7",
          "#EXT-X-TARGETDURATION:2",
          "#EXT-X-MEDIA-SEQUENCE:0",
          "#EXT-X-PLAYLIST-TYPE:VOD",
          `#EXT-X-MAP:URI="${base}media/${mediaKey}/stream.m3u8/video/init.mp4"`,
          ...Array.from(
            { length: 6 },
            (_, index) =>
              `#EXTINF:2.000000,\n${base}media/${mediaKey}/stream.m3u8/video/${index}.m4s`,
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
  await expect(page.getByRole("button", { name: "Open image" })).toBeVisible();
  expect(new URL(page.url()).hash).toBe("");
  expect(fixture.exchanges()).toBe(1);
  await page.getByRole("button", { name: "Open image" }).click();
  await expect(page.locator(".yarl__portal")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Download original" }),
  ).toHaveCount(0);
  expect(fixture.requests).not.toContain("graphql");
});

test("guest HLS playback reuses the player and stops at expiry", async ({
  page,
}) => {
  const fixture = await serveShare(page, true, 12);
  await page.goto(`${base}#test-capability`);
  await expect(page.locator("video")).toBeVisible();
  await page.getByRole("button", { name: "Play", exact: true }).first().click();
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
