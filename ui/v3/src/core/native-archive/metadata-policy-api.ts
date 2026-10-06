import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { createCollectionAPI } from "./collection-api";
import { fieldDefinitionSchema, identitySchema } from "./metadata-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const revision = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
export const policyKindSchema = z.enum(["scene", "image"]);
const mappingSchema = z
  .union([
    z
      .object({
        jq: z.string().min(1).max(131072),
        fallback: z.json().optional(),
        performer_names: z.boolean().optional(),
        reference_names: z.boolean().optional(),
      })
      .strict(),
    z
      .object({
        value: z.json(),
        performer_names: z.boolean().optional(),
        reference_names: z.boolean().optional(),
      })
      .strict(),
  ])
  .refine((mapping) => !(mapping.performer_names && mapping.reference_names));
export const policyMappingSchema = mappingSchema.transform(
  (mapping): z.infer<typeof mappingSchema> => {
    const { performer_names, reference_names, ...value } = mapping;
    return {
      ...value,
      ...(performer_names ? { performer_names: true } : {}),
      ...(reference_names ? { reference_names: true } : {}),
    };
  },
);
const ruleSchema = z
  .object({
    on_create: z.boolean(),
    on_existing: z.boolean(),
    skip_organized_on_create: z.boolean(),
    mark_organized: z.boolean(),
    organized_requires: z.array(z.string().min(1).max(128)).max(32).optional(),
    filename_title_fallback: z.boolean(),
    mappings: z
      .record(z.string().min(1).max(128), policyMappingSchema)
      .nullable(),
  })
  .strict();
export const policyRuleSchema = ruleSchema.transform(
  (rule): z.infer<typeof ruleSchema> => {
    const { organized_requires, ...rest } = rule;
    return organized_requires?.length ? { ...rest, organized_requires } : rest;
  },
);
export const policyDefinitionSchema = z
  .object({
    enabled: z.boolean(),
    apply_to_scans: z.boolean(),
    rules: z.partialRecord(policyKindSchema, policyRuleSchema).nullable(),
  })
  .strict();
export const policyInputSchema = z
  .object({
    collection_uuid: uuid,
    expected_revision: revision,
    expected_collection_revision: revision.positive(),
    definition: policyDefinitionSchema,
    reason: z
      .string()
      .refine(
        (value) =>
          new TextEncoder().encode(value).length <= 4096 &&
          !/\p{Cc}/u.test(value),
      ),
  })
  .strict();
export const metadataPolicySchema = z.object({
  collection_uuid: uuid,
  revision: revision.positive(),
  collection_revision: revision.positive(),
  definition: policyDefinitionSchema,
  origin: z.string(),
  reason: z.string(),
  created_at: z.string(),
});
const sourceShape = z.object({
  capture_uuid: uuid,
  attachment_uuid: uuid.optional(),
  post_media_decision_uuid: uuid.optional(),
});
const oneSourceLink = (source: z.infer<typeof sourceShape>) =>
  Boolean(source.attachment_uuid) !== Boolean(source.post_media_decision_uuid);
const sourceSchema = sourceShape.strict().refine(oneSourceLink);

export function policySourceKey(source: z.infer<typeof sourceShape>) {
  return `${source.capture_uuid}:${source.attachment_uuid ?? source.post_media_decision_uuid ?? ""}`;
}
export const policyDraftInputSchema = z
  .object({
    collection_uuid: uuid,
    expected_collection_revision: revision.positive(),
    expected_policy_revision: revision,
    definition: policyDefinitionSchema,
    entity_uuid: uuid,
    file_uuid: uuid,
    source: sourceSchema.optional(),
    event: z.enum(["create", "existing"]),
    include_data: z.boolean(),
  })
  .strict();
const previewSchema = z
  .object({
    context: z.object({
      collection_uuid: uuid,
      collection_revision: revision.positive(),
      policy_revision: revision,
      entity_uuid: uuid,
      expected_entity_revision: revision.positive(),
      relative_path: z.string(),
      created: z.boolean(),
      source: sourceSchema.optional(),
    }),
    state: z.enum([
      "ready",
      "disabled",
      "not_enabled_for_event",
      "organized_at_creation",
      "collection_changed",
    ]),
    data: z
      .object({
        entity: z.record(z.string(), z.unknown()),
        source: z.unknown(),
        context: z.object({
          created: z.boolean(),
          filename: z.string(),
          relative_path: z.string(),
        }),
      })
      .strict()
      .optional(),
    changes: z
      .array(
        z.object({
          field: z.string(),
          status: z.enum([
            "ready",
            "unchanged",
            "review",
            "protected",
            "omitted",
            "older_capture",
          ]),
          current: z.unknown(),
          value: z.unknown().optional(),
          origin: z.string().optional(),
          capture_uuid: uuid.optional(),
          post_media_decision_uuid: uuid.optional(),
          used_fallback: z.boolean().optional(),
          reference_revisions: z.record(uuid, revision.positive()).optional(),
          message: z.string().optional(),
          names: z
            .array(
              z.object({
                name: z.string(),
                status: z.enum(["matched", "unmatched", "ambiguous"]),
                more: z.boolean(),
                candidates: z
                  .array(
                    z.object({
                      uuid,
                      name: z.string(),
                      disambiguation: z.string(),
                      revision: revision.positive(),
                    }),
                  )
                  .max(100),
              }),
            )
            .max(128)
            .optional(),
        }),
      )
      .max(32),
  })
  .strict();
