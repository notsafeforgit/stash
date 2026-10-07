import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { createSourcePostAPI } from "./source-post-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const count = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
const signature = z.string().regex(/^[0-9a-f]{64}$/);
const timestamp = z.string().datetime({ offset: true });
export const albumPolicySchema = z.enum([
  "source-identifiers-v1",
  "legacy-reddit-filename-v1",
]);
const action = z.enum(["create", "sync", "disabled", "ineligible"]);
export const albumIdentitySchema = z
  .object({
    uuid,
    kind: z.enum(["scene", "image", "gallery"]),
    state: z.enum(["active", "deleted", "redirected"]),
    revision,
    local_id: revision.optional(),
  })
  .refine((row) => (row.state === "active") === (row.local_id !== undefined));
const mediaIdentity = albumIdentitySchema.refine(
  (row) => row.kind !== "gallery",
);
const proof = z.object({
  evidence_uuid: uuid,
  post_file_uuid: uuid.optional(),
  match_uuid: uuid.optional(),
  file_uuid: uuid.optional(),
  generation: revision.optional(),
  archive_file_uuid: uuid.optional(),
  archive_generation: revision.optional(),
  relative_path: z.string().optional(),
  basis: z.enum(["source-id", "legacy-reddit-filename", "attachment-evidence"]),
  status: z.enum([
    "valid",
    "file-changed",
    "owner-changed",
    "media-unavailable",
    "evidence-only",
  ]),
});
const match = z
  .object({
    attachment_uuid: uuid,
    attachment_revision: revision,
    reference: z.object({
      namespace: z.string().min(1),
      value: z.string().min(1),
    }),
    decision_uuid: uuid.optional(),
    status: z.enum([
      "matched",
      "preserved",
      "ambiguous",
      "review",
      "unavailable",
    ]),
    reason: z.string().optional(),
    candidates: z
      .array(
        z.object({
          media_uuid: uuid,
          media_revision: revision,
          media_kind: z.enum(["scene", "image"]),
          proofs: z.array(proof).min(1).max(8192),
        }),
      )
      .max(8192),
  })
  .refine((row) => {
    if (row.status === "preserved")
      return !!row.decision_uuid && row.candidates.length === 0;
    if (row.decision_uuid) return false;
    if (row.status === "matched")
      return (
        row.candidates.length === 1 &&
        row.candidates[0]?.proofs.some((p) => p.status === "valid")
      );
    if (row.status === "unavailable") return row.candidates.length === 0;
    return row.status === "ambiguous"
      ? row.candidates.length > 1
      : row.candidates.length === 1;
  });
export const albumPreviewSchema = z
  .object({
    post_uuid: uuid,
    policy: albumPolicySchema,
    signature,
    action: z.enum(["create", "sync", "disabled", "ineligible", "review"]),
    selection_uuid: uuid.optional(),
    gallery: albumIdentitySchema
      .refine((row) => row.kind === "gallery")
      .optional(),
    association: z
      .object({
        uuid,
        state: z.enum(["linked", "disabled"]),
        revision,
        origin: z.string(),
        reason: z.string().optional(),
      })
      .optional(),
    initial_metadata: z
      .object({
        title: z.string(),
        details: z.string(),
        date: z.string().nullable(),
      })
      .optional(),
    entries: z
      .array(
        z.object({
          position: z.number().int().min(0).max(999999),
          attachment_uuid: uuid,
          media_uuid: uuid.optional(),
          media_kind: z.enum(["scene", "image"]).optional(),
          media_revision: revision.optional(),
          status: z.enum([
            "linked",
            "unselected",
            "unlinked",
            "deleted",
            "excluded",
            "post_unlinked",
            "post_conflict",
          ]),
        }),
      )
      .max(8192),
    add: z.array(mediaIdentity).max(8192),
    remove: z.array(mediaIdentity).max(8192),
    matches: z.array(match).max(8192),
  })
  .refine((preview) => {
    if (
      (preview.action === "create") !==
      (preview.initial_metadata !== undefined)
    )
      return false;
    if (preview.action === "create" && preview.gallery) return false;
    let previous = -1;
    for (const entry of preview.entries) {
      if (entry.position <= previous) return false;
      previous = entry.position;
    }
    const attachments = new Set(
      preview.entries.map((entry) => entry.attachment_uuid),
    );
    if (
      new Set(preview.matches.map((row) => row.attachment_uuid)).size !==
      preview.matches.length
    )
      return false;
    if (preview.matches.some((row) => !attachments.has(row.attachment_uuid)))
      return false;
    const added = new Set(preview.add.map((row) => row.uuid));
    if (
      added.size !== preview.add.length ||
      new Set(preview.remove.map((row) => row.uuid)).size !==
        preview.remove.length ||
      preview.remove.some((row) => added.has(row.uuid))
    )
      return false;
    return (
      preview.matches.reduce(
        (total, row) =>
          total + row.candidates.reduce((n, c) => n + c.proofs.length, 0),
        0,
      ) <= 8192
    );
  });
