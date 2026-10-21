import { useEffect, useRef } from "react";
import { createPreparedScrubPreview } from "./prepared-scrub-preview";

/** A passive surface in the normal zoom layer, outside the main media store. */
export function SceneScrubPreview({
  sceneId,
  source,
  time,
  start,
  end,
  offset,
  onFailure,
  onReady,
}: {
  sceneId: string;
  source: string;
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

  useEffect(() => {
    const surface = surfaceRef.current;
    if (!surface) return;
    const controller = createPreparedScrubPreview({
      surface,
      sceneId,
      source,
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
  }, [sceneId, source, start, end, offset, onFailure, onReady]);
  // The controller is replaced when its owner/source changes, even if the
  // pointer is still at the same timestamp; pass that timestamp to the new one.
  // biome-ignore lint/correctness/useExhaustiveDependencies: synchronize the replacement controller
  useEffect(() => {
    controllerRef.current?.preview(time);
  }, [time, sceneId, source, start, end, offset, onFailure, onReady]);

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
