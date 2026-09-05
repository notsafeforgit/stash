import { isHlsPlaylist } from "./hls";

/** Convenience wrapper for callers that just want a yes/no. */
export function usesStartOffset(src: string | null | undefined): boolean {
  return isHlsPlaylist(src);
}

export function injectStartOffset(src: string, offset: number): string {
  if (offset <= 0) return src;
  try {
    const url = new URL(src);
    url.searchParams.set(
      "start",
      (Math.floor(offset * 1000) / 1000).toString(),
    );
    return url.toString();
  } catch {
    return src;
  }
}

/**
 * Append a `?end=<seconds>` clip-end parameter. The backend trims the
 * playlist to end at the segment containing this value (and trims the
 * leading segments to match `?start=`); together they produce a
 * clip-only playlist whose MSE timeline is rebased so the clip start
 * lands at MSE 0. Used for marker / clip playback where the native
 * fullscreen player should expose only the clip range as seekable.
 */
export function injectEndOffset(src: string, end: number): string {
  if (end <= 0) return src;
  try {
    const url = new URL(src);
    url.searchParams.set("end", (Math.floor(end * 1000) / 1000).toString());
    return url.toString();
  } catch {
    return src;
  }
}

/** True when the source URL is the direct-stream byte-range endpoint
 *  (`/scene/<id>/stream` with no `?resolution=`). Used to detect when
 *  to filter the source out of the candidate list for clipped mode
 *  on iOS Safari (where the OS-level fullscreen UI reads the slider
 *  bounds from `video.duration`, which a direct stream reports as
 *  `fileDuration` even with a `#t=` fragment URI). */
export function isDirectStreamSrc(src: string | null | undefined): boolean {
  if (!src) return false;
  try {
    const url = new URL(src);
    return (
      url.pathname.endsWith("/stream") && !url.searchParams.has("resolution")
    );
  } catch {
    return false;
  }
}

/**
 * Append a Media Fragments URI `#t=<start>,<end>` so the browser
 * initialises `<video>.currentTime` to `start` and stops playback at
 * `end`. Used for clip mode on direct-stream sources outside iOS
 * Safari — desktop browsers and Android Chrome respect this fragment
 * for both initial seek and end-of-clip stop. iOS Safari does not
 * (it ignores the `end` half), which is why the player filters
 * direct stream out of the source candidates for clipped mode on
 * that platform.
 *
 * Strips any existing `#…` fragment first so the URL stays idempotent
 * across re-renders.
 */
export function injectFragmentTimeRange(
  src: string,
  start: number,
  end: number,
): string {
  if (end <= start) return src;
  const base = src.split("#")[0];
  const s = Math.max(0, Math.floor(start * 1000) / 1000);
  const e = Math.floor(end * 1000) / 1000;
  return `${base}#t=${s},${e}`;
}

/**
 * Append a Media Fragments URI `#t=<seconds>` so the browser initialises
 * `<video>.currentTime` to that value before any media data is fetched.
 *
 * Used for sources where the server can't pre-position the stream
 * (direct byte-range files). Browsers honouring the fragment skip the
 * time-0 buffer pass entirely and request the byte range that contains
 * the target frame on the first round trip. Browsers that ignore it
 * fall back to the existing post-`canPlay` `s.seek(target)`, which
 * lands as a no-op when the fragment was honoured.
 *
 * Truncates to 3 decimals to match `injectStartOffset`. Strips any
 * existing `#…` fragment first so the URL stays idempotent under
 * repeated calls (e.g. across source changes that reuse the same
 * activeSrc).
 */
export function injectFragmentTime(src: string, t: number): string {
  if (t <= 0) return src;
  const base = src.split("#")[0];
  return `${base}#t=${(Math.floor(t * 1000) / 1000).toString()}`;
}

/**
 * True for sources where appending `#t=<seconds>` lands on the right frame
 * without breaking loading: direct byte-range file streams (`/stream`).
 * Excludes:
 *   - HLS playlists (`.m3u8`) — Safari's native HLS does not honour Media
 *     Fragments URI on the playlist URL; setting `#t=N` either gets
 *     silently dropped or trips up the loader so playback stalls at 0.
 *     HLS uses `?start=` on the playlist URL instead (server trims the
 *     playlist to start at the matching segment).
 *   - Transcode sources (`/stream.mp4` etc.) — they use server-side
 *     `?start=` which is strictly better.
 */
export function supportsFragmentTime(src: string | null | undefined): boolean {
  if (!src) return false;
  try {
    const path = new URL(src).pathname;
    return path.endsWith("/stream");
  } catch {
    return false;
  }
}

/**
 * Force the browser to reload a stable stream URL after the underlying file
 * changes. Keep the query parameter before any Media Fragments suffix: a
 * cachebuster appended after `#t=` becomes part of the fragment value and can
 * invalidate a direct stream's resume timestamp.
 */
