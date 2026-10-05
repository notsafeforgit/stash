import { z } from "zod";
import { withReviewRecord } from "./review-storage";
import { NativeArchiveError } from "./client";
import {
  policyInputSchema,
  type MetadataPolicyAPI,
  type PolicyInput,
} from "./metadata-policy-api";

const savedSchema = z
  .object({
    body: z.string().max(135168),
    state: z.enum(["pending", "rejected"]),
  })
  .strict();
export type SavedPolicy = z.infer<typeof savedSchema>;
export function createMetadataPolicyOutbox(api: MetadataPolicyAPI) {
  function transaction<T>(
    id: string,
    mode: IDBTransactionMode,
    action: (saved: SavedPolicy | null, store: IDBObjectStore) => T,
  ) {
    return withReviewRecord(
      `stash-metadata-policies:v1:${api.endpoint}`,
      id,
      mode,
      (value, store) => {
        if (value === undefined) return action(null, store);
        const saved = savedSchema.parse(value);
        if (
          policyInputSchema.parse(JSON.parse(saved.body)).collection_uuid !==
            id ||
          new TextEncoder().encode(saved.body).length > 135168
        )
          throw new NativeArchiveError(0, "invalid_saved_request");
        return action(saved, store);
      },
    );
  }
  const read = (id: string) => transaction(id, "readonly", (value) => value);
  async function prepare(input: PolicyInput) {
    const checked = policyInputSchema.parse(input);
    const body = JSON.stringify(checked);
    if (new TextEncoder().encode(body).length > 135168)
      throw new NativeArchiveError(0, "request_too_large");
    return transaction(
      checked.collection_uuid,
      "readwrite",
      (existing, store) => {
        if (existing) {
          if (existing.body !== body || existing.state !== "pending")
            throw new NativeArchiveError(0, "pending_review");
          return existing;
        }
        const saved: SavedPolicy = { body, state: "pending" };
        store.add(saved, checked.collection_uuid);
        return saved;
      },
    );
  }
  async function deliver(id: string) {
    const saved = await read(id);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state === "rejected")
      throw new NativeArchiveError(409, "preview_changed");
    const input = policyInputSchema.parse(JSON.parse(saved.body));
    try {
      let result = await api.recover(input);
      if (!result) {
        try {
          result = await api.save(saved.body);
        } catch (error) {
          if (!(error instanceof NativeArchiveError && error.status === 409))
            throw error;
          result = await api.recover(input);
          if (!result) throw error;
        }
      }
      await transaction(id, "readwrite", (current, store) => {
        if (current?.body === saved.body) store.delete(id);
      });
      return result;
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        ((error.status === 409 && error.code === "preview_changed") ||
          error.status === 400)
      )
        await transaction(id, "readwrite", (current, store) => {
          if (current?.body === saved.body)
            store.put({ ...current, state: "rejected" }, id);
        });
      throw error;
    }
  }
  async function forgetRejected(id: string, body: string) {
    return transaction(id, "readwrite", (saved, store) => {
      if (!saved) return;
      if (saved.state !== "rejected" || saved.body !== body)
        throw new NativeArchiveError(0, "pending_review");
      store.delete(id);
    });
  }
  return { read, prepare, deliver, forgetRejected };
}
