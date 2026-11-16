import { readTimeRanges } from "./buffered-ranges";
import {
  KEYFRAME_TOLERANCE_SECONDS,
  ScrubFrameCache,
  type ScrubStill,
  scrubPreparationTimes,
} from "./scrub-frame-cache";
import {
  backgroundKeyframeBatches,
  denseKeyframeBatch,
  fetchKeyframeBatch,
  fetchKeyframeIndex,
  fetchKeyframeStill,
  KEYFRAME_BATCH_SIZE,
  type KeyframeBatchResult,
  type KeyframeIndex,
  type KeyframeStillResult,
  keyframeAt,
  keyframeBatchURL,
  keyframePosition,
  keyframePreparationTimes,
  keyframeStillURL,
} from "./scrub-keyframes";
import { createScrubStream } from "./scrub-stream";
import { recordScrubTiming } from "./scrub-timings";

/** Healthy main playback required before background still requests. */
const PREPARATION_DELAY_MS = 1500;
/** Background coverage per scene; the rest of the cache serves the finger. */
const BACKGROUND_STILLS = 64;
/** Single-still background requests, without an index or batch endpoint. */
const BACKGROUND_REQUESTS = 2;
const FOREGROUND_REQUESTS = 2;
const INDEX_TIMEOUT_MS = 10_000;
const STILL_TIMEOUT_MS = 5000;
const BATCH_TIMEOUT_MS = 10_000;
const INACTIVITY_MS = 5 * 60_000;
/** Consecutive extraction failures before this preview stops asking. */
const STILL_FAILURE_LIMIT = 3;
/** A finger crossing at most this many keyframes per second is slowing. */
const SLOW_KEYFRAMES_PER_SECOND = 8;
/** A stationary finger prefetches its dense neighbourhood after this. */
const DWELL_MS = 250;

interface StillRequest {
  key: string;
  url: string;
  /** The requested scene time; the response may be an earlier keyframe. */
  time: number;
  background: boolean;
}

