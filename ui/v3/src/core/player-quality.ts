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

/** Only advertised transcodes can satisfy a bandwidth ceiling. Direct and
 * remux preserve the original bitrate even when their labels look smaller. */
export function selectFixedQuality<T extends { src: string }>(
  sources: readonly T[],
  quality: Extract<PlayerQuality, { kind: "fixed" }>,
  dimensions: { width?: number; height?: number },
): T | null {
  const ceiling = qualityHeight[quality.resolution];
  const original = Math.min(dimensions.width ?? 0, dimensions.height ?? 0);
  let selected: { source: T; height: number } | undefined;
  for (const source of sources) {
    let url: URL;
    try {
      url = new URL(source.src);
    } catch {
      continue;
    }
    if (!url.pathname.endsWith("/stream.master.m3u8")) continue;
    const resolution = url.searchParams.get("resolution");
    const tier = qualityTiers.find((value) => value === resolution);
    const height = tier
      ? qualityHeight[tier]
      : resolution === StreamingResolutionEnum.Original
        ? original
        : 0;
    if (
      height > 0 &&
      height <= ceiling &&
      (!selected || height > selected.height)
    ) {
      selected = { source, height };
    }
  }
  return selected?.source ?? null;
}
