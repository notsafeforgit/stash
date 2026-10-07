import { z } from "zod";
import {
  accountUUIDSchema as uuid,
  ownershipReasonSchema as reason,
} from "./account-review-api";
import { createSourcePostAPI } from "./source-post-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const digest = z.string().regex(/^[0-9a-f]{64}$/);
const count = z.number().int().min(0).max(1000000);
export const selectionModeSchema = z.enum(["pinned", "automatic", "disabled"]);
const inputShape = z
  .object({
    post_uuid: uuid,
    post_revision: revision,
    mode: selectionModeSchema,
    capture_uuid: uuid.optional(),
    reason: reason.optional(),
  })
  .strict();
function validCapture(input: z.infer<typeof inputShape>) {
  return (input.mode === "disabled") === (input.capture_uuid === undefined);
}
export const selectionInputSchema = inputShape.refine(validCapture);
export const selectionApplySchema = inputShape
  .extend({ request_uuid: uuid, digest })
  .refine(validCapture);
const entrySchema = z.object({
  position: z.number().int().min(0).max(999999),
  attachment_uuid: uuid,
  reference: z.object({
    namespace: z.string().min(1).max(128),
    value: z.string().min(1).max(2048),
  }),
  media_kind: z.enum(["image", "video", "unknown"]),
});
export const selectionListSchema = z
  .object({
    decision_uuid: uuid.optional(),
    revision: revision.optional(),
    mode: selectionModeSchema,
    origin: z.enum(["review", "ingest", "migration"]),
    reason,
    capture_uuid: uuid.optional(),
    manifest_uuids: z.array(uuid).max(4099),
    complete: z.boolean(),
    declared_album: z.boolean(),
    expected_count: count.optional(),
    entries: z.array(entrySchema).max(4096),
  })
  .refine((list) => {
    if ((list.decision_uuid === undefined) !== (list.revision === undefined))
      return false;
    if (new Set(list.manifest_uuids).size !== list.manifest_uuids.length)
      return false;
    if (list.mode === "disabled")
      return (
        !list.capture_uuid &&
        list.manifest_uuids.length === 0 &&
        list.entries.length === 0 &&
        !list.complete &&
        !list.declared_album &&
        list.expected_count === undefined
      );
    if (
      !list.capture_uuid ||
      list.manifest_uuids.length === 0 ||
      (list.mode === "pinned" && list.manifest_uuids.length !== 1)
    )
      return false;
    if (list.complete && list.expected_count !== list.entries.length)
      return false;
    let previous = -1;
    for (const entry of list.entries) {
      if (
        entry.position <= previous ||
        (list.expected_count !== undefined &&
          entry.position >= list.expected_count)
      )
        return false;
      previous = entry.position;
    }
    return true;
  });
export const selectionPreviewSchema = z
  .object({
    input: selectionInputSchema,
    current: selectionListSchema.nullable(),
    proposed: selectionListSchema,
    changed: z.boolean(),
    digest,
  })
  .refine((preview) => {
    const { input, current, proposed } = preview;
    return (
      (!current ||
        (!!current.decision_uuid &&
          current.revision !== undefined &&
          current.revision <= input.post_revision)) &&
      proposed.decision_uuid === undefined &&
      proposed.revision === undefined &&
      proposed.origin === "review" &&
      proposed.mode === input.mode &&
      proposed.capture_uuid === input.capture_uuid &&
      proposed.reason === (input.reason ?? "")
    );
  });
export const selectionReceiptSchema = z.object({
  request_uuid: uuid,
  decision_uuid: uuid,
  request: selectionApplySchema,
  created_at: z.string().min(1),
});
export const selectionManifestSchema = z
  .object({
    uuid,
    capture_uuid: uuid,
    complete: z.boolean(),
    declared_album: z.boolean(),
    expected_count: count.optional(),
    entry_count: z.number().int().min(0).max(4096),
  })
  .refine(
    (item) =>
      (!item.complete || item.expected_count === item.entry_count) &&
      (item.expected_count === undefined ||
        item.entry_count <= item.expected_count),
  );
const decisionSchema = z
  .object({
    uuid,
    post_uuid: uuid,
    revision,
    mode: selectionModeSchema,
    origin: z.enum(["review", "ingest", "migration"]),
    reason,
    capture_uuid: uuid.nullable(),
    manifest_uuids: z.array(uuid).max(4099),
    created_at: z.string().min(1),
  })
  .refine(
    (item) =>
      new Set(item.manifest_uuids).size === item.manifest_uuids.length &&
      (item.mode === "disabled"
        ? item.capture_uuid === null && item.manifest_uuids.length === 0
        : item.capture_uuid !== null &&
          item.manifest_uuids.length > 0 &&
          (item.mode !== "pinned" || item.manifest_uuids.length === 1)),
  );

