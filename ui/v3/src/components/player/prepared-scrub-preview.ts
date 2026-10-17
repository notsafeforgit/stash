import { readTimeRanges } from "./buffered-ranges";
import { ScrubFrameCache, scrubPreparationTimes } from "./scrub-frame-cache";
import { createScrubStream } from "./scrub-stream";

/** One decoder serves background sampling and foreground fine seeking. Cached
 * JPEGs cover broad movement while it catches up. Never changes the main video. */
export function createPreparedScrubPreview({
  surface,
  sceneId,
  source,
  start,
  end,
  offset,
  onFailure,
}: {
  surface: HTMLDivElement;
  sceneId: string;
  source: string;
  start: number;
  end: number;
  offset: number;
  onFailure: () => void;
}) {
  const cache = new ScrubFrameCache();
  const image = document.createElement("img");
  image.alt = "";
  image.dataset.scrubStill = "";
  image.className = "absolute inset-0 size-full object-contain";
  image.style.opacity = "0";
  surface.append(image);
  let video: HTMLVideoElement | null = null;
  let controller: ReturnType<typeof createScrubStream> | null = null;
  let canvas: HTMLCanvasElement | null = null;
  let target: number | null = null;
  let warmTarget: number | null = null;
  let warmStarted = 0;
  let eligibleSince = 0;
  let lastActivity = performance.now();
  let samplesInSession = 0;
  let sampleIndex = 0;
  let samples: number[] | null = null;
  let selection = 0;
  let generation = 0;
  let capturing = false;
  let lastCapture = -Infinity;
  let disposed = false;
  let failed = false;
  let next: ReturnType<typeof setTimeout> | undefined;

  const hide = () => {
    selection++;
    surface.style.opacity = "0";
    surface.dataset.previewReady = "false";
  };
  const release = () => {
    warmTarget = null;
    samplesInSession = 0;
    controller?.release();
  };
  const clear = () => {
    generation++;
    release();
    hide();
    cache.clear();
    image.removeAttribute("src");
    surface.dataset.previewFrames = "0";
    samples = null;
    sampleIndex = 0;
    eligibleSince = 0;
  };
  const showCached = (time: number) => {
    const frame = cache.nearest(time, Math.max(2, (end - start) / 8));
    if (!frame) return;
    const requested = ++selection;
    const show = () => {
      if (disposed || target === null || requested !== selection) return;
      image.dataset.scrubTime = String(frame.time);
      image.style.opacity = "1";
      if (video) video.style.opacity = "0";
      surface.style.opacity = "1";
      surface.dataset.previewKind = "still";
      surface.dataset.previewReady = "true";
    };
    image.onload = show;
    if (image.src !== frame.url) image.src = frame.url;
    if (image.complete && image.naturalWidth > 0) show();
  };
  const capture = (time: number, coarse: boolean) => {
    if (
      !video?.videoWidth ||
      capturing ||
      cache.has(time) ||
      (!coarse && performance.now() - lastCapture < 250)
    )
      return;
    canvas ??= document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    const context = canvas.getContext("2d");
    if (!context) return;
    const capturedGeneration = generation;
    try {
      context.drawImage(video, 0, 0);
      capturing = true;
      lastCapture = performance.now();
      canvas.toBlob(
        (blob) => {
          capturing = false;
          if (!blob || disposed || generation !== capturedGeneration) return;
          cache.add(time, blob, coarse);
          surface.dataset.previewFrames = String(cache.size);
        },
        "image/jpeg",
        0.7,
      );
    } catch {
      capturing = false;
      // A tainted cross-origin canvas must not cause repeated background work.
      // Fine scrubbing still uses the video surface without canvas readback.
      samples = [];
    }
  };
  const ensureController = () => {
    if (controller) return controller;
    video = document.createElement("video");
    video.dataset.scrubPreview = "";
    video.muted = true;
    video.playsInline = true;
    video.disableRemotePlayback = true;
    video.preload = "auto";
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
        if (time !== undefined) {
          const coarse =
            !visible &&
            warmTarget !== null &&
            Math.abs(warmTarget - time) < 0.05;
          capture(time, coarse);
          if (coarse) {
            warmTarget = null;
            sampleIndex++;
            samplesInSession++;
            controller?.preview(null);
            // Rotate background batches to bound encoded server cache growth.
            if (samplesInSession >= 8) release();
            clearTimeout(next);
            next = setTimeout(tick, 250);
          }
        }
        if (visible && target !== null) {
          selection++;
          image.style.opacity = "0";
          video!.style.opacity = "1";
          surface.style.opacity = "1";
          surface.dataset.previewKind = "video";
          surface.dataset.previewReady = "true";
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
    if (now - lastActivity > 5 * 60_000) {
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
      release();
      return;
    }
    if (!eligibleSince) eligibleSince = now;
    if (now - eligibleSince < 1500) return;
    if (warmTarget !== null) {
      if (now - warmStarted > 8000) {
        release();
        sampleIndex++;
      }
      return;
    }
    samples ??= [
      Math.max(start, Math.min(end - 0.1, position + offset)),
      ...scrubPreparationTimes(start, end),
    ];
    while (sampleIndex < samples.length && cache.has(samples[sampleIndex]!))
      sampleIndex++;
    if (sampleIndex >= samples.length) return;
    warmTarget = samples[sampleIndex]!;
    warmStarted = now;
    ensureController().prepare(warmTarget);
  }
  const hidden = () => {
    if (document.hidden) clear();
  };
  document.addEventListener("visibilitychange", hidden);
  const interval = setInterval(tick, 500);

  return {
    preview(time: number | null) {
      target = time;
      warmTarget = null;
      eligibleSince = 0;
      lastActivity = performance.now();
      if (time === null) {
        hide();
        controller?.preview(null);
      } else {
        // Nearby decoded video stays on screen while fine seeking catches up;
        // avoid flashing a coarser JPEG between consecutive precise frames.
        if (
          !video ||
          surface.dataset.previewKind !== "video" ||
          !readTimeRanges(video.buffered).some(
            ([from, to]) => from <= time && to > time,
          )
        )
          showCached(time);
        ensureController().preview(time);
      }
    },
    dispose() {
      disposed = true;
      clearInterval(interval);
      clearTimeout(next);
      document.removeEventListener("visibilitychange", hidden);
      clear();
      controller?.dispose();
      video?.remove();
      image.remove();
    },
  };
}
