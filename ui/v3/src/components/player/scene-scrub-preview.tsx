import { useEffect, useRef } from "react";
import { createScrubStream } from "./scrub-stream";

/** A passive media surface inside the ordinary player's zoom layer. Its video
 * is created on first use and never registers with the main Video.js store. */
export function SceneScrubPreview({
  sceneId,
  source,
  time,
  onFailure,
}: {
  sceneId: string;
  source: string;
  time: number | null;
  onFailure: () => void;
}) {
  const surfaceRef = useRef<HTMLDivElement>(null);
  const updateRef = useRef<((time: number | null) => void) | null>(null);

  useEffect(() => {
    const surface = surfaceRef.current;
    if (!surface) return;
    let controller: ReturnType<typeof createScrubStream> | null = null;
    let video: HTMLVideoElement | null = null;
    updateRef.current = (target) => {
      if (!controller && target !== null) {
        video = document.createElement("video");
        video.dataset.scrubPreview = "";
        video.muted = true;
        video.playsInline = true;
        video.disableRemotePlayback = true;
        video.preload = "auto";
        video.tabIndex = -1;
        video.className = "size-full object-contain";
        video.setAttribute("aria-hidden", "true");
        surface.append(video);
        controller = createScrubStream(
          video,
          sceneId,
          source,
          (visible) => {
            // visibility:hidden prevents WebKit's silent video pipeline from
            // initializing its video sink. Keep the layer laid out at opacity 0.
            surface.style.opacity = visible ? "1" : "0";
            surface.dataset.previewReady = String(visible);
          },
          onFailure,
        );
      }
      controller?.preview(target);
    };
    return () => {
      updateRef.current = null;
      controller?.dispose();
      video?.remove();
    };
  }, [sceneId, source, onFailure]);
  // The controller is replaced when its owner/source changes, even if the
  // pointer is still at the same timestamp; pass that timestamp to the new one.
  // biome-ignore lint/correctness/useExhaustiveDependencies: synchronize the replacement controller
  useEffect(() => {
    updateRef.current?.(time);
  }, [time, sceneId, source, onFailure]);

  return (
    <div
      ref={surfaceRef}
      data-scrub-surface
      aria-hidden
      className="absolute inset-0 pointer-events-none bg-black"
      style={{ opacity: 0 }}
    />
  );
}
