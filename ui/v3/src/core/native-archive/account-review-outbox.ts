import { z } from "zod";
import { NativeArchiveError } from "./client";
import { requestUUID, withReviewRecord } from "./review-storage";
import {
  accountUUIDSchema,
  normalizeOwnershipInput,
  ownershipApplySchema,
  ownershipPreviewSchema,
  ownershipRequestKey,
  type AccountReviewAPI,
  type OwnershipPreview,
  type OwnershipReceipt,
} from "./account-review-api";

const savedSchema = z
  .object({
    account_uuid: accountUUIDSchema,
    body: z.string(),
    state: z.enum(["pending", "rejected"]),
  })
  .strict();
export type SavedOwnershipReview = z.infer<typeof savedSchema>;

export function createAccountReviewOutbox(api: AccountReviewAPI) {
  function transaction<T>(
    account: string,
    mode: IDBTransactionMode,
    action: (saved: SavedOwnershipReview | null, store: IDBObjectStore) => T,
  ) {
    accountUUIDSchema.parse(account);
    return withReviewRecord(
      `stash-account-review:v1:${api.endpoint}`,
      account,
      mode,
      (value, store) => {
        if (value === undefined) return action(null, store);
        const saved = savedSchema.parse(value);
        const input = ownershipApplySchema.parse(JSON.parse(saved.body));
        if (
          saved.account_uuid !== account ||
          input.account_uuid !== account ||
          new TextEncoder().encode(saved.body).length > 16384
        )
          throw new NativeArchiveError(0, "invalid_saved_request");
        return action(saved, store);
      },
    );
  }
  const read = (account: string) =>
    transaction(account, "readonly", (saved) => saved);
  async function prepare(
    account: string,
    preview: OwnershipPreview,
  ): Promise<SavedOwnershipReview> {
    const checked = ownershipPreviewSchema.parse(preview);
    if (
      checked.input.account_uuid !== account ||
      checked.account.uuid !== account
    )
      throw new NativeArchiveError(0, "account_mismatch");
    const input = ownershipApplySchema.parse({
      ...normalizeOwnershipInput(checked.input),
      digest: checked.digest,
      request_uuid: requestUUID(),
    });
    const saved: SavedOwnershipReview = {
      account_uuid: account,
      body: JSON.stringify(input),
      state: "pending",
    };
    if (new TextEncoder().encode(saved.body).length > 16384)
      throw new NativeArchiveError(0, "request_too_large");
    return transaction(account, "readwrite", (existing, store) => {
      if (existing) {
        const previous = ownershipApplySchema.parse(JSON.parse(existing.body));
        if (
          existing.state !== "pending" ||
          ownershipRequestKey(input) !==
            ownershipRequestKey({
              ...previous,
              request_uuid: input.request_uuid,
            })
        )
          throw new NativeArchiveError(0, "pending_review");
        return existing;
      }
      store.add(saved, account);
      return saved;
    });
  }
  function update(
    account: string,
    saved: SavedOwnershipReview,
    state: "complete" | "rejected",
  ) {
    return transaction(account, "readwrite", (current, store) => {
      if (current?.body !== saved.body) return;
      if (state === "complete") store.delete(account);
      else store.put({ ...current, state }, account);
    });
  }
  async function deliver(account: string): Promise<OwnershipReceipt> {
    const saved = await read(account);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state === "rejected")
      throw new NativeArchiveError(409, "preview_changed");
    const input = ownershipApplySchema.parse(JSON.parse(saved.body));
    try {
      const receipt =
        (await api.receipt(input)) ?? (await api.applySaved(saved.body)).review;
      await update(account, saved, "complete");
      return receipt;
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        error.code === "preview_changed"
      )
        await update(account, saved, "rejected");
      throw error;
    }
  }
  async function forgetRejected(account: string, request: string) {
    return transaction(account, "readwrite", (saved, store) => {
      if (!saved) return;
      if (
        saved.state !== "rejected" ||
        ownershipApplySchema.parse(JSON.parse(saved.body)).request_uuid !==
          request
      )
        throw new NativeArchiveError(0, "pending_review");
      store.delete(account);
    });
  }
  return { read, prepare, deliver, forgetRejected };
}
