import type { PreviewImageData } from "@/components/shared/preview-image";
/**
 * `<ScenePlayer>` configured for the lightbox carousel use case —
 * autoplay, full-fill, no seek arrows (the lightbox owns horizontal
 * gesture surface for slide-swipes), pinch zoom enabled, fullscreen
 * delegated to the lightbox-level Fullscreen plugin so the slide UI
 * (title overlay, swipe nav, toolbar) survives fullscreen mode.
 *
 * Bakes in the props that are constant across every lightbox-mode
 * scene player (online + offline). Per-slide variations
 * (`scene`, `topOverlay`, `onNext`, `clipRange`, `posterSrc`,
 * `initialTimestamp`, `sendGetCurrentTime`) pass through.
 *
 * The center carousel slot retains this wrapper across online/offline scenes
 * and markers. Selection changes reset playback state through `playbackKey`.
 */

import type React from "react";
import { ScenePlayer } from "src/components/player/scene-player";
import type { ScenePlayerScene } from "src/components/player/scene-player";
import { useConfigurationContext } from "src/hooks/config";

interface LightboxScenePlayerProps {
  scene: ScenePlayerScene;
  playbackKey: string;
  suspended: boolean;
  /** Lightbox preference, retained across closing and reopening. */
  loopEnabled: boolean;
  onLoopToggle: () => void;
  /** Toggles the YARL Fullscreen plugin's fullscreen instead of the
   *  in-player fullscreen, so chrome (title / swipe nav / toolbar)
   *  stays visible in fullscreen. Returns `false` to let the player
   *  fall back to its native fullscreen path on (a) older iOS where
   *  YARL's plugin is `disabled` because `Element.requestFullscreen`
   *  isn't supported, and (b) coarse-pointer / touch devices where
   *  Safari's `requestFullscreen` on a `<div>` silently rejects —
   *  `webkitEnterFullscreen` on the video element is the only
   *  reliable path on phones. */
  onToggleFullscreen: () => boolean | undefined;
  /** Mobile lightbox dismissal, rendered with the playback controls. */
  onClose?: () => void;
  /** Fade-in-step content rendered above the controls overlay. */
  topOverlay?: React.ReactNode;
  /** Surfaces the player's `controlsVisible` flag so the slide chrome
   *  (title, performer badges) can fade in lockstep. */
  onControlsVisibilityChange?: (visible: boolean) => void;
  /** Optional — only set on online slides where the carousel has a
   *  next-slide callback (auto-advance + last-marker bridge). */
  onNext?: () => void;
  /** Marker-clip slides only — restricts player UI to a clip window. */
  clipRange?: { start: number; end: number };
  /** Marker poster override; defaults to the slide's poster. */
  posterSrc?: string;
  posterImage?: PreviewImageData | null;
  /** Marker slides start at the marker's `seconds`. */
  initialTimestamp?: number;
  /** Captures a getter for the live playhead — used by the offline
   *  slide for IDB resume-position writes. */
  sendGetCurrentTime?: (getter: () => number | undefined) => void;
  /** Captures a pauser — the slide content calls this when the user
   *  opens a link in a new tab so the video doesn't keep playing in the
   *  background. */
  sendPause?: (pauser: () => void) => void;
}

export function LightboxScenePlayer({
  scene,
  playbackKey,
  suspended,
  loopEnabled,
  onLoopToggle,
  onToggleFullscreen,
  onClose,
  topOverlay,
  onControlsVisibilityChange,
  onNext,
  clipRange,
  posterSrc,
  posterImage,
  initialTimestamp,
  sendGetCurrentTime,
  sendPause,
}: LightboxScenePlayerProps) {
  const { configuration } = useConfigurationContext();
  const autostartEnabled = configuration.interface.autostartVideo ?? true;

  return (
    <ScenePlayer
      scene={scene}
      playbackKey={playbackKey}
      suspended={suspended}
      autoplay
      autostartEnabled={autostartEnabled}
      playDelayMs={500}
      preload="auto"
      disableSeekArrows
      fill
      enablePinchZoom
      onToggleFullscreenOverride={onToggleFullscreen}
      onClose={onClose}
      loopEnabled={loopEnabled}
      onLoopToggle={onLoopToggle}
      onControlsVisibilityChange={onControlsVisibilityChange}
      onNext={onNext}
      clipRange={clipRange}
      posterSrc={posterSrc}
      posterImage={posterImage}
      initialTimestamp={initialTimestamp}
      sendGetCurrentTime={sendGetCurrentTime}
      sendPause={sendPause}
      topOverlay={topOverlay}
    />
  );
}
