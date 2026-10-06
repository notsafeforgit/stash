import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import {
  albumCancelSchema,
  albumJobSchema,
  albumPolicySchema,
  albumPreviewSchema,
  albumRetrySchema,
  albumSubmitSchema,
  requireAlbumJobScope,
  type AlbumJob,
  type AlbumPreview,
  type AlbumReviewAPI,
} from "./album-review-api";
import { NativeArchiveError } from "./client";
import { requestUUID, withReviewRecord } from "./review-storage";

const recordSchema = z
  .object({
    request_uuid: uuid,
    post_uuid: uuid,
    operation: z.enum(["submit", "retry", "cancel"]),
    policy: albumPolicySchema,
    signature: z.string().regex(/^[0-9a-f]{64}$/),
    body: z.string(),
    previous: albumJobSchema.optional(),
    state: z.enum(["pending", "rejected", "admitted"]),
    job_uuid: uuid.optional(),
    rejection: z
      .enum(["album_preview_changed", "album_cancel_changed"])
      .optional(),
  })
  .strict();
export type SavedAlbumReview = z.infer<typeof recordSchema>;

function invalid(): never {
  throw new NativeArchiveError(0, "invalid_saved_request");
}
function decode(value: unknown, post: string): SavedAlbumReview | null {
  if (value === undefined) return null;
  const saved = recordSchema.parse(value);
  if (
    saved.post_uuid !== post ||
    new TextEncoder().encode(saved.body).length > 4096 ||
    (saved.state === "admitted") !== !!saved.job_uuid ||
    (saved.state === "rejected") !== !!saved.rejection
  )
    invalid();
  if (saved.operation === "submit") {
    const input = albumSubmitSchema.parse(JSON.parse(saved.body));
    if (
      saved.previous ||
      input.request_uuid !== saved.request_uuid ||
      input.policy !== saved.policy ||
      input.signature !== saved.signature
    )
      invalid();
  } else {
    const prior = saved.previous;
    if (!prior) invalid();
    requireAlbumJobScope(prior, saved);
    const input =
      saved.operation === "retry"
        ? albumRetrySchema.parse(JSON.parse(saved.body))
        : albumCancelSchema.parse(JSON.parse(saved.body));
    if (input.expected_revision !== prior.revision) invalid();
    if (saved.operation === "retry") {
      if (
        !("request_uuid" in input) ||
        input.request_uuid !== saved.request_uuid ||
        (prior.state !== "failed" && prior.state !== "cancelled")
      )
        invalid();
    } else if (prior.state !== "queued" && prior.state !== "running") invalid();
  }
  return saved;
}

function sameIntent(a: SavedAlbumReview, b: SavedAlbumReview) {
  const body = (value: SavedAlbumReview) =>
    value.operation === "cancel"
      ? value.body
      : JSON.stringify({
          ...JSON.parse(value.body),
          request_uuid: "same-intent",
        });
  return (
    a.operation === b.operation &&
    a.post_uuid === b.post_uuid &&
    a.policy === b.policy &&
    a.signature === b.signature &&
    a.previous?.job_uuid === b.previous?.job_uuid &&
    body(a) === body(b)
  );
}

/** Opening an album never submits work. Durable intent precedes every POST;
 * explicit recovery looks up the original receipt before replaying its bytes. */
