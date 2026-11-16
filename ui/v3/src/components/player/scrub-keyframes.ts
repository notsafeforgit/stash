import { KEYFRAME_TOLERANCE_SECONDS } from "./scrub-frame-cache";

/** Source keyframe (sync sample) times of the scene's primary file, in scene
 * time. `version` changes with the file's bytes or location. */
export interface KeyframeIndex {
  version: string;
  times: readonly number[];
}

export type KeyframeIndexResult =
  | { status: "ok"; index: KeyframeIndex }
  /** Not available for this file or server; raw still requests remain. */
  | { status: "missing" }
  /** Busy, slow or unreachable; may be retried once after `after` seconds. */
  | { status: "retry"; after: number };

export type KeyframeStillResult =
  | { status: "ok"; blob: Blob; time: number | null }
  /** No still endpoint or no video file: the tier is unavailable. */
  | { status: "missing" }
  /** The server's extraction limit is saturated. */
  | { status: "busy"; after: number }
  | { status: "error" }
  | { status: "aborted" };

export function parseKeyframeIndex(value: unknown): KeyframeIndex | null {
  if (!value || typeof value !== "object") return null;
  const { version, times } = value as { version?: unknown; times?: unknown };
  if (typeof version !== "string" || !Array.isArray(times)) return null;
  const valid = times.filter(
    (time): time is number =>
      typeof time === "number" && Number.isFinite(time) && time >= 0,
  );
  if (!valid.length) return null;
  // The contract promises ascending times; tolerate an unsorted response
  // rather than returning the wrong still from the binary search.
  for (let index = 1; index < valid.length; index++)
    if (valid[index]! < valid[index - 1]!) {
      valid.sort((a, b) => a - b);
      break;
    }
  return { version, times: valid };
}

/** Position of the greatest keyframe at or before `time`, matching the
 * server's tolerance. Targets before the first keyframe resolve to it. */
export function keyframePosition(times: readonly number[], time: number) {
  if (!times.length || !Number.isFinite(time)) return -1;
  let low = 0;
  let high = times.length - 1;
  let found = 0;
  while (low <= high) {
    const middle = (low + high) >> 1;
    if (times[middle]! <= time + KEYFRAME_TOLERANCE_SECONDS) {
      found = middle;
      low = middle + 1;
    } else high = middle - 1;
  }
  return found;
}

export function keyframeAt(times: readonly number[], time: number) {
  return times[keyframePosition(times, time)];
}

/** Breadth-first keyframes within the selected range: the requested first
 * position, then the middle, quarters, eighths, etc. Remaining keyframes that
 * fractional times cannot reach (clustered GOPs) follow in order. */
export function keyframePreparationTimes(
  times: readonly number[],
  start: number,
  end: number,
  limit: number,
  first?: number,
): number[] {
  if (!times.length || !Number.isFinite(start) || !Number.isFinite(end))
    return [];
  if (end <= start) return [];
  const low = keyframePosition(times, start);
  const high = keyframePosition(times, end - KEYFRAME_TOLERANCE_SECONDS * 2);
  const total = high - low + 1;
  const count = Math.min(limit, total);
  const seen = new Set<number>();
  const result: number[] = [];
  const add = (position: number) => {
    if (result.length >= count || seen.has(position)) return;
    seen.add(position);
    result.push(times[position]!);
  };
  if (first !== undefined && Number.isFinite(first))
    add(keyframePosition(times, Math.max(start, Math.min(end, first))));
  for (
    let divisions = 2;
    result.length < count && divisions <= total * 4;
    divisions *= 2
  )
    for (
      let numerator = 1;
      numerator < divisions && result.length < count;
      numerator += 2
    )
      add(
        keyframePosition(
          times,
          start + ((end - start) * numerator) / divisions,
        ),
      );
  for (
    let position = low;
    position <= high && result.length < count;
    position++
  )
    add(position);
  return result;
}

/** Index times are reused exactly, with the file version, so the same
 * keyframe always has one URL and an immutable HTTP cache entry. */
export function keyframeStillURL(base: string, time: number, version?: string) {
  const url = new URL(base);
  url.searchParams.set("t", String(time));
  if (version !== undefined) url.searchParams.set("v", version);
  return url.toString();
}

function retryAfter(
  response: Response,
  fallback: number,
  min: number,
  max: number,
) {
  const header = response.headers.get("Retry-After");
  const seconds = header === null ? Number.NaN : Number(header);
  return Math.max(
    min,
    Math.min(max, Number.isFinite(seconds) ? seconds : fallback),
  );
}

export async function fetchKeyframeIndex(
  url: string,
  signal: AbortSignal,
): Promise<KeyframeIndexResult> {
  try {
    const response = await fetch(url, { signal });
    if (
      response.status === 503 ||
      response.status === 429 ||
      response.status >= 500
    )
      return { status: "retry", after: retryAfter(response, 30, 5, 60) };
    if (!response.ok) return { status: "missing" };
    const index = parseKeyframeIndex(await response.json());
    return index ? { status: "ok", index } : { status: "missing" };
  } catch {
    // A timeout aborts the request; the server keeps computing and caching.
    return { status: "retry", after: 30 };
  }
}

export async function fetchKeyframeStill(
  url: string,
  signal: AbortSignal,
  background: boolean,
): Promise<KeyframeStillResult> {
  try {
    const response = await fetch(url, {
      signal,
      priority: background ? "low" : "high",
    });
    if (response.status === 404) return { status: "missing" };
    if (response.status === 503 || response.status === 429)
      return { status: "busy", after: retryAfter(response, 1, 0.5, 10) };
    const type = response.headers.get("Content-Type") ?? "";
    if (!response.ok || !type.startsWith("image/")) return { status: "error" };
    const header = response.headers.get("X-Keyframe-Time");
    const time = header === null ? Number.NaN : Number(header);
    const blob = await response.blob();
    return { status: "ok", blob, time: Number.isFinite(time) ? time : null };
  } catch {
    return signal.aborted ? { status: "aborted" } : { status: "error" };
  }
}
