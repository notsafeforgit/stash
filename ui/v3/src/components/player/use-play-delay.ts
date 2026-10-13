/**
 * Play-delay gate. Used by the lightbox to keep the video silent and
 * still while the swipe animation is in flight.
 *
 * Leave the `autoPlay` attribute in place and pause playback during the
 * animation. Try to resume once at the deadline using the current audio
 * state. Safari may require a new Play gesture at that point; an autoplay
 * rejection must not silently mute the persistent video element.
 *
 * The deadline resume is gated on `autoplayIntentRef`, so with autostart
 * off the gate doubles as the lightbox's autostart enforcement: any
 * programmatic play that slips out during the opening window is paused
 * and stays paused.
 *
 * That enforcement must not swallow the user's own play: an explicit
 * playback gesture (pre-start play click, play/pause toggle) sets
 * `userPlaybackIntentRef`, which releases the gate — the `playing` it
 * produces goes through, and the deadline takes no action afterwards
 * (the user owns the playback state from then on). The signal is
 * explicit rather than inferred from `playing` events because the gate
 * cannot distinguish a user-initiated play from a programmatic one at
 * the event level.
 *
 * Source discovery can mount the media after this effect starts. Watch the
 * subtree so the gate also covers that late attachment.
 */
import { useEffect, type MutableRefObject, type RefObject } from "react";

export function usePlayDelay(
  rootRef: RefObject<HTMLElement | null>,
  delayMs: number,
  autoplayIntentRef: MutableRefObject<boolean>,
  userPlaybackIntentRef: MutableRefObject<boolean>,
  playbackKey?: string,
  suspended = false,
): void {
  // biome-ignore lint/correctness/useExhaustiveDependencies: Each selected scene/marker owns a new animation deadline.
  useEffect(() => {
    if (suspended || delayMs <= 0) return;
    const root = rootRef.current;
    if (!root) return;

    const deadline = Date.now() + delayMs;
    let suppressed = true;

    function findVideo(): HTMLVideoElement | null {
      return root?.querySelector("video") ?? null;
    }

    function onPlaying(e: Event) {
      if (!suppressed) return;
      // The user pressed play — their gesture wins over the animation
      // gate. Release rather than pause: with autostart off the deadline
      // would never restore a swallowed user play.
      if (userPlaybackIntentRef.current) {
        suppressed = false;
        return;
      }
      const video = e.currentTarget;
      if (video instanceof HTMLVideoElement) video.pause();
    }

    function attach(video: HTMLVideoElement) {
      video.addEventListener("playing", onPlaying);
      if (!video.paused && !userPlaybackIntentRef.current) video.pause();
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
        // The user has taken control of playback during the window —
        // whatever state they chose (playing or paused) stands.
        if (userPlaybackIntentRef.current) return;
        const video = findVideo();
        if (!video?.paused) return;
        if (!autoplayIntentRef.current) return;
        // Keep the user's sound setting. If autoplay is denied, the existing
        // Play control supplies the gesture Safari needs for audible playback.
        video.play().catch(() => {});
      },
      Math.max(0, deadline - Date.now()),
    );

    return () => {
      window.clearTimeout(t);
      observer.disconnect();
      if (attached) detach(attached);
    };
  }, [
    rootRef,
    delayMs,
    autoplayIntentRef,
    userPlaybackIntentRef,
    playbackKey,
    suspended,
  ]);
}
