import { z } from "zod";
import type { OfflineEntry } from "./offline-db";

const namedEntity = z.looseObject({ id: z.string(), name: z.string() });
/** Validate old browser data before migration without dropping extension data
 * or treating missing ownership evidence as proof of the current installation. */
export const offlineEntrySchema = z.looseObject({
  scene_id: z.string().regex(/^[1-9]\d*$/),
  request_id: z.string().optional(),
  queued_at: z.number().optional(),
  cancel_requested: z.boolean().optional(),
  title: z.string(),
  details: z.string().nullable().optional(),
  studio_name: z.string().nullable(),
  studio_id: z.string().nullable(),
  performers: z.array(namedEntity),
  tags: z.array(namedEntity),
  duration: z.number(),
  width: z.number(),
  height: z.number(),
  date: z.string().nullable(),
  paths: z.looseObject({
    screenshot: z.string().nullable(),
    preview: z.string().nullable(),
    sprite: z.string().nullable(),
    vtt: z.string().nullable(),
  }),
  last_position_seconds: z.number().optional(),
  format: z.enum(["copy", "copy-aac", "hevc", "h264", "av1"]),
  source_video_codec: z.string(),
  source_audio_codec: z.string(),
  source_file_path: z.string().optional(),
  resolution: z.string(),
  width_actual: z.number(),
  height_actual: z.number(),
  bytes: z.number().nonnegative(),
  downloaded_at: z.number(),
  status: z.enum(["queued", "downloading", "complete", "error"]),
  bytes_downloaded: z.number().nonnegative().optional(),
  error: z.string().optional(),
  opfs_path: z.string(),
  server_status: z.enum(["present", "missing", "unknown"]),
}) satisfies z.ZodType<OfflineEntry>;
