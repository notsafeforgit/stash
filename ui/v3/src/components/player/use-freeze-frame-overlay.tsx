/**
 * Persistent canvas overlay that masks media-engine teardown while retaining
 * the same video element. Late JPEG exports are scoped to their capture.
 *
 * Two load-bearing details:
 *   1. The canvas is a sibling of `Player.Player`, so media-engine and
 *      element swaps don't unmount it.
 *   2. Visibility is driven by canvas content (drawn vs. cleared
 *      transparent), not opacity. iOS Safari skips compositor-layer
 *      creation for opacity-0 elements even with `will-change` /
 *      `translateZ(0)` hints, so the first 0 → 1 flip would expose a
 *      frame-long black gap.
 */
import { useCallback, useEffect, useMemo, useRef, type RefObject } from "react";

interface UseFreezeFrameOverlayArgs {
  /**
   * Element under which the active `<video>` lives. `captureFrame`
   * does a `querySelector("video")` from this root.
   */
  rootRef: RefObject<HTMLElement | null>;
  /** Native dimensions from scene metadata. Used to pin the canvas's
   *  backing buffer to the correct aspect before a capture, so subsequent
   *  captures don't reallocate the GPU texture. */
  fileWidth: number | undefined;
  fileHeight: number | undefined;
}

interface UseFreezeFrameOverlayResult {
  /** Ready-to-render persistent canvas. Place inside the same
   *  positioned/`isolate` container that holds `Player.Player` so
   *  z-index ordering against the controls bar resolves correctly. */
  canvasElement: React.ReactNode;
  /** Snapshot the current video before the adapter transitions its engine. */
  captureFrame: () => void;
  /** Repaint the canvas as fully transparent. Call when the new
   *  source has finished seeking / first-painting and the overlay
   *  should disappear. */
  clear: () => void;
}

