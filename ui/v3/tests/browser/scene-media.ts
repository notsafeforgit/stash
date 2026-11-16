import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";

const KEYFRAME_STILL = readFileSync(
  new URL("./fixture/media/keyframe.jpg", import.meta.url),
);
/** Every synthetic fixture video is twelve seconds with two-second GOPs. */
const KEYFRAME_SECONDS = 2;
const KEYFRAME_TIMES = Array.from(
  { length: 6 },
  (_, index) => index * KEYFRAME_SECONDS,
);
export const FIXTURE_KEYFRAME_VERSION = "fixture-1";
const keyframeAt = (time: number) =>
  Math.min(
    KEYFRAME_TIMES.at(-1)!,
    Math.floor((time + 0.01) / KEYFRAME_SECONDS) * KEYFRAME_SECONDS,
  );
const keyframeCache = (url: URL) =>
  url.searchParams.get("v") === FIXTURE_KEYFRAME_VERSION
    ? "private, max-age=31536000, immutable"
    : "no-cache";

/** Real player engines against synthetic media, with transcode leases kept
 * inside the fixture. No library or remote requests are permitted. Keyframe
 * index, single and batch stills are served like the library endpoints;
 * `keyframes: false` answers them with 404, as an older server would (the
 * browser logs those errors). */
export async function serveSceneMedia(
  page: Page,
  orientation: "landscape" | "portrait" = "landscape",
  { clipEnd, keyframes = true }: { clipEnd?: number; keyframes?: boolean } = {},
) {
  await page.route("**/scene/*/**", async (route) => {
    const url = new URL(route.request().url());
    if (/\/streams\.(stop|keepalive)$/.test(url.pathname))
      await route.fulfill({ status: 204 });
    else if (/\/keyframes?(?:\/stills)?$/.test(url.pathname) && !keyframes)
      await route.fulfill({ status: 404 });
    else if (url.pathname.endsWith("/keyframes"))
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          version: FIXTURE_KEYFRAME_VERSION,
          duration: 12,
          times: KEYFRAME_TIMES,
        }),
      });
    else if (url.pathname.endsWith("/keyframes/stills")) {
      const requested = (url.searchParams.get("t") ?? "")
        .split(",")
        .map(Number);
      if (
        requested.length > 16 ||
        requested.some((time) => !Number.isFinite(time) || time < 0)
      ) {
        await route.fulfill({ status: 400 });
        return;
      }
      // The same still for every slot, concatenated in request order.
      await route.fulfill({
        contentType: "image/jpeg",
        body: Buffer.concat(requested.map(() => KEYFRAME_STILL)),
        headers: {
          "X-Keyframe-Times": requested.map(keyframeAt).join(","),
          "X-Keyframe-Lengths": requested
            .map(() => KEYFRAME_STILL.length)
            .join(","),
          "Cache-Control": keyframeCache(url),
        },
      });
    } else if (url.pathname.endsWith("/keyframe")) {
      const requested = Number(url.searchParams.get("t"));
      if (!Number.isFinite(requested) || requested < 0) {
        await route.fulfill({ status: 400 });
        return;
      }
      await route.fulfill({
        body: KEYFRAME_STILL,
        contentType: "image/jpeg",
        headers: {
          "X-Keyframe-Time": String(keyframeAt(requested)),
          "Cache-Control": keyframeCache(url),
        },
      });
    } else if (url.pathname.endsWith("/caption"))
      await route.fulfill({
        contentType: "text/vtt",
        body: "WEBVTT\n\n00:00.000 --> 00:12.000\nSample caption\n",
      });
    else if (url.pathname.endsWith("/stream")) {
      // Keep range handling on the fixture server and request cancellation in
      // the browser, without a fetch response that can outlive test teardown.
      await route.continue({
        url: new URL(
          orientation === "portrait"
            ? "/media/portrait.mp4"
            : "/media/audio.mp4",
          url,
        ).href,
      });
    } else if (url.pathname.endsWith("/stream.scrub.master.m3u8")) {
      url.pathname = url.pathname.replace(".master.m3u8", ".m3u8/video.m3u8");
      await route.fulfill({
        contentType: "application/vnd.apple.mpegurl",
        body: `#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,CODECS="avc1.4d400b",RESOLUTION=160x90\n${url.href}\n`,
      });
    } else if (
      url.pathname.endsWith("/stream.master.m3u8") ||
      url.pathname.endsWith("/stream.hevc.master.m3u8") ||
      url.pathname.endsWith("/stream.scrub.m3u8/video.m3u8")
    ) {
      const session = url.searchParams.get("stream_session");
      const media = url.pathname.includes("/stream.scrub.") ? "scrub" : "hls";
      const query = session
        ? `?stream_session=${encodeURIComponent(session)}`
        : "";
      const clipped = url.searchParams.has("end");
      const first = clipped
        ? Math.floor(Number(url.searchParams.get("start")) / 2)
        : 0;
      const end = clipped
        ? Math.ceil((clipEnd ?? Number(url.searchParams.get("end"))) / 2)
        : 6;
      await route.fulfill({
        contentType: "application/vnd.apple.mpegurl",
        body: [
          "#EXTM3U",
          "#EXT-X-VERSION:7",
          "#EXT-X-TARGETDURATION:2",
          `#EXT-X-MEDIA-SEQUENCE:${first}`,
          "#EXT-X-PLAYLIST-TYPE:VOD",
          `#EXT-X-MAP:URI="/media/${media}/init.mp4${query}"`,
          ...Array.from(
            { length: end - first },
            (_, index) =>
              `#EXTINF:2.000000,\n/media/${media}/segment-${first + index}.m4s${query}`,
          ),
          "#EXT-X-ENDLIST",
          "",
        ].join("\n"),
      });
    } else throw new Error(`Unexpected scene request: ${url}`);
  });
}
