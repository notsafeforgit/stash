/**
 * Persistent canvas overlay that masks the gap between an old `<video>`
 * being unmounted and a new one rendering its first decoded frame.
 *
 * Two load-bearing details:
 *   1. The canvas is a sibling of `Player.Provider`, so the keyed
 *      remount on source change doesn't unmount it.
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
   *  backing buffer to the correct aspect at mount, so subsequent
   *  captures don't reallocate the GPU texture. */
  fileWidth: number | undefined;
  fileHeight: number | undefined;
  /** Setter for the player's `reloading` state — flipped to true in
   *  phase 1 of `beginRemount`. */
  setReloading: (reloading: boolean) => void;
}

interface UseFreezeFrameOverlayResult {
  /** Ready-to-render persistent canvas. Place inside the same
   *  positioned/`isolate` container that holds `Player.Provider` so
   *  z-index ordering against the controls bar resolves correctly. */
  canvasElement: React.ReactNode;
  /**
   * Phase 1: capture current frame + flip `reloading=true`. Phase 2,
   * two animation frames later: invoke `applyChanges`. Caller uses
   * `applyChanges` to commit whatever state actually triggers the
   * Provider remount (e.g. `setManualSource`, `setOffsetStart`).
   */
  beginRemount: (applyChanges: () => void) => void;
  /** Snapshot the current `<video>` frame onto the canvas. The
   *  stable-`<video>` variant uses this directly instead of
   *  `beginRemount` — it doesn't need the force-abort dance because
   *  `HlsMedia.src = newSrc` already cancels the outgoing engine. */
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
  setReloading,
}: UseFreezeFrameOverlayArgs): UseFreezeFrameOverlayResult {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  // Object URL of the captured frame as a JPEG blob. Set on
  // `<video>.poster` so iOS Safari's native fullscreen player has
  // something to display during the brief window where the
  // MediaSource is detached for an engine swap — the canvas overlay
  // is invisible in fullscreen (only the `<video>` element is
  // on-screen), so without poster, fullscreen seeks past buffered
  // flash to black. Revoked + replaced on each `captureFrame`.
  const lastPosterUrlRef = useRef<string | null>(null);

  const captureFrame = useCallback(() => {
    const root = rootRef.current;
    const video = root?.querySelector("video");
    if (!(video instanceof HTMLVideoElement)) return;
    if (video.videoWidth === 0 || video.videoHeight === 0) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    // Canvas dimensions are pinned to the file's aspect at mount (see
    // pre-warm effect below), so we deliberately do NOT touch
    // canvas.width / canvas.height here — that would trigger a buffer
    // reallocation and a fresh GPU texture upload, which is what we
    // were paying for on the first source change. Instead, we draw
    // the (variable-resolution) video stretched to fill the canvas.
    // Aspect is preserved because the canvas was sized to the file's
    // aspect ratio, and every transcode/stream of this scene shares
    // that aspect.
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
          if (!blob) return;
          const url = URL.createObjectURL(blob);
          // Bail if the video element is gone or this capture has
          // been superseded by a later one (lastPosterUrlRef was
          // replaced before we resolved).
          if (lastPosterUrlRef.current) {
            URL.revokeObjectURL(lastPosterUrlRef.current);
          }
          lastPosterUrlRef.current = url;
          const liveVideo = rootRef.current?.querySelector("video");
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
  }, [rootRef]);

  const clear = useCallback(() => {
    const canvas = canvasRef.current;
    if (canvas) {
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

  // Pre-warm: pin the canvas dimensions to the file's aspect (capped at
  // 1920px wide so we don't burn 33MB of GPU memory on a 4K source) and
  // force the GPU texture to actually allocate now, not on the first
  // real captureFrame. clearRect on a fresh-transparent buffer is a
  // recognized no-op and gets elided, so we have to actually write a
  // pixel and read it back via getImageData (which forces a synchronous
  // GPU round-trip) to commit the texture.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const fw = fileWidth ?? 0;
    const fh = fileHeight ?? 0;
    let w = fw > 0 && fh > 0 ? fw : 1920;
    let h = fw > 0 && fh > 0 ? fh : 1080;
    if (w > 1920) {
      h = Math.round((1920 / w) * h);
      w = 1920;
    }
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.fillStyle = "rgba(0,0,0,0.5)";
    ctx.fillRect(0, 0, 1, 1);
    try {
      ctx.getImageData(0, 0, 1, 1);
    } catch {
      // Pre-warm runs at mount before any cross-origin drawImage, so
      // the canvas is not tainted; this catch is defensive.
    }
    ctx.clearRect(0, 0, canvas.width, canvas.height);
  }, [fileWidth, fileHeight]);

  // Revoke any in-flight poster object URL on unmount.
  useEffect(
    () => () => {
      if (lastPosterUrlRef.current) {
        URL.revokeObjectURL(lastPosterUrlRef.current);
        lastPosterUrlRef.current = null;
      }
    },
    [],
  );

  const beginRemount = useCallback(
    (applyChanges: () => void) => {
      // Everything synchronous so the new <video> mounts inside the
      // user-gesture window. iOS Safari otherwise refuses to load data
      // on the fresh element and `canplay` never fires. Z-ordering does
      // the masking the rAF defer was attempting: the canvas is z-[5],
      // the <video> below it inside Player.Container is z:auto, so the
      // captured frame occludes the new element's first paint regardless
      // of compositor timing.
      captureFrame();

      // Force-abort the outgoing <video>'s network activity *before* we
      // trigger the keyed remount. Chrome (and Firefox to a lesser
      // degree) keeps the underlying media engine — and the in-flight
      // HTTP response — alive across React's unmount until garbage
      // collection runs, so the backend's live transcode (mp4 / webm /
      // mkv served via `streamTranscode`) and any pending HLS / DASH
      // segment fetches stay attached to a connection the browser has
      // visually thrown away. The result: ffmpeg keeps running for many
      // seconds after a source switch.
      //
      // `pause()` + `removeAttribute("src")` + `load()` is the
      // documented sequence for cancelling an active <video> resource:
      // it tears down the media engine synchronously, which closes the
      // socket; the Go handler sees `r.Context().Done()` fire, the
      // attached ffmpeg cmd's context cancels, and the process exits.
      // Order matters — captureFrame runs first so the freeze-frame
      // overlay still has pixels to draw, *then* the abort blanks the
      // element.
      const video = rootRef.current?.querySelector("video");
      if (video instanceof HTMLVideoElement) {
        try {
          video.pause();
          video.removeAttribute("src");
          video.load();
        } catch {
          /* defensive — element may already be torn down */
        }
      }

      setReloading(true);
      applyChanges();
    },
    [captureFrame, setReloading, rootRef],
  );

  const canvasElement = useMemo(
    () => (
      <canvas
        ref={canvasRef}
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

  return { canvasElement, beginRemount, captureFrame, clear };
}
