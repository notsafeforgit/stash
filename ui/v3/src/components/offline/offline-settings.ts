/**
 * Per-device settings for the offline-downloads feature.
 *
 * Only one knob in Phase 1: max-resolution preference. Stored in
 * `localStorage` (not server config) because it's a device-specific
 * preference — a user might want 1080p downloads on their phone and
 * 4K on their desktop.
 *
 * Default: 1080p — the bandwidth + storage sweet spot for the
 * mobile-PWA primary use case. Desktop users can dial up.
 */

import { StreamingResolutionEnum } from "src/core/generated-graphql";

export const OFFLINE_MAX_RESOLUTION_KEY = "stash-offline-max-resolution";

const DEFAULT_MAX_RESOLUTION = StreamingResolutionEnum.FullHd;

const ALL_VALID = new Set<string>([
  StreamingResolutionEnum.Original,
  StreamingResolutionEnum.FourK,
  StreamingResolutionEnum.FullHd,
  StreamingResolutionEnum.StandardHd,
  StreamingResolutionEnum.Standard,
  StreamingResolutionEnum.Low,
]);

export function loadOfflineMaxResolution(): StreamingResolutionEnum {
  try {
    const v = localStorage.getItem(OFFLINE_MAX_RESOLUTION_KEY);
    if (v && ALL_VALID.has(v)) {
      return v as StreamingResolutionEnum;
    }
  } catch {
    /* localStorage unavailable (private mode etc.) */
  }
  return DEFAULT_MAX_RESOLUTION;
}

export function saveOfflineMaxResolution(value: StreamingResolutionEnum): void {
  try {
    localStorage.setItem(OFFLINE_MAX_RESOLUTION_KEY, value);
  } catch {
    /* ignore */
  }
}

/** Highest-to-lowest order, matching the streaming-resolution UI
 *  convention elsewhere in the app. Labels live in the locale file
 *  under `offline.resolution.*`; the UI looks them up via the
 *  `intl_id` returned here. */
export const OFFLINE_RESOLUTION_OPTIONS: ReadonlyArray<{
  value: StreamingResolutionEnum;
  intl_id: string;
}> = [
  {
    value: StreamingResolutionEnum.Original,
    intl_id: "offline.resolution.original",
  },
  {
    value: StreamingResolutionEnum.FourK,
    intl_id: "offline.resolution.four_k",
  },
  {
    value: StreamingResolutionEnum.FullHd,
    intl_id: "offline.resolution.full_hd",
  },
  {
    value: StreamingResolutionEnum.StandardHd,
    intl_id: "offline.resolution.standard_hd",
  },
  {
    value: StreamingResolutionEnum.Standard,
    intl_id: "offline.resolution.standard",
  },
  { value: StreamingResolutionEnum.Low, intl_id: "offline.resolution.low" },
];
