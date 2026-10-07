import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import {
  postMergeInputSchema,
  postMergeApplySchema,
  postMergePreviewSchema,
  postMergeReceiptSchema,
  postMergeRecordSchema,
  postMergeRequestKey,
  postMergeBytes,
  type PostMergeInput,
  type PostMergeApply,
  type PostMergeReceipt,
} from "./post-consolidation-schema";

function savedInput(body: string) {
  if (new TextEncoder().encode(body).length > postMergeBytes)
    throw new NativeArchiveError(0, "request_too_large");
  return postMergeApplySchema.parse(JSON.parse(body));
}
function checkedReceipt(
  value: unknown,
  input: PostMergeApply,
): PostMergeReceipt {
  const receipt = postMergeReceiptSchema.parse(value);
  if (postMergeRequestKey(receipt.request) !== postMergeRequestKey(input))
    throw new NativeArchiveError(0, "receipt_mismatch");
  return receipt;
}
const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
export const mergeNotificationSchema = z
  .object({
    sequence: revision,
    job_uuid: uuid,
    review_uuid: uuid,
    state: z.enum(["queued", "running", "succeeded", "failed", "cancelled"]),
    revision,
    resume_from_job_uuid: z.union([z.literal(""), uuid]),
    resume_from_job_revision: revision.or(z.literal(0)),
    hooks_finished: z.boolean(),
    error_code: z.string().max(128),
    created_at: z.string().datetime({ offset: true }),
    updated_at: z.string().datetime({ offset: true }),
  })
  .refine(
    (value) =>
      value.hooks_finished === (value.state === "succeeded") &&
      (value.resume_from_job_uuid === "") ===
        (value.resume_from_job_revision === 0),
  );
export type MergeNotification = z.infer<typeof mergeNotificationSchema>;
export const mergeNotificationRetrySchema = z
  .object({
    request_uuid: uuid,
    review_uuid: uuid,
    job_uuid: uuid,
    expected_revision: revision,
  })
  .strict();
export type MergeNotificationRetry = z.infer<
  typeof mergeNotificationRetrySchema
>;

export function createPostConsolidationAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = globalThis.fetch.bind(globalThis),
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  function retryMatches(job: MergeNotification, input: MergeNotificationRetry) {
    if (
      job.review_uuid !== input.review_uuid ||
      job.resume_from_job_uuid !== input.job_uuid ||
      job.resume_from_job_revision !== input.expected_revision
    )
      throw new NativeArchiveError(0, "receipt_mismatch");
    return job;
  }
  return {
    endpoint,
    pageLimit,
    async preview(input: PostMergeInput, signal?: AbortSignal) {
      const valid = postMergeInputSchema.parse(input);
      const preview = await request(
        "post-consolidation/preview",
        postMergePreviewSchema,
        JSON.stringify(valid),
        signal,
      );
      if (
        preview.source.uuid !== valid.source_uuid ||
        preview.destination.uuid !== valid.destination_uuid
      )
        throw new NativeArchiveError(0, "preview_mismatch");
      return { input: valid, preview };
    },
    async receipt(body: string, signal?: AbortSignal) {
      const input = savedInput(body);
      try {
        return checkedReceipt(
          await request(
            `post-consolidation/requests/${input.request_uuid}/check`,
            postMergeReceiptSchema,
            body,
            signal,
          ),
          input,
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
    async applySaved(body: string, signal?: AbortSignal) {
      const input = savedInput(body);
      const result = await request(
        "post-consolidation/apply",
        z.object({ review: postMergeReceiptSchema, replayed: z.boolean() }),
        body,
        signal,
      );
      return { ...result, review: checkedReceipt(result.review, input) };
    },
    async review(id: string, signal?: AbortSignal) {
      const receipt = await request(
        `post-consolidation/requests/${uuid.parse(id)}`,
        postMergeReceiptSchema,
        undefined,
        signal,
      );
      if (receipt.request.request_uuid !== id)
        throw new NativeArchiveError(0, "receipt_mismatch");
      return receipt;
    },
    async history(id: string, after = 0, signal?: AbortSignal) {
      z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(after);
      const rows = await request(
        `posts/${uuid.parse(id)}/consolidation-history?after=${after}&limit=${pageLimit}`,
        z.array(postMergeRecordSchema).max(pageLimit),
        undefined,
        signal,
      );
      let prior = after;
      for (const row of rows) {
        if (
          row.sequence <= prior ||
          (row.source_uuid !== id && row.destination_uuid !== id)
        )
          throw new NativeArchiveError(0, "invalid_response");
        prior = row.sequence;
      }
      return rows;
    },
    async notification(id: string, review: string, signal?: AbortSignal) {
      uuid.parse(review);
      const job = await request(
        `post-merge-notifications/${uuid.parse(id)}`,
        mergeNotificationSchema,
        undefined,
        signal,
      );
      if (job.job_uuid !== id || job.review_uuid !== review)
        throw new NativeArchiveError(0, "receipt_mismatch");
      return job;
    },
    async notifications(review: string, before = 0, signal?: AbortSignal) {
      z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(before);
      const rows = await request(
        `post-consolidation/requests/${uuid.parse(review)}/notifications?before=${before}&limit=${pageLimit}`,
        z.array(mergeNotificationSchema).max(pageLimit),
        undefined,
        signal,
      );
      let previous = before || Number.MAX_SAFE_INTEGER;
      for (const row of rows) {
        if (row.review_uuid !== review || row.sequence >= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = row.sequence;
      }
      return rows;
    },
    async cancelNotification(job: MergeNotification, signal?: AbortSignal) {
      const input = mergeNotificationSchema.parse(job);
      const result = await request(
        `post-merge-notifications/${input.job_uuid}/cancel`,
        mergeNotificationSchema,
        JSON.stringify({ expected_revision: input.revision }),
        signal,
      );
      if (
        result.job_uuid !== input.job_uuid ||
        result.review_uuid !== input.review_uuid ||
        result.state !== "cancelled"
      )
        throw new NativeArchiveError(0, "receipt_mismatch");
      return result;
    },
    async retryReceipt(input: MergeNotificationRetry, signal?: AbortSignal) {
      const valid = mergeNotificationRetrySchema.parse(input);
      try {
        return retryMatches(
          await request(
            `post-merge-notification-requests/${valid.request_uuid}`,
            mergeNotificationSchema,
            undefined,
            signal,
          ),
          valid,
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
    async retryNotification(
      input: MergeNotificationRetry,
      signal?: AbortSignal,
    ) {
      const valid = mergeNotificationRetrySchema.parse(input);
      return retryMatches(
        await request(
          `post-merge-notifications/${valid.job_uuid}/retry`,
          mergeNotificationSchema,
          JSON.stringify({
            request_uuid: valid.request_uuid,
            expected_revision: valid.expected_revision,
          }),
          signal,
        ),
        valid,
      );
    },
  };
}
export type PostConsolidationAPI = ReturnType<
  typeof createPostConsolidationAPI
>;
