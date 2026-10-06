import { z } from "zod";
import { NativeArchiveError } from "./client";
import { requestUUID, withReviewRecord } from "./review-storage";
import {
  sourceAssociationSchema,
  sourceLinkInputSchema,
  sourceLinkKey,
  type SourceAssociation,
  type SourceLinkState,
  type SourceReviewAPI,
} from "./source-review-api";

const targetSchema = z
  .object({
    kind: z.enum(["scene", "image"]),
    localId: z.string().regex(/^[1-9]\d*$/),
  })
  .strict();
export type SourceReviewTarget = z.infer<typeof targetSchema>;
const savedSchema = z
  .object({
    target: targetSchema,
    body: z.string(),
    state: z.enum(["pending", "rejected"]),
  })
  .strict();
export type SavedSourceReview = z.infer<typeof savedSchema>;
function key(target: SourceReviewTarget) {
  const value = targetSchema.parse(target);
  return `${value.kind}:${value.localId}`;
}
function decode(
  value: unknown,
  target: SourceReviewTarget,
): SavedSourceReview | null {
  if (value === undefined) return null;
  const saved = savedSchema.parse(value);
  if (
    key(saved.target) !== key(target) ||
    new TextEncoder().encode(saved.body).length > 65536
  )
    throw new NativeArchiveError(0, "invalid_saved_request");
  sourceLinkInputSchema.parse(JSON.parse(saved.body));
  return saved;
}

/** One unresolved link change per item. Opening review only reads the journal;
 * explicit recovery checks the original receipt before sending identical bytes. */
export function createSourceReviewOutbox(api: SourceReviewAPI) {
  function transaction<T>(
    target: SourceReviewTarget,
    mode: IDBTransactionMode,
    action: (saved: SavedSourceReview | null, store: IDBObjectStore) => T,
  ) {
    return withReviewRecord(
      `stash-source-review:v1:${api.endpoint}`,
      key(target),
      mode,
      (value, store) => action(decode(value, target), store),
    );
  }
  function read(target: SourceReviewTarget) {
    return transaction(target, "readonly", (saved) => saved);
  }
  async function prepare(
    target: SourceReviewTarget,
    association: SourceAssociation,
    state: SourceLinkState,
    reason: string,
  ) {
    const a = sourceAssociationSchema.parse(association);
    if (a.post_state !== "active" || a.media_state !== "active")
      throw new NativeArchiveError(0, "source_unavailable");
    const input = sourceLinkInputSchema.parse({
      uuid: requestUUID(),
      post_uuid: a.post_uuid,
      media_uuid: a.media_uuid,
      expected_post_revision: a.post_revision,
      expected_media_revision: a.media_revision,
      expected_decisions: a.decisions.map((d) => d.uuid).sort(),
      state,
      origin: "review",
      reason,
    });
    const saved: SavedSourceReview = {
      target,
      body: sourceLinkKey(input),
      state: "pending",
    };
    decode(saved, target);
    return transaction(target, "readwrite", (existing, store) => {
      if (existing) {
        const prior = sourceLinkInputSchema.parse(JSON.parse(existing.body));
        if (
          existing.state !== "pending" ||
          sourceLinkKey(input) !== sourceLinkKey({ ...prior, uuid: input.uuid })
        )
          throw new NativeArchiveError(0, "pending_review");
        return existing;
      }
      store.add(saved, key(target));
      return saved;
    });
  }
  function update(
    target: SourceReviewTarget,
    saved: SavedSourceReview,
    state: "rejected" | "complete",
  ) {
    return transaction(target, "readwrite", (current, store) => {
      if (current?.body !== saved.body) return;
      if (state === "complete") store.delete(key(target));
      else store.put({ ...current, state }, key(target));
    });
  }
  async function deliver(target: SourceReviewTarget) {
    const saved = await read(target);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state === "rejected")
      throw new NativeArchiveError(409, "post_media_conflict");
    const input = sourceLinkInputSchema.parse(JSON.parse(saved.body));
    try {
      const result =
        (await api.receipt(input)) ?? (await api.applySaved(saved.body));
      await update(target, saved, "complete");
      return result;
    } catch (error) {
      // Only a rejected current-state guard proves this saved request did not
      // commit. UUID reuse, wrong receipts and transport errors remain pending.
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        error.code === "post_media_conflict"
      )
        await update(target, saved, "rejected");
      throw error;
    }
  }
  function forgetRejected(target: SourceReviewTarget, request: string) {
    return transaction(target, "readwrite", (saved, store) => {
      if (!saved) return;
      if (
        saved.state !== "rejected" ||
        sourceLinkInputSchema.parse(JSON.parse(saved.body)).uuid !== request
      )
        throw new NativeArchiveError(0, "pending_review");
      store.delete(key(target));
    });
  }
  return { read, prepare, deliver, forgetRejected };
}
