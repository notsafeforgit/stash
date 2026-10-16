import { StreamingResolutionEnum } from "./generated-graphql";
import {
  playerQualitySchema,
  qualityHeight,
  qualityTiers,
  type PlayerQuality,
} from "./player-quality";

export const QUALITY_STORAGE_KEY = "stash-player-quality";
const best: PlayerQuality = { kind: "best" };

/** Preference upgrade only: older v3 players persisted the display label.
 * Live source selection and new writes use the resolution enum instead. */
function upgradeQualityLabel(label: string): PlayerQuality {
  const height = /^HLS(?: [^(]+)? \((240|480|720|1080|2160)p\)$/.exec(
    label,
  )?.[1];
  const resolution = qualityTiers.find(
    (tier) => qualityHeight[tier] === Number(height),
  );
  return resolution ? { kind: "fixed", resolution } : best;
}

export function loadPlayerQualityPreference(): PlayerQuality {
  try {
    const saved = localStorage.getItem(QUALITY_STORAGE_KEY);
    if (!saved) return best;
    try {
      return playerQualitySchema.parse(JSON.parse(saved));
    } catch {
      return upgradeQualityLabel(saved);
    }
  } catch {
    return best;
  }
}

/** An explicit source applies now; its resolution ceiling applies to later
 * online items. Offline blobs must not replace the online preference. */
export function savePlayerQualityPreference(source: { src: string }): void {
  try {
    const url = new URL(source.src);
    let quality: PlayerQuality;
    if (
      url.pathname.endsWith("/stream.master.m3u8") ||
      url.pathname.endsWith("/stream.hevc.master.m3u8")
    ) {
      const resolution = url.searchParams.get("resolution");
      const tier = qualityTiers.find((value) => value === resolution);
      if (!tier && resolution !== StreamingResolutionEnum.Original) return;
      quality = tier ? { kind: "fixed", resolution: tier } : best;
    } else if (
      (url.pathname.endsWith("/stream") &&
        !url.searchParams.has("resolution")) ||
      url.pathname.endsWith("/stream.fmp4.master.m3u8") ||
      url.pathname.endsWith("/stream.fmp4.aac.master.m3u8")
    ) {
      quality = best;
    } else return;
    localStorage.setItem(QUALITY_STORAGE_KEY, JSON.stringify(quality));
  } catch {
    // Restricted browser storage must not prevent an explicit source change.
  }
}
