/** Time from a scrub target request until each preview tier displays it:
 * a cached keyframe still, a fetched keyframe still, the exact frame from the
 * silent scrub stream, or a seek inside the main player's own buffer. */
export const SCRUB_TIMING_TIERS = [
  "still-cache",
  "still-network",
  "exact-frame",
  "main-buffer",
] as const;
export type ScrubTimingTier = (typeof SCRUB_TIMING_TIERS)[number];

export interface ScrubTiming {
  tier: ScrubTimingTier;
  /** Milliseconds from the target request until it was displayed. */
  ms: number;
  /** Wall-clock time of the measurement. */
  at: number;
}

export interface ScrubTimingStats {
  count: number;
  median: number | null;
  p90: number | null;
}

export type ScrubTimingSummary = Record<ScrubTimingTier, ScrubTimingStats>;

const CAPACITY = 256;
let ring: ScrubTiming[] = [];
let cursor = 0;
let snapshot: ScrubTimingSummary | null = null;
const listeners = new Set<() => void>();

/** A small in-memory ring buffer for measuring on a device; nothing is sent. */
export function recordScrubTiming(tier: ScrubTimingTier, ms: number) {
  if (!Number.isFinite(ms) || ms < 0) return;
  const entry = { tier, ms: Math.round(ms * 10) / 10, at: Date.now() };
  if (ring.length < CAPACITY) ring.push(entry);
  else {
    ring[cursor] = entry;
    cursor = (cursor + 1) % CAPACITY;
  }
  snapshot = null;
  for (const listener of listeners) listener();
}

/** Recorded timings, oldest first. */
export function scrubTimings(): ScrubTiming[] {
  return [...ring.slice(cursor), ...ring.slice(0, cursor)];
}

/** Nearest-rank percentile of ascending values. */
function percentile(sorted: readonly number[], fraction: number) {
  if (!sorted.length) return null;
  return sorted[Math.max(0, Math.ceil(fraction * sorted.length) - 1)]!;
}

export function summarizeScrubTimings(
  entries: readonly ScrubTiming[] = ring,
): ScrubTimingSummary {
  const summary = {} as ScrubTimingSummary;
  for (const tier of SCRUB_TIMING_TIERS) {
    const values = entries
      .filter((entry) => entry.tier === tier)
      .map((entry) => entry.ms)
      .sort((a, b) => a - b);
    summary[tier] = {
      count: values.length,
      median: percentile(values, 0.5),
      p90: percentile(values, 0.9),
    };
  }
  return summary;
}

/** Stable between recordings, for `useSyncExternalStore`. */
export function scrubTimingSnapshot() {
  snapshot ??= summarizeScrubTimings();
  return snapshot;
}

export function subscribeScrubTimings(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function clearScrubTimings() {
  ring = [];
  cursor = 0;
  snapshot = null;
  for (const listener of listeners) listener();
}

export const SCRUB_DEBUG_STORAGE_KEY = "stash.scrubDebug";

/** Opt-in on-screen readout: `localStorage["stash.scrubDebug"] = "1"`. */
export function scrubDebugEnabled() {
  try {
    return localStorage.getItem(SCRUB_DEBUG_STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

declare global {
  interface Window {
    /** Scrub preview time-to-display measurements, for device diagnosis. */
    __stashScrubTimings?: {
      readonly entries: ScrubTiming[];
      summary: () => ScrubTimingSummary;
      clear: () => void;
    };
  }
}

if (typeof window !== "undefined")
  window.__stashScrubTimings = {
    get entries() {
      return scrubTimings();
    },
    summary: () => summarizeScrubTimings(),
    clear: clearScrubTimings,
  };