export type SelectionInput = z.infer<typeof selectionInputSchema>;
export type SelectionApply = z.infer<typeof selectionApplySchema>;
export type SelectionPreview = z.infer<typeof selectionPreviewSchema>;
export type SelectionReceipt = z.infer<typeof selectionReceiptSchema>;
export type SelectionManifest = z.infer<typeof selectionManifestSchema>;
export type SelectionList = z.infer<typeof selectionListSchema>;
export type SelectionDecision = z.infer<typeof decisionSchema>;
export function normalizeSelectionInput(input: SelectionInput): SelectionInput {
  const value = selectionInputSchema.parse(input);
  if (value.reason === "") delete value.reason;
  return value;
}
function inputKey(input: SelectionInput) {
  return JSON.stringify(normalizeSelectionInput(input));
}
export function selectionRequestKey(input: SelectionApply) {
  const { request_uuid, digest, ...choice } = selectionApplySchema.parse(input);
  return JSON.stringify([inputKey(choice), request_uuid, digest]);
}
function checkedReceipt(value: unknown, input: SelectionApply) {
  const receipt = selectionReceiptSchema.parse(value);
  if (
    receipt.request_uuid !== input.request_uuid ||
    selectionRequestKey(receipt.request) !== selectionRequestKey(input)
  )
    throw new NativeArchiveError(0, "receipt_mismatch");
  return receipt;
}

export function createAttachmentSelectionAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const posts = createSourcePostAPI(endpoint, transport);
  const pageLimit = 25;
  return {
    endpoint,
    pageLimit,
    async post(post: string, signal?: AbortSignal) {
      return (await posts.identity(post, signal)).canonical;
    },
    async manifests(post: string, after?: string, signal?: AbortSignal) {
      const query = new URLSearchParams({ limit: String(pageLimit) });
      if (after) query.set("after", uuid.parse(after));
      const values = await request(
        `posts/${uuid.parse(post)}/attachment-manifests?${query}`,
        z.array(selectionManifestSchema).max(pageLimit),
        undefined,
        signal,
      );
      let previous = after ?? "";
      for (const item of values) {
        if (item.uuid <= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = item.uuid;
      }
      return values;
    },
    async history(post: string, after = 0, signal?: AbortSignal) {
      z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(after);
      const values = await request(
        `posts/${uuid.parse(post)}/attachment-selection-history?after=${after}&limit=${pageLimit}`,
        z.array(decisionSchema).max(pageLimit),
        undefined,
        signal,
      );
      let previous = after;
      for (const item of values) {
        if (item.post_uuid !== post || item.revision <= previous)
          throw new NativeArchiveError(0, "invalid_response");
        previous = item.revision;
      }
      return values;
    },
    async preview(input: SelectionInput, signal?: AbortSignal) {
      const value = normalizeSelectionInput(input);
      const result = await request(
        "attachment-selection/preview",
        selectionPreviewSchema,
        JSON.stringify(value),
        signal,
      );
      if (inputKey(value) !== inputKey(result.input))
        throw new NativeArchiveError(0, "preview_mismatch");
      return result;
    },
    async receipt(input: SelectionApply, signal?: AbortSignal) {
      const value = selectionApplySchema.parse(input);
      try {
        return checkedReceipt(
          await request(
            `attachment-selection/requests/${value.request_uuid}`,
            selectionReceiptSchema,
            undefined,
            signal,
          ),
          value,
        );
      } catch (error) {
        if (
          error instanceof NativeArchiveError &&
          error.status === 404 &&
          error.code === "not_found"
        )
          return null;
        throw error;
      }
    },
    async applySaved(body: string, signal?: AbortSignal) {
      if (new TextEncoder().encode(body).length > 16384)
        throw new NativeArchiveError(0, "request_too_large");
      const input = selectionApplySchema.parse(JSON.parse(body));
      const result = await request(
        "attachment-selection/apply",
        z.object({ review: selectionReceiptSchema, replayed: z.boolean() }),
        body,
        signal,
      );
      return { ...result, review: checkedReceipt(result.review, input) };
    },
  };
}
export type AttachmentSelectionAPI = ReturnType<
  typeof createAttachmentSelectionAPI
>;
