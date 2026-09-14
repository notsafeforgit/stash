import {
  markerRange,
  type PlaybackRange,
  type SceneTiming,
} from "../marker-range";
import type { TvSettings } from "./settings";

export function seededValue(seed: number, key: string): number {
  let hash = seed | 0;
  for (const character of key)
    hash = Math.imul(hash ^ character.charCodeAt(0), 16777619);
  hash ^= hash >>> 16;
  return (hash >>> 0) / 4294967296;
}
export type TvPlaybackPlan =
  | { kind: "ready"; start: number; range?: PlaybackRange }
  | { kind: "invalid"; reason: string };
export function tvPlaybackPlan(
  scene: SceneTiming,
  settings: TvSettings,
  seed: number,
  key: string,
  marker?: { id: string; seconds: number; end_seconds?: number | null },
): TvPlaybackPlan {
  const duration = scene.files[0]?.duration;
  if (!duration || !Number.isFinite(duration) || duration <= 0)
    return { kind: "invalid", reason: "This scene has no playable duration" };
  if (marker) {
    const range = markerRange(scene, marker);
    return range
      ? { kind: "ready", start: range.start, range }
      : { kind: "invalid", reason: "This marker has no playable range" };
  }
  const random = seededValue(seed, key);
  const starts = scene.scene_markers
    .map((item) => item.seconds)
    .filter((time) => Number.isFinite(time) && time >= 0 && time < duration);
  let start =
    settings.start === "resume"
      ? (scene.resume_time ?? 0)
      : settings.start === "beginning"
        ? 0
        : settings.start === "random-marker" && starts.length
          ? (starts[Math.floor(random * starts.length)] ?? 0)
          : random * Math.max(0, duration - 1);
  if (!Number.isFinite(start) || start < 0 || start >= duration - 0.05)
    start = 0;
  if (settings.window.kind === "full") return { kind: "ready", start };
  const length =
    settings.window.kind === "fixed"
      ? settings.window.seconds
      : settings.window.min +
        seededValue(seed, `${key}:length`) *
          (settings.window.max - settings.window.min);
  return {
    kind: "ready",
    start,
    range: { start, end: Math.min(duration, start + length) },
  };
}

export function markerAwareSeek(
  position: number,
  direction: -1 | 1,
  markers: readonly { seconds: number }[],
  range: PlaybackRange,
): number {
  const candidates = markers
    .map((marker) => marker.seconds)
    .filter(
      (time) =>
        time >= range.start &&
        time < range.end &&
        (direction > 0 ? time > position + 0.25 : time < position - 1),
    );
  const next =
    direction > 0 ? Math.min(...candidates) : Math.max(...candidates);
  return Math.max(
    range.start,
    Math.min(
      range.end,
      Number.isFinite(next) ? next : position + direction * 10,
    ),
  );
}
