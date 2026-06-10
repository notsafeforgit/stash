import { useEffect } from "react";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface PlayerSource {
  src: string;
  type?: string;
  label?: string;
}

export interface IMarker {
  title: string;
  seconds: number;
  end_seconds?: number | null;
  primaryTag: { name: string };
}

// ── Capability detection ──────────────────────────────────────────────────────

const _probeVideo = document.createElement("video");

/**
 * Returns true when the browser supports native HLS playback (i.e. it's
 * WebKit-on-Apple: macOS Safari or any iOS browser — iOS forces all engines
 * through WebKit).
 *
 * We can't use `canPlayType("application/vnd.apple.mpegurl")` because Chrome
 * returns `"maybe"` despite having zero native HLS support — a long-standing
 * browser quirk. UA sniffing is the only reliable signal here.
 */
export function hasNativeHLS(): boolean {
  if (typeof navigator === "undefined") return false;
  const ua = navigator.userAgent;
  // iOS / iPadOS — all browsers are WebKit
  if (/iPad|iPhone|iPod/.test(ua)) return true;
  // iPadOS 13+ masquerades as desktop macOS; detect via touch-capable Mac.
  if (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) return true;
  // macOS Safari proper — exclude every other engine that includes "Safari"
  // in its UA string (Chrome, Edge, Opera, Brave, Firefox-via-GeckoView, etc.)
  return /Safari/.test(ua) && !/Chrome|Chromium|Edg|OPR|Firefox|FxiOS/.test(ua);
}

/**
 * Returns true when running on iOS / iPadOS (all browsers there are
 * WebKit under the hood — Apple mandates it).
 */
export function isIOS(): boolean {
  if (typeof navigator === "undefined") return false;
  const ua = navigator.userAgent;
  if (/iPad|iPhone|iPod/.test(ua)) return true;
  // iPadOS 13+ masquerades as desktop macOS; detect via touch-capable Mac.
  if (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) return true;
  return false;
}

/**
 * Pick the same MSE constructor hls.js will use, so codec-support probes
 * match the runtime-active engine. iOS Safari 17.1+ ships
 * `ManagedMediaSource` (a stricter, power-aware MSE variant), and hls.js
 * prefers it via `getMediaSource(preferManagedMediaSource = true)`. Without
 * mirroring that preference here, an `isTypeSupported` query against the
 * regular `MediaSource` (or worse, the absence of one) would over- or
 * under-report what hls.js can actually demux on the current device.
 */
function getActiveMSE(): typeof MediaSource | undefined {
  if (typeof self === "undefined") return undefined;
  const mms = (self as unknown as { ManagedMediaSource?: typeof MediaSource })
    .ManagedMediaSource;
  if (mms) return mms;
  if (typeof MediaSource !== "undefined") return MediaSource;
  return (self as unknown as { WebKitMediaSource?: typeof MediaSource })
    .WebKitMediaSource;
}

function hasMSE(): boolean {
  const ms = getActiveMSE();
  return (
    !!ms && ms.isTypeSupported('video/mp4; codecs="avc1.42E01E,mp4a.40.2"')
  );
}

// ── Codec probe for the fMP4-remux HLS path ──────────────────────────────────
//
// Safari's `canPlayType` on `video/mp4; codecs="…"` reports whether the
// browser can decode the given codec combination inside an MP4 container.
// Used to decide, per-client, whether the backend's `/stream.fmp4.m3u8`
// endpoint (codec copy into fMP4 HLS) is actually going to play — e.g.
// AV1 needs Safari 17+ on Apple Silicon M3+ / A17 Pro+; older hardware
// must fall back to the full transcode.

function videoCodecMp4Spec(codec: string | null | undefined): string | null {
  // Codec levels are picked to match the 4K60 probe resolution below:
  // MediaCapabilities rejects a config whose stated Level can't accommodate
  // the stated width×height×framerate, so Level 5.1 is the right point for
  // all three codecs — it covers 4K60 (≥3840×2160@60) with headroom.
  switch (codec) {
    case "h264":
      return "avc1.640033"; // H.264 High @ Level 5.1
    case "hevc":
    case "h265":
      return "hvc1.1.6.L153.B0"; // HEVC Main @ Level 5.1
    case "av1":
      return "av01.0.09M.08"; // AV1 Main @ Level 5.1, 8-bit 4:2:0
    default:
      return null;
  }
}

function audioCodecMp4Spec(codec: string | null | undefined): string | null {
  switch (codec) {
    case "aac":
      return "mp4a.40.2";
    case "opus":
      return "opus";
    case "mp3":
      return "mp4a.40.34";
    default:
      return null;
  }
}