interface BatchRequest {
  positions: readonly number[];
  background: boolean;
  /** Position in the background plan, for background batches. */
  plan: number | null;
  abort: AbortController;
  timer: ReturnType<typeof setTimeout>;
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
  keyframes: { index: string; still: string; batch: string } | null;
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
  let batchesAvailable = keyframes !== null;
  let batchFailures = 0;
  /** Deterministic background batches for the selected range. */
  let plan: number[][] | null = null;
  let planIndex = 0;
  let backoffUntil = 0;
  let backoffTimer: ReturnType<typeof setTimeout> | undefined;
  /** The current target still needs a foreground request once a slot frees. */
  let foregroundWaiting = false;
  /** Dense batches wanted near the finger, most important first. */
  let denseWanted: number[] = [];
  let dwellTimer: ReturnType<typeof setTimeout> | undefined;
  let lastMove: { position: number; at: number } | null = null;
  let direction = 0;
  const requests = new Map<
    string,
    {
      background: boolean;
      abort: AbortController;
      timer: ReturnType<typeof setTimeout>;
    }
  >();
  const batches = new Map<string, BatchRequest>();

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
  const cachedPosition = (position: number) =>
    !!index && cache.has(index.times[position]!);
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
  const countBatches = (background: boolean) => {
    let count = 0;
    for (const request of batches.values())
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
  const abortBatches = (
    predicate: (request: BatchRequest) => boolean = () => true,
  ) => {
    for (const [url, request] of batches)
      if (predicate(request)) {
        clearTimeout(request.timer);
        request.abort.abort();
        batches.delete(url);
      }
  };
  /** Index positions of the selected scene/marker range. */
  const rangePositions = (times: readonly number[]) =>
    [
      keyframePosition(times, start),
      keyframePosition(times, end - KEYFRAME_TOLERANCE_SECONDS * 2),
    ] as const;

  const hide = () => {
    onReady(null);
    selection++;
    surface.style.opacity = "0";
    surface.dataset.previewReady = "false";
  };
  const clear = () => {
    generation++;
    abortRequests();
    abortBatches();
    foregroundWaiting = false;
    denseWanted = [];
    clearTimeout(dwellTimer);
    controller?.release();
    hide();
    cache.clear();
    image.removeAttribute("src");
    delete image.dataset.scrubTime;
    surface.dataset.previewFrames = "0";
    samples = null;
    sampleIndex = 0;
    planIndex = 0;
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
  /** After new stills arrive: update the display and continue fetching. */
  const resume = () => {
    if (target !== null) {
      present(target);
      pumpForeground();
      pumpDense();
    } else {
      clearTimeout(next);
      next = setTimeout(tick, 0);
    }
  };
  const backoff = (seconds: number) => {
    backoffUntil = performance.now() + seconds * 1000;
    clearTimeout(backoffTimer);
    backoffTimer = setTimeout(() => {
      pumpForeground();
      pumpDense();
    }, seconds * 1000);
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
        backoff(result.after);
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
    resume();
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
  const settleBatch = (
    request: BatchRequest,
    times: readonly number[],
    result: KeyframeBatchResult,
  ) => {
    if (result.status === "aborted") return;
    // Saturation retries the same background batch; other outcomes move on.
    if (request.plan !== null && result.status !== "busy")
      planIndex = Math.max(planIndex, request.plan + 1);
    switch (result.status) {
      case "ok":
        batchFailures = 0;
        result.stills.forEach((still, slot) => {
          if (still)
            cache.add(
              still.time ?? times[slot]!,
              still.blob,
              request.background,
            );
        });
        surface.dataset.previewFrames = String(cache.size);
        // A batch that lands first answers pending single requests.
        abortRequests((key) => !!index && cache.has(Number(key)));
        break;
      case "busy":
        backoff(result.after);
        break;
      case "missing":
        batchesAvailable = false;
        break;
      case "error":
        if (++batchFailures >= STILL_FAILURE_LIMIT) batchesAvailable = false;
        break;
    }
    if (!batchesAvailable) {
      abortBatches();
      denseWanted = [];
    }
    resume();
  };
  const fetchBatch = (
    positions: readonly number[],
    background: boolean,
    planned: number | null,
  ) => {
    if (!keyframes || !index || !positions.length) return false;
    const times = positions.map((position) => index!.times[position]!);
    const url = keyframeBatchURL(keyframes.batch, times, index.version);
    if (batches.has(url)) return false;
    const abort = new AbortController();
    const entry: BatchRequest = {
      positions,
      background,
      plan: planned,
      abort,
      timer: setTimeout(() => abort.abort(), BATCH_TIMEOUT_MS),
    };
    batches.set(url, entry);
    const owner = generation;
    void fetchKeyframeBatch(
      url,
      abort.signal,
      times.length,
      background ? "low" : "auto",
    ).then((result) => {
      clearTimeout(entry.timer);
      const current = batches.get(url) === entry;
      if (current) batches.delete(url);
      if (disposed || owner !== generation) return;
      settleBatch(
        entry,
        times,
        result.status === "aborted" && current ? { status: "error" } : result,
      );
    });
    return true;
  };
  /** Latest target wins: at most two foreground requests, and a newer target
   * replaces any target still waiting for a slot. Superseded responses only
   * populate the cache. */
  function pumpForeground() {
    if (target === null || !foregroundWaiting) return;
    if (!stillsAvailable || videoBuffered(target) || cachedStill(target)) {
      foregroundWaiting = false;
      return;
    }
    if (backoffUntil > performance.now()) return;
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
  /** One dense batch near the finger at a time, for the latest wanted area. */
  function pumpDense() {
    if (target === null || !index || !batchesAvailable) return;
    if (countBatches(false) || backoffUntil > performance.now()) return;
    for (const batch of denseWanted) {
      const positions = denseKeyframeBatch(index.times.length, batch);
      if (positions.every(cachedPosition)) continue;
      if (fetchBatch(positions, false, null)) {
        denseWanted = denseWanted.filter((wanted) => wanted !== batch);
        return;
      }
    }
  }
  const wantDense = (position: number, neighbour: boolean) => {
    const batch = Math.floor(position / KEYFRAME_BATCH_SIZE);
    denseWanted = neighbour && direction ? [batch, batch + direction] : [batch];
    pumpDense();
  };
  /** Dense batches follow a foreground miss, a slowing drag or a dwell. */
  const followFinger = (time: number, miss: boolean) => {
    if (!index || !batchesAvailable) return;
    const position = keyframePosition(index.times, time);
    const now = performance.now();
    let slow = false;
    if (lastMove && position !== lastMove.position) {
      const seconds = Math.max(0.001, (now - lastMove.at) / 1000);
      slow =
        Math.abs(position - lastMove.position) / seconds <=
        SLOW_KEYFRAMES_PER_SECOND;
      direction = Math.sign(position - lastMove.position);
    }
    if (!lastMove || position !== lastMove.position)
      lastMove = { position, at: now };
    if (miss || slow) wantDense(position, slow);
    // A fast pass over cached stills supersedes any earlier neighbourhood.
    else denseWanted = [];
    clearTimeout(dwellTimer);
    dwellTimer = setTimeout(() => {
      if (target === time) wantDense(position, true);
    }, DWELL_MS);
  };
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
        const [low, high] = rangePositions(index.times);
        plan = backgroundKeyframeBatches(
          index.times.length,
          low,
          high,
          BACKGROUND_STILLS,
        );
        planIndex = 0;
      } else if (result.status === "retry" && indexAttempts < 2) {
        indexState = "retry";
        indexRetryAt = performance.now() + result.after * 1000;
      } else indexState = "unavailable";
      surface.dataset.keyframeIndex =
        indexState === "ready" ? "ready" : "unavailable";
      if (target !== null) followFinger(target, !cachedStill(target));
      resume();
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

  /** Background batches for the range, one at a time, in plan order. */
  const prepareBatches = () => {
    if (!plan || countBatches(true)) return;
    while (planIndex < plan.length && plan[planIndex]!.every(cachedPosition))
      planIndex++;
    if (planIndex < plan.length) fetchBatch(plan[planIndex]!, true, planIndex);
  };
  /** Without batches: single stills, at most two at a time. */
  const prepareStills = (position: number) => {
    if (!stillsAvailable) return;
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
      abortBatches((request) => request.background);
      controller?.release();
      return;
    }
    if (!eligibleSince) eligibleSince = now;
    if (now - eligibleSince < PREPARATION_DELAY_MS) return;
    loadIndex();
    if (!keyframes || indexState === "loading" || backoffUntil > now) return;
    if (index && batchesAvailable) prepareBatches();
    else prepareStills(position);
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
        denseWanted = [];
        lastMove = null;
        direction = 0;
        clearTimeout(dwellTimer);
        abortRequests((_key, background) => !background);
        abortBatches((request) => !request.background);
        hide();
        controller?.preview(null);
        return;
      }
      if (previous === null) {
        // Foreground scrubbing takes priority over background coverage.
        const wanted = stillRequest(time, false)?.key;
        abortRequests((key, background) => background && key !== wanted);
        const position = index ? keyframePosition(index.times, time) : -1;
        abortBatches(
          (request) =>
            request.background && !request.positions.includes(position),
        );
      }
      loadIndex();
      present(time);
      foregroundWaiting = true;
      pumpForeground();
      if (time !== previous)
        followFinger(time, !targetCached && !videoBuffered(time));
      ensureController().preview(time);
    },
    dispose() {
      disposed = true;
      clearInterval(interval);
      clearTimeout(next);
      clearTimeout(backoffTimer);
      clearTimeout(dwellTimer);
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