export const albumPublicationSchema = z
  .object({
    event_uuid: uuid,
    post_uuid: uuid,
    gallery_uuid: uuid.optional(),
    action,
    created: z.boolean(),
    selected: count.max(8192),
    review: count.max(8192),
    unavailable: count.max(8192),
    added: count.max(8192),
    removed: count.max(8192),
  })
  .refine(
    (p) =>
      p.created === (p.action === "create") &&
      p.selected + p.review + p.unavailable <= 8192 &&
      (!(p.created || p.added > 0 || p.removed > 0) || !!p.gallery_uuid) &&
      (p.action === "create" ||
        p.action === "sync" ||
        !(
          p.created ||
          p.selected ||
          p.review ||
          p.unavailable ||
          p.added ||
          p.removed
        )),
  );
export const albumJobSchema = z
  .object({
    job_uuid: uuid,
    sequence: revision,
    post_uuid: uuid,
    policy: albumPolicySchema,
    signature,
    state: z.enum(["queued", "running", "succeeded", "failed", "cancelled"]),
    revision,
    attempts: count,
    max_attempts: revision,
    available_at: timestamp,
    publication_committed: z.boolean(),
    publication: albumPublicationSchema.optional(),
    hooks_finished: z.boolean(),
    error_code: z.string().optional(),
    resume_from_job_uuid: uuid.optional(),
    created_at: timestamp,
    updated_at: timestamp,
  })
  .refine(
    (job) =>
      job.publication_committed === (job.publication !== undefined) &&
      job.hooks_finished === (job.state === "succeeded") &&
      (!job.hooks_finished || job.publication_committed) &&
      job.resume_from_job_uuid !== job.job_uuid &&
      (!job.publication ||
        (job.publication.post_uuid === job.post_uuid &&
          (!!job.resume_from_job_uuid ||
            job.publication.event_uuid === job.job_uuid))),
  );
export const albumSubmitSchema = z
  .object({ request_uuid: uuid, policy: albumPolicySchema, signature })
  .strict();
export const albumRetrySchema = z
  .object({ request_uuid: uuid, expected_revision: revision })
  .strict();
export const albumCancelSchema = z
  .object({ expected_revision: revision })
  .strict();
const attempt = z.object({
  job_uuid: uuid,
  fence: revision,
  owner_uuid: uuid,
  started_at: timestamp,
  ended_at: timestamp.optional(),
  outcome: z.enum([
    "running",
    "succeeded",
    "failed",
    "retry",
    "expired",
    "cancelled",
  ]),
  result: z.unknown(),
  error_code: z.string(),
});
export type AlbumPolicy = z.infer<typeof albumPolicySchema>;
export type AlbumPreview = z.infer<typeof albumPreviewSchema>;
export type AlbumMatch = z.infer<typeof match>;
export type AlbumJob = z.infer<typeof albumJobSchema>;
export type AlbumIdentity = z.infer<typeof albumIdentitySchema>;
export type AlbumAttempt = z.infer<typeof attempt>;