// ── Hardware-decode probe ────────────────────────────────────────────────────
//
// Deciding whether to offer the codec-copy fMP4 HLS endpoint on Safari
// requires distinguishing hardware that can decode the source's codecs
// from hardware that can't:
// - M1/M2 MacBooks advertise AV1 support (`canPlayType` returns "probably")
//   but playback fails at runtime with "Media failed to decode" — no AV1
//   hardware, and Safari's AVFoundation AV1 path is broken on pre-M3.
// - M3+ Macs and A17 Pro / A18 iPhones have real AV1 hardware and play
//   fine; they must *not* be lumped in with M1/M2.
//
// `MediaCapabilities.decodingInfo.powerEfficient` is unusable — WebKit
// returns `false` unconditionally (known quirk), so gating on it
// misclassifies every Safari device. `smooth` at 4K60 works cleanly on
// *macOS* Safari (hardware decoders report smooth=true, M1/M2 software
// AV1 can't sustain 4K60 and reports smooth=false), but iOS WebKit
// under-reports `smooth` even for A17 Pro hardware — so relying on it
// there incorrectly rejects the device the remux path was built for.
//
// Split the logic by platform: macOS Safari requires `smooth` at 4K60
// (distinguishes M1/M2 from M3+). iOS / iPadOS Safari trusts `supported`
// alone — iOS codec registration is honest (Safari doesn't advertise
// decoders it can't run, unlike macOS listing AV1 on pre-M3 chips).

type ProbeKey = string;
const codecProbeCache = new Map<ProbeKey, boolean>();
const codecProbePending = new Map<ProbeKey, Promise<boolean>>();

function probeKey(v: string, a: string): ProbeKey {
  return `${v}|${a}`;
}

async function runCodecProbe(
  videoSpec: string,
  audioSpec: string | null,
): Promise<boolean> {
  // hls.js drives every HLS playback path now (MSE on Chrome/Firefox/Edge,
  // ManagedMediaSource on iOS Safari 17.1+, classic MediaSource on macOS
  // Safari). The codec-copy fMP4 variant feeds source-codec data straight
  // into a SourceBuffer with no transcode, so the MSE/MMS that hls.js
  // chooses must actually accept the combined codec string. Browsers can
  // disagree with their `<video>.canPlayType` self-report here (notably:
  // iOS Safari plays Opus from a top-level `<video src>` but its MMS
  // doesn't always accept Opus in fMP4, leaving hls.js to silently drop
  // the audio track). Probe the MSE engine first; if it rejects the
  // combined codec, no amount of MediaCapabilities sugar will save us.
  const ms = getActiveMSE();
  if (ms?.isTypeSupported) {
    const codecs = audioSpec ? `${videoSpec},${audioSpec}` : videoSpec;
    if (!ms.isTypeSupported(`video/mp4; codecs="${codecs}"`)) return false;
  }

  // MediaCapabilities is available in every modern browser that can run
  // this code (Safari 14+, Chrome 66+, Firefox 63+). When it's somehow
  // missing, fall back to the strict `"probably"` branch of canPlayType —
  // less accurate, but better than assuming yes.
  const mc =
    typeof navigator !== "undefined" ? navigator.mediaCapabilities : undefined;
  if (!mc?.decodingInfo) {
    const codecs = audioSpec ? `${videoSpec},${audioSpec}` : videoSpec;
    return (
      _probeVideo.canPlayType(`video/mp4; codecs="${codecs}"`) === "probably"
    );
  }
  try {
    // On iOS, omit the audio half of the probe. iOS Safari's
    // MediaCapabilities under-reports `audio/mp4; codecs="opus"` as
    // unsupported even though Safari has shipped Opus-in-MP4 since
    // version 13 (2019). With audio included, an AV1+Opus source
    // returns `supported=false` on an A17 Pro+ iPhone that can
    // decode both tracks fine — suppressing the fMP4-remux offer.
    // `audioCodecMp4Spec` already constrains audio to AAC/Opus/MP3,
    // all of which iOS plays reliably in MP4, so a video-only probe
    // is safe here. macOS Safari keeps the combined probe because we
    // want `info.smooth` to gate AV1 against M1/M2 software-decode
    // paths, and the combined probe's `supported` bit happens to be
    // honest there for the audio codecs we accept.
    const info = await mc.decodingInfo({
      type: "file",
      video: {
        contentType: `video/mp4; codecs="${videoSpec}"`,
        // 4K60 is the stress point that forces `smooth` to require a
        // hardware decoder on macOS Safari. Codec levels in
        // `videoCodecMp4Spec` are picked to match — otherwise
        // MediaCapabilities returns `supported=false` because the
        // stated Level can't carry these dimensions.
        width: 3840,
        height: 2160,
        bitrate: 25_000_000,
        framerate: 60,
      },
      audio:
        audioSpec && !isIOS()
          ? {
              contentType: `audio/mp4; codecs="${audioSpec}"`,
              channels: "2",
              bitrate: 128_000,
              samplerate: 48_000,
            }
          : undefined,
    });
    if (!info.supported) return false;
    // iOS video codec registration is honest — if `supported=true`
    // on iOS/iPadOS Safari for the video track, playback works (and
    // audio is trusted per the comment above). macOS Safari needs
    // `smooth` as the tiebreaker because it over-reports `supported`
    // (listing AV1 on M1/M2 despite no HW decoder and a broken
    // software path).
    if (isIOS()) return true;
    return !!info.smooth;
  } catch {
    return false;
  }
}

