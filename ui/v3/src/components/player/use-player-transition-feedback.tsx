import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import type { VideoPlayerStore } from "@videojs/react";
import { useFreezeFrameOverlay } from "./use-freeze-frame-overlay";

/** Visual feedback and DOM readiness for both in-place seeks and source reloads. */
export function usePlayerTransitionFeedback({
  rootRef,
  storeRef,
  fileWidth,
  fileHeight,
}: {
  rootRef: RefObject<HTMLDivElement | null>;
  storeRef: RefObject<VideoPlayerStore | null>;
  fileWidth: number | undefined;
  fileHeight: number | undefined;
}) {
  const [reloading, setReloadingRaw] = useState(false);
  // Hold the dim+spinner visible for a minimum window after a source
  // change begins. With an in-place src swap the new playlist's first
  // `canplay` can fire within a frame or two of the state update,
  // leaving the spinner's opacity-1 paint indistinguishable from no
  // spinner at all. Anchoring the clear to a min-duration timer ensures
  // the user actually sees the loading affordance before the new video
  // takes over.
  const RELOADING_MIN_DURATION_MS = 250;
  const reloadingStartTsRef = useRef<number | null>(null);
  const reloadingClearTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const setReloading = useCallback((value: boolean) => {
    if (value) {
      if (reloadingClearTimerRef.current) {
        clearTimeout(reloadingClearTimerRef.current);
        reloadingClearTimerRef.current = null;
      }
      reloadingStartTsRef.current = performance.now();
      setReloadingRaw(true);
      return;
    }
    const elapsed = performance.now() - (reloadingStartTsRef.current ?? 0);
    const remaining = RELOADING_MIN_DURATION_MS - elapsed;
    if (remaining <= 0) {
      reloadingStartTsRef.current = null;
      setReloadingRaw(false);
      return;
    }
    if (reloadingClearTimerRef.current) {
      clearTimeout(reloadingClearTimerRef.current);
    }
    reloadingClearTimerRef.current = setTimeout(() => {
      reloadingClearTimerRef.current = null;
      reloadingStartTsRef.current = null;
      setReloadingRaw(false);
    }, remaining);
  }, []);
  useEffect(
    () => () => {
      if (reloadingClearTimerRef.current) {
        clearTimeout(reloadingClearTimerRef.current);
      }
    },
    [],
  );

  // Freeze-frame canvas: snapshots the current `<video>` frame just
  // before the state setters fire, so the engine-detach / blob-URL-
  // swap path (which natively goes black) sits under the captured
  // pixels instead of nothing. Cleared in `handleCanPlay` once the
  // new source is decoding. Direct-stream in-buffer seeks don't go
  // through this — the browser holds the last frame natively. See
  // `use-freeze-frame-overlay.tsx` for the canvas-content vs.
  // opacity-toggle rationale.
  const {
    canvasElement: freezeFrameCanvas,
    captureFrame,
    clear: clearCapturedFrame,
  } = useFreezeFrameOverlay({
    rootRef,
    fileWidth,
    fileHeight,
    setReloading,
  });

  // Replaces `beginSourceRemount` from the original variant. With an
  // in-place src swap the helper is: snapshot the current frame for
  // the freeze-frame canvas, flip the spinner, commit the state.
  // No <video> abort (HlsJsMedia hands the new URL to the active engine,
  // or replaces that engine when its start-position config changes) and
  // no rAF defer (the canvas masks first paint via z-ordering, not
  // opacity timing).
  const beginSourceRemount = useCallback(
    (applyChanges: () => void) => {
      captureFrame();
      setReloading(true);
      applyChanges();
    },
    [captureFrame, setReloading],
  );

  // While a seek is in flight, the player UI displays this value
  // instead of the live `currentTime` — otherwise the time-display and
  // progress bar bounce to 0:00 / 0% the instant the source URL swap
  // detaches the old MediaSource (which resets `video.currentTime` to
  // 0 until the new source's `loadedmetadata` pre-positions it back to
  // the target). The display target stays pinned for the entire
  // `reloading` window (covers both in-buffer direct seeks and
  // out-of-buffer remount seeks), then clears with a short fade so the
  // live `currentTime` can take back over once playback resumes.
  const [seekDisplayTarget, setSeekDisplayTarget] = useState<number | null>(
    null,
  );
  const seekDisplayClearTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const armSeekDisplay = useCallback((target: number) => {
    if (seekDisplayClearTimerRef.current) {
      clearTimeout(seekDisplayClearTimerRef.current);
      seekDisplayClearTimerRef.current = null;
    }
    setSeekDisplayTarget(target);
  }, []);
  // Clear the display target shortly after `reloading` flips false.
  // The brief delay lets the player render one frame at the resume
  // position before swapping from `seekDisplayTarget` back to the live
  // `currentTime`, which avoids a visible bounce when the
  // post-`canPlay` `currentTime` update lags the spinner-clear by a
  // few ms.
  useEffect(() => {
    if (reloading) return;
    if (seekDisplayTarget == null) return;
    if (seekDisplayClearTimerRef.current) {
      clearTimeout(seekDisplayClearTimerRef.current);
    }
    seekDisplayClearTimerRef.current = setTimeout(() => {
      seekDisplayClearTimerRef.current = null;
      setSeekDisplayTarget(null);
    }, 100);
    return () => {
      if (seekDisplayClearTimerRef.current) {
        clearTimeout(seekDisplayClearTimerRef.current);
        seekDisplayClearTimerRef.current = null;
      }
    };
  }, [reloading, seekDisplayTarget]);
  useEffect(
    () => () => {
      if (seekDisplayClearTimerRef.current) {
        clearTimeout(seekDisplayClearTimerRef.current);
      }
    },
    [],
  );

  // Hold the dim+spinner up through an in-place seek (no player-root
  // remount) until the new position is actually decoding frames.
  //
  // The caller is expected to have already paused the video and called
  // `s.seek(...)` so the spinner overlays a frozen frame instead of an
  // old-position video that keeps playing under the dim. After `seeked`
  // fires (frame at target is decoded), this helper either:
  //   - calls `s.play()` and waits for the `playing` event to clear
  //     the spinner (`shouldResume` — slider seek that was playing,
  //     or marker-tap which always wants playback), or
  //   - clears the spinner immediately on `seeked` (paused-state seek;
  //     no `playing` event will ever fire).
  // A 5 s safety timeout catches the rare case where neither fires
  // (e.g. seek aborted by a follow-up source change). Combined with
  // the 250 ms minimum duration baked into `setReloading`, this gives
  // short in-buffer seeks a brief flicker and long out-of-buffer seeks
  // the full dim+spinner through the buffer wait — all paths converge
  // to "old frame holds, spinner shows, new position plays, spinner
  // clears".
  const awaitSeekReady = useCallback(
    (video: HTMLVideoElement, shouldResume: boolean) => {
      let cleared = false;
      const clear = () => {
        if (cleared) return;
        cleared = true;
        video.removeEventListener("seeked", onSeeked);
        video.removeEventListener("playing", onPlaying);
        clearTimeout(timeoutId);
        // Idempotent — direct-seek paths don't snapshot, so this is a
        // no-op there. Guards the case where a captured frame survived
        // a follow-up direct seek without going through handleCanPlay.
        clearCapturedFrame();
        setReloading(false);
      };
      const onSeeked = () => {
        if (shouldResume) {
          // Restart playback; spinner clears once the new position is
          // actually decoding frames (`playing` event below).
          storeRef.current?.play();
        } else {
          // Stay paused — clear immediately, no `playing` event will
          // ever fire to release the spinner otherwise.
          clear();
        }
      };
      const onPlaying = () => clear();
      const timeoutId = setTimeout(clear, 5000);
      video.addEventListener("seeked", onSeeked, { once: true });
      video.addEventListener("playing", onPlaying, { once: true });
    },
    [setReloading, storeRef, clearCapturedFrame],
  );

  return {
    reloading,
    setReloading,
    freezeFrameCanvas,
    captureFrame,
    clearCapturedFrame,
    beginSourceRemount,
    seekDisplayTarget,
    armSeekDisplay,
    awaitSeekReady,
  };
}