export function injectReloadNonce(src: string, nonce: number): string {
  if (nonce === 0) return src;
  const fragmentIndex = src.indexOf("#");
  const base = fragmentIndex >= 0 ? src.slice(0, fragmentIndex) : src;
  const fragment = fragmentIndex >= 0 ? src.slice(fragmentIndex) : "";
  const separator = base.includes("?") ? "&" : "?";
  return `${base}${separator}_r=${nonce}${fragment}`;
}

export function scenePlayerSourceURL(
  activeSrc: string | undefined,
  fragmentTime: number | null,
  clipRange: { start: number; end: number } | undefined,
  reloadNonce: number,
): string | undefined {
  // The cachebuster is added only to the playlist / file URL. HLS segment
  // URIs don't carry it, so only the playlist re-fetch is forced.
  const withReloadNonce = (url: string): string =>
    injectReloadNonce(url, reloadNonce);
  if (!activeSrc) return undefined;
  if (usesStartOffset(activeSrc)) {
    // HLS: encode the desired scene-time start position into the URL
    // as `?start=N`. The frontend parses this back out in
    // `StableHlsVideo` to set hls.js `config.startPosition`, so the
    // FIRST segment request lands on the segment containing N (no
    // segment-0 cold-start detour). Server-side, the value is
    // ignored for playlist generation when `?end=` is absent (the
    // playlist always covers segments 0..last) — see
    // `serveHLSManifestFMP4`.
    //
    // For the marker / clip case (clipRange set), `?start=` is
    // pinned to `clipRange.start` (NOT the live playhead) so the
    // server's playlist always covers the full clip [start, end].
    // Otherwise a mid-clip resolution swap would walk the playlist
    // floor forward to the playhead, leaving the clip's earlier
    // range unreachable until the user re-opens the marker. `?end=`
    // pins the upper bound. Together they trim to
    // `[⌊start/segDur⌋, ⌈end/segDur⌉-1]`, MSE-time rebases to 0 at
    // the clip start, and native iOS fullscreen sees only the clip
    // range as seekable.
    const trim = clipRange ? clipRange.start : (fragmentTime ?? 0);
    let url = injectStartOffset(activeSrc, trim);
    if (clipRange) {
      url = injectEndOffset(url, clipRange.end);
    }
    return withReloadNonce(url);
  }
  if (
    clipRange &&
    isDirectStreamSrc(activeSrc) &&
    supportsFragmentTime(activeSrc)
  ) {
    // Direct-stream marker on non-iOS browsers (the sources
    // candidate filter above drops direct stream from clip mode on
    // iOS). Use the Media Fragments URI temporal range `#t=N,M`:
    // browsers respecting it seek to N at load and stop playback at
    // M. Combined with `PlaybackRangeEffect`'s scene-time stop, the
    // clip boundary is enforced both natively and at the React
    // layer. Our custom fullscreen (`Element.requestFullscreen` on
    // the player container) keeps the clipRange-aware custom
    // controls visible, so the fullscreen slider already reads
    // clip-relative — no `video.duration` rewrite needed.
    return withReloadNonce(
      injectFragmentTimeRange(activeSrc, clipRange.start, clipRange.end),
    );
  }
  if (
    fragmentTime != null &&
    fragmentTime > 0 &&
    supportsFragmentTime(activeSrc)
  ) {
    return withReloadNonce(injectFragmentTime(activeSrc, fragmentTime));
  }
  return withReloadNonce(activeSrc);
}

/**
 * Map an HLS master-playlist source URL to the backend stream-type
 * identifier baked into the segment cache dir (see
 * `pkg/ffmpeg/stream_segmented.go` — `StreamType.Name`). Used by the
 * client-initiated `streams.stop?keep_type=...&keep_resolution=...`
 * beacon so a resolution swap can tear down the previous transcode
 * without racing the new one's first segment request. Returns null
 * for non-HLS sources or URLs that don't parse.
 */
export function hlsStreamTypeName(src: string): string | null {
  try {
    const path = new URL(src, "https://stash.invalid").pathname;
    if (path.endsWith("/stream.fmp4.aac.master.m3u8"))
      return "hls-copy-fmp4-aac";
    if (path.endsWith("/stream.fmp4.master.m3u8")) return "hls-copy-fmp4";
    if (path.endsWith("/stream.master.m3u8")) return "hls";
    return null;
  } catch {
    return null;
  }
}

/** Extract the `?resolution=` value from a stream URL, or "" if absent. */
export function streamResolution(src: string): string {
  try {
    return (
      new URL(src, "https://stash.invalid").searchParams.get("resolution") ?? ""
    );
  } catch {
    return "";
  }
}