function cacheKeyForCodecs(
  videoCodec: string | null | undefined,
  audioCodec: string | null | undefined,
): ProbeKey | null {
  const v = videoCodecMp4Spec(videoCodec);
  if (!v) return null;
  const a = audioCodec ? audioCodecMp4Spec(audioCodec) : null;
  if (audioCodec && !a) return null;
  return probeKey(v, a ?? "");
}

/**
 * Async probe: can the browser decode the given (video, audio) codec pair
 * inside an MP4 container using a hardware decoder? Cached module-level so
 * repeated probes within a session are free. Returns false when either
 * codec is unrecognised, or when MediaCapabilities can't deliver a smooth
 * 4K60 stream with the requested codec — which on Apple platforms
 * distinguishes hardware-accelerated decode (M3+/A17+) from software-only
 * fallback paths (M1/M2) that either fail outright or stall under load.
 */
export async function probeCodecsDecodableInMp4(
  videoCodec: string | null | undefined,
  audioCodec: string | null | undefined,
): Promise<boolean> {
  const key = cacheKeyForCodecs(videoCodec, audioCodec);
  if (!key) return false;
  const cached = codecProbeCache.get(key);
  if (cached !== undefined) return cached;
  const inflight = codecProbePending.get(key);
  if (inflight) return inflight;
  const v = videoCodecMp4Spec(videoCodec)!;
  const a = audioCodec ? audioCodecMp4Spec(audioCodec) : null;
  const promise = runCodecProbe(v, a).then((result) => {
    codecProbeCache.set(key, result);
    codecProbePending.delete(key);
    return result;
  });
  codecProbePending.set(key, promise);
  return promise;
}

/**
 * Warm the probe cache synchronously so the first render of a new scene
 * sees the right `canDecode` value — direct stream and fMP4 remux can be
 * offered on the first load instead of only after the async
 * MediaCapabilities probe resolves and the page is reloaded.
 *
 * The MSE/MMS engine that hls.js will use exposes a synchronous
 * `isTypeSupported`, which is the same authoritative gate the async probe
 * applies first. So the prewarm can deliver a definitive `false` for any
 * codec MSE rejects (e.g. Opus on an iPhone whose ManagedMediaSource
 * doesn't accept it) without waiting for MediaCapabilities to round-trip.
 *
 * The async probe still has to run to handle the cases MSE doesn't catch:
 * macOS Safari over-reports AV1 on M1/M2 via `isTypeSupported` even
 * though playback stutters under load — only `MediaCapabilities.smooth`
 * at 4K60 distinguishes the hardware decoder. So the prewarm only writes
 * a definitive `false` (which is invariant — if MSE can't even claim
 * support, no MediaCapabilities check could rescue it). When MSE accepts
 * the combined codec, the prewarm leaves the cache empty and lets the
 * async probe decide.
 */
function prewarmCodecCacheSync(
  videoCodec: string | null | undefined,
  audioCodec: string | null | undefined,
): void {
  const key = cacheKeyForCodecs(videoCodec, audioCodec);
  if (!key) return;
  if (codecProbeCache.has(key)) return;
  const videoSpec = videoCodecMp4Spec(videoCodec);
  if (!videoSpec) return;
  const audioSpec = audioCodec ? audioCodecMp4Spec(audioCodec) : null;
  const ms = getActiveMSE();
  if (!ms?.isTypeSupported) return;
  const codecs = audioSpec ? `${videoSpec},${audioSpec}` : videoSpec;
  if (!ms.isTypeSupported(`video/mp4; codecs="${codecs}"`)) {
    codecProbeCache.set(key, false);
  }
}

