import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { mediaRootSchema, definitionStateSchema } from "./collection-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const revision = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
const text = (limit: number) =>
  z
    .string()
    .refine(
      (value) =>
        new TextEncoder().encode(value).length <= limit &&
        !/\p{Cc}/u.test(value),
    );
export const rootBindingSchema = z
  .object({
    path: text(4096).refine((value) => value.length > 0),
    directory_identity: text(128).refine((value) => value.length > 0),
  })
  .strict();
export const mediaRootInputSchema = z
  .object({
    uuid,
    expected_revision: revision,
    label: text(1024).refine((value) => value.trim().length > 0),
    state: definitionStateSchema,
    binding: rootBindingSchema.nullable(),
    reason: text(4096),
  })
  .strict();
export const mediaRootFilterSchema = z.object({
  q: text(256),
  state: z.union([definitionStateSchema, z.literal("")]),
});
export const mediaRootSearchSchema = z.object({
  q: text(256).catch(""),
  state: z.union([definitionStateSchema, z.literal("")]).catch(""),
  root: uuid.optional().catch(undefined),
  create: z.boolean().optional().catch(undefined),
});
const historySchema = mediaRootSchema.extend({
  origin: z.string(),
  reason: z.string(),
  recorded_at: z.string(),
});
export type MediaRoot = z.infer<typeof mediaRootSchema>;
export type MediaRootInput = z.infer<typeof mediaRootInputSchema>;
export type MediaRootFilter = z.infer<typeof mediaRootFilterSchema>;
export type RootBinding = z.infer<typeof rootBindingSchema>;
export type MediaRootRevision = z.infer<typeof historySchema>;

export function sameRootDefinition(input: MediaRootInput, root: MediaRoot) {
  return (
    input.label === root.label &&
    input.state === root.state &&
    input.binding?.path === root.binding?.path &&
    input.binding?.directory_identity === root.binding?.directory_identity
  );
}

export function createMediaRootAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  const path = (id: string) => `media-roots/${uuid.parse(id)}`;
  async function root(id: string, signal?: AbortSignal) {
    const found = await request(path(id), mediaRootSchema, undefined, signal);
    if (found.uuid !== id) throw new NativeArchiveError(0, "root_mismatch");
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
      z.array(historySchema).max(limit),
      undefined,
      signal,
    );
    if (
      rows.some(
        (row, i) =>
          row.uuid !== id || row.revision <= (rows[i - 1]?.revision ?? after),
      )
    )
      throw new NativeArchiveError(0, "history_mismatch");
    return rows;
  }
  async function recover(input: MediaRootInput) {
    try {
      const rows = await history(input.uuid, input.expected_revision, 1);
      const found = rows[0];
      if (found) {
        if (
          found.revision === input.expected_revision + 1 &&
          found.origin === "review" &&
          found.reason === input.reason &&
          sameRootDefinition(input, found)
        )
          return found;
        throw new NativeArchiveError(409, "preview_changed");
      }
      const current = await root(input.uuid);
      if (current.revision !== input.expected_revision)
        throw new NativeArchiveError(409, "preview_changed");
      return sameRootDefinition(input, current) ? current : null;
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
    root,
    history,
    recover,
    async roots(filter: MediaRootFilter, after = "", signal?: AbortSignal) {
      const values = mediaRootFilterSchema.parse(filter);
      const params = new URLSearchParams({ limit: String(pageLimit) });
      if (values.q) params.set("q", values.q);
      if (values.state) params.set("state", values.state);
      if (after) params.set("after", uuid.parse(after));
      const rows = await request(
        `media-roots?${params}`,
        z.array(mediaRootSchema).max(pageLimit),
        undefined,
        signal,
      );
      if (rows.some((row, i) => row.uuid <= (rows[i - 1]?.uuid ?? after)))
        throw new NativeArchiveError(0, "root_mismatch");
      return rows;
    },
    async probe(path: string, signal?: AbortSignal) {
      const checked = text(4096).parse(path);
      return request(
        "media-roots/probe",
        rootBindingSchema,
        JSON.stringify({ server_path: checked }),
        signal,
      );
    },
    async save(body: string) {
      if (new TextEncoder().encode(body).length > 24576)
        throw new NativeArchiveError(0, "request_too_large");
      const input = mediaRootInputSchema.parse(JSON.parse(body));
      const found = await request(
        path(input.uuid),
        mediaRootSchema,
        body,
        undefined,
        "PUT",
      );
      if (
        found.uuid !== input.uuid ||
        ![input.expected_revision, input.expected_revision + 1].includes(
          found.revision,
        ) ||
        !sameRootDefinition(input, found)
      )
        throw new NativeArchiveError(0, "root_mismatch");
      return found;
    },
  };
}
export type MediaRootAPI = ReturnType<typeof createMediaRootAPI>;
