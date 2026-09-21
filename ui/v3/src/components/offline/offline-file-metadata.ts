import type { OfflineEntry } from "./offline-db";

/** Source properties retained even when the server is unavailable. */
export interface SourceFileMetadata {
  frame_rate: number | null;
  bit_rate: number | null;
  bit_depth: number | null;
  color_range: string | null;
  color_space: string | null;
  color_transfer: string | null;
  color_primaries: string | null;
}

export function snapshotFileMetadata(
  file: Partial<SourceFileMetadata>,
): SourceFileMetadata {
  return {
    frame_rate: file.frame_rate ?? null,
    bit_rate: file.bit_rate ?? null,
    bit_depth: file.bit_depth ?? null,
    color_range: file.color_range ?? null,
    color_space: file.color_space ?? null,
    color_transfer: file.color_transfer ?? null,
    color_primaries: file.color_primaries ?? null,
  };
}

/** Describe the downloaded rendition, without presenting source bitrate or
 * HDR tags as measurements of a different encoding. No local probe is run. */
export function offlineFileMetadata(entry: OfflineEntry) {
  const source = entry.source_file_metadata;
  const copiedVideo = entry.format === "copy" || entry.format === "copy-aac";
  const transfer = source?.color_transfer?.trim().toLowerCase();
  // The HEVC/AV1 download encoders explicitly preserve PQ/HLG in 10-bit.
  // Other transcodes do not guarantee output colour tags, so leave them unknown.
  const preservesHDR =
    (entry.format === "hevc" || entry.format === "av1") &&
    (transfer === "smpte2084" || transfer === "arib-std-b67");
  const color = copiedVideo || preservesHDR ? source : undefined;
  return {
    video_codec: copiedVideo ? entry.source_video_codec : entry.format,
    audio_codec:
      entry.format === "copy" || !entry.source_audio_codec
        ? entry.source_audio_codec
        : "aac",
    frame_rate: source?.frame_rate ?? 0,
    // The completed file's average includes audio and container overhead, as
    // does the server's file bitrate. Never use partial bytes as a final rate.
    bit_rate:
      entry.status === "complete" && entry.bytes > 0 && entry.duration > 0
        ? Math.round((entry.bytes * 8) / entry.duration)
        : entry.format === "copy"
          ? (source?.bit_rate ?? 0)
          : 0,
    bit_depth: copiedVideo
      ? (source?.bit_depth ?? null)
      : preservesHDR
        ? 10
        : null,
    color_range: color?.color_range ?? null,
    color_space: color?.color_space ?? null,
    color_transfer: color?.color_transfer ?? null,
    color_primaries: color?.color_primaries ?? null,
  };
}
