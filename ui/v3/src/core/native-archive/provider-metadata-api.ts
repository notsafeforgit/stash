import { z } from "zod";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import { createMetadataReviewAPI } from "./metadata-review-api";

const uuid = z.string().regex(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/);
const integer = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const providerKind = z.enum(["scene", "performer", "studio", "tag"]);
export type ProviderEntityKind = z.infer<typeof providerKind>;
export const providerMetadataImportSchema = z.object({
  sequence: integer,
  uuid,
  entity_uuid: uuid,
  original_entity_uuid: uuid,
  entity_kind: providerKind,
  entity_revision: integer,
  endpoint: z.url().refine((value) => {
    const url = new URL(value);
    return (
      /^https?:$/.test(url.protocol) &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash
    );
  }),
  remote_id: z.string().min(1).max(1024),
  operation: z.enum(["review", "identify", "batch"]),
  values: z.record(z.string(), z.unknown()).refine((values) => {
    const length = Object.keys(values).length;
    return length > 0 && length <= 24;
  }),
  signature: z.string().regex(/^[0-9a-f]{64}$/),
  created_at: z.string().datetime({ offset: true }),
});
export type ProviderMetadataImport = z.infer<
  typeof providerMetadataImportSchema
>;

export function createProviderMetadataAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = globalThis.fetch.bind(globalThis),
) {
  const request = createArchiveRequest(endpoint, transport);
  const review = createMetadataReviewAPI(endpoint, transport);
  const pageLimit = 25;
  return {
    pageLimit,
    async identity(
      kind: ProviderEntityKind,
      localId: string,
      signal?: AbortSignal,
    ) {
      providerKind.parse(kind);
      const identity = await review.identity(kind, localId, signal);
      if (identity.kind !== kind || String(identity.local_id) !== localId)
        throw new NativeArchiveError(0, "identity_mismatch");
      return identity;
    },
    async history(
      id: string,
      kind: ProviderEntityKind,
      after = 0,
      signal?: AbortSignal,
    ) {
      uuid.parse(id);
      providerKind.parse(kind);
      z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).parse(after);
      const params = new URLSearchParams({
        after: String(after),
        limit: String(pageLimit),
      });
      const rows = await request(
        `entities/${id}/provider-metadata-history?${params}`,
        z.array(providerMetadataImportSchema).max(pageLimit),
        undefined,
        signal,
      );
      let previous = after;
      const seen = new Set<string>();
      for (const row of rows) {
        if (
          row.entity_uuid !== id ||
          row.entity_kind !== kind ||
          row.sequence <= previous ||
          seen.has(row.uuid)
        )
          throw new NativeArchiveError(0, "invalid_response");
        previous = row.sequence;
        seen.add(row.uuid);
      }
      return rows;
    },
  };
}
