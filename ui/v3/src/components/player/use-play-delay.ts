/**
 * Play-delay gate. Used by the lightbox to keep the video silent and
 * still while the swipe animation is in flight.
 *
 * We deliberately do NOT toggle the `autoPlay` attribute off — iOS's
 * muted-fallback autoplay path is gated on the *attribute* being set
 * when canplay fires; flipping it off and calling play() programmatically
 * later gets rejected by the media-engagement heuristic, even with the
 * muted retry. Instead, listen for the `playing` event during the window
 * and pause immediately, which iOS treats as a normal pause of an
 * already-authorized stream. After the window expires we call play() once
 * to resume — this is allowed because the original play was authorized
 * by the autoplay attribute.
 *
 * The `<video>` element can be replaced when the player swaps sources
 * (`key={finalSrc}` on `Player.Provider` triggers a remount). The hook
 * watches `rootRef`'s subtree with a `MutationObserver` and re-attaches
 * the `playing` listener so the gate keeps suppressing across remounts.
 */
import { useEffect, type MutableRefObject, type RefObject } from "react";

export function usePlayDelay(
  rootRef: RefObject<HTMLElement | null>,
  delayMs: number,
  autoplayIntentRef: MutableRefObject<boolean>,
): void {
  useEffect(() => {
    if (delayMs <= 0) return;
    const root = rootRef.current;
    if (!root) return;

    const deadline = Date.now() + delayMs;
    let suppressed = true;

    function findVideo(): HTMLVideoElement | null {
      return root?.querySelector("video") ?? null;
    }

    function onPlaying(e: Event) {
      if (!suppressed) return;
      const video = e.currentTarget;
      if (video instanceof HTMLVideoElement) video.pause();
    }

    function attach(video: HTMLVideoElement) {
      video.addEventListener("playing", onPlaying);
    }
    function detach(video: HTMLVideoElement) {
      video.removeEventListener("playing", onPlaying);
    }

    let attached: HTMLVideoElement | null = findVideo();
    if (attached) attach(attached);

    const observer = new MutationObserver(() => {
      const next = findVideo();
      if (next === attached) return;
      if (attached) detach(attached);
      attached = next;
      if (attached) attach(attached);
    });
    observer.observe(root, { childList: true, subtree: true });

    const t = window.setTimeout(
      () => {
        suppressed = false;
        const video = findVideo();
        if (!video?.paused) return;
        if (!autoplayIntentRef.current) return;
        video.play().catch(() => {
          video.muted = true;
          video.play().catch(() => {});
        });
      },
      Math.max(0, deadline - Date.now()),
    );

    return () => {
      window.clearTimeout(t);
      observer.disconnect();
      if (attached) detach(attached);
    };
  }, [rootRef, delayMs, autoplayIntentRef]);
}
