import type { PlayerSource } from "./player-utils";

/** Menu choices from the already capability-filtered playback catalog.
 * Keep the complete catalog for decoding recovery; only collapse presentation.
 * HDR and SDR are delivery variants of the same resolution, not two qualities.
 */
export function getQualityMenuSources(
  sources: readonly PlayerSource[],
  activeSource: PlayerSource | null,
): PlayerSource[] {
  const choices = new Map<string, { source: PlayerSource; hdr: boolean }>();
  for (const source of sources) {
    let key = source.src;
    let hdr = false;
    try {
      const url = new URL(source.src);
      const endpoint = /\/stream(\.hevc)?\.master\.m3u8$/.exec(url.pathname);
      if (endpoint) {
        hdr = !!endpoint[1];
        key = JSON.stringify([
          url.origin,
          url.pathname.slice(0, endpoint.index),
          url.searchParams.get("resolution") ?? "ORIGINAL",
          url.searchParams.get("start"),
          url.searchParams.get("end"),
        ]);
      }
    } catch {
      // Offline and synthetic sources retain their exact URL identity.
    }
    const previous = choices.get(key);
    // A codec failure may have selected SDR. Represent what is actually
    // playing instead of hiding its selection or immediately retrying HDR.
    if (
      !previous ||
      source.src === activeSource?.src ||
      (previous.source.src !== activeSource?.src && hdr && !previous.hdr)
    ) {
      choices.set(key, { source, hdr });
    }
  }

  const preferred = [...choices.values()].map(({ source }) => source);
  const hasDirect = preferred.some(
    (source) => source.label === "Direct stream",
  );
  const hasRemux = preferred.some((source) => source.label === "HLS (remux)");
  const redundantLabels = new Set<string>();
  if (hasDirect) redundantLabels.add("HLS (remux)");
  if (hasDirect || hasRemux) {
    redundantLabels.add("HLS (transcode)");
    redundantLabels.add("HLS (HDR)");
  }

  return preferred.filter(
    (source) =>
      !source.label ||
      !redundantLabels.has(source.label) ||
      source.src === activeSource?.src,
  );
}
