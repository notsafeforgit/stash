import { useEffect, useRef } from "react";
import { createPreparedScrubPreview } from "./prepared-scrub-preview";

/** A passive surface in the normal zoom layer, outside the main media store. */
export function SceneScrubPreview({
  sceneId,
  source,
  keyframes,
  time,
  start,
  end,
  offset,
  onFailure,
  onReady,
}: {
  sceneId: string;
  source: string;
  /** Keyframe index and still endpoints; null leaves the still tier off. */
  keyframes: { index: string; still: string } | null;
  time: number | null;
  start: number;
  end: number;
  offset: number;
  onFailure: () => void;
  onReady: (time: number | null) => void;
}) {
  const surfaceRef = useRef<HTMLDivElement>(null);
  const controllerRef = useRef<ReturnType<
    typeof createPreparedScrubPreview
  > | null>(null);
  const keyframeIndex = keyframes?.index ?? null;
  const keyframeStill = keyframes?.still ?? null;

  useEffect(() => {
    const surface = surfaceRef.current;
    if (!surface) return;
    const controller = createPreparedScrubPreview({
      surface,
      sceneId,
      source,
      keyframes:
        keyframeIndex && keyframeStill
          ? { index: keyframeIndex, still: keyframeStill }
          : null,
      start,
      end,
      offset,
      onFailure,
      onReady,
    });
    controllerRef.current = controller;
    return () => {
      controllerRef.current = null;
      controller.dispose();
    };
  }, [
    sceneId,
    source,
    keyframeIndex,
    keyframeStill,
    start,
    end,
    offset,
    onFailure,
    onReady,
  ]);
  // The controller is replaced when its owner/source changes, even if the
  // pointer is still at the same timestamp; pass that timestamp to the new one.
  // biome-ignore lint/correctness/useExhaustiveDependencies: synchronize the replacement controller
  useEffect(() => {
    controllerRef.current?.preview(time);
  }, [
    time,
    sceneId,
    source,
    keyframeIndex,
    keyframeStill,
    start,
    end,
    offset,
    onFailure,
    onReady,
  ]);

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
