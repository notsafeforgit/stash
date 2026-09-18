import { isHlsPlaylist } from "./hls";

type Preparation = { dispose: () => void };
type Playlist =
  | { kind: "master"; tracks: string[] }
  | { kind: "media"; init?: string; segments: string[] };

/** Reads Stash's single-variant, unencrypted VOD playlists. This is only a
 * bounded startup fetch; the shared player remains the playback engine. */
export function readPreparationPlaylist(
  text: string,
  source: string,
): Playlist {
  const lines = text
    .trim()
    .split(/\r?\n/)
    .map((line) => line.trim());
  if (lines[0] !== "#EXTM3U") throw new Error("Invalid HLS playlist");
  const tracks: string[] = [];
  const segments: string[] = [];
  let init: string | undefined;
  let variant = false;
  let media = false;
  for (const line of lines) {
    if (line.startsWith("#EXT-X-MEDIA:") && line.includes("TYPE=AUDIO")) {
      const uri = /(?:^|,)URI="([^"]+)"/.exec(
        line.slice(line.indexOf(":") + 1),
      )?.[1];
      if (uri) tracks.push(new URL(uri, source).href);
    } else if (line.startsWith("#EXT-X-STREAM-INF:")) variant = true;
    else if (line.startsWith("#EXT-X-MAP:")) {
      const uri = /(?:^|,)URI="([^"]+)"/.exec(
        line.slice(line.indexOf(":") + 1),
      )?.[1];
      if (uri) init = new URL(uri, source).href;
    } else if (line.startsWith("#EXTINF:")) media = true;
    else if (line && !line.startsWith("#")) {
      if (variant) tracks.push(new URL(line, source).href);
      else if (media && segments.length < 2)
        segments.push(new URL(line, source).href);
      variant = false;
      media = false;
    }
  }
  if (tracks.length)
    return { kind: "master", tracks: [...new Set(tracks)].slice(0, 2) };
  if (!segments.length) throw new Error("HLS playlist has no media");
  return { kind: "media", init, segments };
}

async function prepareHls(
  source: string,
  signal: AbortSignal,
): Promise<Preparation> {
  const playlist = async (url: string) => {
    const response = await fetch(url, { signal });
    if (!response.ok) throw new Error("Could not prepare playlist");
    return readPreparationPlaylist(await response.text(), url);
  };
  const root = await playlist(source);
  const tracks =
    root.kind === "master"
      ? await Promise.all(root.tracks.map(playlist))
      : [root];
  const download = async (url: string) => {
    const response = await fetch(url, { signal });
    if (!response.ok) throw new Error("Could not prepare media");
    // Consume the response so the normal HTTP cache can serve the player.
    await response.arrayBuffer();
  };
  await Promise.all(
    tracks.map(async (track) => {
      if (track.kind !== "media") throw new Error("Unexpected nested playlist");
      // Request the actual starting segment first. An init-only request would
      // start the server encoder at scene-time zero instead of this clip.
      for (const segment of track.segments) await download(segment);
      if (track.init) await download(track.init);
    }),
  );
  return { dispose: () => {} };
}

function prepareDirect(
  source: string,
  position: number,
  signal: AbortSignal,
): Promise<Preparation> {
  return new Promise((resolve, reject) => {
    const video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    video.preload = "auto";
    let disposed = false;
    const dispose = () => {
      if (disposed) return;
      disposed = true;
      signal.removeEventListener("abort", abort);
      video.onloadedmetadata = null;
      video.oncanplay = null;
      video.onerror = null;
      video.pause();
      video.removeAttribute("src");
      video.load();
    };
    const abort = () => {
      dispose();
      reject(signal.reason);
    };
    signal.addEventListener("abort", abort, { once: true });
    video.onloadedmetadata = () => {
      if (position > 0) video.currentTime = position;
    };
    video.oncanplay = () => {
      // Hold the prepared frame without playing, audio, activity tracking,
      // or the full shared-player component tree for every nearby item.
      video.preload = "metadata";
      resolve({ dispose });
    };
    video.onerror = () => {
      dispose();
      reject(new Error("Could not prepare video"));
    };
    if (signal.aborted) abort();
    else video.src = source;
  });
}

export function preparePlayerSource(
  source: string,
  position: number,
  signal: AbortSignal,
) {
  signal.throwIfAborted();
  return isHlsPlaylist(source)
    ? prepareHls(source, signal)
    : prepareDirect(source, position, signal);
}
