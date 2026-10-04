import { z } from "zod";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
export { NativeArchiveError } from "./client";

const uuid = z.string().regex(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/);
const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const digest = z.string().regex(/^[0-9a-f]{64}$/);
const kind = z.enum([
  "scene",
  "image",
  "gallery",
  "performer",
  "studio",
  "tag",
  "group",
  "file",
]);
export const identitySchema = z.object({
  uuid,
  kind,
  revision,
  local_id: revision.optional(),
});
export const nameSelectionSchema = z.object({ uuid, revision }).strict();
export const editInputSchema = z
  .object({
    entity_uuid: uuid,
    history_uuid: uuid,
    source_field: z.string().min(1).max(128),
    match_uuid: uuid,
    selections: z
      .record(z.string().min(1).max(1024), nameSelectionSchema)
      .refine((value) => Object.keys(value).length <= 128)
      .optional(),
  })
  .strict();
export const editApplySchema = editInputSchema
  .extend({ request_uuid: uuid, digest })
  .strict();
export const editReceiptSchema = z.object({
  request_uuid: uuid,
  decision_uuid: uuid,
  field: z.string().min(1),
  request: editApplySchema,
  created_at: z.string(),
});
const nameCandidateSchema = z.object({
  uuid,
  local_id: revision,
  revision,
  name: z.string(),
  disambiguation: z.string().optional(),
});
export const editPreviewSchema = z.object({
  input: editInputSchema,
  entity_revision: revision,
  field: z.string(),
  current_value: z.unknown(),
  current_mode: z.string(),
  current_origin: z.string(),
  current_decision_uuid: uuid.optional(),
  protected: z.boolean(),
  mode: z.string(),
  value: z.unknown().optional(),
  file_uuid: uuid,
  generation: revision,
  archive_file_uuid: uuid.optional(),
  archive_generation: revision.optional(),
  reference_revisions: z.record(uuid, revision).optional(),
  names: z
    .array(
      z.object({
        name: z.string(),
        candidates: z.array(nameCandidateSchema).max(100),
        more: z.boolean(),
        selected: nameCandidateSchema.optional(),
      }),
    )
    .max(128)
    .optional(),
  status: z.enum(["ready", "unresolved_names", "unsupported"]),
  digest,
});
const decisionProvenanceSchema = z.object({
  uuid,
  sequence: revision,
  capture_uuid: uuid.optional(),
  reason: z.string(),
  created_at: z.string(),
  file_edit: editReceiptSchema.optional(),
  // Policy provenance is retained, but is not a request input or plugin config.
  policy: z.unknown().optional(),
});
export const fieldDefinitionSchema = z.object({
  name: z.string(),
  type: z.enum([
    "string",
    "date",
    "integer",
    "boolean",
    "urls",
    "custom_fields",
    "reference",
    "references",
    "groups",
  ]),
  clear_value: z.unknown(),
  reference_kind: kind.optional(),
});
export const metadataFieldsSchema = z.object({
  entity: identitySchema,
  fields: z.array(
    z.object({
      definition: fieldDefinitionSchema,
      value: z.unknown(),
      mode: z.string(),
      origin: z.string(),
      protected: z.boolean(),
      decision: decisionProvenanceSchema.optional(),
      references: z.array(identitySchema).optional(),
    }),
  ),
});
export const decisionSchema = decisionProvenanceSchema.extend({
  entity_uuid: uuid,
  field: z.string(),
  mode: z.string(),
  origin: z.string(),
  value: z.unknown(),
});
export const editCandidateSchema = z.object({
  history_uuid: uuid,
  match_uuid: uuid,
  collection_uuid: uuid,
  source_time: z.string(),
  relative_path: z.string(),
  file_uuid: uuid,
});
export const fileHistorySchema = z.object({
  uuid,
  kind: z.literal("metadata_edit"),
  collection_uuid: uuid,
  source_time: z.string(),
  edits: z.array(
    z.object({
      field: z.string(),
      target_field: z.string(),
      value_type: z.string(),
      mode: z.string(),
      value: z.unknown(),
    }),
  ),
});
export type NativeIdentity = z.infer<typeof identitySchema>;
export type EditInput = z.infer<typeof editInputSchema>;
export type EditApply = z.infer<typeof editApplySchema>;
export type EditReceipt = z.infer<typeof editReceiptSchema>;
export type EditPreview = z.infer<typeof editPreviewSchema>;
export type EditCandidate = z.infer<typeof editCandidateSchema>;
export type MetadataFields = z.infer<typeof metadataFieldsSchema>;
export type FieldDecision = z.infer<typeof decisionSchema>;
export type FileHistory = z.infer<typeof fileHistorySchema>;
export type NameCandidate = z.infer<typeof nameCandidateSchema>;

