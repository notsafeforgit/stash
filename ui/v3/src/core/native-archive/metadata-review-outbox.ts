import { z } from "zod";
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
const storeName = "requests";

// getRandomValues remains available for self-hosted Stash over LAN HTTP, where
// randomUUID may be unavailable. Keep the same cryptographically random v4 ID.
function requestUUID(): string {
  const hex = Array.from(
    crypto.getRandomValues(new Uint8Array(16)),
    (byte, index) => {
      const value =
        index === 6 ? (byte & 15) | 64 : index === 8 ? (byte & 63) | 128 : byte;
      return value.toString(16).padStart(2, "0");
    },
  ).join("");
  return [
    hex.slice(0, 8),
    hex.slice(8, 12),
    hex.slice(12, 16),
    hex.slice(16, 20),
    hex.slice(20),
  ].join("-");
}

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
  async function transaction<T>(
    target: ReviewTarget,
    mode: IDBTransactionMode,
    action: (saved: SavedReview | null, store: IDBObjectStore) => T,
  ): Promise<T> {
    const recordKey = key(target);
    const database = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(
        `stash-metadata-review:v1:${api.endpoint}`,
        1,
      );
      request.onupgradeneeded = () =>
        request.result.createObjectStore(storeName);
      request.onerror = () => reject(request.error);
      request.onblocked = () =>
        reject(new NativeArchiveError(0, "storage_blocked"));
      request.onsuccess = () => resolve(request.result);
    });
    try {
      return await new Promise<T>((resolve, reject) => {
        const tx = database.transaction(storeName, mode, {
          durability: "strict",
        });
        let result: T;
        let failure: unknown;
        const store = tx.objectStore(storeName);
        const request = store.get(recordKey);
        request.onsuccess = () => {
          try {
            result = action(decode(request.result, target), store);
          } catch (error) {
            failure = error;
            tx.abort();
          }
        };
        tx.oncomplete = () => resolve(result);
        tx.onabort = () =>
          reject(
            failure ?? tx.error ?? new NativeArchiveError(0, "storage_aborted"),
          );
        tx.onerror = () => reject(tx.error);
      });
    } finally {
      database.close();
    }
  }

  function read(target: ReviewTarget) {
    return transaction(target, "readonly", (saved) => saved);
  }

  async function prepare(
    target: ReviewTarget,
    preview: EditPreview,
  ): Promise<SavedReview> {
    const checked = editPreviewSchema.parse(preview);
    if (checked.status !== "ready")
      throw new NativeArchiveError(0, "preview_not_ready");
    const input = editApplySchema.parse({
      ...normalizeEditInput(checked.input),
      digest: checked.digest,
      request_uuid: requestUUID(),
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
