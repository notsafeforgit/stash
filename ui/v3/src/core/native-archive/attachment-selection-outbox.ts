import { accountUUIDSchema as selectionUUIDSchema } from "./account-review-api";
import { z } from "zod";
import { NativeArchiveError } from "./client";
import { requestUUID, withReviewRecord } from "./review-storage";
import {
  normalizeSelectionInput,
  selectionApplySchema,
  selectionPreviewSchema,
  selectionRequestKey,
  type AttachmentSelectionAPI,
  type SelectionPreview,
  type SelectionReceipt,
} from "./attachment-selection-api";

const savedSchema = z
  .object({
    post_uuid: selectionUUIDSchema,
    body: z.string(),
    state: z.enum(["pending", "rejected"]),
  })
  .strict();
export type SavedSelectionReview = z.infer<typeof savedSchema>;

export function createAttachmentSelectionOutbox(api: AttachmentSelectionAPI) {
  function transaction<T>(
    post: string,
    mode: IDBTransactionMode,
    action: (saved: SavedSelectionReview | null, store: IDBObjectStore) => T,
  ) {
    selectionUUIDSchema.parse(post);
    return withReviewRecord(
      `stash-attachment-selection-review:v1:${api.endpoint}`,
      post,
      mode,
      (value, store) => {
        if (value === undefined) return action(null, store);
        const saved = savedSchema.parse(value);
        const input = selectionApplySchema.parse(JSON.parse(saved.body));
        if (
          saved.post_uuid !== post ||
          input.post_uuid !== post ||
          new TextEncoder().encode(saved.body).length > 16384
        )
          throw new NativeArchiveError(0, "invalid_saved_request");
        return action(saved, store);
      },
    );
  }
  const read = (post: string) =>
    transaction(post, "readonly", (saved) => saved);
  async function prepare(
    post: string,
    preview: SelectionPreview,
  ): Promise<SavedSelectionReview> {
    const checked = selectionPreviewSchema.parse(preview);
    if (checked.input.post_uuid !== post)
      throw new NativeArchiveError(0, "post_mismatch");
    const input = selectionApplySchema.parse({
      ...normalizeSelectionInput(checked.input),
      digest: checked.digest,
      request_uuid: requestUUID(),
    });
    const saved: SavedSelectionReview = {
      post_uuid: post,
      body: JSON.stringify(input),
      state: "pending",
    };
    if (new TextEncoder().encode(saved.body).length > 16384)
      throw new NativeArchiveError(0, "request_too_large");
    return transaction(post, "readwrite", (existing, store) => {
      if (existing) {
        const previous = selectionApplySchema.parse(JSON.parse(existing.body));
        if (
          existing.state !== "pending" ||
          selectionRequestKey(input) !==
            selectionRequestKey({
              ...previous,
              request_uuid: input.request_uuid,
            })
        )
          throw new NativeArchiveError(0, "pending_review");
        return existing;
      }
      store.add(saved, post);
      return saved;
    });
  }
  function update(
    post: string,
    saved: SavedSelectionReview,
    state: "complete" | "rejected",
  ) {
    return transaction(post, "readwrite", (current, store) => {
      if (current?.body !== saved.body) return;
      if (state === "complete") store.delete(post);
      else store.put({ ...current, state }, post);
    });
  }
  async function deliver(post: string): Promise<SelectionReceipt> {
    const saved = await read(post);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state === "rejected")
      throw new NativeArchiveError(409, "preview_changed");
    const input = selectionApplySchema.parse(JSON.parse(saved.body));
    try {
      const receipt =
        (await api.receipt(input)) ?? (await api.applySaved(saved.body)).review;
      await update(post, saved, "complete");
      return receipt;
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
        selectionApplySchema.parse(JSON.parse(saved.body)).request_uuid !==
          request
      )
        throw new NativeArchiveError(0, "pending_review");
      store.delete(post);
    });
  }
  return { read, prepare, deliver, forgetRejected };
}