export function createAlbumReviewOutbox(api: AlbumReviewAPI) {
  function transaction<T>(
    post: string,
    mode: IDBTransactionMode,
    action: (saved: SavedAlbumReview | null, store: IDBObjectStore) => T,
  ) {
    uuid.parse(post);
    return withReviewRecord(
      `stash-album-review:v1:${api.endpoint}`,
      post,
      mode,
      (value, store) => action(decode(value, post), store),
    );
  }
  const read = (post: string) =>
    transaction(post, "readonly", (saved) => saved);
  function save(record: SavedAlbumReview) {
    decode(record, record.post_uuid);
    return transaction(record.post_uuid, "readwrite", (existing, store) => {
      if (existing && existing.state !== "admitted") {
        if (existing.state !== "pending" || !sameIntent(existing, record))
          throw new NativeArchiveError(0, "pending_review");
        return existing;
      }
      store.put(record, record.post_uuid);
      return record;
    });
  }
  async function prepare(preview: AlbumPreview) {
    const p = albumPreviewSchema.parse(preview);
    if (p.action !== "create" && p.action !== "sync")
      throw new NativeArchiveError(0, "album_not_applicable");
    const request = requestUUID();
    return save({
      request_uuid: request,
      post_uuid: p.post_uuid,
      operation: "submit",
      policy: p.policy,
      signature: p.signature,
      body: JSON.stringify(
        albumSubmitSchema.parse({
          request_uuid: request,
          policy: p.policy,
          signature: p.signature,
        }),
      ),
      state: "pending",
    });
  }
  async function prepareJob(job: AlbumJob, operation: "retry" | "cancel") {
    const previous = albumJobSchema.parse(job),
      request = requestUUID();
    const body = JSON.stringify(
      operation === "retry"
        ? albumRetrySchema.parse({
            request_uuid: request,
            expected_revision: previous.revision,
          })
        : albumCancelSchema.parse({ expected_revision: previous.revision }),
    );
    return save({
      request_uuid: request,
      post_uuid: previous.post_uuid,
      operation,
      policy: previous.policy,
      signature: previous.signature,
      body,
      previous,
      state: "pending",
    });
  }
  function validateResult(saved: SavedAlbumReview, job: AlbumJob) {
    requireAlbumJobScope(job, saved);
    if (saved.state === "admitted" && saved.job_uuid !== job.job_uuid)
      throw new NativeArchiveError(0, "mismatched_receipt");
    if (saved.operation === "submit" && job.resume_from_job_uuid)
      throw new NativeArchiveError(0, "mismatched_receipt");
    const prior = saved.previous;
    if (prior) {
      if (
        saved.operation === "retry" &&
        (job.resume_from_job_uuid !== prior.job_uuid ||
          job.sequence <= prior.sequence)
      )
        throw new NativeArchiveError(0, "mismatched_receipt");
      if (
        saved.operation === "cancel" &&
        (job.job_uuid !== prior.job_uuid ||
          job.revision < prior.revision ||
          job.resume_from_job_uuid !== prior.resume_from_job_uuid)
      )
        throw new NativeArchiveError(0, "mismatched_receipt");
      if (
        prior.publication &&
        JSON.stringify(prior.publication) !== JSON.stringify(job.publication)
      )
        throw new NativeArchiveError(0, "mismatched_receipt");
    }
    return job;
  }
  function update(
    saved: SavedAlbumReview,
    job: AlbumJob | null,
    rejection?: SavedAlbumReview["rejection"],
  ) {
    return transaction(saved.post_uuid, "readwrite", (current, store) => {
      // A response from another tab cannot overwrite a later explicit action.
      if (current?.request_uuid !== saved.request_uuid) return;
      if (current.state === "admitted" && !job) return;
      store.put(
        {
          ...current,
          state: job ? "admitted" : "rejected",
          job_uuid: job?.job_uuid,
          rejection,
        },
        saved.post_uuid,
      );
    });
  }
  async function cancel(saved: SavedAlbumReview) {
    const prior = saved.previous ?? invalid();
    async function inspect() {
      const current = validateResult(saved, await api.job(prior.job_uuid));
      if (current.state === "cancelled") return current;
      if (
        current.revision !== prior.revision ||
        (current.state !== "queued" && current.state !== "running")
      )
        throw new NativeArchiveError(409, "album_cancel_changed");
      return null;
    }
    const existing = await inspect();
    if (existing) return existing;
    try {
      return validateResult(saved, await api.cancelSaved(prior, saved.body));
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        error.code === "album_job_changed"
      ) {
        const current = await inspect();
        if (current) return current;
      }
      throw error;
    }
  }
  async function deliver(post: string) {
    const saved = await read(post);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state === "rejected")
      throw new NativeArchiveError(
        409,
        saved.rejection ?? "invalid_saved_request",
      );
    if (saved.state === "admitted" && saved.job_uuid)
      return validateResult(saved, await api.job(saved.job_uuid));
    try {
      let result: AlbumJob;
      if (saved.operation === "cancel") result = await cancel(saved);
      else {
        const receipt = await api.receipt(saved.request_uuid);
        result =
          receipt ??
          (saved.operation === "submit"
            ? await api.submitSaved(post, saved.body)
            : saved.previous
              ? await api.retrySaved(saved.previous, saved.body)
              : invalid());
      }
      validateResult(saved, result);
      await update(saved, result);
      return result;
    } catch (error) {
      // Generic job conflicts can also mean UUID reuse. They are not proof of
      // rejection. Cancel is resolved by current state because it has no receipt.
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        (error.code === "album_preview_changed" ||
          error.code === "album_cancel_changed")
      )
        await update(saved, null, error.code);
      throw error;
    }
  }
  function forgetRejected(post: string, request: string) {
    return transaction(post, "readwrite", (saved, store) => {
      if (!saved) return;
      if (saved.request_uuid !== request || saved.state !== "rejected")
        throw new NativeArchiveError(0, "pending_review");
      store.delete(post);
    });
  }
  async function inspectAdmitted(
    record: SavedAlbumReview,
    signal?: AbortSignal,
  ) {
    const saved = decode(record, record.post_uuid);
    if (saved?.state !== "admitted" || !saved.job_uuid) invalid();
    return validateResult(saved, await api.job(saved.job_uuid, signal));
  }
  return {
    read,
    prepare,
    prepareRetry: (job: AlbumJob) => prepareJob(job, "retry"),
    prepareCancel: (job: AlbumJob) => prepareJob(job, "cancel"),
    deliver,
    forgetRejected,
    inspectAdmitted,
  };
}
