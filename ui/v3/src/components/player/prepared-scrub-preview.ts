import { readTimeRanges } from "./buffered-ranges";
import {
  ScrubFrameCache,
  type ScrubStill,
  scrubPreparationTimes,
} from "./scrub-frame-cache";
import {
  fetchKeyframeIndex,
  fetchKeyframeStill,
  type KeyframeIndex,
  type KeyframeStillResult,
  keyframeAt,
  keyframePreparationTimes,
  keyframeStillURL,
} from "./scrub-keyframes";
import { createScrubStream } from "./scrub-stream";
import { recordScrubTiming } from "./scrub-timings";

/** Healthy main playback required before background still requests. */
const PREPARATION_DELAY_MS = 1500;
/** Background coverage; the rest of the cache serves foreground targets. */
const BACKGROUND_STILLS = 128;
const BACKGROUND_REQUESTS = 2;
const FOREGROUND_REQUESTS = 2;
const INDEX_TIMEOUT_MS = 10_000;
const STILL_TIMEOUT_MS = 5000;
const INACTIVITY_MS = 5 * 60_000;
/** Consecutive extraction failures before this preview stops asking. */
const STILL_FAILURE_LIMIT = 3;

interface StillRequest {
  key: string;
  url: string;
  /** The requested scene time; the response may be an earlier keyframe. */
  time: number;
  background: boolean;
}

/** Keyframe stills cover broad movement immediately; one silent decoder
 * supplies the exact frame as the finger dwells. Never changes the main video. */
