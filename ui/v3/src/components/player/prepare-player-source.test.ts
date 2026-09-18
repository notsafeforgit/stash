import { afterEach, expect, it, vi } from "vitest";
import {
  preparePlayerSource,
  readPreparationPlaylist,
} from "./prepare-player-source";

afterEach(() => vi.unstubAllGlobals());

it("prepares both tracks at the clip start before requesting init, without fetching the whole clip", async () => {
  const requested: string[] = [];
  const root =
    "https://stash.test/base/scene/1/stream.master.m3u8?stream_session=a&start=40&end=90";
  const fetch = vi.fn<typeof globalThis.fetch>(async (input) => {
    const url = String(input);
    requested.push(url);
    if (url === root)
      return new Response(
        '#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,URI="audio.m3u8?stream_session=a"\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nvideo.m3u8?stream_session=a\n',
      );
    const track = url.includes("audio") ? "audio" : "video";
    if (url.includes(".m3u8"))
      return new Response(
        `#EXTM3U\n#EXT-X-MAP:URI="${track}/init.mp4?stream_session=a"\n#EXTINF:2,\n${track}/20.m4s?stream_session=a\n#EXTINF:2,\n${track}/21.m4s?stream_session=a\n#EXTINF:2,\n${track}/22.m4s?stream_session=a\n`,
      );
    return new Response(new Uint8Array([0, 1, 2]));
  });
  vi.stubGlobal("fetch", fetch);
  await preparePlayerSource(root, 40, new AbortController().signal);
  expect(requested).toHaveLength(9);
  expect(
    requested.every(
      (url) => new URL(url).searchParams.get("stream_session") === "a",
    ),
  ).toBe(true);
  expect(requested.some((url) => url.includes("22.m4s"))).toBe(false);
  for (const track of ["audio", "video"])
    expect(
      requested.findIndex((url) => url.includes(`${track}/20.m4s`)),
    ).toBeLessThan(
      requested.findIndex((url) => url.includes(`${track}/init.mp4`)),
    );
});

it("rejects malformed or empty playlists", () => {
  expect(() =>
    readPreparationPlaylist("not a playlist", "https://stash.test"),
  ).toThrow();
  expect(() =>
    readPreparationPlaylist("#EXTM3U\n", "https://stash.test"),
  ).toThrow();
});

it("passes cancellation through every startup fetch", async () => {
  const abort = new AbortController();
  const fetch = vi.fn<typeof globalThis.fetch>(async (_input, init) => {
    expect(init?.signal).toBe(abort.signal);
    abort.abort();
    abort.signal.throwIfAborted();
    return new Response();
  });
  vi.stubGlobal("fetch", fetch);
  await expect(
    preparePlayerSource(
      "https://stash.test/scene/1/stream.master.m3u8",
      0,
      abort.signal,
    ),
  ).rejects.toThrow();
  expect(fetch).toHaveBeenCalledOnce();
});