export function useFreezeFrameOverlay({
  rootRef,
  fileWidth,
  fileHeight,
}: UseFreezeFrameOverlayArgs): UseFreezeFrameOverlayResult {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const preparedRef = useRef<HTMLCanvasElement | null>(null);

  const prepareCanvas = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return null;
    const fw = fileWidth ?? 0;
    const fh = fileHeight ?? 0;
    let w = fw > 0 && fh > 0 ? fw : 1920;
    let h = fw > 0 && fh > 0 ? fh : 1080;
    if (w > 1920) {
      h = Math.round((1920 / w) * h);
      w = 1920;
    }
    const ctx = canvas.getContext("2d");
    if (!ctx) return null;
    if (
      preparedRef.current === canvas &&
      canvas.width === w &&
      canvas.height === h
    )
      return ctx;
    canvas.width = w;
    canvas.height = h;
    ctx.fillStyle = "rgba(0,0,0,0.5)";
    ctx.fillRect(0, 0, 1, 1);
    try {
      // Commit the texture before a source change needs its first capture.
      ctx.getImageData(0, 0, 1, 1);
    } catch {
      // A capture can still work if a browser disallows readback.
    }
    ctx.clearRect(0, 0, w, h);
    preparedRef.current = canvas;
    return ctx;
  }, [fileWidth, fileHeight]);

  // Object URL of the captured frame as a JPEG blob. Set on
  // `<video>.poster` so iOS Safari's native fullscreen player has
  // something to display during the brief window where the
  // MediaSource is detached for an engine swap — the canvas overlay
  // is invisible in fullscreen (only the `<video>` element is
  // on-screen), so without poster, fullscreen seeks past buffered
  // flash to black. Revoked + replaced on each `captureFrame`.
  const lastPosterUrlRef = useRef<string | null>(null);
  const captureIdRef = useRef(0);

  const captureFrame = useCallback(() => {
    const captureId = ++captureIdRef.current;
    const root = rootRef.current;
    const video = root?.querySelector("video");
    if (!(video instanceof HTMLVideoElement)) return;
    if (video.videoWidth === 0 || video.videoHeight === 0) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = prepareCanvas();
    if (!ctx) return;
    // Usually prepared during idle time after playback has data. An immediate
    // source change also prepares it synchronously, preserving the freeze frame.
    // Subsequent captures reuse the buffer at the file's aspect ratio.
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    try {
      ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
    } catch {
      // drawImage from a cross-origin video taints the canvas but
      // does not throw; if it ever does (e.g. detached element),
      // fall back to plain dim+spinner.
      return;
    }
    // Async export to JPEG blob → object URL → `<video>.poster`. We
    // do this async (`toBlob`) rather than `toDataURL` because the
    // sync path stalls the main thread for 50–150 ms on a 1920×1080
    // canvas, which would visibly delay the seek the user just
    // initiated. `toBlob` runs the encode off-thread and resolves
    // within a couple of frames; if the engine teardown beats it,
    // the poster lands after the video has already cleared and shows
    // on the next paint — still better than persistent black.
    try {
      canvas.toBlob(
        (blob) => {
          if (!blob || captureId !== captureIdRef.current) return;
          const liveVideo = rootRef.current?.querySelector("video");
          if (liveVideo !== video) return;
          const url = URL.createObjectURL(blob);
          if (lastPosterUrlRef.current) {
            URL.revokeObjectURL(lastPosterUrlRef.current);
          }
          lastPosterUrlRef.current = url;
          if (liveVideo instanceof HTMLVideoElement) {
            liveVideo.poster = url;
          }
        },
        "image/jpeg",
        0.7,
      );
    } catch {
      /* tainted canvas → toBlob throws SecurityError. Defer to the
         dim+spinner overlay for the visible mask. */
    }
  }, [rootRef, prepareCanvas]);

  const clear = useCallback(() => {
    captureIdRef.current += 1;
    const canvas = canvasRef.current;
    if (canvas && preparedRef.current === canvas) {
      const ctx = canvas.getContext("2d");
      if (ctx) ctx.clearRect(0, 0, canvas.width, canvas.height);
    }
    // Also clear the `<video>.poster` so the iOS fullscreen player
    // doesn't keep showing the stale frame after the new source is
    // decoding.
    const video = rootRef.current?.querySelector("video");
    if (video instanceof HTMLVideoElement) {
      video.removeAttribute("poster");
    }
    if (lastPosterUrlRef.current) {
      URL.revokeObjectURL(lastPosterUrlRef.current);
      lastPosterUrlRef.current = null;
    }
  }, [rootRef]);

  // GPU readback is synchronous and can hold up both route paint and scrolling.
  // Wait for playable data and a paint, then warm during idle time. The capture
  // path above remains ready if the user changes source before this runs.
  useEffect(() => {
    const video = rootRef.current?.querySelector("video");
    if (!video) return;
    let frame: number | undefined;
    let idle: number | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const warm = () => {
      if (!document.hidden) prepareCanvas();
    };
    const schedule = () => {
      frame = requestAnimationFrame(() => {
        frame = requestAnimationFrame(() => {
          if (typeof window.requestIdleCallback === "function")
            idle = window.requestIdleCallback(warm);
          else timer = setTimeout(warm, 0);
        });
      });
    };
    if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) schedule();
    else video.addEventListener("loadeddata", schedule, { once: true });
    return () => {
      video.removeEventListener("loadeddata", schedule);
      if (frame !== undefined) cancelAnimationFrame(frame);
      if (idle !== undefined) window.cancelIdleCallback(idle);
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [rootRef, prepareCanvas]);

  // Invalidate pending exports as well as revoking the current poster.
  useEffect(() => clear, [clear]);

  const canvasElement = useMemo(
    () => (
      <canvas
        ref={canvasRef}
        width={1}
        height={1}
        aria-hidden
        className="absolute inset-0 w-full h-full object-contain pointer-events-none z-[5]"
        // `transform: translateZ(0)` forces a continuous GPU compositor
        // layer so first-paint isn't blocked on layer promotion. The
        // canvas's "visibility" is driven by its backing buffer being
        // either drawn (frozen frame) or cleared (fully-transparent
        // pixels — video shows through); we deliberately do NOT toggle
        // opacity, because iOS Safari tends to skip layer creation
        // while opacity is 0 and pay for it on the first 0 → 1
        // transition with a black frame.
        style={{ transform: "translateZ(0)" }}
      />
    ),
    [],
  );

  return { canvasElement, captureFrame, clear };
}
