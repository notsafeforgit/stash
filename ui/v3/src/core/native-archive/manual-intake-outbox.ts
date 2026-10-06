import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { NativeArchiveError } from "./client";
import {
  manualBatchLimit,
  manualCancelSchema,
  manualFilePreviewSchema,
  manualFileRequestSchema,
  manualFileStatusSchema,
  manualRetrySchema,
  requireManualStatus,
  type ManualFilePreview,
  type ManualFileStatus,
  type ManualIntakeAPI,
} from "./manual-intake-api";
import { requestUUID, withReviewRecord } from "./review-storage";

const itemSchema = z
  .object({
    preview: manualFilePreviewSchema,
    body: z.string().max(8192),
    status: manualFileStatusSchema.optional(),
    rejection: z.string().optional(),
    retry: z
      .object({ body: z.string(), prior: manualFileStatusSchema })
      .strict()
      .optional(),
    cancel: z
      .object({ token: uuid, body: z.string(), prior: manualFileStatusSchema })
      .strict()
      .optional(),
  })
  .strict();
const batchSchema = z
  .object({
    batch_uuid: uuid,
    collection_uuid: uuid,
    created_at: z.string().datetime(),
    items: z.array(itemSchema).min(1).max(manualBatchLimit),
  })
  .strict();
export type SavedManualBatch = z.infer<typeof batchSchema>;
export type SavedManualFile = SavedManualBatch["items"][number];
export function manualBatchSettled(batch: SavedManualBatch) {
  return batch.items.every(
    (item) =>
      !item.cancel &&
      (item.rejection ||
        (item.status &&
          item.status.state !== "queued" &&
          item.status.state !== "running")),
  );
}
function invalid(): never {
  throw new NativeArchiveError(0, "invalid_saved_request");
}
function decode(value: unknown, collection: string): SavedManualBatch | null {
  if (value === undefined) return null;
  const batch = batchSchema.parse(value);
  if (
    batch.collection_uuid !== collection ||
    new Set(batch.items.map((i) => i.preview.relative_path)).size !==
      batch.items.length
  )
    invalid();
  const requests = new Set<string>();
  for (const item of batch.items) {
    const input = manualFileRequestSchema.parse(JSON.parse(item.body));
    if (
      input.collection_uuid !== collection ||
      item.preview.collection_uuid !== collection ||
      input.relative_path !== item.preview.relative_path ||
      input.media_kind !== item.preview.media_kind ||
      input.signature !== item.preview.signature ||
      requests.has(input.request_uuid) ||
      (item.status && item.rejection)
    )
      invalid();
    requests.add(input.request_uuid);
    if (item.status) requireManualStatus(item.status, input);
    if (item.retry) {
      const action = manualRetrySchema.parse(JSON.parse(item.retry.body));
      const prior = item.retry.prior;
      requireManualStatus(prior, {
        ...input,
        request_uuid: prior.request_uuid,
      });
      if (
        action.request_uuid !== input.request_uuid ||
        action.expected_revision !== prior.revision ||
        action.request_uuid === prior.request_uuid ||
        !["failed", "cancelled"].includes(prior.state)
      )
        invalid();
      if (
        item.status &&
        (item.status.resume_from_job_uuid !== prior.job_uuid ||
          item.status.resume_from_revision !== prior.revision)
      )
        invalid();
    } else if (item.status?.resume_from_job_uuid) invalid();
    if (item.cancel) {
      requireManualStatus(item.cancel.prior, input);
      const action = manualCancelSchema.parse(JSON.parse(item.cancel.body));
      if (
        !item.status ||
        action.expected_revision !== item.cancel.prior.revision ||
        !["queued", "running"].includes(item.cancel.prior.state) ||
        item.status.job_uuid !== item.cancel.prior.job_uuid
      )
        invalid();
    }
  }
  return batch;
}

/** One active review per collection. Whole-batch intent is durable before any
 * admission; inspection never submits work. Completed server jobs outlive this
 * browser record, which can only be dismissed when every request is resolved. */