export const metadataReviewEndpoint = nativeArchiveEndpoint;

export function normalizeEditInput(input: EditInput): EditInput {
  const result = editInputSchema.parse(input);
  if (result.selections && Object.keys(result.selections).length === 0)
    delete result.selections;
  return result;
}

/** Equality independent of a JSON object's ordering. Requests contain only
 * strings and safe revision integers; retained arbitrary source values are
 * never reserialized into an Apply body. */
export function editRequestKey(input: EditApply): string {
  const valid = editApplySchema.parse(input);
  const { request_uuid, digest, ...choice } = valid;
  return JSON.stringify([editInputKey(choice), request_uuid, digest]);
}

function editInputKey(input: EditInput): string {
  const valid = normalizeEditInput(input);
  return JSON.stringify({
    ...valid,
    selections: Object.entries(valid.selections ?? {})
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      .map(([name, target]) => [name, target.uuid, target.revision]),
  });
}

export function validateEditReceipt(
  value: unknown,
  input: EditApply,
): EditReceipt {
  const receipt = editReceiptSchema.parse(value);
  if (
    receipt.request_uuid !== input.request_uuid ||
    editRequestKey(receipt.request) !== editRequestKey(input)
  )
    throw new NativeArchiveError(0, "receipt_mismatch");
  return receipt;
}

/** Application session transport. Same-origin requests preserve the public
 * proxy mount, including in Vite; producer tokens never belong in this client. */
export function createMetadataReviewAPI(
  endpoint = metadataReviewEndpoint(),
  transport: typeof fetch = globalThis.fetch.bind(globalThis),
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  return {
    endpoint,
    pageLimit,
    identity(
      entityKind: NativeIdentity["kind"],
      localId: string,
      signal?: AbortSignal,
    ) {
      kind.parse(entityKind);
      if (!/^[1-9]\d*$/.test(localId))
        throw new NativeArchiveError(0, "invalid_identity");
      return request(
        `entity-identities/${entityKind}/${localId}`,
        identitySchema,
        undefined,
        signal,
      );
    },
    fields(entity: string, signal?: AbortSignal) {
      return request(
        `entities/${uuid.parse(entity)}/metadata-fields`,
        metadataFieldsSchema,
        undefined,
        signal,
      );
    },
    decisions(entity: string, field: string, after = 0, signal?: AbortSignal) {
      z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(after);
      return request(
        `entities/${uuid.parse(entity)}/metadata-fields/${encodeURIComponent(field)}/history?after=${after}&limit=${pageLimit}`,
        z.array(decisionSchema).max(pageLimit),
        undefined,
        signal,
      );
    },
    edits(entity: string, after?: EditCandidate, signal?: AbortSignal) {
      const query = new URLSearchParams({ limit: String(pageLimit) });
      if (after) {
        query.set("after_history", uuid.parse(after.history_uuid));
        query.set("after_match", uuid.parse(after.match_uuid));
      }
      return request(
        `entities/${uuid.parse(entity)}/file-edits?${query}`,
        z.array(editCandidateSchema).max(pageLimit),
        undefined,
        signal,
      );
    },
    history(history: string, signal?: AbortSignal) {
      return request(
        `file-history/${uuid.parse(history)}`,
        fileHistorySchema,
        undefined,
        signal,
      );
    },
    async preview(input: EditInput, signal?: AbortSignal) {
      const body = normalizeEditInput(input);
      const result = await request(
        "metadata-file-edits/preview",
        editPreviewSchema,
        JSON.stringify(body),
        signal,
      );
      if (editInputKey(body) !== editInputKey(result.input))
        throw new NativeArchiveError(0, "preview_mismatch");
      return result;
    },
    async receipt(input: EditApply, signal?: AbortSignal) {
      const valid = editApplySchema.parse(input);
      try {
        const result = await request(
          `metadata-file-edits/requests/${valid.request_uuid}`,
          editReceiptSchema,
          undefined,
          signal,
        );
        return validateEditReceipt(result, valid);
      } catch (error) {
        if (error instanceof NativeArchiveError && error.status === 404)
          return null;
        throw error;
      }
    },
    async applySaved(body: string, signal?: AbortSignal) {
      if (new TextEncoder().encode(body).length > 262144)
        throw new NativeArchiveError(0, "request_too_large");
      const input = editApplySchema.parse(JSON.parse(body));
      const result = await request(
        "metadata-file-edits/apply",
        z.object({ review: editReceiptSchema, replayed: z.boolean() }),
        body,
        signal,
      );
      return { ...result, review: validateEditReceipt(result.review, input) };
    },
  };
}
export type MetadataReviewAPI = ReturnType<typeof createMetadataReviewAPI>;
