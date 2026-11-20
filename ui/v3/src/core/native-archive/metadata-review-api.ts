import { z } from "zod";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
export { NativeArchiveError } from "./client";

const uuid = z.string().regex(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/);
const revision = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const kind = z.enum([
  "scene",
  "image",
  "gallery",
  "performer",
  "studio",
  "tag",
  "group",
  "file",
  "folder",
  "scene_marker",
  "gallery_chapter",
  "saved_filter",
]);
export const identitySchema = z.object({
  uuid,
  kind,
  revision,
});
const decisionProvenanceSchema = z.object({
  uuid,
  sequence: revision,
  capture_uuid: uuid.optional(),
  post_media_decision_uuid: uuid.optional(),
  reason: z.string(),
  created_at: z.string(),
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
export type NativeIdentity = z.infer<typeof identitySchema>;
export type MetadataFields = z.infer<typeof metadataFieldsSchema>;
export type FieldDecision = z.infer<typeof decisionSchema>;

export const metadataReviewEndpoint = nativeArchiveEndpoint;

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
    async identity(
      entityKind: NativeIdentity["kind"],
      id: string,
      signal?: AbortSignal,
    ) {
      kind.parse(entityKind);
      const local = /^[1-9]\d*$/.test(id);
      if (!local && !uuid.safeParse(id).success)
        throw new NativeArchiveError(0, "invalid_identity");
      const identity = await request(
        `entity-identities/${entityKind}/${id}`,
        identitySchema,
        undefined,
        signal,
      );
      if (identity.kind !== entityKind)
        throw new NativeArchiveError(0, "identity_mismatch");
      return identity;
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
  };
}
export type MetadataReviewAPI = ReturnType<typeof createMetadataReviewAPI>;