export function createPreparedScrubPreview({
  surface,
  sceneId,
  source,
  keyframes,
  start,
  end,
  offset,
  onFailure,
  onReady,
}: {
  surface: HTMLDivElement;
  sceneId: string;
  source: string;
  /** Null for players without library access; stills are then unavailable. */
  keyframes: { index: string; still: string } | null;
  start: number;
  end: number;
  offset: number;
  onFailure: () => void;
  /** The exact requested target is decoded, or null when unavailable. */
  onReady: (time: number | null) => void;
}) {
  const cache = new ScrubFrameCache();
  const image = document.createElement("img");
  image.alt = "";
  image.dataset.scrubStill = "";
  image.className = "absolute inset-0 size-full object-contain";
  image.style.opacity = "0";
  surface.append(image);
  surface.dataset.previewFrames = "0";
  let video: HTMLVideoElement | null = null;
  let controller: ReturnType<typeof createScrubStream> | null = null;
  let target: number | null = null;
  /** When the current target was requested, for time-to-display timings. */
  let targetAt = 0;
  let targetCached = false;
  let stillTimed = false;
  let exactTimed = false;
  let exactShown = false;
  let eligibleSince = 0;
  let lastActivity = performance.now();
  let sampleIndex = 0;
  let samples: number[] | null = null;
  let selection = 0;
  let generation = 0;
  let disposed = false;
  let failed = false;
  let next: ReturnType<typeof setTimeout> | undefined;
  let index: KeyframeIndex | null = null;
  let indexState: "idle" | "loading" | "retry" | "ready" | "unavailable" =
    keyframes ? "idle" : "unavailable";
  let indexAttempts = 0;
  let indexRetryAt = 0;
  let indexRequest: AbortController | null = null;
  let stillsAvailable = keyframes !== null;
  let stillFailures = 0;
  let backoffUntil = 0;
  let backoffTimer: ReturnType<typeof setTimeout> | undefined;
  /** The current target still needs a foreground request once a slot frees. */
  let foregroundWaiting = false;
  const requests = new Map<
    string,
    {
      background: boolean;
      abort: AbortController;
      timer: ReturnType<typeof setTimeout>;
    }
  >();

  const stillRequest = (
    time: number,
    background: boolean,
  ): StillRequest | null => {
    if (!keyframes) return null;
    if (index) {
      // Canonical URL: the exact index time and file version.
      const keyframe = keyframeAt(index.times, time);
      if (keyframe === undefined) return null;
      return {
        key: String(keyframe),
        url: keyframeStillURL(keyframes.still, keyframe, index.version),
        time,
        background,
      };
    }
    const raw = Math.round(time * 1000) / 1000;
    return {
      key: `raw:${raw}`,
      url: keyframeStillURL(keyframes.still, raw),
      time,
      background,
    };
  };
  /** The still for the keyframe at or before `time`, when already cached. */
  const cachedStill = (time: number) => {
    if (!index) return cache.covering(time);
    const keyframe = keyframeAt(index.times, time);
    return keyframe === undefined ? undefined : cache.get(keyframe);
  };
  const videoBuffered = (time: number) =>
    !!video &&
    readTimeRanges(video.buffered).some(
      ([from, to]) => from <= time && to > time,
    );
  // Nearby decoded video stays on screen while fine seeking catches up;
  // avoid flashing a coarser still between consecutive precise frames.
  const keepVideo = (time: number) =>
    surface.dataset.previewKind === "video" &&
    (exactShown || videoBuffered(time));
  const countRequests = (background: boolean) => {
    let count = 0;
    for (const request of requests.values())
      if (request.background === background) count++;
    return count;
  };
  const abortRequests = (
    predicate: (key: string, background: boolean) => boolean = () => true,
  ) => {
    for (const [key, request] of requests)
      if (predicate(key, request.background)) {
        clearTimeout(request.timer);
        request.abort.abort();
        requests.delete(key);
      }
  };

  const hide = () => {
    onReady(null);
    selection++;
    surface.style.opacity = "0";
    surface.dataset.previewReady = "false";
  };
  const clear = () => {
    generation++;
    abortRequests();
    foregroundWaiting = false;
    controller?.release();
    hide();
    cache.clear();
    image.removeAttribute("src");
    delete image.dataset.scrubTime;
    surface.dataset.previewFrames = "0";
    samples = null;
    sampleIndex = 0;
    eligibleSince = 0;
  };
  const showStill = (frame: ScrubStill, exact: boolean) => {
    if (target === null) return;
    const unchanged =
      surface.dataset.previewKind === "still" &&
      surface.dataset.previewReady === "true" &&
      image.dataset.scrubTime === String(frame.time);
    // An already displayed still is not a new measurement for this target.
    if (exact && unchanged) stillTimed = true;
    const timed = exact && !stillTimed;
    const tier = targetCached ? "still-cache" : "still-network";
    const requestedAt = targetAt;
    const requested = ++selection;
    const show = () => {
      if (disposed || target === null || requested !== selection) return;
      image.dataset.scrubTime = String(frame.time);
      image.style.opacity = "1";
      if (video) video.style.opacity = "0";
      exactShown = false;
      surface.style.opacity = "1";
      surface.dataset.previewKind = "still";
      surface.dataset.previewReady = "true";
      if (timed && !stillTimed) {
        stillTimed = true;
        recordScrubTiming(tier, performance.now() - requestedAt);
      }
    };
    image.onload = show;
    if (image.src !== frame.url) image.src = frame.url;
    if (image.complete && image.naturalWidth > 0) show();
  };
  /** Shows the target's keyframe, or the nearest prepared still meanwhile.
   * Neither counts as the requested frame for the precision dwell. */
  const present = (time: number) => {
    if (keepVideo(time)) return;
    const exact = cachedStill(time);
    if (exact) {
      showStill(exact, true);
      return;
    }
    const nearest = cache.nearest(time, Math.max(2, (end - start) / 8));
    if (nearest) showStill(nearest, false);
  };

  const settle = (request: StillRequest, result: KeyframeStillResult) => {
    switch (result.status) {
      case "aborted":
        return;
      case "ok": {
        stillFailures = 0;
        const time =
          result.time ?? (index ? Number(request.key) : request.time);
        cache.add(time, result.blob, request.background, request.time);
        surface.dataset.previewFrames = String(cache.size);
        break;
      }
      case "busy":
        backoffUntil = performance.now() + result.after * 1000;
        break;
      case "missing":
        stillsAvailable = false;
        break;
      case "error":
        if (++stillFailures >= STILL_FAILURE_LIMIT) stillsAvailable = false;
        break;
    }
    if (!stillsAvailable) {
      abortRequests();
      foregroundWaiting = false;
    }
    if (target !== null) {
      present(target);
      pumpForeground();
    } else {
      clearTimeout(next);
      next = setTimeout(tick, 0);
    }
  };
  const fetchStill = (request: StillRequest) => {
    const existing = requests.get(request.key);
    if (existing) {
      // Foreground interest in a background request keeps it alive.
      if (!request.background) existing.background = false;
      return;
    }
    const abort = new AbortController();
    const entry = {
      background: request.background,
      abort,
      timer: setTimeout(() => abort.abort(), STILL_TIMEOUT_MS),
    };
    requests.set(request.key, entry);
    const owner = generation;
    void fetchKeyframeStill(request.url, abort.signal, request.background).then(
      (result) => {
        clearTimeout(entry.timer);
        // Cancellation removes its entry first; an entry still present when
        // its request aborted reached the timeout, a failed extraction.
        const current = requests.get(request.key) === entry;
        if (current) requests.delete(request.key);
        if (disposed || owner !== generation) return;
        settle(
          { ...request, background: entry.background },
          result.status === "aborted" && current ? { status: "error" } : result,
        );
      },
    );
  };
  /** Latest target wins: at most two foreground requests, and a newer target
   * replaces any target still waiting for a slot. Superseded responses only
   * populate the cache. */
  function pumpForeground() {
    clearTimeout(backoffTimer);
    if (target === null || !foregroundWaiting) return;
    if (!stillsAvailable || videoBuffered(target) || cachedStill(target)) {
      foregroundWaiting = false;
      return;
    }
    const wait = backoffUntil - performance.now();
    if (wait > 0) {
      backoffTimer = setTimeout(pumpForeground, wait);
      return;
    }
    const request = stillRequest(target, false);
    if (!request) {
      foregroundWaiting = false;
      return;
    }
    if (
      !requests.has(request.key) &&
      countRequests(false) >= FOREGROUND_REQUESTS
    )
      return;
    foregroundWaiting = false;
    fetchStill(request);
  }
  const loadIndex = () => {
    if (!keyframes || disposed) return;
    if (indexState !== "idle" && indexState !== "retry") return;
    if (indexState === "retry" && performance.now() < indexRetryAt) return;
    indexState = "loading";
    indexAttempts++;
    surface.dataset.keyframeIndex = "loading";
    const abort = new AbortController();
    indexRequest = abort;
    const timer = setTimeout(() => abort.abort(), INDEX_TIMEOUT_MS);
    void fetchKeyframeIndex(keyframes.index, abort.signal).then((result) => {
      clearTimeout(timer);
      if (disposed || indexRequest !== abort) return;
      indexRequest = null;
      if (result.status === "ok") {
        index = result.index;
        indexState = "ready";
        // Restart background ordering over the index's canonical times.
        samples = null;
        sampleIndex = 0;
      } else if (result.status === "retry" && indexAttempts < 2) {
        indexState = "retry";
        indexRetryAt = performance.now() + result.after * 1000;
      } else indexState = "unavailable";
      surface.dataset.keyframeIndex =
        indexState === "ready" ? "ready" : "unavailable";
      if (target !== null) pumpForeground();
      else {
        clearTimeout(next);
        next = setTimeout(tick, 0);
      }
    });
  };

  const ensureController = () => {
    if (controller) return controller;
    video = document.createElement("video");
    video.dataset.scrubPreview = "";
    video.muted = true;
    video.playsInline = true;
    video.disableRemotePlayback = true;
    video.preload = "auto";
    // The freeze frame draws the decoded preview frame into its canvas.
    video.crossOrigin = "anonymous";
    video.tabIndex = -1;
    video.className = "absolute inset-0 size-full object-contain";
    video.setAttribute("aria-hidden", "true");
    surface.prepend(video);
    controller = createScrubStream(
      video,
      sceneId,
      source,
      (visible, time) => {
        if (disposed) return;
        if (
          visible &&
          target !== null &&
          time !== undefined &&
          Math.abs(time - target) < 0.05
        ) {
          selection++;
          image.style.opacity = "0";
          video!.style.opacity = "1";
          surface.style.opacity = "1";
          surface.dataset.previewKind = "video";
          surface.dataset.previewReady = "true";
          exactShown = true;
          if (!exactTimed) {
            exactTimed = true;
            recordScrubTiming("exact-frame", performance.now() - targetAt);
          }
          onReady(target);
        } else if (!visible && target !== null) {
          onReady(null);
        } else if (target === null) hide();
      },
      () => {
        failed = true;
        onFailure();
      },
    );
    return controller;
  };

  function tick() {
    if (disposed || failed || target !== null || document.hidden) return;
    const root = surface.closest<HTMLElement>("[data-scene-player]");
    const main = root?.querySelector<HTMLVideoElement>(
      "video[data-playback-video]",
    );
    const now = performance.now();
    if (main && !main.paused) lastActivity = now;
    if (now - lastActivity > INACTIVITY_MS) {
      clear();
      return;
    }
    const ranges = main ? readTimeRanges(main.buffered) : [];
    const position = main?.currentTime ?? 0;
    const ahead =
      ranges.find(([from, to]) => from <= position && to > position)?.[1] ??
      position;
    const saveData = (
      navigator as Navigator & { connection?: { saveData?: boolean } }
    ).connection?.saveData;
    const healthy =
      !saveData &&
      !root?.querySelector('[data-position-scrubber][data-dragging="true"]') &&
      root?.dataset.playbackReady === "true" &&
      main &&
      !main.seeking &&
      !main.ended &&
      main.readyState >= 2 &&
      (main.paused || ahead - position >= 8 || ahead >= end - offset - 0.1);
    const fullyBuffered = ranges.some(
      ([from, to]) =>
        from <= start - offset + 0.05 && to >= end - offset - 0.05,
    );
    if (!healthy || fullyBuffered) {
      eligibleSince = 0;
      // Yield the server and network to the main player.
      abortRequests((_key, background) => background);
      controller?.release();
      return;
    }
    if (!eligibleSince) eligibleSince = now;
    if (now - eligibleSince < PREPARATION_DELAY_MS) return;
    loadIndex();
    if (!stillsAvailable || indexState === "loading" || backoffUntil > now)
      return;
    const first = Math.max(start, Math.min(end - 0.1, position + offset));
    samples ??= index
      ? keyframePreparationTimes(
          index.times,
          start,
          end,
          BACKGROUND_STILLS,
          first,
        )
      : [first, ...scrubPreparationTimes(start, end)];
    while (
      countRequests(true) < BACKGROUND_REQUESTS &&
      sampleIndex < samples.length
    ) {
      const time = samples[sampleIndex++]!;
      if (cachedStill(time)) continue;
      const request = stillRequest(time, true);
      if (request) fetchStill(request);
    }
  }
  const hidden = () => {
    if (document.hidden) clear();
  };
  document.addEventListener("visibilitychange", hidden);
  const interval = setInterval(tick, 500);

  return {
    preview(time: number | null) {
      const previous = target;
      if (time !== target) {
        onReady(null);
        target = time;
        targetAt = performance.now();
        stillTimed = false;
        exactTimed = false;
        exactShown = false;
        targetCached = time !== null && !!cachedStill(time);
      }
      eligibleSince = 0;
      lastActivity = performance.now();
      if (time === null) {
        // Release/cancel stops foreground fetching, leaving the server free
        // for the committed seek. Background coverage resumes when healthy.
        foregroundWaiting = false;
        clearTimeout(backoffTimer);
        abortRequests((_key, background) => !background);
        hide();
        controller?.preview(null);
        return;
      }
      if (previous === null) {
        // Foreground scrubbing takes priority over background coverage.
        const wanted = stillRequest(time, false)?.key;
        abortRequests((key, background) => background && key !== wanted);
      }
      loadIndex();
      present(time);
      foregroundWaiting = true;
      pumpForeground();
      ensureController().preview(time);
    },
    dispose() {
      disposed = true;
      clearInterval(interval);
      clearTimeout(next);
      clearTimeout(backoffTimer);
      indexRequest?.abort();
      indexRequest = null;
      document.removeEventListener("visibilitychange", hidden);
      clear();
      controller?.dispose();
      video?.remove();
      image.remove();
    },
  };
}