/**
 * Hook wrapper around `probeCodecsDecodableInMp4`. Reads the module-level
 * probe cache each render and fires the async probe to populate it for
 * future consumers — deliberately *does not* setState on the probe's
 * resolution. A consuming component that read `false` at mount would
 * otherwise see it flip to `true` mid-scene once the probe completes,
 * causing the preferred-source picker to swap HLS transcode → remux and
 * force a MediaPlayer remount. That presents to the user as a fresh-
 * stream buffer load just to play a second. Consumers that need
 * stability across renders should pin the first-render value (e.g.
 * `ScenePlayer` resets its ref on `scene.id` change).
 *
 * On native-HLS platforms the cache is warmed synchronously from
 * `canPlayType` before the async probe fires (except for AV1 on macOS
 * Safari, where canPlayType over-reports — see `prewarmCodecCacheSync`).
 * That means the first render already returns the true answer for the
 * common codec cases, so direct stream / fMP4 remux are offered on the
 * first scene of a fresh page load instead of only after a reload.
 */
export function useCodecsDecodableInMp4(
  videoCodec: string | null | undefined,
  audioCodec: string | null | undefined,
): boolean {
  prewarmCodecCacheSync(videoCodec, audioCodec);
  useEffect(() => {
    void probeCodecsDecodableInMp4(videoCodec, audioCodec);
  }, [videoCodec, audioCodec]);
  const key = cacheKeyForCodecs(videoCodec, audioCodec);
  return key ? (codecProbeCache.get(key) ?? false) : false;
}

/**
 * Audio-agnostic variant of `useCodecsDecodableInMp4` — returns true if
 * the source video codec alone is MSE/MMS-decodable in fMP4. Gates the
 * "video-copy + AAC-transcode" HLS variant, which always produces AAC
 * audio (so the source's audio codec doesn't constrain the decision).
 *
 * Reuses the same probe cache as the combined check because passing
 * `audioCodec=undefined` to `cacheKeyForCodecs` produces a distinct
 * video-only cache key — the combined and video-only entries don't
 * conflict.
 */
export function useVideoCodecDecodableInMp4(
  videoCodec: string | null | undefined,
): boolean {
  return useCodecsDecodableInMp4(videoCodec, undefined);
}

// HLS MIME types — every variant the Apple-registered IANA list and common
// servers emit, plus the legacy non-vendor forms.
const HLS_MIME_TYPES = new Set([
  "application/vnd.apple.mpegurl",
  "application/x-mpegurl",
  "audio/mpegurl",
  "audio/x-mpegurl",
  "video/x-mpegurl",
  "video/mpegurl",
  "application/mpegurl",
]);

function isHLSSource(source: PlayerSource): boolean {
  if (source.type && HLS_MIME_TYPES.has(source.type)) return true;
  try {
    return new URL(source.src).pathname.endsWith(".m3u8");
  } catch {
    return false;
  }
}

/**
 * Returns true if the browser can play the given source.
 *
 * - HLS: requires either native HLS (Safari) or MSE (Chrome/Firefox/Edge).
 *   All HLS playback is driven by hls.js (via `<StableHlsVideo>` →
 *   `@videojs/core`'s `HlsMedia`), which uses MSE on every modern browser
 *   and falls back to native HLS where MSE isn't available. The
 *   per-source `canDecode` check upstream gates the codec-copy fMP4
 *   variant on whether the source's actual codecs play in the browser;
 *   here we only care that there's *some* HLS pipeline available.
 * - MKV (by MIME `video/x-matroska` on the truthful direct endpoint):
 *   Chrome/Firefox play Matroska containers natively for codecs the
 *   browser supports, but `canPlayType("video/x-matroska")` returns `""`
 *   everywhere so we can't delegate. Gate positively on MSE-capable
 *   non-Safari browsers — Safari can't play MKV from `<video>`.
 * - Everything else delegates to `HTMLVideoElement.canPlayType`.
 */
export function canPlaySource(source: PlayerSource): boolean {
  const type = source.type;

  if (isHLSSource(source)) {
    return hasNativeHLS() || hasMSE();
  }

  if (
    type === "video/x-matroska" ||
    type === "video/x-mkv" ||
    type === "video/matroska"
  ) {
    return hasMSE() && !hasNativeHLS();
  }

  if (!type) return true;
  return _probeVideo.canPlayType(type) !== "";
}

// ── Tag colour computation ─────────────────────────────────────────────────────

/** djb2 hash — fast, good distribution for short strings. */
function djb2(str: string): number {
  let hash = 5381;
  for (let i = 0; i < str.length; i++) {
    hash = ((hash << 5) + hash + str.charCodeAt(i)) >>> 0;
  }
  return hash;
}

