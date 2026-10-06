import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { NativeArchiveError } from "./client";
import { requestUUID, withReviewRecord } from "./review-storage";
import type {
  AssociationInput,
  AssociationRequest,
  AssociationPreview,
  AssociationReceipt,
  AssociationProtocol,
} from "./association-review-protocol";

const savedSchema = z
  .object({
    scope_uuid: uuid,
    body: z.string(),
    state: z.enum(["pending", "rejected"]),
  })
  .strict();
export type SavedAssociationReview = z.infer<typeof savedSchema>;

export function createAssociationReviewOutbox<
  Input extends AssociationInput,
  Apply extends Input & AssociationRequest,
  Preview extends AssociationPreview<Input>,
  Receipt extends AssociationReceipt<Apply>,
>(api: AssociationProtocol<Input, Apply, Preview, Receipt>) {
  function transaction<T>(
    scope: string,
    mode: IDBTransactionMode,
    action: (saved: SavedAssociationReview | null, store: IDBObjectStore) => T,
  ) {
    uuid.parse(scope);
    return withReviewRecord(
      `stash-association-review:v1:${api.family}:${api.endpoint}`,
      scope,
      mode,
      (value, store) => {
        if (value === undefined) return action(null, store);
        const saved = savedSchema.parse(value);
        const input = api.parseApply(JSON.parse(saved.body));
        if (
          saved.scope_uuid !== scope ||
          api.scope(input) !== scope ||
          new TextEncoder().encode(saved.body).length > 16384
        )
          throw new NativeArchiveError(0, "invalid_saved_request");
        return action(saved, store);
      },
    );
  }
  const read = (scope: string) =>
    transaction(scope, "readonly", (saved) => saved);
  async function prepare(scope: string, preview: Preview) {
    const checked = api.parsePreview(preview);
    if (api.scope(checked.input) !== scope)
      throw new NativeArchiveError(0, "scope_mismatch");
    const input = api.parseApply({
      ...api.normalizeInput(checked.input),
      request_uuid: requestUUID(),
      digest: checked.digest,
    });
    const saved: SavedAssociationReview = {
      scope_uuid: scope,
      body: JSON.stringify(input),
      state: "pending",
    };
    if (new TextEncoder().encode(saved.body).length > 16384)
      throw new NativeArchiveError(0, "request_too_large");
    return transaction(scope, "readwrite", (existing, store) => {
      if (existing) {
        const prior = api.parseApply(JSON.parse(existing.body));
        if (
          existing.state !== "pending" ||
          api.requestKey(input) !==
            api.requestKey({ ...prior, request_uuid: input.request_uuid })
        )
          throw new NativeArchiveError(0, "pending_review");
        return existing;
      }
      store.add(saved, scope);
      return saved;
    });
  }
  function update(
    scope: string,
    saved: SavedAssociationReview,
    state: "complete" | "rejected",
  ) {
    return transaction(scope, "readwrite", (current, store) => {
      if (current?.body !== saved.body) return;
      if (state === "complete") store.delete(scope);
      else store.put({ ...current, state }, scope);
    });
  }
  async function deliver(scope: string): Promise<Receipt> {
    const saved = await read(scope);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    if (saved.state === "rejected")
      throw new NativeArchiveError(409, "preview_changed");
    const input = api.parseApply(JSON.parse(saved.body));
    try {
      const receipt =
        (await api.receipt(input)) ?? (await api.applySaved(saved.body)).review;
      await update(scope, saved, "complete");
      return receipt;
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        error.code === "preview_changed"
      )
        await update(scope, saved, "rejected");
      throw error;
    }
  }
  async function forgetRejected(scope: string, request: string) {
    return transaction(scope, "readwrite", (saved, store) => {
      if (!saved) return;
      if (
        saved.state !== "rejected" ||
        api.parseApply(JSON.parse(saved.body)).request_uuid !== request
      )
        throw new NativeArchiveError(0, "pending_review");
      store.delete(scope);
    });
  }
  return { read, prepare, deliver, forgetRejected };
}
