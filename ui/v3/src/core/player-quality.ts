import { z } from "zod";
import { StreamingResolutionEnum } from "./generated-graphql";

export const qualityTiers = [
  StreamingResolutionEnum.Low,
  StreamingResolutionEnum.Standard,
  StreamingResolutionEnum.StandardHd,
  StreamingResolutionEnum.FullHd,
  StreamingResolutionEnum.FourK,
] as const;

export const playerQualitySchema = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("best") }),
  z.object({ kind: z.literal("fixed"), resolution: z.enum(qualityTiers) }),
]);
export type PlayerQuality = z.infer<typeof playerQualitySchema>;
export const qualityHeight: Record<(typeof qualityTiers)[number], number> = {
  LOW: 240,
  STANDARD: 480,
  STANDARD_HD: 720,
  FULL_HD: 1080,
  FOUR_K: 2160,
};

/** Select from browser-compatible sources under a resolution ceiling (the
 * shorter edge, including portrait files). Prefer the original video when its
 * dimensions are known to fit; this is not a bitrate limit. */
export function selectFixedQuality<T extends { src: string }>(
  sources: readonly T[],
  quality: Extract<PlayerQuality, { kind: "fixed" }>,
  dimensions: { width?: number; height?: number },
): T | null {
  const ceiling = qualityHeight[quality.resolution];
  const original = Math.min(dimensions.width ?? 0, dimensions.height ?? 0);
  const originalFits =
    Number.isFinite(dimensions.width) &&
    Number.isFinite(dimensions.height) &&
    original > 0 &&
    original <= ceiling;
  let preserved: { source: T; priority: number } | undefined;
  let selected: { source: T; height: number; hdr: boolean } | undefined;
  for (const source of sources) {
    let url: URL;
    try {
      url = new URL(source.src);
    } catch {
      continue;
    }
    if (originalFits) {
      const priority = [
        "/stream",
        "/stream.fmp4.master.m3u8",
        "/stream.fmp4.aac.master.m3u8",
      ].findIndex((suffix) => url.pathname.endsWith(suffix));
      if (
        priority >= 0 &&
        (priority > 0 || !url.searchParams.has("resolution")) &&
        (!preserved || priority < preserved.priority)
      ) {
        preserved = { source, priority };
      }
    }
    const hdr = url.pathname.endsWith("/stream.hevc.master.m3u8");
    if (!hdr && !url.pathname.endsWith("/stream.master.m3u8")) continue;
    const resolution = url.searchParams.get("resolution");
    const tier = qualityTiers.find((value) => value === resolution);
    const height = tier
      ? qualityHeight[tier]
      : resolution === StreamingResolutionEnum.Original && originalFits
        ? original
        : 0;
    if (
      height > 0 &&
      height <= ceiling &&
      (!selected ||
        height > selected.height ||
        (height === selected.height && hdr && !selected.hdr))
    ) {
      selected = { source, height, hdr };
    }
  }
  return preserved?.source ?? selected?.source ?? null;
}