export function createManualIntakeOutbox(api: ManualIntakeAPI) {
  function transaction<T>(
    collection: string,
    mode: IDBTransactionMode,
    action: (saved: SavedManualBatch | null, store: IDBObjectStore) => T,
  ) {
    uuid.parse(collection);
    return withReviewRecord(
      `stash-manual-intake:v1:${api.endpoint}`,
      collection,
      mode,
      (value, store) => action(decode(value, collection), store),
    );
  }
  const read = (collection: string) =>
    transaction(collection, "readonly", (saved) => saved);
  function update(
    batch: SavedManualBatch,
    request: string,
    change: (item: SavedManualFile) => SavedManualFile,
  ) {
    return transaction(batch.collection_uuid, "readwrite", (current, store) => {
      if (!current || current.batch_uuid !== batch.batch_uuid)
        throw new NativeArchiveError(0, "pending_review");
      const next = {
        ...current,
        items: current.items.map((item) =>
          JSON.parse(item.body).request_uuid === request ? change(item) : item,
        ),
      };
      decode(next, batch.collection_uuid);
      store.put(next, batch.collection_uuid);
      return next;
    });
  }
  function accept(
    batch: SavedManualBatch,
    item: SavedManualFile,
    status: ManualFileStatus,
    clearCancel = "",
  ) {
    const input = manualFileRequestSchema.parse(JSON.parse(item.body));
    requireManualStatus(status, input);
    if (item.retry) {
      const prior = item.retry.prior;
      if (
        status.resume_from_job_uuid !== prior.job_uuid ||
        status.resume_from_revision !== prior.revision ||
        (prior.publication &&
          JSON.stringify(prior.publication) !==
            JSON.stringify(status.publication))
      )
        throw new NativeArchiveError(0, "mismatched_receipt");
    } else if (status.resume_from_job_uuid)
      throw new NativeArchiveError(0, "mismatched_receipt");
    return update(batch, input.request_uuid, (current) => {
      if (
        current.status &&
        (status.job_uuid !== current.status.job_uuid ||
          status.revision < current.status.revision)
      )
        throw new NativeArchiveError(0, "mismatched_receipt");
      if (
        current.status?.publication &&
        JSON.stringify(current.status.publication) !==
          JSON.stringify(status.publication)
      )
        throw new NativeArchiveError(0, "mismatched_receipt");
      return {
        ...current,
        status,
        rejection: undefined,
        cancel:
          clearCancel === current.cancel?.token ? undefined : current.cancel,
      };
    });
  }
  async function prepare(previews: ManualFilePreview[]) {
    const rows = z
      .array(manualFilePreviewSchema)
      .min(1)
      .max(manualBatchLimit)
      .parse(previews);
    const first = rows[0]!;
    if (
      rows.some(
        (p) =>
          p.collection_uuid !== first.collection_uuid ||
          p.collection_revision !== first.collection_revision ||
          p.root_uuid !== first.root_uuid ||
          p.root_revision !== first.root_revision ||
          p.policy_revision !== first.policy_revision,
      )
    )
      throw new NativeArchiveError(409, "intake_preview_changed");
    const batch: SavedManualBatch = {
      batch_uuid: requestUUID(),
      collection_uuid: first.collection_uuid,
      created_at: new Date().toISOString(),
      items: rows.map((preview) => ({
        preview,
        body: JSON.stringify(
          manualFileRequestSchema.parse({
            collection_uuid: preview.collection_uuid,
            relative_path: preview.relative_path,
            media_kind: preview.media_kind,
            signature: preview.signature,
            request_uuid: requestUUID(),
          }),
        ),
      })),
    };
    decode(batch, batch.collection_uuid);
    return transaction(batch.collection_uuid, "readwrite", (current, store) => {
      if (current) {
        if (
          JSON.stringify(current.items.map((i) => i.preview)) !==
          JSON.stringify(rows)
        )
          throw new NativeArchiveError(0, "pending_review");
        return current;
      }
      store.put(batch, batch.collection_uuid);
      return batch;
    });
  }
  async function inspect(
    batch: SavedManualBatch,
    item: SavedManualFile,
    signal?: AbortSignal,
  ) {
    const input = manualFileRequestSchema.parse(JSON.parse(item.body));
    const status = await api.receipt(input.request_uuid, signal);
    if (!status && item.status)
      throw new NativeArchiveError(0, "missing_receipt");
    if (status)
      await accept(
        batch,
        item,
        status,
        status.state === "cancelled" ? item.cancel?.token : undefined,
      );
    return status;
  }
  async function deliverFile(batch: SavedManualBatch, item: SavedManualFile) {
    if (item.rejection) return;
    if (item.cancel) return deliverCancel(batch, item);
    if (await inspect(batch, item)) return;
    try {
      await accept(
        batch,
        item,
        item.retry
          ? await api.retrySaved(item.retry.prior, item.retry.body)
          : await api.submitSaved(item.body),
      );
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        ((error.status === 409 && error.code === "intake_preview_changed") ||
          (error.status === 404 && error.code === "file_not_found"))
      )
        await update(batch, JSON.parse(item.body).request_uuid, (current) =>
          current.status ? current : { ...current, rejection: error.code },
        );
      throw error;
    }
  }
  async function deliverCancel(batch: SavedManualBatch, item: SavedManualFile) {
    const action = item.cancel ?? invalid();
    const current = await inspect(batch, item);
    if (!current) invalid();
    if (current.state === "cancelled") return;
    if (
      current.revision !== action.prior.revision ||
      !["queued", "running"].includes(current.state)
    ) {
      await accept(batch, item, current, action.token);
      throw new NativeArchiveError(409, "intake_cancel_changed");
    }
    try {
      await accept(
        batch,
        item,
        await api.cancelSaved(action.prior, action.body),
        action.token,
      );
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        error.status === 409 &&
        error.code === "intake_request_changed"
      ) {
        const latest = await inspect(batch, item);
        if (latest?.state === "cancelled") return;
        if (latest && latest.revision !== action.prior.revision)
          await accept(batch, item, latest, action.token);
      }
      throw error;
    }
  }
  async function visit(
    collection: string,
    send: boolean,
    signal?: AbortSignal,
  ) {
    const saved = await read(collection);
    if (!saved) throw new NativeArchiveError(0, "missing_saved_request");
    let failure: unknown;
    for (const item of saved.items) {
      if (signal?.aborted) throw signal.reason;
      try {
        if (send) await deliverFile(saved, item);
        else if (!item.rejection) await inspect(saved, item, signal);
      } catch (error) {
        failure ??= error;
      }
    }
    if (failure) throw failure;
    return read(collection);
  }
  async function cancel(collection: string, status: ManualFileStatus) {
    const prior = manualFileStatusSchema.parse(status);
    if (!["queued", "running"].includes(prior.state))
      throw new NativeArchiveError(409, "intake_cancel_changed");
    const saved = await read(collection);
    if (!saved) invalid();
    const prepared = await update(saved, prior.request_uuid, (item) => {
      if (
        !item.status ||
        item.status.revision !== prior.revision ||
        item.status.job_uuid !== prior.job_uuid
      )
        invalid();
      if (item.cancel) return item;
      return {
        ...item,
        cancel: {
          token: requestUUID(),
          body: JSON.stringify({ expected_revision: prior.revision }),
          prior,
        },
      };
    });
    const item =
      prepared.items.find(
        (i) => JSON.parse(i.body).request_uuid === prior.request_uuid,
      ) ?? invalid();
    await deliverCancel(prepared, item);
  }
  async function retry(collection: string, prior: ManualFileStatus) {
    manualFileStatusSchema.parse(prior);
    if (!["failed", "cancelled"].includes(prior.state))
      throw new NativeArchiveError(409, "intake_retry_changed");
    const saved = await read(collection);
    if (!saved) invalid();
    const request = requestUUID();
    const prepared = await update(saved, prior.request_uuid, (item) => {
      if (
        !item.status ||
        item.status.job_uuid !== prior.job_uuid ||
        item.status.revision !== prior.revision ||
        item.cancel
      )
        invalid();
      return {
        ...item,
        body: JSON.stringify(
          manualFileRequestSchema.parse({
            ...JSON.parse(item.body),
            request_uuid: request,
          }),
        ),
        status: undefined,
        rejection: undefined,
        retry: {
          prior,
          body: JSON.stringify({
            request_uuid: request,
            expected_revision: prior.revision,
          }),
        },
      };
    });
    const item =
      prepared.items.find(
        (row) => JSON.parse(row.body).request_uuid === request,
      ) ?? invalid();
    await deliverFile(prepared, item);
  }
  function dismiss(collection: string, batch: string) {
    return transaction(collection, "readwrite", (saved, store) => {
      if (!saved) return;
      if (saved.batch_uuid !== batch || !manualBatchSettled(saved))
        throw new NativeArchiveError(0, "pending_review");
      store.delete(collection);
    });
  }
  return {
    read,
    prepare,
    cancel,
    retry,
    dismiss,
    deliver: (collection: string) => visit(collection, true),
    inspect: (collection: string, signal?: AbortSignal) =>
      visit(collection, false, signal),
  };
}
export type ManualIntakeOutbox = ReturnType<typeof createManualIntakeOutbox>;
