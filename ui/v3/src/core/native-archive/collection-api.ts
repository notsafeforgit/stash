import { z } from "zod";
import {
  accountSearchSchema,
  accountUUIDSchema as uuid,
} from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

function text(bytes: number) {
  return z
    .string()
    .refine(
      (value) =>
        new TextEncoder().encode(value).length <= bytes &&
        !/\p{Cc}/u.test(value),
    );
}
export const collectionKindSchema = z.enum([
  "account",
  "feed",
  "subreddit",
  "search",
  "manual_batch",
  "directory",
  "legacy_catalog",
  "collection",
]);
export const definitionStateSchema = z.enum(["active", "disabled", "retired"]);
const revision = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
export const collectionQuerySchema = z.object({
  q: text(256),
  state: z.union([definitionStateSchema, z.literal("")]),
  kind: z.union([collectionKindSchema, z.literal("")]),
});
export const collectionFilterSchema = collectionQuerySchema.extend({
  q: collectionQuerySchema.shape.q.default(""),
  state: collectionQuerySchema.shape.state.default(""),
  kind: collectionQuerySchema.shape.kind.default(""),
});
export const collectionSearchSchema = collectionFilterSchema.extend({
  collection: uuid.optional(),
  create: z.boolean().optional(),
});
const definitionSchema = z.object({
  label: text(1024).refine((value) => value.trim().length > 0),
  kind: collectionKindSchema,
  namespace: accountSearchSchema.shape.namespace,
  state: definitionStateSchema,
  target_url: text(8192).refine((value) => {
    if (!value) return true;
    try {
      const url = new URL(value);
      return (
        value.trim() === value &&
        /^https?:$/.test(url.protocol) &&
        !url.username &&
        !url.password
      );
    } catch {
      return false;
    }
  }),
  account_uuid: uuid.nullable(),
  root_uuid: uuid.nullable(),
  path_prefix: text(4096),
});
export const collectionSchema = definitionSchema.extend({
  uuid,
  revision: revision.positive(),
  created_at: z.string(),
});
export const collectionInputSchema = definitionSchema
  .extend({
    uuid,
    expected_revision: revision,
    reason: text(4096),
  })
  .strict()
  .superRefine((input, ctx) => {
    if (input.account_uuid && !input.namespace)
      ctx.addIssue({
        code: "custom",
        path: ["namespace"],
        message: "Select a service for this account.",
      });
    const path = input.path_prefix;
    if (
      input.root_uuid
        ? !(
            path === "." ||
            (path.length > 0 &&
              !path.includes("\\") &&
              !path
                .split("/")
                .some((part) => !part || part === "." || part === "..") &&
              !/^[a-z]:/i.test(path))
          )
        : path !== ""
    )
      ctx.addIssue({
        code: "custom",
        path: ["path_prefix"],
        message: "Use a relative folder beneath the media root.",
      });
  });
const historySchema = collectionSchema.extend({
  origin: z.string(),
  reason: z.string(),
  recorded_at: z.string(),
});
export const mediaRootSchema = z.object({
  uuid,
  label: z.string(),
  state: definitionStateSchema,
  revision: revision.positive(),
  created_at: z.string(),
  binding: z
    .object({ path: z.string(), directory_identity: z.string() })
    .nullable(),
});
export type Collection = z.infer<typeof collectionSchema>;
export type CollectionInput = z.infer<typeof collectionInputSchema>;
export type CollectionFilter = z.infer<typeof collectionFilterSchema>;
export type MediaRoot = z.infer<typeof mediaRootSchema>;
export type CollectionRevision = z.infer<typeof historySchema>;

export function sameCollectionDefinition(
  input: CollectionInput,
  found: Collection,
) {
  return Object.entries(definitionSchema.parse(input)).every(
    ([key, value]) => Reflect.get(found, key) === value,
  );
}

export function createCollectionAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  function query(filter: CollectionFilter, after = "") {
    const checked = collectionFilterSchema.parse(filter);
    const params = new URLSearchParams({ limit: String(pageLimit) });
    for (const [key, value] of Object.entries(checked))
      if (value) params.set(key, value);
    if (after) params.set("after", uuid.parse(after));
    return params;
  }
  async function collection(id: string, signal?: AbortSignal) {
    const result = await request(
      `collections/${uuid.parse(id)}`,
      collectionSchema,
      undefined,
      signal,
    );
    if (result.uuid !== id)
      throw new NativeArchiveError(0, "collection_mismatch");
    return result;
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
      `collections/${uuid.parse(id)}/history?after=${after}&limit=${limit}`,
      z.array(historySchema).max(limit),
      undefined,
      signal,
    );
    if (
      rows.some(
        (row, index) =>
          row.uuid !== id ||
          row.revision <= (rows[index - 1]?.revision ?? after),
      )
    )
      throw new NativeArchiveError(0, "history_mismatch");
    return rows;
  }
  // Look up the reserved revision, not just the latest definition: another tab
  // may have edited the collection after our response was lost.
  async function recover(input: CollectionInput) {
    try {
      const rows = await history(input.uuid, input.expected_revision, 1);
      const found = rows[0];
      if (found) {
        if (
          found.revision === input.expected_revision + 1 &&
          found.origin === "review" &&
          found.reason === input.reason &&
          sameCollectionDefinition(input, found)
        )
          return found;
        throw new NativeArchiveError(409, "preview_changed");
      }
      const current = await collection(input.uuid);
      if (current.revision !== input.expected_revision)
        throw new NativeArchiveError(409, "preview_changed");
      return sameCollectionDefinition(input, current) ? current : null;
    } catch (error) {
      if (
        error instanceof NativeArchiveError &&
        error.status === 404 &&
        input.expected_revision === 0
      )
        return null;
      throw error;
    }
  }
  return {
    endpoint,
    pageLimit,
    collection,
    history,
    recover,
    collections: (filter: CollectionFilter, after = "", signal?: AbortSignal) =>
      request(
        `collections?${query(filter, after)}`,
        z.array(collectionSchema).max(pageLimit),
        undefined,
        signal,
      ),
    roots: (q: string, signal?: AbortSignal) =>
      request(
        `media-roots?${query({ q, state: "active", kind: "" })}`,
        z.array(mediaRootSchema).max(pageLimit),
        undefined,
        signal,
      ),
    async root(id: string, signal?: AbortSignal) {
      const root = await request(
        `media-roots/${uuid.parse(id)}`,
        mediaRootSchema,
        undefined,
        signal,
      );
      if (root.uuid !== id) throw new NativeArchiveError(0, "root_mismatch");
      return root;
    },
    async save(body: string) {
      if (new TextEncoder().encode(body).length > 24576)
        throw new NativeArchiveError(0, "request_too_large");
      const input = collectionInputSchema.parse(JSON.parse(body));
      const found = await request(
        `collections/${input.uuid}`,
        collectionSchema,
        body,
        undefined,
        "PUT",
      );
      if (
        found.uuid !== input.uuid ||
        ![input.expected_revision, input.expected_revision + 1].includes(
          found.revision,
        ) ||
        !sameCollectionDefinition(input, found)
      )
        throw new NativeArchiveError(0, "collection_mismatch");
      return found;
    },
  };
}
export type CollectionAPI = ReturnType<typeof createCollectionAPI>;