function invalid(): never {
  throw new NativeArchiveError(0, "invalid_response");
}
export function requireAlbumJobScope(
  job: AlbumJob,
  scope: Pick<AlbumJob, "post_uuid" | "policy" | "signature">,
) {
  if (
    job.post_uuid !== scope.post_uuid ||
    job.policy !== scope.policy ||
    job.signature !== scope.signature
  )
    invalid();
}
export function createAlbumReviewAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const posts = createSourcePostAPI(endpoint, transport);
  const pageLimit = 25;
  const postPath = (post: string) => `posts/${uuid.parse(post)}`;
  const jobPath = (job: string) => `album-backfills/${uuid.parse(job)}`;
  function page(after = 0) {
    return new URLSearchParams({
      limit: String(pageLimit),
      after: String(count.parse(after)),
    });
  }
  return {
    endpoint,
    pageLimit,
    async post(post: string, signal?: AbortSignal) {
      return (await posts.identity(post, signal)).canonical;
    },
    async preview(post: string, policy: AlbumPolicy, signal?: AbortSignal) {
      const result = await request(
        `${postPath(post)}/album-backfill/preview`,
        albumPreviewSchema,
        JSON.stringify({ policy: albumPolicySchema.parse(policy) }),
        signal,
      );
      if (result.post_uuid !== post || result.policy !== policy) invalid();
      return result;
    },
    async submitSaved(post: string, body: string) {
      const input = albumSubmitSchema.parse(JSON.parse(body));
      const result = await request(
        `${postPath(post)}/album-backfills`,
        albumJobSchema,
        body,
      );
      requireAlbumJobScope(result, { ...input, post_uuid: post });
      if (result.resume_from_job_uuid) invalid();
      return result;
    },
    async receipt(id: string) {
      try {
        return await request(
          `album-backfill-requests/${uuid.parse(id)}`,
          albumJobSchema,
        );
      } catch (error) {
        if (
          error instanceof NativeArchiveError &&
          error.status === 404 &&
          error.code === "not_found"
        )
          return null;
        throw error;
      }
    },
    async job(id: string, signal?: AbortSignal) {
      const result = await request(
        jobPath(id),
        albumJobSchema,
        undefined,
        signal,
      );
      if (result.job_uuid !== id) invalid();
      return result;
    },
    async history(post: string, after = 0, signal?: AbortSignal) {
      const rows = await request(
        `${postPath(post)}/album-backfills?${page(after)}`,
        z.array(albumJobSchema).max(pageLimit),
        undefined,
        signal,
      );
      let prior = after;
      for (const row of rows) {
        if (row.post_uuid !== post || row.sequence <= prior) invalid();
        prior = row.sequence;
      }
      return rows;
    },
    async attempts(job: string, after = 0, signal?: AbortSignal) {
      const rows = await request(
        `${jobPath(job)}/attempts?${page(after)}`,
        z.array(attempt).max(pageLimit),
        undefined,
        signal,
      );
      let prior = after;
      for (const row of rows) {
        if (row.job_uuid !== job || row.fence <= prior) invalid();
        prior = row.fence;
      }
      return rows;
    },
    async retrySaved(previous: AlbumJob, body: string) {
      const input = albumRetrySchema.parse(JSON.parse(body));
      if (input.expected_revision !== previous.revision)
        throw new NativeArchiveError(0, "invalid_saved_request");
      const result = await request(
        `${jobPath(previous.job_uuid)}/retry`,
        albumJobSchema,
        body,
      );
      requireAlbumJobScope(result, previous);
      if (result.resume_from_job_uuid !== previous.job_uuid) invalid();
      return result;
    },
    async cancelSaved(previous: AlbumJob, body: string) {
      const input = albumCancelSchema.parse(JSON.parse(body));
      if (input.expected_revision !== previous.revision)
        throw new NativeArchiveError(0, "invalid_saved_request");
      const result = await request(
        `${jobPath(previous.job_uuid)}/cancel`,
        albumJobSchema,
        body,
      );
      requireAlbumJobScope(result, previous);
      if (result.job_uuid !== previous.job_uuid || result.state !== "cancelled")
        invalid();
      return result;
    },
  };
}
export type AlbumReviewAPI = ReturnType<typeof createAlbumReviewAPI>;