function hsvToRgb(h: number, s: number, v: number): [number, number, number] {
  const i = Math.floor(h * 6);
  const f = h * 6 - i;
  const p = v * (1 - s);
  const q = v * (1 - f * s);
  const t = v * (1 - (1 - f) * s);
  let r: number, g: number, b: number;
  switch (i % 6) {
    case 0:
      r = v;
      g = t;
      b = p;
      break;
    case 1:
      r = q;
      g = v;
      b = p;
      break;
    case 2:
      r = p;
      g = v;
      b = t;
      break;
    case 3:
      r = p;
      g = q;
      b = v;
      break;
    case 4:
      r = t;
      g = p;
      b = v;
      break;
    default:
      r = v;
      g = p;
      b = q;
      break;
  }
  return [Math.round(r * 255), Math.round(g * 255), Math.round(b * 255)];
}

function toHex(n: number) {
  return n.toString(16).padStart(2, "0");
}

function hueToColor(hue: number): string {
  const [r, g, b] = hsvToRgb(hue / 360, 0.65, 0.95);
  const alpha = Math.round(0.6 * 255);
  return `#${toHex(r)}${toHex(g)}${toHex(b)}${toHex(alpha)}`;
}

function calculateDeltaMin(n: number): number {
  const scalingFactor = n <= 4 ? 0.8 : n <= 10 ? 0.6 : 0.4;
  return Math.min((360 / n) * scalingFactor, 35);
}

/**
 * Assigns a unique colour per tag name. Uses djb2 → base hue then spreads
 * hues apart so nearby tags don't share similar colours.
 */
export function computeTagColors(tagNames: string[]): Record<string, string> {
  if (tagNames.length === 0) return {};

  const baseHues: Record<string, number> = {};
  for (const tag of tagNames) {
    baseHues[tag] = djb2(tag) % 360;
  }

  const n = tagNames.length;
  const deltaMin = calculateDeltaMin(n);
  const sorted = [...tagNames].sort((a, b) => baseHues[a] - baseHues[b]);
  const unwrapped = sorted.map((t) => baseHues[t]);

  for (let i = 1; i < n; i++) {
    if (unwrapped[i] <= unwrapped[i - 1]) unwrapped[i] += 360;
  }
  for (let i = 1; i < n; i++) {
    const required = unwrapped[i - 1] + deltaMin;
    if (unwrapped[i] < required) unwrapped[i] = required;
  }

  if (n > 1) {
    const endGap = unwrapped[0] + 360 - unwrapped[n - 1];
    if (endGap < deltaMin) {
      const adj = (deltaMin - endGap) / 2;
      unwrapped[0] = Math.max(
        unwrapped[0] - adj,
        unwrapped[1] - 360 + deltaMin,
      );
      unwrapped[n - 1] += adj;
    }
  }

  const colors: Record<string, string> = {};
  for (let i = 0; i < n; i++) {
    colors[sorted[i]] = hueToColor(unwrapped[i] % 360);
  }
  return colors;
}

// ── MWIS ──────────────────────────────────────────────────────────────────────

/**
 * Maximum Weight Independent Set: returns the subset of non-overlapping
 * range markers with the greatest total duration. Used layer-by-layer so
 * overlapping ranges are pushed to higher rows.
 */
export function findMWIS(markers: IMarker[]): IMarker[] {
  if (!markers.length) return [];
  const ms = [...markers].sort(
    (a, b) => (a.end_seconds ?? 0) - (b.end_seconds ?? 0),
  );
  const n = ms.length;
  const p: number[] = new Array(n).fill(-1);
  for (let j = 0; j < n; j++) {
    for (let i = j - 1; i >= 0; i--) {
      if ((ms[i].end_seconds ?? 0) <= ms[j].seconds) {
        p[j] = i;
        break;
      }
    }
  }
  const M: number[] = new Array(n).fill(0);
  for (let j = 0; j < n; j++) {
    const inc = (ms[j].end_seconds ?? 0) - ms[j].seconds + (M[p[j]] ?? 0);
    const exc = j > 0 ? M[j - 1] : 0;
    M[j] = Math.max(inc, exc);
  }
  const findSolution = (j: number): IMarker[] => {
    if (j < 0) return [];
    const inc = (ms[j].end_seconds ?? 0) - ms[j].seconds + (M[p[j]] ?? 0);
    const exc = j > 0 ? M[j - 1] : 0;
    return inc >= exc ? [...findSolution(p[j]), ms[j]] : findSolution(j - 1);
  };
  return findSolution(n - 1);
}