const sampleFileSchema = z.object({
  file_uuid: uuid,
  relative_path: z.string(),
});
const sampleSourceSchema = sourceShape
  .extend({
    post_uuid: uuid,
    title: z.string(),
    platform: z.string(),
    origin: z.string(),
    captured_at: z.string().nullable(),
  })
  .refine(oneSourceLink);
export type PolicyKind = z.infer<typeof policyKindSchema>;
export type PolicyMapping = z.infer<typeof policyMappingSchema>;
export type PolicyRule = z.infer<typeof policyRuleSchema>;
export type PolicyDefinition = z.infer<typeof policyDefinitionSchema>;
export type PolicyInput = z.infer<typeof policyInputSchema>;
export type MetadataPolicy = z.infer<typeof metadataPolicySchema>;
export type PolicyDraftInput = z.infer<typeof policyDraftInputSchema>;
export type PolicyPreview = z.infer<typeof previewSchema>;
export type PolicySampleFile = z.infer<typeof sampleFileSchema>;
export type PolicySampleSource = z.infer<typeof sampleSourceSchema>;
export type PolicyField = z.infer<typeof fieldDefinitionSchema>;
const policyReferenceSchema = z.object({
  requested_uuid: uuid,
  entity: identitySchema.nullable(),
  name: z.string(),
  disambiguation: z.string(),
});
export type PolicyReference = z.infer<typeof policyReferenceSchema>;

function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object")
    return `{${Object.entries(value)
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      .map(([key, entry]) => `${JSON.stringify(key)}:${canonical(entry)}`)
      .join(",")}}`;
  const result = JSON.stringify(value);
  if (result === undefined) throw new NativeArchiveError(0, "invalid_policy");
  return result;
}
export function samePolicyDefinition(a: PolicyDefinition, b: PolicyDefinition) {
  return canonical(a) === canonical(b);
}

