import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { collectionSchema } from "./collection-api";

const integer = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
const positive = integer.positive();
const timestamp = z.string().datetime({ offset: true });
const errorCode = z
  .string()
  .max(128)
  .regex(/^[a-z0-9_.-]*$/);
export const jobKindSchema = z.enum([
  "media.verify",
  "album.backfill",
  "text.translate",
  "post.enrich",
  "account.list_page",
  "post.verify_candidate",
  "post.merge_notify",
]);
export const jobStateSchema = z.enum([
  "queued",
  "running",
  "succeeded",
  "failed",
  "cancelled",
]);
export const runStateSchema = z.enum([
  "queued",
  "running",
  "succeeded",
  "deferred",
  "cancelled",
]);
export const jobActivityFilterSchema = z.object({
  kind: z.union([jobKindSchema, z.literal("")]).default(""),
  state: z.union([jobStateSchema, z.literal("")]).default(""),
});
export const runActivityFilterSchema = z.object({
  collection: z.union([uuid, z.literal("")]).default(""),
  state: z.union([runStateSchema, z.literal("")]).default(""),
});
const common = z.object({
  sequence: positive,
  uuid,
  revision: positive,
  attempt_count: integer,
  available_at: timestamp,
  lease_until: timestamp.nullable(),
  error_code: errorCode,
  created_at: timestamp,
  updated_at: timestamp,
});
export const jobActivitySchema = common
  .extend({
    kind: jobKindSchema,
    state: jobStateSchema,
    max_attempts: positive,
  })
  .refine(
    (row) =>
      row.attempt_count <= row.max_attempts &&
      (row.state === "running") === (row.lease_until !== null),
  );
export const runActivitySchema = common
  .extend({
    collection_uuid: uuid,
    canonical_collection_uuid: uuid.optional(),
    collection_revision: positive,
    collection_label: collectionSchema.shape.label,
    target_url: collectionSchema.shape.target_url,
    operation: z.enum(["download", "enrich"]),
    state: runStateSchema,
    failures: integer,
    pending_windows: integer.max(64),
    completed_windows: integer.max(64),
  })
  .refine(
    (row) =>
      (row.state === "running") === (row.lease_until !== null) &&
      (row.state !== "succeeded" || row.pending_windows === 0),
  );
const sourceWindowSchema = z
  .object({
    since: timestamp.nullable(),
    until: timestamp,
    basis: z.literal("traversal").optional(),
  })
  .refine(
    (row) =>
      row.since === null ||
      (row.basis !== "traversal" &&
        Date.parse(row.since) < Date.parse(row.until)),
  );
export const runActivityDetailSchema = z
  .object({
    summary: runActivitySchema,
    pending: z.array(sourceWindowSchema).max(64),
    completed: z.array(sourceWindowSchema).max(64),
    window: sourceWindowSchema.nullable(),
    progress: z.object({
      items_seen: integer,
      files_completed: integer,
      cursor: z.string().max(4096),
    }),
    root_uuid: uuid.nullable(),
    path_prefix: collectionSchema.shape.path_prefix,
    recovery: z
      .object({ activation_uuid: uuid, replay_archive: z.boolean() })
      .nullable(),
  })
  .refine(
    (row) =>
      row.pending.length === row.summary.pending_windows &&
      row.completed.length === row.summary.completed_windows &&
      (row.summary.state === "running") === (row.window !== null) &&
      new Set(
        [
          ...row.pending,
          ...row.completed,
          ...(row.window ? [row.window] : []),
        ].map((window) => window.basis),
      ).size <= 1 &&
      (row.summary.operation === "download" ||
        [
          ...row.pending,
          ...row.completed,
          ...(row.window ? [row.window] : []),
        ].every((window) => !window.basis)),
  );
export const activityAttemptSchema = z
  .object({
    number: positive,
    started_at: timestamp,
    ended_at: timestamp.nullable(),
    outcome: z.enum([
      "running",
      "succeeded",
      "retry",
      "failed",
      "cancelled",
      "expired",
      "deferred",
    ]),
    error_code: errorCode,
  })
  .refine(
    (row) =>
      (row.outcome === "running") === (row.ended_at === null) &&
      (row.ended_at === null ||
        Date.parse(row.ended_at) >= Date.parse(row.started_at)),
  );

export type JobActivityFilter = z.infer<typeof jobActivityFilterSchema>;
const activitySubjectSchema = z.object({
  kind: z.enum(["post", "collection", "scene", "image", "gallery"]),
  uuid,
  requested_uuid: uuid,
  title: z.string(),
  title_truncated: z.boolean(),
  state: z.enum(["active", "forgotten", "disabled", "retired", "deleted"]),
  revision: positive,
  local_id: positive.nullable(),
});
export const jobActivityDetailSchema = z.object({
  summary: jobActivitySchema,
  subjects: z.array(activitySubjectSchema).max(100),
  context_available: z.boolean(),
  relative_path: z.string().max(4096),
  manual_request_uuid: z.union([uuid, z.literal("")]),
  merge_request_uuid: z.union([uuid, z.literal("")]),
});
export const activitySearchSchema = z
  .object({
    view: z.enum(["jobs", "runs"]).default("jobs"),
    kind: z.union([jobKindSchema, z.literal("")]).default(""),
    state: z
      .union([jobStateSchema, z.literal("deferred"), z.literal("")])
      .default(""),
    collection: z.union([uuid, z.literal("")]).default(""),
    item: uuid.optional(),
  })
  .superRefine((value, ctx) => {
    if (
      value.view === "jobs"
        ? value.state === "deferred" || value.collection !== ""
        : value.state === "failed" || value.kind !== ""
    )
      ctx.addIssue({ code: "custom", message: "Invalid activity filter" });
  });
export type ActivitySearch = z.infer<typeof activitySearchSchema>;
export type ActivitySubject = z.infer<typeof activitySubjectSchema>;
export type JobActivityDetail = z.infer<typeof jobActivityDetailSchema>;
export type RunActivityFilter = z.infer<typeof runActivityFilterSchema>;
export type JobActivity = z.infer<typeof jobActivitySchema>;
export type RunActivity = z.infer<typeof runActivitySchema>;
export type RunActivityDetail = z.infer<typeof runActivityDetailSchema>;
export type ActivityAttempt = z.infer<typeof activityAttemptSchema>;
