import { z } from "zod";
import { requestUUID, withReviewRecord } from "./review-storage";
import {
  editApplySchema,
  editPreviewSchema,
  editRequestKey,
  normalizeEditInput,
  NativeArchiveError,
  type EditPreview,
  type EditReceipt,
  type MetadataReviewAPI,
} from "./metadata-review-api";

const targetSchema = z
  .object({
    kind: z.enum(["scene", "image"]),
    localId: z.string().regex(/^[1-9]\d*$/),
  })
  .strict();
export type ReviewTarget = z.infer<typeof targetSchema>;
const savedSchema = z
  .object({
    target: targetSchema,
    body: z.string(),
    state: z.enum(["pending", "rejected"]),
  })
  .strict();
export type SavedReview = z.infer<typeof savedSchema>;
function key(target: ReviewTarget): string {
  const checked = targetSchema.parse(target);
  return `${checked.kind}:${checked.localId}`;
}

function decode(value: unknown, target: ReviewTarget): SavedReview | null {
  if (value === undefined) return null;
  const saved = savedSchema.parse(value);
  if (
    key(saved.target) !== key(target) ||
    new TextEncoder().encode(saved.body).length > 262144
  )
    throw new NativeArchiveError(0, "invalid_saved_request");
  editApplySchema.parse(JSON.parse(saved.body));
  return saved;
}

/** One outstanding choice per library entity, isolated by public deployment.
 * IndexedDB serializes competing tabs and reports transaction completion before
 * any request is sent. Opening the panel never sends a mutation. */
export function createMetadataReviewOutbox(api: MetadataReviewAPI) {
  function transaction<T>(
    target: ReviewTarget,
    mode: IDBTransactionMode,
    action: (saved: SavedReview | null, store: IDBObjectStore) => T,
  ): Promise<T> {
    return withReviewRecord(
      `stash-metadata-review:v1:${api.endpoint}`,
      key(target),
      mode,
      (value, store) => action(decode(value, target), store),
    );
  }

  function read(target: ReviewTarget) {
    return transaction(target, "readonly", (saved) => saved);
  }

  async function prepare(
    target: ReviewTarget,
    preview: EditPreview,
    keepCurrent = false,
  ): Promise<SavedReview> {
    const checked = editPreviewSchema.parse(preview);
    if (!keepCurrent && checked.status !== "ready")
      throw new NativeArchiveError(0, "preview_not_ready");
    const input = editApplySchema.parse({
      ...normalizeEditInput(checked.input),
      digest: checked.digest,
      request_uuid: requestUUID(),
      ...(keepCurrent ? { keep_current: true } : {}),
    });
    const saved: SavedReview = {
      target,
      body: JSON.stringify(input),
      state: "pending",
    };
    decode(saved, target);
    return transaction(target, "readwrite", (existing, store) => {
      if (existing) {
        const previous = editApplySchema.parse(JSON.parse(existing.body));
        if (
          existing.state !== "pending" ||
          editRequestKey(input) !==
            editRequestKey({ ...previous, request_uuid: input.request_uuid })
        )
          throw new NativeArchiveError(0, "pending_review");
        return existing;
      }
      store.add(saved, key(target));
      return saved;
    });
  }

  async function update(
    target: ReviewTarget,
    saved: SavedReview,
    state: "rejected" | "complete",
  ) {
    return transaction(target, "readwrite", (current, store) => {
      // A late result must never remove another tab's subsequent choice.
      if (current?.body !== saved.body) return;
      if (state === "complete") store.delete(key(target));
      else store.put({ ...current, state }, key(target));
    });
  }

  async function deliver(target: ReviewTarget): Promise<EditReceipt> {
    const saved = await read(target);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state === "rejected")
      throw new NativeArchiveError(409, "preview_changed");
    const input = editApplySchema.parse(JSON.parse(saved.body));
    try {
      // Receipt inspection first also recovers after adoption or file deletion.
      const receipt =
        (await api.receipt(input)) ?? (await api.applySaved(saved.body)).review;
      await update(target, saved, "complete");
      return receipt;
    } catch (error) {
      // This response is definitive: Apply checks a committed request before
      // checking its preview. Network failures and mismatched receipts remain
      // pending and cannot be discarded as though they had never committed.
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        error.code === "preview_changed"
      )
        await update(target, saved, "rejected");
      throw error;
    }
  }

  async function forgetRejected(target: ReviewTarget, requestUUID: string) {
    return transaction(target, "readwrite", (saved, store) => {
      if (!saved) return;
      if (
        saved.state !== "rejected" ||
        editApplySchema.parse(JSON.parse(saved.body)).request_uuid !==
          requestUUID
      )
        throw new NativeArchiveError(0, "pending_review");
      store.delete(key(target));
    });
  }

  return { read, prepare, deliver, forgetRejected };
}