export function createMetadataPolicyAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const collections = createCollectionAPI(endpoint, transport);
  const pageLimit = 25;
  const path = (id: string) => `collections/${uuid.parse(id)}/metadata-policy`;
  function samplePath(
    collection: string,
    collectionRevision: number,
    entity: string,
    type: string,
    cursor: Record<string, string>,
  ) {
    const params = new URLSearchParams({
      collection_revision: String(
        revision.positive().parse(collectionRevision),
      ),
      limit: String(pageLimit),
      ...cursor,
    });
    return `${path(collection)}/samples/${uuid.parse(entity)}/${type}?${params}`;
  }
  async function policy(id: string, signal?: AbortSignal) {
    const found = await request(
      path(id),
      metadataPolicySchema.nullable(),
      undefined,
      signal,
    );
    if (found && found.collection_uuid !== id)
      throw new NativeArchiveError(0, "policy_mismatch");
    return found;
  }
  async function history(
    id: string,
    after = 0,
    limit = pageLimit,
    signal?: AbortSignal,
  ) {
    revision.parse(after);
    z.number().int().min(1).max(pageLimit).parse(limit);
    const rows = await request(
      `${path(id)}/history?after=${after}&limit=${limit}`,
      z.array(metadataPolicySchema).max(limit),
      undefined,
      signal,
    );
    if (
      rows.some(
        (row, i) =>
          row.collection_uuid !== id ||
          row.revision <= (rows[i - 1]?.revision ?? after),
      )
    )
      throw new NativeArchiveError(0, "history_mismatch");
    return rows;
  }
  async function recover(input: PolicyInput) {
    const rows = await history(
      input.collection_uuid,
      input.expected_revision,
      1,
    );
    const found = rows[0];
    if (found) {
      if (
        found.revision === input.expected_revision + 1 &&
        found.collection_revision === input.expected_collection_revision &&
        found.origin === "review" &&
        found.reason === input.reason &&
        samePolicyDefinition(input.definition, found.definition)
      )
        return found;
      throw new NativeArchiveError(409, "preview_changed");
    }
    const collection = await collections.collection(input.collection_uuid);
    if (
      collection.revision !== input.expected_collection_revision ||
      collection.state === "retired"
    )
      throw new NativeArchiveError(409, "preview_changed");
    const current = await policy(input.collection_uuid);
    if ((current?.revision ?? 0) !== input.expected_revision)
      throw new NativeArchiveError(409, "preview_changed");
    return current?.collection_revision ===
      input.expected_collection_revision &&
      samePolicyDefinition(input.definition, current.definition)
      ? current
      : null;
  }
  return {
    endpoint,
    pageLimit,
    policy,
    history,
    recover,
    async references(ids: string[], signal?: AbortSignal) {
      const valid = z.array(uuid).max(100).parse(ids);
      const rows = await request(
        "metadata-policy/references",
        z.array(policyReferenceSchema).max(100),
        JSON.stringify({ uuids: valid }),
        signal,
      );
      if (
        rows.length !== valid.length ||
        rows.some((row, i) => row.requested_uuid !== valid[i])
      )
        throw new NativeArchiveError(0, "reference_mismatch");
      return rows;
    },
    fields(kind: PolicyKind, signal?: AbortSignal) {
      return request(
        `metadata-fields/${policyKindSchema.parse(kind)}`,
        z.array(fieldDefinitionSchema).max(32),
        undefined,
        signal,
      );
    },
    async save(body: string) {
      if (new TextEncoder().encode(body).length > 135168)
        throw new NativeArchiveError(0, "request_too_large");
      const input = policyInputSchema.parse(JSON.parse(body));
      const found = await request(
        path(input.collection_uuid),
        metadataPolicySchema,
        body,
        undefined,
        "PUT",
      );
      if (
        found.collection_uuid !== input.collection_uuid ||
        found.collection_revision !== input.expected_collection_revision ||
        ![input.expected_revision, input.expected_revision + 1].includes(
          found.revision,
        ) ||
        !samePolicyDefinition(input.definition, found.definition)
      )
        throw new NativeArchiveError(0, "policy_mismatch");
      return found;
    },
    async draft(input: PolicyDraftInput, signal?: AbortSignal) {
      const valid = policyDraftInputSchema.parse(input);
      const body = JSON.stringify(valid);
      if (new TextEncoder().encode(body).length > 147456)
        throw new NativeArchiveError(0, "request_too_large");
      const result = await request(
        "metadata-policy/draft-preview",
        previewSchema,
        body,
        signal,
      );
      const context = result.context;
      if (
        context.collection_uuid !== input.collection_uuid ||
        context.collection_revision !== input.expected_collection_revision ||
        context.policy_revision !== input.expected_policy_revision ||
        context.entity_uuid !== input.entity_uuid ||
        context.created !== (input.event === "create") ||
        context.source?.capture_uuid !== input.source?.capture_uuid ||
        context.source?.attachment_uuid !== input.source?.attachment_uuid ||
        context.source?.post_media_decision_uuid !==
          input.source?.post_media_decision_uuid
      )
        throw new NativeArchiveError(0, "preview_mismatch");
      return result;
    },
    async files(
      collection: string,
      collectionRevision: number,
      entity: string,
      after = "",
      signal?: AbortSignal,
    ) {
      if (after) uuid.parse(after);
      const rows = await request(
        samplePath(
          collection,
          collectionRevision,
          entity,
          "files",
          after ? { after } : {},
        ),
        z.array(sampleFileSchema).max(pageLimit),
        undefined,
        signal,
      );
      if (
        rows.some(
          (row, i) => row.file_uuid <= (rows[i - 1]?.file_uuid ?? after),
        )
      )
        throw new NativeArchiveError(0, "sample_mismatch");
      return rows;
    },
    async sources(
      collection: string,
      collectionRevision: number,
      entity: string,
      after?: PolicySampleSource,
      signal?: AbortSignal,
    ) {
      const cursor: Record<string, string> = after
        ? {
            after_capture: uuid.parse(after.capture_uuid),
            ...(after.attachment_uuid
              ? { after_attachment: uuid.parse(after.attachment_uuid) }
              : {
                  after_post_media_decision: uuid.parse(
                    after.post_media_decision_uuid,
                  ),
                }),
          }
        : {};
      const rows = await request(
        samplePath(collection, collectionRevision, entity, "sources", cursor),
        z.array(sampleSourceSchema).max(pageLimit),
        undefined,
        signal,
      );
      const key = (row?: PolicySampleSource) =>
        row ? policySourceKey(row) : "";
      if (rows.some((row, i) => key(row) <= key(rows[i - 1] ?? after)))
        throw new NativeArchiveError(0, "sample_mismatch");
      return rows;
    },
  };
}
export type MetadataPolicyAPI = ReturnType<typeof createMetadataPolicyAPI>;
