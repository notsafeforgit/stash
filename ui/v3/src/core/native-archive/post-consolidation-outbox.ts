import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { NativeArchiveError } from "./client";
import { requestUUID, withReviewRecord } from "./review-storage";
import {
  postMergeApplySchema,
  postMergeInputSchema,
  postMergePreviewSchema,
  postMergeRequestKey,
  postMergeBytes,
  type PostMergeInput,
  type PostMergePreview,
} from "./post-consolidation-schema";
import {
  mergeNotificationRetrySchema,
  mergeNotificationSchema,
  type PostConsolidationAPI,
  type MergeNotification,
} from "./post-consolidation-api";

const savedSchema = z
  .object({
    post_uuid: uuid,
    body: z.string(),
    state: z.enum(["pending", "rejected"]),
  })
  .strict();
export type SavedPostMerge = z.infer<typeof savedSchema>;

export function createPostConsolidationOutbox(api: PostConsolidationAPI) {
  function transaction<T>(
    post: string,
    mode: IDBTransactionMode,
    action: (saved: SavedPostMerge | null, store: IDBObjectStore) => T,
  ) {
    uuid.parse(post);
    return withReviewRecord(
      `stash-post-consolidation:v1:${api.endpoint}`,
      post,
      mode,
      (value, store) => {
        if (value === undefined) return action(null, store);
        const saved = savedSchema.parse(value);
        const input = postMergeApplySchema.parse(JSON.parse(saved.body));
        if (
          saved.post_uuid !== post ||
          input.source_uuid !== post ||
          new TextEncoder().encode(saved.body).length > postMergeBytes
        )
          throw new NativeArchiveError(0, "invalid_saved_request");
        return action(saved, store);
      },
    );
  }
  const read = (post: string) =>
    transaction(post, "readonly", (value) => value);
  async function prepare(
    post: string,
    input: PostMergeInput,
    preview: PostMergePreview,
  ) {
    const checked = postMergePreviewSchema.parse(preview);
    const choice = postMergeInputSchema.parse(input);
    if (
      !checked.ready ||
      checked.source.uuid !== post ||
      choice.source_uuid !== post ||
      choice.destination_uuid !== checked.destination.uuid
    )
      throw new NativeArchiveError(0, "post_mismatch");
    const apply = postMergeApplySchema.parse({
      ...choice,
      digest: checked.digest,
      request_uuid: requestUUID(),
    });
    const saved: SavedPostMerge = {
      post_uuid: post,
      body: JSON.stringify(apply),
      state: "pending",
    };
    if (new TextEncoder().encode(saved.body).length > postMergeBytes)
      throw new NativeArchiveError(0, "request_too_large");
    return transaction(post, "readwrite", (current, store) => {
      if (current) {
        const prior = postMergeApplySchema.parse(JSON.parse(current.body));
        if (
          current.state !== "pending" ||
          postMergeRequestKey(apply) !==
            postMergeRequestKey({ ...prior, request_uuid: apply.request_uuid })
        )
          throw new NativeArchiveError(0, "pending_review");
        return current;
      }
      store.add(saved, post);
      return saved;
    });
  }
  function update(
    post: string,
    saved: SavedPostMerge,
    state: "complete" | "rejected",
  ) {
    return transaction(post, "readwrite", (current, store) => {
      if (current?.body !== saved.body) return;
      if (state === "complete") store.delete(post);
      else store.put({ ...current, state }, post);
    });
  }
  async function deliver(post: string) {
    const saved = await read(post);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state !== "pending")
      throw new NativeArchiveError(409, "preview_changed");
    try {
      const result =
        (await api.receipt(saved.body)) ??
        (await api.applySaved(saved.body)).review;
      await update(post, saved, "complete");
      return result;
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        error.code === "preview_changed"
      )
        await update(post, saved, "rejected");
      throw error;
    }
  }
  async function forgetRejected(post: string, request: string) {
    return transaction(post, "readwrite", (saved, store) => {
      if (!saved) return;
      if (
        saved.state !== "rejected" ||
        postMergeApplySchema.parse(JSON.parse(saved.body)).request_uuid !==
          request
      )
        throw new NativeArchiveError(0, "pending_review");
      store.delete(post);
    });
  }
  return { read, prepare, deliver, forgetRejected };
}

// Retrying a notification has its own saved request and never resends the merge.
export function createMergeNotificationOutbox(api: PostConsolidationAPI) {
  type Retry = z.infer<typeof mergeNotificationRetrySchema>;
  function transaction<T>(
    review: string,
    mode: IDBTransactionMode,
    action: (saved: Retry | null, store: IDBObjectStore) => T,
  ) {
    uuid.parse(review);
    return withReviewRecord(
      `stash-post-merge-notification:v1:${api.endpoint}`,
      review,
      mode,
      (value, store) => {
        const saved =
          value === undefined
            ? null
            : mergeNotificationRetrySchema.parse(value);
        if (saved && saved.review_uuid !== review)
          throw new NativeArchiveError(0, "invalid_saved_request");
        return action(saved, store);
      },
    );
  }
  const read = (review: string) =>
    transaction(review, "readonly", (saved) => saved);
  async function prepare(value: MergeNotification) {
    const job = mergeNotificationSchema.parse(value);
    if (job.state !== "failed" && job.state !== "cancelled")
      throw new NativeArchiveError(409, "notification_job_changed");
    const request: Retry = {
      request_uuid: requestUUID(),
      review_uuid: job.review_uuid,
      job_uuid: job.job_uuid,
      expected_revision: job.revision,
    };
    return transaction(job.review_uuid, "readwrite", (saved, store) => {
      if (saved) {
        if (
          saved.job_uuid !== request.job_uuid ||
          saved.expected_revision !== request.expected_revision
        )
          throw new NativeArchiveError(0, "pending_review");
        return saved;
      }
      store.add(request, job.review_uuid);
      return request;
    });
  }
  async function deliver(review: string) {
    const saved = await read(review);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    const job =
      (await api.retryReceipt(saved)) ?? (await api.retryNotification(saved));
    await transaction(review, "readwrite", (current, store) => {
      if (current?.request_uuid === saved.request_uuid) store.delete(review);
    });
    return job;
  }
  return { read, prepare, deliver };
}
