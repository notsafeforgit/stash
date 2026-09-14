import type { Page } from "@playwright/test";

/** Real player engines against synthetic media, with transcode leases kept
 * inside the fixture. No library or remote requests are permitted. */
export async function serveSceneMedia(page: Page) {
  await page.route("**/scene/*/**", async (route) => {
    const url = new URL(route.request().url());
    if (/\/streams\.(stop|keepalive)$/.test(url.pathname))
      await route.fulfill({ status: 204 });
    else if (url.pathname.endsWith("/caption"))
      await route.fulfill({
        contentType: "text/vtt",
        body: "WEBVTT\n\n00:00.000 --> 00:12.000\nSample caption\n",
      });
    else if (url.pathname.endsWith("/stream")) {
      const response = await route.fetch({
        url: new URL("/media/audio.mp4", url).href,
      });
      await route.fulfill({ response });
    } else if (url.pathname.endsWith("/stream.master.m3u8")) {
      const clipped = url.searchParams.has("end");
      const first = clipped
        ? Math.floor(Number(url.searchParams.get("start")) / 2)
        : 0;
      const end = clipped
        ? Math.ceil(Number(url.searchParams.get("end")) / 2)
        : 6;
      await route.fulfill({
        contentType: "application/vnd.apple.mpegurl",
        body: [
          "#EXTM3U",
          "#EXT-X-VERSION:7",
          "#EXT-X-TARGETDURATION:2",
          `#EXT-X-MEDIA-SEQUENCE:${first}`,
          "#EXT-X-PLAYLIST-TYPE:VOD",
          '#EXT-X-MAP:URI="/media/hls/init.mp4"',
          ...Array.from(
            { length: end - first },
            (_, index) =>
              `#EXTINF:2.000000,\n/media/hls/segment-${first + index}.m4s`,
          ),
          "#EXT-X-ENDLIST",
          "",
        ].join("\n"),
      });
    } else throw new Error(`Unexpected scene request: ${url}`);
  });
}
