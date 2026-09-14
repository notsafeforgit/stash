export type PlaybackRange = Readonly<{ start: number; end: number }>;
export interface SceneTiming {
  files: readonly { duration?: number | null }[];
  scene_markers: readonly {
    id: string;
    seconds: number;
    end_seconds?: number | null;
  }[];
  resume_time?: number | null;
}

export function markerRange(
  scene: SceneTiming,
  marker: { id: string; seconds: number; end_seconds?: number | null },
): PlaybackRange | undefined {
  const duration = scene.files[0]?.duration;
  const own =
    scene.scene_markers.find((item) => item.id === marker.id) ?? marker;
  const start = own.seconds;
  if (
    !Number.isFinite(start) ||
    start < 0 ||
    !duration ||
    !Number.isFinite(duration) ||
    start >= duration
  )
    return;
  const explicitEnd = own.end_seconds;
  let end =
    explicitEnd != null && Number.isFinite(explicitEnd) && explicitEnd > start
      ? Math.min(explicitEnd, duration)
      : duration;
  if (
    explicitEnd == null ||
    !Number.isFinite(explicitEnd) ||
    explicitEnd <= start
  ) {
    for (const candidate of scene.scene_markers) {
      if (candidate.seconds > start && candidate.seconds < end)
        end = candidate.seconds;
    }
  }
  return end > start ? { start, end } : undefined;
}
